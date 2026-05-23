package main

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"
)

var ErrKeyNotFound = pebble.ErrNotFound

type DB struct {
	db *pebble.DB
}

func openPebbleDB(path string) (*DB, error) {
	opts := &pebble.Options{}
	db, err := pebble.Open(path, opts)
	if err != nil {
		return nil, err
	}
	return &DB{db: db}, nil
}

func (d *DB) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

func (d *DB) View(fn func(txn *Txn) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("db view failed: %v", r)
		}
	}()
	if d == nil || d.db == nil {
		return errors.New("db not initialized")
	}
	snap := d.db.NewSnapshot()
	defer snap.Close()
	txn := &Txn{
		db:       d.db,
		snap:     snap,
		readOnly: true,
	}
	return fn(txn)
}

func (d *DB) Update(fn func(txn *Txn) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("db update failed: %v", r)
		}
	}()
	if d == nil || d.db == nil {
		return errors.New("db not initialized")
	}
	snap := d.db.NewSnapshot()
	defer snap.Close()
	batch := d.db.NewBatch()
	txn := &Txn{
		db:    d.db,
		snap:  snap,
		batch: batch,
	}
	if err := fn(txn); err != nil {
		_ = batch.Close()
		return err
	}
	if err := batch.Commit(pebble.Sync); err != nil {
		_ = batch.Close()
		return err
	}
	return batch.Close()
}

type Txn struct {
	db       *pebble.DB
	snap     *pebble.Snapshot
	batch    *pebble.Batch
	readOnly bool
}

func (t *Txn) Get(key []byte) (*Item, error) {
	if t == nil || t.snap == nil {
		return nil, errors.New("transaction not initialized")
	}
	val, closer, err := t.snap.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrKeyNotFound
		}
		return nil, err
	}
	defer closer.Close()
	out := append([]byte{}, val...)
	return &Item{
		key: append([]byte{}, key...),
		val: out,
	}, nil
}

func (t *Txn) Set(key, val []byte) error {
	if t == nil || t.batch == nil {
		return errors.New("read-only transaction")
	}
	return t.batch.Set(key, val, nil)
}

func (t *Txn) Delete(key []byte) error {
	if t == nil || t.batch == nil {
		return errors.New("read-only transaction")
	}
	return t.batch.Delete(key, nil)
}

func (t *Txn) NewIterator(opts IteratorOptions) *Iterator {
	if t == nil || t.snap == nil {
		return &Iterator{}
	}
	iterOpts := &pebble.IterOptions{}
	if len(opts.Prefix) > 0 {
		iterOpts.LowerBound = append([]byte{}, opts.Prefix...)
		iterOpts.UpperBound = prefixUpperBound(opts.Prefix)
	}
	iter, err := t.snap.NewIter(iterOpts)
	if err != nil {
		return &Iterator{}
	}
	return &Iterator{
		iter:   iter,
		prefix: append([]byte{}, opts.Prefix...),
	}
}

type IteratorOptions struct {
	Prefix         []byte
	PrefetchValues bool
}

var DefaultIteratorOptions = IteratorOptions{}

type Iterator struct {
	iter   *pebble.Iterator
	prefix []byte
}

func (it *Iterator) Close() error {
	if it == nil || it.iter == nil {
		return nil
	}
	return it.iter.Close()
}

func (it *Iterator) Rewind() {
	if it == nil || it.iter == nil {
		return
	}
	it.iter.First()
}

func (it *Iterator) Seek(prefix []byte) {
	if it == nil || it.iter == nil {
		return
	}
	it.iter.SeekGE(prefix)
}

func (it *Iterator) Valid() bool {
	if it == nil || it.iter == nil {
		return false
	}
	return it.iter.Valid()
}

func (it *Iterator) ValidForPrefix(prefix []byte) bool {
	if it == nil || it.iter == nil {
		return false
	}
	if !it.iter.Valid() {
		return false
	}
	return bytes.HasPrefix(it.iter.Key(), prefix)
}

func (it *Iterator) Next() {
	if it == nil || it.iter == nil {
		return
	}
	it.iter.Next()
}

func (it *Iterator) Item() *Item {
	if it == nil || it.iter == nil || !it.iter.Valid() {
		return nil
	}
	key := append([]byte{}, it.iter.Key()...)
	val := append([]byte{}, it.iter.Value()...)
	return &Item{
		key: key,
		val: val,
	}
}

type Item struct {
	key []byte
	val []byte
}

func (i *Item) Key() []byte {
	if i == nil {
		return nil
	}
	return i.key
}

func (i *Item) Value(fn func(val []byte) error) error {
	if i == nil {
		return ErrKeyNotFound
	}
	return fn(append([]byte{}, i.val...))
}

func prefixUpperBound(prefix []byte) []byte {
	if len(prefix) == 0 {
		return nil
	}
	out := append([]byte{}, prefix...)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i] != 0xFF {
			out[i]++
			return out[:i+1]
		}
	}
	return nil
}
