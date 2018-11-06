// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package repairer

import (
	"context"
	"time"

	"github.com/vivint/infectious"
	"go.uber.org/zap"

	"storj.io/storj/pkg/datarepair/queue"
	"storj.io/storj/pkg/eestream"
	"storj.io/storj/pkg/miniogw"
	"storj.io/storj/pkg/overlay"
	"storj.io/storj/pkg/pointerdb/pdbclient"
	"storj.io/storj/pkg/provider"
	ecclient "storj.io/storj/pkg/storage/ec"
	segment "storj.io/storj/pkg/storage/segments"
	"storj.io/storj/pkg/transport"
	"storj.io/storj/storage/redis"
)

// Config contains configurable values for repairer
type Config struct {
	QueueAddress string        `help:"data repair queue address" default:"redis://127.0.0.1:6378?db=1&password=abc123"`
	MaxRepair    int           `help:"maximum segments that can be repaired concurrently" default:"100"`
	Interval     time.Duration `help:"how frequently checker should audit segments" default:"3600s"`
	miniogw.ClientConfig
	miniogw.RSConfig
}

// Run runs the repairer with configured values
func (c Config) Run(ctx context.Context, server *provider.Provider) (err error) {
	client, err := redis.NewClientFrom(c.QueueAddress)
	if err != nil {
		return Error.Wrap(err)
	}

	queue := queue.NewQueue(client)

	ss, err := c.getSegmentStore(ctx, server.Identity())
	if err != nil {
		return Error.Wrap(err)
	}

	repairer := newRepairer(queue, ss, c.Interval, c.MaxRepair)

	// TODO(coyle): we need to figure out how to propagate the error up to cancel the service
	go func() {
		if err := repairer.Run(ctx); err != nil {
			zap.L().Error("Error running repairer", zap.Error(err))
		}
	}()

	return server.Run(ctx)
}

// getSegmentStore creates a new segment store from storeConfig values
func (c Config) getSegmentStore(ctx context.Context, identity *provider.FullIdentity) (ss segment.Store, err error) {
	defer mon.Task()(&ctx)(&err)

	t := transport.NewClient(identity)

	var oc overlay.Client
	oc, err = overlay.NewOverlayClient(identity, c.OverlayAddr)
	if err != nil {
		return nil, err
	}

	pdb, err := pdbclient.NewClient(identity, c.PointerDBAddr, c.APIKey)
	if err != nil {
		return nil, err
	}

	ec := ecclient.NewClient(identity, t, c.MaxBufferMem)
	fc, err := infectious.NewFEC(c.MinThreshold, c.MaxThreshold)
	if err != nil {
		return nil, err
	}
	rs, err := eestream.NewRedundancyStrategy(eestream.NewRSScheme(fc, c.ErasureShareSize), c.RepairThreshold, c.SuccessThreshold)
	if err != nil {
		return nil, err
	}

	return segment.NewSegmentStore(oc, ec, pdb, rs, c.MaxInlineSize), nil
}
