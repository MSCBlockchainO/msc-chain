// Package storage owns durability and atomic persistence contracts.
package storage

import "errors"

type Reader interface{ Get([]byte) ([]byte, error) }

type Batch interface {
	Set(key, value []byte) error
	Delete(key []byte) error
	Commit(sync bool) error
	Abort() error
}

type Store interface {
	Reader
	NewBatch() (Batch, error)
	Snapshot() (Reader, error)
	Close() error
}

var ErrDiskFull = errors.New("storage: disk full")

type Mutation struct {
	Key    []byte
	Value  []byte
	Delete bool
}

// ApplyAtomic writes a complete transition through one storage batch. Any
// staging or commit failure aborts the batch, so callers cannot publish a
// partial execution state after disk-full or power-failure style errors.
func ApplyAtomic(store Store, mutations []Mutation, sync bool) error {
	if store == nil {
		return errors.New("storage: nil store")
	}
	batch, err := store.NewBatch()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = batch.Abort()
		}
	}()
	for _, mutation := range mutations {
		if mutation.Delete {
			err = batch.Delete(mutation.Key)
		} else {
			err = batch.Set(mutation.Key, mutation.Value)
		}
		if err != nil {
			return err
		}
	}
	if err := batch.Commit(sync); err != nil {
		return err
	}
	committed = true
	return nil
}
