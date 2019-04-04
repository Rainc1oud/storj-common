// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package satellitedb

import (
	"context"
	"time"

	"github.com/skyrings/skyring-common/tools/uuid"
	"github.com/zeebo/errs"

	"storj.io/storj/internal/memory"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/satellite/console"
	dbx "storj.io/storj/satellite/satellitedb/dbx"
)

// usagerollups implements console.UsageRollups
type usagerollups struct {
	db *dbx.DB
}

// GetProjectTotal retrieves project usage for a given period
func (db *usagerollups) GetProjectTotal(ctx context.Context, projectID uuid.UUID, since, before time.Time) (usage *console.ProjectUsage, err error) {
	storageQuery := db.db.All_BucketStorageTally_By_ProjectId_And_BucketName_And_IntervalStart_GreaterOrEqual_And_IntervalStart_LessOrEqual_OrderBy_Desc_IntervalStart

	roullupsQuery := `SELECT SUM(settled), SUM(inline), action
			FROM bucket_bandwidth_rollups 
			WHERE project_id = ? AND interval_start >= ? AND interval_start <= ?
			GROUP BY action`

	rollupsRows, err := db.db.QueryContext(ctx, db.db.Rebind(roullupsQuery), []byte(projectID.String()), since, before)
	if err != nil {
		return nil, err
	}
	defer func() { err = errs.Combine(err, rollupsRows.Close()) }()

	var totalEgress int64
	for rollupsRows.Next() {
		var action pb.PieceAction
		var settled, inline int64

		err = rollupsRows.Scan(&settled, &inline, &action)
		if err != nil {
			return nil, err
		}

		// add values for egress
		if action == pb.PieceAction_GET || action == pb.PieceAction_GET_AUDIT || action == pb.PieceAction_GET_REPAIR {
			totalEgress += settled + inline
		}
	}

	bucketsQuery := "SELECT DISTINCT bucket_name FROM bucket_bandwidth_rollups where project_id = ? and interval_start >= ? and interval_start <= ?"
	bucketRows, err := db.db.QueryContext(ctx, db.db.Rebind(bucketsQuery), []byte(projectID.String()), since, before)
	if err != nil {
		return nil, err
	}
	defer func() { err = errs.Combine(err, bucketRows.Close()) }()

	var buckets []string
	for bucketRows.Next() {
		var bucket string
		err = bucketRows.Scan(&bucket)
		if err != nil {
			return nil, err
		}

		buckets = append(buckets, bucket)
	}

	bucketsTallies := make(map[string]*[]*dbx.BucketStorageTally)
	for _, bucket := range buckets {
		storageTallies, err := storageQuery(ctx,
			dbx.BucketStorageTally_ProjectId([]byte(projectID.String())),
			dbx.BucketStorageTally_BucketName([]byte(bucket)),
			dbx.BucketStorageTally_IntervalStart(since),
			dbx.BucketStorageTally_IntervalStart(before))

		if err != nil {
			return nil, err
		}

		bucketsTallies[bucket] = &storageTallies
	}

	usage = new(console.ProjectUsage)
	usage.Egress = memory.Size(totalEgress).GB()

	// sum up storage and objects
	for _, tallies := range bucketsTallies {
		for i := len(*tallies) - 1; i > 0; i-- {
			current := (*tallies)[i]

			hours := (*tallies)[i-1].IntervalStart.Sub(current.IntervalStart).Hours()

			usage.Storage += memory.Size(current.Inline).GB() * hours
			usage.Storage += memory.Size(current.Remote).GB() * hours
			usage.ObjectsCount += float64(current.ObjectCount) * hours
		}
	}

	usage.Since = since
	usage.Before = before
	return usage, nil
}
