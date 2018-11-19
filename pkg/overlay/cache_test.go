// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package overlay_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"storj.io/storj/internal/testcontext"
	"storj.io/storj/internal/testplanet"
	"storj.io/storj/pkg/overlay"
	"storj.io/storj/pkg/pb"
	"storj.io/storj/storage"
	"storj.io/storj/storage/boltdb"
	"storj.io/storj/storage/redis"
	"storj.io/storj/storage/redis/redisserver"
	"storj.io/storj/storage/teststore"
)

func testCache(ctx context.Context, t *testing.T, store storage.KeyValueStore) {
	cache := overlay.Cache{DB: store}

	{ // Put
		err := cache.Put("valid1", pb.Node{Address: &pb.NodeAddress{Transport: pb.NodeTransport_TCP_TLS_GRPC, Address: "127.0.0.1:9001"}})
		if err != nil {
			t.Fatal(err)
		}
		err = cache.Put("valid2", pb.Node{Address: &pb.NodeAddress{Transport: pb.NodeTransport_TCP_TLS_GRPC, Address: "127.0.0.1:9002"}})
		if err != nil {
			t.Fatal(err)
		}
	}

	{ // Get
		valid2, err := cache.Get(ctx, "valid2")
		if assert.NoError(t, err) {
			assert.Equal(t, valid2.Address.Address, "127.0.0.1:9002")
		}

		invalid2, err := cache.Get(ctx, "invalid2")
		assert.Error(t, err)
		assert.Nil(t, invalid2)

		if storeClient, ok := store.(*teststore.Client); ok {
			storeClient.ForceError++
			_, err := cache.Get(ctx, "valid1")
			assert.Error(t, err)
		}
	}

	{ // GetAll
		nodes, err := cache.GetAll(ctx, []string{"valid2", "valid1", "valid2"})
		if assert.NoError(t, err) {
			assert.Equal(t, nodes[0].Address.Address, "127.0.0.1:9002")
			assert.Equal(t, nodes[1].Address.Address, "127.0.0.1:9001")
			assert.Equal(t, nodes[2].Address.Address, "127.0.0.1:9002")
		}

		nodes, err = cache.GetAll(ctx, []string{"valid1", "invalid"})
		if assert.NoError(t, err) {
			assert.Equal(t, nodes[0].Address.Address, "127.0.0.1:9001")
			assert.Nil(t, nodes[1])
		}

		nodes, err = cache.GetAll(ctx, []string{"", ""})
		if assert.NoError(t, err) {
			assert.Nil(t, nodes[0])
			assert.Nil(t, nodes[1])
		}

		_, err = cache.GetAll(ctx, []string{})
		assert.True(t, overlay.OverlayError.Has(err))

		if storeClient, ok := store.(*teststore.Client); ok {
			storeClient.ForceError++
			_, err := cache.GetAll(ctx, []string{"valid1", "valid2"})
			assert.Error(t, err)
		}
	}
}

func TestCache_Redis(t *testing.T) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	redisAddr, cleanup, err := redisserver.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	store, err := redis.NewClient(redisAddr, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer ctx.Check(store.Close)

	testCache(ctx, t, store)
}

func TestCache_Bolt(t *testing.T) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	client, err := boltdb.New(ctx.File("overlay.db"), "overlay")
	if err != nil {
		t.Fatal(err)
	}
	defer ctx.Check(client.Close)

	testCache(ctx, t, client)
}

func TestCache_Store(t *testing.T) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	testCache(ctx, t, teststore.New())
}

func TestCache_Refresh(t *testing.T) {
	ctx := testcontext.New(t)
	defer ctx.Cleanup()

	planet, err := testplanet.New(t, 1, 30, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer ctx.Check(planet.Shutdown)

	planet.Start(ctx)

	err = planet.Satellites[0].Overlay.Refresh(ctx)
	assert.NoError(t, err)
}
