// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information

package sync2_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	"storj.io/storj/internal/sync2"
)

func TestCycle_Basic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	var inplace sync2.Cycle
	inplace.SetInterval(time.Second)

	var pointer = sync2.NewCycle(time.Second)

	for _, cycle := range []*sync2.Cycle{pointer, &inplace} {
		cycle := cycle
		t.Run("", func(t *testing.T) {
			defer cycle.Close()

			t.Parallel()

			count := int64(0)

			var group errgroup.Group

			start := time.Now()

			cycle.Start(ctx, &group, func(ctx context.Context) error {
				atomic.AddInt64(&count, 1)
				return nil
			})

			group.Go(func() error {
				defer cycle.Stop()

				const expected = 10
				cycle.Pause()

				startingCount := atomic.LoadInt64(&count)
				for i := 0; i < expected-1; i++ {
					cycle.Trigger()
				}
				cycle.TriggerWait()
				countAfterTrigger := atomic.LoadInt64(&count)

				change := countAfterTrigger - startingCount
				if expected != change {
					return fmt.Errorf("invalid triggers expected %d got %d", expected, change)
				}

				cycle.Restart()
				time.Sleep(3 * time.Second)

				countAfterRestart := atomic.LoadInt64(&count)
				if countAfterRestart == countAfterTrigger {
					return fmt.Errorf("cycle has not restarted")
				}

				return nil
			})

			err := group.Wait()
			if err != nil {
				t.Error(err)
			}

			testDuration := time.Since(start)
			if testDuration > 7*time.Second {
				t.Errorf("test took too long %v, expected approximately 3s", testDuration)
			}

			// shouldn't block
			cycle.Trigger()
		})
	}
}

func TestCycle_MultipleStops(t *testing.T) {
	t.Parallel()

	cycle := sync2.NewCycle(time.Second)
	defer cycle.Close()

	ctx := context.Background()

	var group errgroup.Group
	var count int64
	cycle.Start(ctx, &group, func(ctx context.Context) error {
		atomic.AddInt64(&count, 1)
		return nil
	})

	go cycle.Stop()
	cycle.Stop()
	cycle.Stop()
}

func TestCycle_StopCancelled(t *testing.T) {
	t.Parallel()

	cycle := sync2.NewCycle(time.Second)
	defer cycle.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var group errgroup.Group
	var count int64
	cycle.Start(ctx, &group, func(ctx context.Context) error {
		atomic.AddInt64(&count, 1)
		return nil
	})

	cycle.Stop()
	cycle.Stop()
}
