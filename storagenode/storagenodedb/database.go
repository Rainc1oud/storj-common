// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package storagenodedb

import (
	"context"

	"github.com/zeebo/errs"

	"storj.io/storj/pkg/piecestore/psserver/psdb"
	"storj.io/storj/storage"
	"storj.io/storj/storage/teststore"
	"storj.io/storj/storagenode"
)

var _ storagenode.DB = (*DB)(nil)

// DB contains access to different database tables
type DB struct {
	disk     string
	psdb     *psdb.DB
	kdb, ndb storage.KeyValueStore
}

// NewInMemory creates new inmemory database for storagenode
// TODO: still stores data on disk
func NewInMemory(storageDir string) (*DB, error) {
	// TODO: OpenInMemory shouldn't need context argument
	psdb, err := psdb.OpenInMemory(context.TODO(), storageDir)
	if err != nil {
		return nil, err
	}

	return &DB{
		disk: storageDir,
		psdb: psdb,
		kdb:  teststore.New(),
		ndb:  teststore.New(),
	}, nil
}

// Close closes any resources.
func (db *DB) Close() error {
	return errs.Combine(
		db.psdb.Close(),
		db.kdb.Close(),
		db.ndb.Close(),
	)
}

// Disk returns piecestore data folder
func (db *DB) Disk() string {
	return db.disk
}

// PSDB returns piecestore database
func (db *DB) PSDB() *psdb.DB {
	return db.psdb
}

// RoutingTable returns kademlia routing table
func (db *DB) RoutingTable() (kdb, ndb storage.KeyValueStore) {
	return db.kdb, db.ndb
}
