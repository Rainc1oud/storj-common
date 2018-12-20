// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package provider

import (
	"bytes"
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net"
	"os"

	"github.com/zeebo/errs"
	"go.uber.org/zap"
	"golang.org/x/crypto/sha3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	"storj.io/storj/pkg/peertls"
	"storj.io/storj/pkg/storj"
	"storj.io/storj/pkg/utils"
)

// PeerIdentity represents another peer on the network.
type PeerIdentity struct {
	RestChain []*x509.Certificate
	// CA represents the peer's self-signed CA
	CA *x509.Certificate
	// Leaf represents the leaf they're currently using. The leaf should be
	// signed by the CA. The leaf is what is used for communication.
	Leaf *x509.Certificate
	// The ID taken from the CA public key
	ID storj.NodeID
}

// FullIdentity represents you on the network. In addition to a PeerIdentity,
// a FullIdentity also has a Key, which a PeerIdentity doesn't have.
type FullIdentity struct {
	RestChain []*x509.Certificate
	// CA represents the peer's self-signed CA. The ID is taken from this cert.
	CA *x509.Certificate
	// Leaf represents the leaf they're currently using. The leaf should be
	// signed by the CA. The leaf is what is used for communication.
	Leaf *x509.Certificate
	// The ID taken from the CA public key
	ID storj.NodeID
	// Key is the key this identity uses with the leaf for communication.
	Key crypto.PrivateKey
}

// IdentitySetupConfig allows you to run a set of Responsibilities with the given
// identity. You can also just load an Identity from disk.
type IdentitySetupConfig struct {
	CertPath  string `help:"path to the certificate chain for this identity" default:"$CONFDIR/identity.cert"`
	KeyPath   string `help:"path to the private key for this identity" default:"$CONFDIR/identity.key"`
	Overwrite bool   `help:"if true, existing identity certs AND keys will overwritten for" default:"false"`
	Version   string `help:"semantic version of identity storage format" default:"0"`
	Server    ServerConfig
}

// IdentityConfig allows you to run a set of Responsibilities with the given
// identity. You can also just load an Identity from disk.
type IdentityConfig struct {
	CertPath string `help:"path to the certificate chain for this identity" default:"$CONFDIR/identity.cert"`
	KeyPath  string `help:"path to the private key for this identity" default:"$CONFDIR/identity.key"`
	Server   ServerConfig
}

// ServerConfig holds server specific configuration parameters
type ServerConfig struct {
	RevocationDBURL     string `help:"url for revocation database (e.g. bolt://some.db OR redis://127.0.0.1:6378?db=2&password=abc123)" default:"bolt://$CONFDIR/revocations.db"`
	PeerCAWhitelistPath string `help:"path to the CA cert whitelist (peer identities must be signed by one these to be verified)"`
	Address             string `help:"address to listen on" default:":7777"`
	Extensions          peertls.TLSExtConfig
}

// ServerOptions holds config, identity, and peer verification function data for use with a grpc server.
type ServerOptions struct {
	Config   ServerConfig
	Ident    *FullIdentity
	RevDB    *peertls.RevocationDB
	PCVFuncs []peertls.PeerCertVerificationFunc
}

// NewServerOptions is a constructor for `serverOptions` given an identity and config
func NewServerOptions(i *FullIdentity, c ServerConfig) (*ServerOptions, error) {
	serverOpts := &ServerOptions{
		Config: c,
		Ident:  i,
	}

	err := c.configure(serverOpts)
	if err != nil {
		return nil, err
	}

	return serverOpts, nil
}

// FullIdentityFromPEM loads a FullIdentity from a certificate chain and
// private key PEM-encoded bytes
func FullIdentityFromPEM(chainPEM, keyPEM []byte) (*FullIdentity, error) {
	chain, err := decodeAndParseChainPEM(chainPEM)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	keysBytes, err := decodePEM(keyPEM)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	// NB: there shouldn't be multiple keys in the key file but if there
	// are, this uses the first one
	key, err := x509.ParseECPrivateKey(keysBytes[0])
	if err != nil {
		return nil, errs.New("unable to parse EC private key: %v", err)
	}
	nodeID, err := NodeIDFromKey(chain[peertls.CAIndex].PublicKey)
	if err != nil {
		return nil, err
	}

	return &FullIdentity{
		RestChain: chain[peertls.CAIndex+1:],
		CA:        chain[peertls.CAIndex],
		Leaf:      chain[peertls.LeafIndex],
		Key:       key,
		ID:        nodeID,
	}, nil
}

// ParseCertChain converts a chain of certificate bytes into x509 certs
func ParseCertChain(chain [][]byte) ([]*x509.Certificate, error) {
	c := make([]*x509.Certificate, len(chain))
	for i, ct := range chain {
		cp, err := x509.ParseCertificate(ct)
		if err != nil {
			return nil, errs.Wrap(err)
		}
		c[i] = cp
	}
	return c, nil
}

// PeerIdentityFromCerts loads a PeerIdentity from a pair of leaf and ca x509 certificates
func PeerIdentityFromCerts(leaf, ca *x509.Certificate, rest []*x509.Certificate) (*PeerIdentity, error) {
	i, err := NodeIDFromKey(ca.PublicKey.(crypto.PublicKey))
	if err != nil {
		return nil, err
	}

	return &PeerIdentity{
		RestChain: rest,
		CA:        ca,
		ID:        i,
		Leaf:      leaf,
	}, nil
}

// PeerIdentityFromPeer loads a PeerIdentity from a peer connection
func PeerIdentityFromPeer(peer *peer.Peer) (*PeerIdentity, error) {
	tlsInfo := peer.AuthInfo.(credentials.TLSInfo)
	c := tlsInfo.State.PeerCertificates
	if len(c) < 2 {
		return nil, Error.New("invalid certificate chain")
	}
	pi, err := PeerIdentityFromCerts(c[0], c[1], c[2:])
	if err != nil {
		return nil, err
	}

	return pi, nil
}

// PeerIdentityFromContext loads a PeerIdentity from a ctx TLS credentials
func PeerIdentityFromContext(ctx context.Context) (*PeerIdentity, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, Error.New("unable to get grpc peer from contex")
	}

	return PeerIdentityFromPeer(p)
}

// NodeIDFromKey hashes a publc key and creates a node ID from it
func NodeIDFromKey(k crypto.PublicKey) (storj.NodeID, error) {
	kb, err := x509.MarshalPKIXPublicKey(k)
	if err != nil {
		return storj.NodeID{}, storj.ErrNodeID.Wrap(err)
	}
	hash := make([]byte, len(storj.NodeID{}))
	sha3.ShakeSum256(hash, kb)
	return storj.NodeIDFromBytes(hash)
}

// NewFullIdentity creates a new ID for nodes with difficulty and concurrency params
func NewFullIdentity(ctx context.Context, difficulty uint16, concurrency uint) (*FullIdentity, error) {
	ca, err := NewCA(ctx, NewCAOptions{
		Difficulty:  difficulty,
		Concurrency: concurrency,
	})
	if err != nil {
		return nil, err
	}
	identity, err := ca.NewIdentity()
	if err != nil {
		return nil, err
	}
	return identity, err
}

// Status returns the status of the identity cert/key files for the config
func (is IdentitySetupConfig) Status() TLSFilesStatus {
	return statTLSFiles(is.CertPath, is.KeyPath)
}

// Create generates and saves a CA using the config
func (is IdentitySetupConfig) Create(ca *FullCertificateAuthority) (*FullIdentity, error) {
	fi, err := ca.NewIdentity()
	if err != nil {
		return nil, err
	}
	fi.CA = ca.Cert
	ic := IdentityConfig{
		CertPath: is.CertPath,
		KeyPath:  is.KeyPath,
	}
	return fi, ic.Save(fi)
}

// Load loads a FullIdentity from the config
func (ic IdentityConfig) Load() (*FullIdentity, error) {
	c, err := ioutil.ReadFile(ic.CertPath)
	if err != nil {
		return nil, peertls.ErrNotExist.Wrap(err)
	}
	k, err := ioutil.ReadFile(ic.KeyPath)
	if err != nil {
		return nil, peertls.ErrNotExist.Wrap(err)
	}
	fi, err := FullIdentityFromPEM(c, k)
	if err != nil {
		return nil, errs.New("failed to load identity %#v, %#v: %v",
			ic.CertPath, ic.KeyPath, err)
	}
	return fi, nil
}

// Save saves a FullIdentity according to the config
func (ic IdentityConfig) Save(fi *FullIdentity) error {
	var (
		certData, keyData                                              bytes.Buffer
		writeChainErr, writeChainDataErr, writeKeyErr, writeKeyDataErr error
	)

	chain := []*x509.Certificate{fi.Leaf, fi.CA}
	chain = append(chain, fi.RestChain...)

	if ic.CertPath != "" {
		writeChainErr = peertls.WriteChain(&certData, chain...)
		writeChainDataErr = writeChainData(ic.CertPath, certData.Bytes())
	}

	if ic.KeyPath != "" {
		writeKeyErr = peertls.WriteKey(&keyData, fi.Key)
		writeKeyDataErr = writeKeyData(ic.KeyPath, keyData.Bytes())
	}

	writeErr := utils.CombineErrors(writeChainErr, writeKeyErr)
	if writeErr != nil {
		return writeErr
	}

	return utils.CombineErrors(
		writeChainDataErr,
		writeKeyDataErr,
	)
}

// Run will run the given responsibilities with the configured identity.
func (ic IdentityConfig) Run(ctx context.Context, interceptor grpc.UnaryServerInterceptor, responsibilities ...Responsibility) (err error) {
	defer mon.Task()(&ctx)(&err)

	ident, err := ic.Load()
	if err != nil {
		return err
	}

	lis, err := net.Listen("tcp", ic.Server.Address)
	if err != nil {
		return err
	}
	defer func() { _ = lis.Close() }()

	opts, err := NewServerOptions(ident, ic.Server)
	if err != nil {
		return err
	}
	defer func() { err = utils.CombineErrors(err, opts.RevDB.Close()) }()

	s, err := NewProvider(opts, lis, interceptor, responsibilities...)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	zap.S().Infof("Node %s started", s.Identity().ID)
	return s.Run(ctx)
}

// RestChainRaw returns the rest (excluding leaf and CA) of the certficate chain as a 2d byte slice
func (fi *FullIdentity) RestChainRaw() [][]byte {
	var chain [][]byte
	for _, cert := range fi.RestChain {
		chain = append(chain, cert.Raw)
	}
	return chain
}

// ServerOption returns a grpc `ServerOption` for incoming connections
// to the node with this full identity
func (fi *FullIdentity) ServerOption(pcvFuncs ...peertls.PeerCertVerificationFunc) (grpc.ServerOption, error) {
	ch := [][]byte{fi.Leaf.Raw, fi.CA.Raw}
	ch = append(ch, fi.RestChainRaw()...)
	c, err := peertls.TLSCert(ch, fi.Leaf, fi.Key)
	if err != nil {
		return nil, err
	}

	pcvFuncs = append(
		[]peertls.PeerCertVerificationFunc{peertls.VerifyPeerCertChains},
		pcvFuncs...,
	)
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{*c},
		InsecureSkipVerify: true,
		ClientAuth:         tls.RequireAnyClientCert,
		VerifyPeerCertificate: peertls.VerifyPeerFunc(
			pcvFuncs...,
		),
	}

	return grpc.Creds(credentials.NewTLS(tlsConfig)), nil
}

// DialOption returns a grpc `DialOption` for making outgoing connections
// to the node with this peer identity
// id is an optional id of the node we are dialing
func (fi *FullIdentity) DialOption(id storj.NodeID) (grpc.DialOption, error) {
	ch := [][]byte{fi.Leaf.Raw, fi.CA.Raw}
	ch = append(ch, fi.RestChainRaw()...)
	c, err := peertls.TLSCert(ch, fi.Leaf, fi.Key)
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{*c},
		InsecureSkipVerify: true,
		VerifyPeerCertificate: peertls.VerifyPeerFunc(
			peertls.VerifyPeerCertChains,
			verifyIdentity(id),
		),
	}

	return grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)), nil
}

// NewRevDB returns a new revocation database given the config
func (c ServerConfig) NewRevDB() (*peertls.RevocationDB, error) {
	driver, source, err := utils.SplitDBURL(c.RevocationDBURL)
	if err != nil {
		return nil, peertls.ErrRevocationDB.Wrap(err)
	}

	var db *peertls.RevocationDB
	switch driver {
	case "bolt":
		db, err = peertls.NewRevocationDBBolt(source)
		if err != nil {
			return nil, peertls.ErrRevocationDB.Wrap(err)
		}
		zap.S().Info("Starting overlay cache with BoltDB")
	case "redis":
		db, err = peertls.NewRevocationDBRedis(c.RevocationDBURL)
		if err != nil {
			return nil, peertls.ErrRevocationDB.Wrap(err)
		}
		zap.S().Info("Starting overlay cache with Redis")
	default:
		return nil, peertls.ErrRevocationDB.New("database scheme not supported: %s", driver)
	}

	return db, nil
}

// configure adds peer certificate verification functions and revocation datase to the config.
func (c ServerConfig) configure(opts *ServerOptions) (err error) {
	var pcvs []peertls.PeerCertVerificationFunc
	parseOpts := peertls.ParseExtOptions{}

	if c.PeerCAWhitelistPath != "" {
		caWhitelist, err := loadWhitelist(c.PeerCAWhitelistPath)
		if err != nil {
			return err
		}
		parseOpts.CAWhitelist = caWhitelist
		pcvs = append(pcvs, peertls.VerifyCAWhitelist(caWhitelist))
	}

	if c.Extensions.Revocation {
		opts.RevDB, err = c.NewRevDB()
		if err != nil {
			return err
		}
		pcvs = append(pcvs, peertls.VerifyUnrevokedChainFunc(opts.RevDB))
	}

	exts := peertls.ParseExtensions(c.Extensions, parseOpts)
	pcvs = append(pcvs, exts.VerifyFunc())

	// NB: remove nil elements
	for i, f := range pcvs {
		if f == nil {
			copy(pcvs[i:], pcvs[i+1:])
			pcvs = pcvs[:len(pcvs)-1]
		}
	}

	opts.PCVFuncs = pcvs
	return nil
}

func (so *ServerOptions) grpcOpts() (grpc.ServerOption, error) {
	return so.Ident.ServerOption(so.PCVFuncs...)
}

func verifyIdentity(id storj.NodeID) peertls.PeerCertVerificationFunc {
	return func(_ [][]byte, parsedChains [][]*x509.Certificate) error {
		if id == (storj.NodeID{}) {
			return nil
		}

		peer, err := PeerIdentityFromCerts(parsedChains[0][0], parsedChains[0][1], parsedChains[0][2:])
		if err != nil {
			return err
		}

		if peer.ID.String() != id.String() {
			return Error.New("peer ID did not match requested ID")
		}

		return nil
	}
}

func loadWhitelist(path string) ([]*x509.Certificate, error) {
	w, err := ioutil.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	var whitelist []*x509.Certificate
	if w != nil {
		whitelist, err = decodeAndParseChainPEM(w)
		if err != nil {
			return nil, errs.Wrap(err)
		}
	}
	return whitelist, nil
}
