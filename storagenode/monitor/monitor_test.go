// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package monitor_test

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"storj.io/storj/internal/memory"
	"storj.io/storj/internal/testcontext"
	"storj.io/storj/internal/testplanet"
	"storj.io/storj/pkg/pb"
)

func TestMonitor(t *testing.T) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	planet, err := testplanet.New(t, 1, 6, 1)
	require.NoError(t, err)
	defer ctx.Check(planet.Shutdown)

	planet.Start(ctx)

	var freeBandwidth int64
	for _, storageNode := range planet.StorageNodes {
		storageNode.Storage2.Monitor.Loop.Pause()

		info, err := storageNode.Kademlia.Service.FetchInfo(ctx, storageNode.Local())
		require.NoError(t, err)

		// assume that all storage nodes have the same initial values
		freeBandwidth = info.Capacity.FreeBandwidth
	}

	expectedData := make([]byte, 100*memory.KiB)
	_, err = rand.Read(expectedData)
	require.NoError(t, err)

	err = planet.Uplinks[0].Upload(ctx, planet.Satellites[0], "test/bucket", "test/path", expectedData)
	require.NoError(t, err)

	nodeAssertions := 0
	for _, storageNode := range planet.StorageNodes {
		storageNode.Storage2.Monitor.Loop.TriggerWait()

		info, err := storageNode.Kademlia.Service.FetchInfo(ctx, storageNode.Local())
		require.NoError(t, err)

		stats, err := storageNode.Storage2.Inspector.Stats(ctx, &pb.StatsRequest{})
		require.NoError(t, err)
		if stats.UsedSpace > 0 {
			assert.Equal(t, freeBandwidth-stats.UsedBandwidth, info.Capacity.FreeBandwidth)
			nodeAssertions++
		}
	}
	assert.NotZero(t, nodeAssertions, "No storage node were verifed")
}
