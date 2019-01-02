// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zeebo/errs"
	"golang.org/x/sync/errgroup"

	"storj.io/storj/internal/fpath"
	"storj.io/storj/internal/processgroup"
	"storj.io/storj/pkg/utils"
)

const folderPermissions = 0744

func networkExec(flags *Flags, args []string, command string) error {
	processes, err := newNetwork(flags.Directory, flags.SatelliteCount, flags.StorageNodeCount)
	if err != nil {
		return err
	}

	ctx, cancel := NewCLIContext(context.Background())
	defer cancel()

	err = processes.Exec(ctx, command)
	closeErr := processes.Close()

	return errs.Combine(err, closeErr)
}

func networkTest(flags *Flags, command string, args []string) error {
	processes, err := newNetwork(flags.Directory, flags.SatelliteCount, flags.StorageNodeCount)
	if err != nil {
		return err
	}

	ctx, cancel := NewCLIContext(context.Background())

	var group errgroup.Group
	processes.Start(ctx, &group, "run")

	time.Sleep(2 * time.Second)

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = append(os.Environ(), processes.Env()...)
	stdout := processes.Output.Prefixed("test:out")
	stderr := processes.Output.Prefixed("test:err")
	cmd.Stdout, cmd.Stderr = stdout, stderr
	processgroup.Setup(cmd)

	if printCommands {
		fmt.Fprintf(processes.Output, "exec: %v\n", strings.Join(cmd.Args, " "))
	}
	errRun := cmd.Run()

	cancel()
	return errs.Combine(errRun, processes.Close(), group.Wait())
}

func networkDestroy(flags *Flags, args []string) error {
	if fpath.IsRoot(flags.Directory) {
		return errors.New("safety check: disallowed to remove root directory " + flags.Directory)
	}
	if printCommands {
		fmt.Println("sdk | exec: rm -rf", flags.Directory)
	}
	return os.RemoveAll(flags.Directory)
}

// newNetwork creates a default network
func newNetwork(dir string, satelliteCount, storageNodeCount int) (*Processes, error) {
	processes := NewProcesses()

	const (
		host            = "127.0.0.1"
		gatewayPort     = 9000
		satellitePort   = 10000
		storageNodePort = 11000
	)

	defaultSatellite := net.JoinHostPort(host, strconv.Itoa(satellitePort+0))

	arguments := func(name, command, addr string, rest ...string) []string {
		return append([]string{
			"--log.level", "debug",
			"--config-dir", ".",
			command,
			"--server.address", addr,
		}, rest...)
	}

	for i := 0; i < satelliteCount; i++ {
		name := fmt.Sprintf("satellite/%d", i)

		dir := filepath.Join(dir, "satellite", fmt.Sprint(i))
		if err := os.MkdirAll(dir, folderPermissions); err != nil {
			return nil, err
		}

		process, err := processes.New(name, "satellite", dir)
		if err != nil {
			return nil, utils.CombineErrors(err, processes.Close())
		}
		process.Info.Address = net.JoinHostPort(host, strconv.Itoa(satellitePort+i))

		process.Arguments["setup"] = arguments(name, "setup", process.Info.Address)
		process.Arguments["run"] = arguments(name, "run", process.Info.Address,
			"--kademlia.bootstrap-addr", defaultSatellite,
		)
	}

	gatewayArguments := func(name, command string, addr string, rest ...string) []string {
		return append([]string{
			"--log.level", "debug",
			"--config-dir", ".",
			command,
			"--server.address", addr,
		}, rest...)
	}

	for i := 0; i < satelliteCount; i++ {
		name := fmt.Sprintf("gateway/%d", i)

		dir := filepath.Join(dir, "gateway", fmt.Sprint(i))
		if err := os.MkdirAll(dir, folderPermissions); err != nil {
			return nil, err
		}

		satellite := processes.List[i]

		process, err := processes.New(name, "gateway", dir)
		if err != nil {
			return nil, utils.CombineErrors(err, processes.Close())
		}
		process.Info.Address = net.JoinHostPort(host, strconv.Itoa(gatewayPort+i))

		process.Arguments["setup"] = gatewayArguments(name, "setup", process.Info.Address,
			"--satellite-addr", satellite.Info.Address,
		)
		process.Arguments["run"] = gatewayArguments(name, "run", process.Info.Address)
	}

	for i := 0; i < storageNodeCount; i++ {
		name := fmt.Sprintf("storage/%d", i)

		dir := filepath.Join(dir, "storage", fmt.Sprint(i))
		if err := os.MkdirAll(dir, folderPermissions); err != nil {
			return nil, err
		}

		process, err := processes.New(name, "storagenode", dir)
		if err != nil {
			return nil, utils.CombineErrors(err, processes.Close())
		}
		process.Info.Address = net.JoinHostPort(host, strconv.Itoa(storageNodePort+i))

		process.Arguments["setup"] = arguments(name, "setup", process.Info.Address,
			"--piecestore.agreementsender.overlay-addr", defaultSatellite,
		)
		process.Arguments["run"] = arguments(name, "run", process.Info.Address,
			"--piecestore.agreementsender.overlay-addr", defaultSatellite,
			"--kademlia.bootstrap-addr", defaultSatellite,
			"--kademlia.operator.email", fmt.Sprintf("storage%d@example.com", i),
			"--kademlia.operator.wallet", "0x0123456789012345678901234567890123456789",
		)
	}

	return processes, nil
}
