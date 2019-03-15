// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package irreparable

import (
	"context"

	"storj.io/storj/pkg/pb"
)

// Inspector is a gRPC service for inspecting irreparable internals
type Inspector struct {
	irrdb DB
}

// NewInspector creates an Inspector
func NewInspector(irrdb DB) *Inspector {
	return &Inspector{irrdb: irrdb}
}

// ListSegments returns a number of irreparable segments by limit and offset
func (srv *Inspector) ListSegments(ctx context.Context, req *pb.ListSegmentsRequest) (*pb.ListSegmentsResponse, error) {
	segments, err := srv.irrdb.GetLimited(ctx, int(req.GetLimit()), int64(req.GetOffset()))
	if err != nil {
		return nil, err
	}

	return &pb.ListSegmentsResponse{Segments: segments}, err
}
