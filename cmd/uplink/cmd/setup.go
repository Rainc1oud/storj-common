// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package cmd

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zeebo/errs"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh/terminal"

	"storj.io/storj/internal/fpath"
	"storj.io/storj/pkg/cfgstruct"
	"storj.io/storj/pkg/process"
)

var (
	setupCmd = &cobra.Command{
		Use:         "setup",
		Short:       "Create an uplink config file",
		RunE:        cmdSetup,
		Annotations: map[string]string{"type": "setup"},
	}
	setupCfg    UplinkFlags
	confDir     string
	identityDir string
	isDev       bool

	// Error is the default uplink setup errs class
	Error = errs.Class("uplink setup error")
)

func init() {
	defaultConfDir := fpath.ApplicationDir("storj", "uplink")
	defaultIdentityDir := fpath.ApplicationDir("storj", "identity", "uplink")
	cfgstruct.SetupFlag(zap.L(), RootCmd, &confDir, "config-dir", defaultConfDir, "main directory for uplink configuration")
	cfgstruct.SetupFlag(zap.L(), RootCmd, &identityDir, "identity-dir", defaultIdentityDir, "main directory for uplink identity credentials")
	cfgstruct.DevFlag(RootCmd, &isDev, false, "use development and test configuration settings")
	RootCmd.AddCommand(setupCmd)
	cfgstruct.BindSetup(setupCmd.Flags(), &setupCfg, isDev, cfgstruct.ConfDir(confDir), cfgstruct.IdentityDir(identityDir))
}

func cmdSetup(cmd *cobra.Command, args []string) (err error) {
	// Ensure use the default port if the user only specifies a host.
	err = ApplyDefaultHostAndPortToAddrFlag(cmd, "satellite-addr")
	if err != nil {
		return err
	}

	setupDir, err := filepath.Abs(confDir)
	if err != nil {
		return err
	}

	valid, _ := fpath.IsValidSetupDir(setupDir)
	if !valid {
		return fmt.Errorf("uplink configuration already exists (%v)", setupDir)
	}

	err = os.MkdirAll(setupDir, 0700)
	if err != nil {
		return err
	}

	var override map[string]interface{}
	if !setupCfg.NonInteractive {
		fmt.Print("Enter your Satellite address: ")
		var satelliteAddress string
		fmt.Scanln(&satelliteAddress)

		// TODO add better validation
		if satelliteAddress == "" {
			return errs.New("Satellite address cannot be empty")
		}

		fmt.Print("Enter your API key: ")
		var apiKey string
		fmt.Scanln(&apiKey)

		if apiKey == "" {
			return errs.New("API key cannot be empty")
		}

		fmt.Print("Enter your encryption passphrase: ")
		encKey, err := terminal.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return err
		}
		fmt.Println()

		fmt.Print("Enter your encryption passphrase again: ")
		repeatedEncKey, err := terminal.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return err
		}
		fmt.Println()

		if !bytes.Equal(encKey, repeatedEncKey) {
			return errs.New("encryption passphrases doesn't match")
		}

		if len(encKey) == 0 {
			fmt.Println("Warning: Encryption passphrase is empty!")
		}

		override = map[string]interface{}{
			"satellite-addr": satelliteAddress,
			"api-key":        apiKey,
			"enc.key":        string(encKey),
		}
	}

	return process.SaveConfigWithAllDefaults(cmd.Flags(), filepath.Join(setupDir, "config.yaml"), override)
}

// ApplyDefaultHostAndPortToAddrFlag applies the default host and/or port if either is missing in the specified flag name.
func ApplyDefaultHostAndPortToAddrFlag(cmd *cobra.Command, flagName string) error {
	defaultHost, defaultPort, err := net.SplitHostPort(cmd.Flags().Lookup(flagName).DefValue)
	if err != nil {
		return Error.Wrap(err)
	}

	flag := cmd.Flags().Lookup(flagName)
	if flag == nil {
		// No flag found for us to handle.
		return nil
	}
	address := flag.Value.String()

	addressParts := strings.Split(address, ":")
	numberOfParts := len(addressParts)

	if numberOfParts > 1 && len(addressParts[0]) > 0 {
		// address is host:port so skip applying any defaults.
		return nil
	}

	// We are missing a host:port part. Figure out which part we are missing.
	indexOfPortSeparator := strings.Index(address, ":")
	lengthOfFirstPart := len(addressParts[0])

	if indexOfPortSeparator == -1 {
		if lengthOfFirstPart == 0 {
			// address is blank.
			address = net.JoinHostPort(defaultHost, defaultPort)
		} else {
			// address is host
			address = net.JoinHostPort(addressParts[0], defaultPort)
		}
	} else if indexOfPortSeparator == 0 {
		// address is :1234
		address = net.JoinHostPort(defaultHost, addressParts[1])
	} else if indexOfPortSeparator > 0 {
		// address is host:
		address = net.JoinHostPort(defaultPort, addressParts[0])
	}

	err = flag.Value.Set(address)
	if err != nil {
		return Error.Wrap(err)
	}
	return nil
}
