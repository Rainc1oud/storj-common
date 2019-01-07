// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/alicebob/miniredis"
	"github.com/spf13/cobra"
	"github.com/zeebo/errs"

	"storj.io/storj/pkg/auth/grpcauth"
	"storj.io/storj/pkg/cfgstruct"
	"storj.io/storj/pkg/process"
	"storj.io/storj/pkg/utils"
	"storj.io/storj/satellite/satellitedb"
)

const (
	storagenodeCount = 10
)

var (
	runCmd = &cobra.Command{
		Use:   "run",
		Short: "Run all servers",
		RunE:  cmdRun,
	}

	runCfg Captplanet
)

func init() {
	rootCmd.AddCommand(runCmd)
	cfgstruct.Bind(runCmd.Flags(), &runCfg, cfgstruct.ConfDir(defaultConfDir))
}

func cmdRun(cmd *cobra.Command, args []string) (err error) {
	ctx := process.Ctx(cmd)
	defer mon.Task()(&ctx)(&err)

	errch := make(chan error, len(runCfg.StorageNodes)+2)
	// start mini redis
	m := miniredis.NewMiniRedis()
	m.RequireAuth("abc123")

	if err = m.StartAddr(":6378"); err != nil {
		errch <- err
	} else {
		defer m.Close()
	}

	// start satellite
	go func() {
		_, _ = fmt.Printf("Starting satellite on %s\n",
			runCfg.Satellite.Server.Address)

		if runCfg.Satellite.Audit.SatelliteAddr == "" {
			runCfg.Satellite.Audit.SatelliteAddr = runCfg.Satellite.Server.Address
		}

		if runCfg.Satellite.Web.SatelliteAddr == "" {
			runCfg.Satellite.Web.SatelliteAddr = runCfg.Satellite.Server.Address
		}

		database, err := satellitedb.New(runCfg.Satellite.Database)
		if err != nil {
			errch <- errs.New("Error starting master database on satellite: %+v", err)
			return
		}

		err = database.CreateTables()
		if err != nil {
			errch <- errs.New("Error creating tables for master database on satellite: %+v", err)
			return
		}

		//nolint ignoring context rules to not create cyclic dependency, will be removed later
		ctx = context.WithValue(ctx, "masterdb", database)

		// Run satellite
		errch <- runCfg.Satellite.Server.Run(ctx,
			grpcauth.NewAPIKeyInterceptor(),
			runCfg.Satellite.Kademlia,
			runCfg.Satellite.Audit,
			runCfg.Satellite.Overlay,
			runCfg.Satellite.Discovery,
			runCfg.Satellite.PointerDB,
			runCfg.Satellite.Checker,
			runCfg.Satellite.Repairer,
			runCfg.Satellite.BwAgreement,
			runCfg.Satellite.Web,
			runCfg.Satellite.Tally,
			runCfg.Satellite.Rollup,

			// NB(dylan): Inspector is only used for local development and testing.
			// It should not be added to the Satellite startup
			runCfg.Satellite.Inspector,
		)
	}()

	// hack-fix t oensure that satellite gets up and running before starting storage nodes
	time.Sleep(2 * time.Second)

	// start the storagenodes
	for i, v := range runCfg.StorageNodes {
		go func(i int, v StorageNode) {
			identity, err := v.Server.Identity.Load()
			if err != nil {
				return
			}

			address := v.Server.Address
			storagenode := fmt.Sprintf("%s:%s", identity.ID.String(), address)

			_, _ = fmt.Printf("Starting storage node %d %s (kad on %s)\n", i, storagenode, address)
			errch <- v.Server.Run(ctx, nil, v.Kademlia, v.Storage)
		}(i, v)
	}
	// start s3 uplink
	go func() {
		_, _ = fmt.Printf("Starting s3-gateway on %s\nAccess key: %s\nSecret key: %s\n",
			runCfg.Uplink.Server.Address,
			runCfg.Uplink.Minio.AccessKey,
			runCfg.Uplink.Minio.SecretKey)
		errch <- runCfg.Uplink.Run(ctx)
	}()

	return utils.CollectErrors(errch, 5*time.Second)
}
