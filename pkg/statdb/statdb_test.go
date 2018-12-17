// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package statdb_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"storj.io/storj/internal/testcontext"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/statdb"
	"storj.io/storj/pkg/storj"
	"storj.io/storj/satellite/satellitedb"
	"storj.io/storj/satellite/satellitedb/satellitedbtest"
)

func getRatio(s, t int) (success, total int64, ratio float64) {
	ratio = float64(s) / float64(t)
	return int64(s), int64(t), ratio
}

func TestStatdb(t *testing.T) {
	satellitedbtest.Run(t, func(t *testing.T, db *satellitedb.DB) {
		ctx := testcontext.New(t)
		defer ctx.Cleanup()

		testDatabase(ctx, t, db.StatDB())
	})
}

func testDatabase(ctx context.Context, t *testing.T, sdb statdb.DB) {
	nodeID := storj.NodeID{1, 2, 3, 4, 5}

	{ // TestCreateNewAndWithStats
		auditSuccessCount, auditCount, auditSuccessRatio := getRatio(4, 10)
		uptimeSuccessCount, uptimeCount, uptimeRatio := getRatio(8, 25)
		nodeStats := &pb.NodeStats{
			AuditSuccessRatio:  auditSuccessRatio,
			UptimeRatio:        uptimeRatio,
			AuditCount:         auditCount,
			AuditSuccessCount:  auditSuccessCount,
			UptimeCount:        uptimeCount,
			UptimeSuccessCount: uptimeSuccessCount,
		}
		createReq := &statdb.CreateRequest{
			Node:  nodeID,
			Stats: nodeStats,
		}
		resp, err := sdb.Create(ctx, createReq)
		assert.NoError(t, err)
		s := resp.Stats
		assert.EqualValues(t, auditSuccessRatio, s.AuditSuccessRatio)
		assert.EqualValues(t, uptimeRatio, s.UptimeRatio)

		getReq := &statdb.GetRequest{
			Node: nodeID,
		}
		getResp, err := sdb.Get(ctx, getReq)
		assert.NoError(t, err)

		assert.EqualValues(t, nodeID, getResp.Stats.NodeId)
		assert.EqualValues(t, auditCount, getResp.Stats.AuditCount)
		assert.EqualValues(t, auditSuccessCount, getResp.Stats.AuditSuccessCount)
		assert.EqualValues(t, auditSuccessRatio, getResp.Stats.AuditSuccessRatio)
		assert.EqualValues(t, uptimeCount, getResp.Stats.UptimeCount)
		assert.EqualValues(t, uptimeSuccessCount, getResp.Stats.UptimeSuccessCount)
		assert.EqualValues(t, uptimeRatio, getResp.Stats.UptimeRatio)
	}

	{ // TestCreateExists
		auditSuccessCount, auditCount, auditSuccessRatio := getRatio(4, 10)
		uptimeSuccessCount, uptimeCount, uptimeRatio := getRatio(8, 25)
		nodeStats := &pb.NodeStats{
			AuditSuccessRatio:  auditSuccessRatio,
			UptimeRatio:        uptimeRatio,
			AuditCount:         auditCount,
			AuditSuccessCount:  auditSuccessCount,
			UptimeCount:        uptimeCount,
			UptimeSuccessCount: uptimeSuccessCount,
		}
		createReq := &statdb.CreateRequest{
			Node:  nodeID,
			Stats: nodeStats,
		}
		_, err := sdb.Create(ctx, createReq)
		assert.Error(t, err)
	}

	{ // TestGetDoesNotExist
		noNodeID := storj.NodeID{255, 255, 255, 255}

		getReq := &statdb.GetRequest{
			Node: noNodeID,
		}
		_, err := sdb.Get(ctx, getReq)
		assert.Error(t, err)
	}

	{ // TestFindInvalidNodes
		for _, tt := range []struct {
			nodeID             storj.NodeID
			auditSuccessCount  int64
			auditCount         int64
			auditSuccessRatio  float64
			uptimeSuccessCount int64
			uptimeCount        int64
			uptimeRatio        float64
		}{
			{storj.NodeID{1}, 20, 20, 1, 20, 20, 1},   // good audit success
			{storj.NodeID{2}, 5, 20, 0.25, 20, 20, 1}, // bad audit success, good uptime
			{storj.NodeID{3}, 20, 20, 1, 5, 20, 0.25}, // good audit success, bad uptime
			{storj.NodeID{4}, 0, 0, 0, 20, 20, 1},     // "bad" audit success, no audits
			{storj.NodeID{5}, 20, 20, 1, 0, 0, 0.25},  // "bad" uptime success, no checks
			{storj.NodeID{6}, 0, 1, 0, 5, 5, 1},       // bad audit success exactly one audit
			{storj.NodeID{7}, 0, 20, 0, 20, 20, 1},    // bad ratios, excluded from query
		} {
			nodeStats := &pb.NodeStats{
				AuditSuccessRatio:  tt.auditSuccessRatio,
				UptimeRatio:        tt.uptimeRatio,
				AuditCount:         tt.auditCount,
				AuditSuccessCount:  tt.auditSuccessCount,
				UptimeCount:        tt.uptimeCount,
				UptimeSuccessCount: tt.uptimeSuccessCount,
			}
			createReq := &statdb.CreateRequest{
				Node:  tt.nodeID,
				Stats: nodeStats,
			}

			_, err := sdb.Create(ctx, createReq)
			assert.NoError(t, err)
		}

		findInvalidNodesReq := &statdb.FindInvalidNodesRequest{
			NodeIds: storj.NodeIDList{
				storj.NodeID{1}, storj.NodeID{2},
				storj.NodeID{3}, storj.NodeID{4},
				storj.NodeID{5}, storj.NodeID{6},
			},
			MaxStats: &pb.NodeStats{
				AuditSuccessRatio: 0.5,
				UptimeRatio:       0.5,
			},
		}

		resp, err := sdb.FindInvalidNodes(ctx, findInvalidNodesReq)
		assert.NoError(t, err)

		invalid := resp.InvalidIds

		assert.Contains(t, invalid, storj.NodeID{2})
		assert.Contains(t, invalid, storj.NodeID{3})
		assert.Contains(t, invalid, storj.NodeID{6})
		assert.Len(t, invalid, 3)
	}

	{ // TestUpdateExists
		auditSuccessCount, auditCount, auditSuccessRatio := getRatio(4, 10)
		uptimeSuccessCount, uptimeCount, uptimeRatio := getRatio(8, 25)

		getReq := &statdb.GetRequest{
			Node: nodeID,
		}
		getResp, err := sdb.Get(ctx, getReq)
		assert.NoError(t, err)

		assert.EqualValues(t, nodeID, getResp.Stats.NodeId)
		assert.EqualValues(t, auditCount, getResp.Stats.AuditCount)
		assert.EqualValues(t, auditSuccessCount, getResp.Stats.AuditSuccessCount)
		assert.EqualValues(t, auditSuccessRatio, getResp.Stats.AuditSuccessRatio)
		assert.EqualValues(t, uptimeCount, getResp.Stats.UptimeCount)
		assert.EqualValues(t, uptimeSuccessCount, getResp.Stats.UptimeSuccessCount)
		assert.EqualValues(t, uptimeRatio, getResp.Stats.UptimeRatio)

		updateReq := &statdb.UpdateRequest{
			Node:               nodeID,
			UpdateAuditSuccess: true,
			AuditSuccess:       true,
			UpdateUptime:       true,
			IsUp:               false,
		}
		updResp, err := sdb.Update(ctx, updateReq)
		assert.NoError(t, err)

		_, _, newAuditRatio := getRatio(int(auditSuccessCount+1), int(auditCount+1))
		_, _, newUptimeRatio := getRatio(int(uptimeSuccessCount), int(uptimeCount+1))
		stats := updResp.Stats
		assert.EqualValues(t, newAuditRatio, stats.AuditSuccessRatio)
		assert.EqualValues(t, newUptimeRatio, stats.UptimeRatio)
	}

	{ // TestUpdateUptimeExists
		auditSuccessCount, auditCount, auditSuccessRatio := getRatio(5, 11)
		uptimeSuccessCount, uptimeCount, uptimeRatio := getRatio(8, 26)

		getReq := &statdb.GetRequest{
			Node: nodeID,
		}
		getResp, err := sdb.Get(ctx, getReq)
		assert.NoError(t, err)

		assert.EqualValues(t, nodeID, getResp.Stats.NodeId)
		assert.EqualValues(t, auditCount, getResp.Stats.AuditCount)
		assert.EqualValues(t, auditSuccessCount, getResp.Stats.AuditSuccessCount)
		assert.EqualValues(t, auditSuccessRatio, getResp.Stats.AuditSuccessRatio)
		assert.EqualValues(t, uptimeCount, getResp.Stats.UptimeCount)
		assert.EqualValues(t, uptimeSuccessCount, getResp.Stats.UptimeSuccessCount)
		assert.EqualValues(t, uptimeRatio, getResp.Stats.UptimeRatio)
		updateReq := &statdb.UpdateUptimeRequest{
			Node: nodeID,
			IsUp: false,
		}
		resp, err := sdb.UpdateUptime(ctx, updateReq)
		assert.NoError(t, err)

		_, _, newUptimeRatio := getRatio(int(uptimeSuccessCount), int(uptimeCount+1))
		stats := resp.Stats
		assert.EqualValues(t, auditSuccessRatio, stats.AuditSuccessRatio)
		assert.EqualValues(t, auditCount, stats.AuditCount)
		assert.EqualValues(t, newUptimeRatio, stats.UptimeRatio)
	}

	{ // TestUpdateAuditSuccessExists
		auditSuccessCount, auditCount, auditSuccessRatio := getRatio(5, 11)
		uptimeSuccessCount, uptimeCount, uptimeRatio := getRatio(8, 27)
		getReq := &statdb.GetRequest{
			Node: nodeID,
		}
		getResp, err := sdb.Get(ctx, getReq)
		assert.NoError(t, err)

		assert.EqualValues(t, nodeID, getResp.Stats.NodeId)
		assert.EqualValues(t, auditCount, getResp.Stats.AuditCount)
		assert.EqualValues(t, auditSuccessCount, getResp.Stats.AuditSuccessCount)
		assert.EqualValues(t, auditSuccessRatio, getResp.Stats.AuditSuccessRatio)
		assert.EqualValues(t, uptimeCount, getResp.Stats.UptimeCount)
		assert.EqualValues(t, uptimeSuccessCount, getResp.Stats.UptimeSuccessCount)
		assert.EqualValues(t, uptimeRatio, getResp.Stats.UptimeRatio)

		updateReq := &statdb.UpdateAuditSuccessRequest{
			Node:         nodeID,
			AuditSuccess: false,
		}

		resp, err := sdb.UpdateAuditSuccess(ctx, updateReq)
		assert.NoError(t, err)

		_, _, newAuditRatio := getRatio(int(auditSuccessCount), int(auditCount+1))
		stats := resp.Stats
		assert.EqualValues(t, newAuditRatio, stats.AuditSuccessRatio)
		assert.EqualValues(t, auditCount+1, stats.AuditCount)
		assert.EqualValues(t, uptimeRatio, stats.UptimeRatio)
	}

	{ // TestUpdateBatchExists
		nodeID1 := storj.NodeID{255, 1}
		nodeID2 := storj.NodeID{255, 2}

		auditSuccessCount1, auditCount1, auditRatio1 := getRatio(4, 10)
		uptimeSuccessCount1, uptimeCount1, uptimeRatio1 := getRatio(8, 25)
		nodeStats := &pb.NodeStats{
			AuditSuccessCount:  auditSuccessCount1,
			AuditCount:         auditCount1,
			AuditSuccessRatio:  auditRatio1,
			UptimeSuccessCount: uptimeSuccessCount1,
			UptimeCount:        uptimeCount1,
			UptimeRatio:        uptimeRatio1,
		}
		createReq := &statdb.CreateRequest{
			Node:  nodeID1,
			Stats: nodeStats,
		}
		resp, err := sdb.Create(ctx, createReq)
		assert.NoError(t, err)
		s := resp.Stats
		assert.EqualValues(t, auditRatio1, s.AuditSuccessRatio)
		assert.EqualValues(t, uptimeRatio1, s.UptimeRatio)

		auditSuccessCount2, auditCount2, auditRatio2 := getRatio(7, 10)
		uptimeSuccessCount2, uptimeCount2, uptimeRatio2 := getRatio(8, 20)
		nodeStats = &pb.NodeStats{
			AuditSuccessCount:  auditSuccessCount2,
			AuditCount:         auditCount2,
			AuditSuccessRatio:  auditRatio2,
			UptimeSuccessCount: uptimeSuccessCount2,
			UptimeCount:        uptimeCount2,
			UptimeRatio:        uptimeRatio2,
		}
		createReq = &statdb.CreateRequest{
			Node:  nodeID2,
			Stats: nodeStats,
		}
		resp, err = sdb.Create(ctx, createReq)
		assert.NoError(t, err)
		s = resp.Stats
		assert.EqualValues(t, auditRatio2, s.AuditSuccessRatio)
		assert.EqualValues(t, uptimeRatio2, s.UptimeRatio)

		node1 := &statdb.UpdateRequest{
			Node:               nodeID1,
			UpdateAuditSuccess: true,
			AuditSuccess:       true,
			UpdateUptime:       true,
			IsUp:               false,
		}
		node2 := &statdb.UpdateRequest{
			Node:               nodeID2,
			UpdateAuditSuccess: true,
			AuditSuccess:       true,
			UpdateUptime:       false,
		}
		updateBatchReq := &statdb.UpdateBatchRequest{
			NodeList: []*statdb.UpdateRequest{node1, node2},
		}
		batchUpdResp, err := sdb.UpdateBatch(ctx, updateBatchReq)
		assert.NoError(t, err)

		_, _, newAuditRatio1 := getRatio(int(auditSuccessCount1+1), int(auditCount1+1))
		_, _, newUptimeRatio1 := getRatio(int(uptimeSuccessCount1), int(uptimeCount1+1))
		_, _, newAuditRatio2 := getRatio(int(auditSuccessCount2+1), int(auditCount2+1))
		stats1 := batchUpdResp.StatsList[0]
		stats2 := batchUpdResp.StatsList[1]
		assert.EqualValues(t, newAuditRatio1, stats1.AuditSuccessRatio)
		assert.EqualValues(t, newUptimeRatio1, stats1.UptimeRatio)
		assert.EqualValues(t, newAuditRatio2, stats2.AuditSuccessRatio)
		assert.EqualValues(t, uptimeRatio2, stats2.UptimeRatio)
	}
}
