// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package discovery_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"storj.io/storj/internal/testcontext"
	"storj.io/storj/internal/testplanet"
)

func TestCache_Refresh(t *testing.T) {
	testplanet.Run(t, testplanet.Config{
		SatelliteCount: 1, StorageNodeCount: 10, UplinkCount: 0,
	}, func(t *testing.T, ctx *testcontext.Context, planet *testplanet.Planet) {
		satellite := planet.Satellites[0]
		for _, storageNode := range planet.StorageNodes {
			node, err := satellite.Overlay.Service.Get(ctx, storageNode.ID())
			if assert.NoError(t, err) {
				assert.Equal(t, storageNode.Addr(), node.Address.Address)
			}
		}
	})
}

func TestCache_Graveyard(t *testing.T) {
	testplanet.Run(t, testplanet.Config{
		SatelliteCount: 1, StorageNodeCount: 10, UplinkCount: 0,
	}, func(t *testing.T, ctx *testcontext.Context, planet *testplanet.Planet) {
		satellite := planet.Satellites[0]
		testnode := planet.StorageNodes[0]
		offlineID := testnode.ID()

		satellite.Discovery.Service.Graveyard.Pause()

		err := satellite.Overlay.Service.Delete(ctx, offlineID)
		assert.NoError(t, err)
		_, err = satellite.Overlay.Service.Get(ctx, offlineID)
		assert.NotNil(t, err)

		satellite.Discovery.Service.Graveyard.TriggerWait()

		found, err := satellite.Overlay.Service.Get(ctx, offlineID)
		assert.NoError(t, err)
		assert.NotNil(t, found)
		assert.Equal(t, offlineID, found.Id)
	})
}
