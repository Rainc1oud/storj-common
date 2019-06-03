// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package filestore

import (
	"context"
	"os"

	"github.com/zeebo/errs"

	"storj.io/storj/storage"
)

// Error is the default filestore error class
var Error = errs.Class("filestore error")

var _ storage.Blobs = (*Store)(nil)

// Store implements a blob store
type Store struct {
	dir *Dir
}

// New creates a new disk blob store in the specified directory
func New(dir *Dir) *Store {
	return &Store{dir}
}

// NewAt creates a new disk blob store in the specified directory
func NewAt(path string) (*Store, error) {
	dir, err := NewDir(path)
	if err != nil {
		return nil, Error.Wrap(err)
	}
	return &Store{dir}, nil
}

// Close closes the store.
func (store *Store) Close() error { return nil }

// Open loads blob with the specified hash
func (store *Store) Open(ctx context.Context, ref storage.BlobRef) (storage.BlobReader, error) {
	file, err := store.dir.Open(ref)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, Error.Wrap(err)
	}
	return newBlobReader(file), nil
}

// Delete deletes blobs with the specified ref
func (store *Store) Delete(ctx context.Context, ref storage.BlobRef) error {
	err := store.dir.Delete(ref)
	return Error.Wrap(err)
}

// GarbageCollect tries to delete any files that haven't yet been deleted
func (store *Store) GarbageCollect(ctx context.Context) error {
	err := store.dir.GarbageCollect()
	return Error.Wrap(err)
}

// Create creates a new blob that can be written
// optionally takes a size argument for performance improvements, -1 is unknown size
func (store *Store) Create(ctx context.Context, ref storage.BlobRef, size int64) (storage.BlobWriter, error) {
	file, err := store.dir.CreateTemporaryFile(size)
	if err != nil {
		return nil, Error.Wrap(err)
	}
	return newBlobWriter(ref, store, file), nil
}

// FreeSpace returns how much space left in underlying directory
func (store *Store) FreeSpace() (int64, error) {
	info, err := store.dir.Info()
	if err != nil {
		return 0, err
	}
	return info.AvailableSpace, nil
}
