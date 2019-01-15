// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"text/tabwriter"

	"github.com/gogo/protobuf/proto"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"storj.io/storj/internal/fpath"
	"storj.io/storj/pkg/certificates"
	"storj.io/storj/pkg/cfgstruct"
	"storj.io/storj/pkg/identity"
	"storj.io/storj/pkg/kademlia"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/piecestore/psserver"
	"storj.io/storj/pkg/piecestore/psserver/psdb"
	"storj.io/storj/pkg/process"
	"storj.io/storj/pkg/server"
	"storj.io/storj/pkg/storj"
)

// StorageNode defines storage node configuration
type StorageNode struct {
	CA              identity.CASetupConfig `setup:"true"`
	Identity        identity.SetupConfig   `setup:"true"`
	EditConf        bool                   `default:"false" help:"open config in default editor"`
	SaveAllDefaults bool                   `default:"false" help:"save all default values to config.yaml file" setup:"true"`

	Server   server.Config
	Kademlia kademlia.StorageNodeConfig
	Storage  psserver.Config
	Signer   certificates.CertClientConfig
}

var (
	rootCmd = &cobra.Command{
		Use:   "storagenode",
		Short: "StorageNode",
	}
	runCmd = &cobra.Command{
		Use:   "run",
		Short: "Run the storagenode",
		RunE:  cmdRun,
	}
	setupCmd = &cobra.Command{
		Use:         "setup",
		Short:       "Create config files",
		RunE:        cmdSetup,
		Annotations: map[string]string{"type": "setup"},
	}
	configCmd = &cobra.Command{
		Use:         "config",
		Short:       "Edit config files",
		RunE:        cmdConfig,
		Annotations: map[string]string{"type": "setup"},
	}
	diagCmd = &cobra.Command{
		Use:   "diag",
		Short: "Diagnostic Tool support",
		RunE:  cmdDiag,
	}

	runCfg   StorageNode
	setupCfg StorageNode

	diagCfg struct {
	}

	defaultConfDir string
	defaultDiagDir string
	confDir        string
)

const (
	// default server address, only for storage node
	defaultServerAddr = ":28967"
)

func init() {
	defaultConfDir = fpath.ApplicationDir("storj", "storagenode")

	dirParam := cfgstruct.FindConfigDirParam()
	if dirParam != "" {
		defaultConfDir = dirParam
	}

	rootCmd.PersistentFlags().StringVar(&confDir, "config-dir", defaultConfDir, "main directory for storagenode configuration")

	defaultDiagDir = filepath.Join(defaultConfDir, "storage")
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(setupCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(diagCmd)
	cfgstruct.Bind(runCmd.Flags(), &runCfg, cfgstruct.ConfDir(defaultConfDir))
	cfgstruct.BindSetup(setupCmd.Flags(), &setupCfg, cfgstruct.ConfDir(defaultConfDir))
	cfgstruct.BindSetup(configCmd.Flags(), &setupCfg, cfgstruct.ConfDir(defaultConfDir))
	cfgstruct.Bind(diagCmd.Flags(), &diagCfg, cfgstruct.ConfDir(defaultDiagDir))
}

func cmdRun(cmd *cobra.Command, args []string) (err error) {
	operatorConfig := runCfg.Kademlia.Operator
	if err := isOperatorEmailValid(operatorConfig.Email); err != nil {
		zap.S().Warn(err)
	} else {
		zap.S().Info("Operator email: ", operatorConfig.Email)
	}
	if err := isOperatorWalletValid(operatorConfig.Wallet); err != nil {
		zap.S().Fatal(err)
	} else {
		zap.S().Info("Operator wallet: ", operatorConfig.Wallet)
	}
	ctx := process.Ctx(cmd)
	if err := process.InitMetricsWithCertPath(ctx, nil, runCfg.Identity.CertPath); err != nil {
		zap.S().Error("Failed to initialize telemetry batcher:", err)
	}

	return runCfg.Server.Run(ctx, nil, runCfg.Kademlia, runCfg.Storage)
}

func cmdSetup(cmd *cobra.Command, args []string) (err error) {
	setupDir, err := filepath.Abs(confDir)
	if err != nil {
		return err
	}

	valid, _ := fpath.IsValidSetupDir(setupDir)
	if !valid {
		return fmt.Errorf("storagenode configuration already exists (%v)", setupDir)
	}

	err = os.MkdirAll(setupDir, 0700)
	if err != nil {
		return err
	}

	setupCfg.CA.CertPath = filepath.Join(setupDir, "ca.cert")
	setupCfg.CA.KeyPath = filepath.Join(setupDir, "ca.key")
	setupCfg.Identity.CertPath = filepath.Join(setupDir, "identity.cert")
	setupCfg.Identity.KeyPath = filepath.Join(setupDir, "identity.key")

	if setupCfg.Signer.AuthToken != "" && setupCfg.Signer.Address != "" {
		err = setupCfg.Signer.SetupIdentity(process.Ctx(cmd), setupCfg.CA, setupCfg.Identity)
		if err != nil {
			zap.S().Warn(err)
		}
	} else {
		err = identity.SetupIdentity(process.Ctx(cmd), setupCfg.CA, setupCfg.Identity)
		if err != nil {
			return err
		}
	}

	overrides := map[string]interface{}{
		"log.level": "info",
	}
	serverAddress := cmd.Flag("server.address")
	if !serverAddress.Changed {
		overrides[serverAddress.Name] = defaultServerAddr
	}

	configFile := filepath.Join(setupDir, "config.yaml")
	if setupCfg.SaveAllDefaults {
		err = process.SaveConfigWithAllDefaults(cmd.Flags(), configFile, overrides)
	} else {
		err = process.SaveConfig(cmd.Flags(), configFile, overrides)
	}
	if err != nil {
		return err
	}

	if setupCfg.EditConf {
		return fpath.EditFile(configFile)
	}

	return err
}

func cmdConfig(cmd *cobra.Command, args []string) (err error) {
	setupDir, err := filepath.Abs(confDir)
	if err != nil {
		return err
	}
	//run setup if we can't access the config file
	conf := filepath.Join(setupDir, "config.yaml")
	if _, err := os.Stat(conf); err != nil {
		return cmdSetup(cmd, args)
	}

	return fpath.EditFile(conf)
}

func cmdDiag(cmd *cobra.Command, args []string) (err error) {
	diagDir, err := filepath.Abs(confDir)
	if err != nil {
		return err
	}

	// check if the directory exists
	_, err = os.Stat(diagDir)
	if err != nil {
		fmt.Println("Storagenode directory doesn't exist", diagDir)
		return err
	}

	// open the sql db
	dbpath := filepath.Join(diagDir, "storage", "piecestore.db")
	db, err := psdb.Open(context.Background(), nil, dbpath)
	if err != nil {
		fmt.Println("Storagenode database couldnt open:", dbpath)
		return err
	}

	//get all bandwidth aggrements entries already ordered
	bwAgreements, err := db.GetBandwidthAllocations()
	if err != nil {
		fmt.Println("storage node 'bandwidth_agreements' table read error:", dbpath)
		return err
	}

	// Agreement is a struct that contains a bandwidth agreement and the associated signature
	type SatelliteSummary struct {
		TotalBytes           int64
		PutActionCount       int64
		GetActionCount       int64
		GetAuditActionCount  int64
		GetRepairActionCount int64
		PutRepairActionCount int64
		TotalTransactions    int64
		// additional attributes add here ...
	}

	// attributes per satelliteid
	summaries := make(map[storj.NodeID]*SatelliteSummary)
	satelliteIDs := storj.NodeIDList{}

	for _, rbaVal := range bwAgreements {
		for _, rbaDataVal := range rbaVal {
			// deserializing rbad you get payerbwallocation, total & storage node id
			rbad := &pb.RenterBandwidthAllocation_Data{}
			if err := proto.Unmarshal(rbaDataVal.Agreement, rbad); err != nil {
				return err
			}

			// deserializing pbad you get satelliteID, uplinkID, max size, exp, serial# & action
			pbad := &pb.PayerBandwidthAllocation_Data{}
			if err := proto.Unmarshal(rbad.GetPayerAllocation().GetData(), pbad); err != nil {
				return err
			}

			summary, ok := summaries[pbad.SatelliteId]
			if !ok {
				summaries[pbad.SatelliteId] = &SatelliteSummary{}
				satelliteIDs = append(satelliteIDs, pbad.SatelliteId)
				summary = summaries[pbad.SatelliteId]
			}

			// fill the summary info
			summary.TotalBytes += rbad.GetTotal()
			summary.TotalTransactions++
			switch pbad.GetAction() {
			case pb.PayerBandwidthAllocation_PUT:
				summary.PutActionCount++
			case pb.PayerBandwidthAllocation_GET:
				summary.GetActionCount++
			case pb.PayerBandwidthAllocation_GET_AUDIT:
				summary.GetAuditActionCount++
			case pb.PayerBandwidthAllocation_GET_REPAIR:
				summary.GetRepairActionCount++
			case pb.PayerBandwidthAllocation_PUT_REPAIR:
				summary.PutRepairActionCount++
			}
		}
	}

	// initialize the table header (fields)
	const padding = 3
	w := tabwriter.NewWriter(os.Stdout, 0, 0, padding, ' ', tabwriter.AlignRight|tabwriter.Debug)
	fmt.Fprintln(w, "SatelliteID\tTotal\t# Of Transactions\tPUT Action\tGET Action\tGET (Audit) Action\tGET (Repair) Action\tPUT (Repair) Action\t")

	// populate the row fields
	sort.Sort(satelliteIDs)
	for _, satelliteID := range satelliteIDs {
		summary := summaries[satelliteID]
		fmt.Fprint(w, satelliteID, "\t", summary.TotalBytes, "\t", summary.TotalTransactions, "\t",
			summary.PutActionCount, "\t", summary.GetActionCount, "\t", summary.GetAuditActionCount,
			"\t", summary.GetRepairActionCount, "\t", summary.PutRepairActionCount, "\t\n")
	}

	// display the data
	err = w.Flush()
	return err
}

func isOperatorEmailValid(email string) error {
	if email == "" {
		return fmt.Errorf("Operator mail address isn't specified")
	}
	return nil
}

func isOperatorWalletValid(wallet string) error {
	if wallet == "" {
		return fmt.Errorf("Operator wallet address isn't specified")
	}
	r := regexp.MustCompile("^0x[a-fA-F0-9]{40}$")
	if match := r.MatchString(wallet); !match {
		return fmt.Errorf("Operator wallet address isn't valid")
	}
	return nil
}

func main() {
	process.Exec(rootCmd)
}
