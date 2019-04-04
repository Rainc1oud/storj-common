// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package console

import (
	"context"
	"time"

	"github.com/skyrings/skyring-common/tools/uuid"
)

// UsageRollups defines how console works with usage rollups
type UsageRollups interface {
	GetProjectTotal(ctx context.Context, projectID uuid.UUID, since, before time.Time) (*ProjectUsage, error)
}

// ProjectUsage consist of period total storage, egress
// and objects count per hour for certain Project
type ProjectUsage struct {
	Storage      float64
	Egress       float64
	ObjectsCount float64

	Since  time.Time
	Before time.Time
}
