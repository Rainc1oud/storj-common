// Copyright (C) 2018 Storj Labs, Inc.
// See LICENSE for copying information.

package uplink

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"

	"storj.io/storj/internal/testcontext"
	"storj.io/storj/internal/testplanet"
	"storj.io/storj/pkg/cfgstruct"
	"storj.io/storj/pkg/storj"
	"storj.io/storj/satellite"
	ul "storj.io/storj/uplink"
)

func TestUplink(t *testing.T) {
	testplanet.Run(t, testplanet.Config{
		SatelliteCount: 1, StorageNodeCount: 6, UplinkCount: 1,
	}, func(t *testing.T, ctx *testcontext.Context, planet *testplanet.Planet) {
		satellite := planet.Satellites[0]

		satelliteAddr := satellite.Addr() // get address
		cfg := getConfig(satellite, planet)

		uplink := NewUplink(planet.Uplinks[0].Identity, satelliteAddr, cfg)

		permissions := Permissions{}
		access := uplink.Access(ctx, permissions)

		opts := CreateBucketOptions{}
		bucket, err := access.CreateBucket(ctx, "testbucket", opts)
		assert.NoError(t, err)
		assert.NotNil(t, bucket)

		bucketListOptions := storj.BucketListOptions{
			Limit:     1000,
			Direction: storj.ListDirection(1),
		}
		buckets, err := access.ListBuckets(ctx, bucketListOptions)
		assert.NoError(t, err)
		assert.NotNil(t, buckets)

		storjBucket, err := access.GetBucketInfo(ctx, "testbucket")
		assert.NoError(t, err)
		assert.NotNil(t, storjBucket)
		assert.Equal(t, storjBucket.Name, "testbucket")

		encOpts := &Encryption{}
		getbucket := access.GetBucket(ctx, "testbucket", encOpts)
		assert.NoError(t, err)
		assert.NotNil(t, getbucket)

		err = access.DeleteBucket(ctx, "testbucket")
		assert.NoError(t, err)

		uploadtest, err := access.CreateBucket(ctx, "uploadtest", opts)
		assert.NoError(t, err)
		assert.NotNil(t, uploadtest)
		assert.Equal(t, uploadtest.Name, "uploadtest")

		uploadBucket := access.GetBucket(ctx, "uploadtest", encOpts)
		assert.NotNil(t, uploadBucket)

		list, err := uploadBucket.List(ctx, ListObjectsConfig{
			Direction: storj.ListDirection(1),
			Limit:     100,
		})

		assert.NoError(t, err)
		assert.NotNil(t, list)
		assert.Equal(t, len(list.Items), 0)

		testdata := []byte{1, 1, 1, 1, 1}
		uploadOpts := UploadOpts{}

		err = uploadBucket.Upload(ctx, "testpath", testdata, uploadOpts)
		assert.NoError(t, err)

		downloadedData, err := uploadBucket.Download(ctx, "testpath")
		assert.NotNil(t, downloadedData)
		assert.NoError(t, err)
		assert.Equal(t, testdata, downloadedData)

		list2, err := uploadBucket.List(ctx, ListObjectsConfig{
			Direction: storj.ListDirection(1),
			Limit:     100,
		})

		assert.NotNil(t, list2)
		assert.NoError(t, err)
		assert.NotNil(t, list2.Items)
		assert.Equal(t, len(list2.Items), 1)
	})
}

func getConfig(satellite *satellite.Peer, planet *testplanet.Planet) ul.Config {
	config := getDefaultConfig()
	config.Client.SatelliteAddr = satellite.Addr()
	config.Client.APIKey = planet.Uplinks[0].APIKey[satellite.ID()]

	config.RS.MinThreshold = 1 * len(planet.StorageNodes) / 5
	config.RS.RepairThreshold = 2 * len(planet.StorageNodes) / 5
	config.RS.SuccessThreshold = 3 * len(planet.StorageNodes) / 5
	config.RS.MaxThreshold = 4 * len(planet.StorageNodes) / 5

	config.TLS.UsePeerCAWhitelist = false
	config.TLS.Extensions.Revocation = false
	config.TLS.Extensions.WhitelistSignedLeaf = false

	return config
}

func getDefaultConfig() ul.Config {
	cfg := ul.Config{}
	cfgstruct.Bind(&pflag.FlagSet{}, &cfg, true)
	return cfg
}
