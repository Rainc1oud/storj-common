// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package satellitedb

import (
	"context"

	"github.com/golang/protobuf/proto"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/pkg/utils"
	dbx "storj.io/storj/satellite/satellitedb/dbx"
	"storj.io/storj/storage"
)

type repairQueue struct {
	db  *dbx.DB
	ctx context.Context
}

func newRepairQueue(db *dbx.DB) *repairQueue {
	return &repairQueue{
		db:  db,
		ctx: context.Background(),
	}
}

func (r *repairQueue) Enqueue(seg *pb.InjuredSegment) error {
	val, err := proto.Marshal(seg)
	if err != nil {
		return err
	}

	_, err = r.db.Create_Injuredsegment(
		r.ctx,
		dbx.Injuredsegment_Info(val),
	)
	return err
}

func (r *repairQueue) Dequeue() (pb.InjuredSegment, error) {
	tx, err := r.db.Open(r.ctx)
	if err != nil {
		return pb.InjuredSegment{}, Error.Wrap(err)
	}

	res, err := tx.First_Injuredsegment(r.ctx)
	if err != nil {
		return pb.InjuredSegment{}, Error.Wrap(utils.CombineErrors(err, tx.Rollback()))
	}
	if res == nil {
		return pb.InjuredSegment{}, Error.Wrap(utils.CombineErrors(storage.ErrEmptyQueue, tx.Rollback()))
	}

	deleted, err := tx.Delete_Injuredsegment_By_Info(
		r.ctx,
		dbx.Injuredsegment_Info(res.Info),
	)
	if err != nil {
		return pb.InjuredSegment{}, Error.Wrap(utils.CombineErrors(err, tx.Rollback()))
	}
	if !deleted {
		return pb.InjuredSegment{}, Error.Wrap(utils.CombineErrors(Error.New("Injured segment not deleted"), tx.Rollback()))
	}

	seg := &pb.InjuredSegment{}
	err = proto.Unmarshal(res.Info, seg)
	if err != nil {
		return pb.InjuredSegment{}, Error.Wrap(utils.CombineErrors(err, tx.Rollback()))
	}
	return *seg, Error.Wrap(tx.Commit())
}

func (r *repairQueue) Peekqueue(limit int) ([]pb.InjuredSegment, error) {
	if limit <= 0 || limit > storage.LookupLimit {
		limit = storage.LookupLimit
	}
	rows, err := r.db.Limited_Injuredsegment(r.ctx, limit, 0)
	if err != nil {
		return nil, err
	}

	segments := make([]pb.InjuredSegment, 0)
	for _, entry := range rows {
		seg := &pb.InjuredSegment{}
		if err = proto.Unmarshal(entry.Info, seg); err != nil {
			return nil, err
		}
		segments = append(segments, *seg)
	}
	return segments, nil
}
