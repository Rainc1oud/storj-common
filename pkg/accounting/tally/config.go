// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package tally

import (
	"context"
	"net/url"
	"time"

	"go.uber.org/zap"
	"storj.io/storj/pkg/accounting"
	dbManager "storj.io/storj/pkg/bwagreement/database-manager"
	"storj.io/storj/pkg/kademlia"
	"storj.io/storj/pkg/overlay"
	"storj.io/storj/pkg/pointerdb"
	"storj.io/storj/pkg/provider"
)

// Config contains configurable values for tally
type Config struct {
	Interval    time.Duration `help:"how frequently tally should run" default:"30s"`
	DatabaseURL string        `help:"the database connection string to use" default:"sqlite3://$CONFDIR/stats.db"`
}

// Initialize a tally struct
func (c Config) initialize(ctx context.Context) (Tally, error) {
	pointerdb := pointerdb.LoadFromContext(ctx)
	overlay := overlay.LoadServerFromContext(ctx)
	kademlia := kademlia.LoadFromContext(ctx)
	db, err := accounting.NewDb(c.DatabaseURL)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(c.DatabaseURL)
	if err != nil {
		return nil, err
	}
	dbx, err := dbManager.NewDBManager(u.Scheme, u.Path)
	if err != nil {
		return nil, err
	}
	return newTally(zap.L(), db, dbx, pointerdb, overlay, kademlia, 0, c.Interval), nil
}

// Run runs the tally with configured values
func (c Config) Run(ctx context.Context, server *provider.Provider) (err error) {
	tally, err := c.initialize(ctx)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)

	go func() {
		if err := tally.Run(ctx); err != nil {
			defer cancel()
			zap.L().Error("Error running tally", zap.Error(err))
		}
	}()

	return server.Run(ctx)
}
