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

type DBMetricsSummary struct {
	DiskSpaceBytes          uint64 `json:"disk_space_bytes"`
	EstimatedCompactionDebt uint64 `json:"estimated_compaction_debt_bytes"`
	CompactionsInProgress   int64  `json:"compactions_in_progress"`
	TotalFiles              int64  `json:"total_files"`
	L0Files                 int64  `json:"l0_files"`
	L0Sublevels             int32  `json:"l0_sublevels"`
	ReadAmplification       int    `json:"read_amplification"`
	ObsoleteTables          int64  `json:"obsolete_tables"`
	ObsoleteTableBytes      uint64 `json:"obsolete_table_bytes"`
	ZombieTables            int64  `json:"zombie_tables"`
	ZombieTableBytes        uint64 `json:"zombie_table_bytes"`
	OpenSnapshots           int    `json:"open_snapshots"`
	OpenTableIterators      int64  `json:"open_table_iterators"`
	WALFiles                int64  `json:"wal_files"`
	WALBytes                uint64 `json:"wal_bytes"`
}

func openPebbleDB(path string) (*DB, error) {
	opts := &pebble.Options{}
	db, err := pebble.Open(path, opts)
	if err != nil {
		return nil, err
	}
	return &DB{db: db}, nil
}

func (d *DB) MetricsSummary() (DBMetricsSummary, error) {
	if d == nil || d.db == nil {
		return DBMetricsSummary{}, errors.New("db not initialized")
	}
	metrics := d.db.Metrics()
	total := metrics.Total()
	return DBMetricsSummary{
		DiskSpaceBytes:          metrics.DiskSpaceUsage(),
		EstimatedCompactionDebt: metrics.Compact.EstimatedDebt,
		CompactionsInProgress:   metrics.Compact.NumInProgress,
		TotalFiles:              total.NumFiles,
		L0Files:                 metrics.Levels[0].NumFiles,
		L0Sublevels:             metrics.Levels[0].Sublevels,
		ReadAmplification:       metrics.ReadAmp(),
		ObsoleteTables:          metrics.Table.ObsoleteCount,
		ObsoleteTableBytes:      metrics.Table.ObsoleteSize,
		ZombieTables:            metrics.Table.ZombieCount,
		ZombieTableBytes:        metrics.Table.ZombieSize,
		OpenSnapshots:           metrics.Snapshots.Count,
		OpenTableIterators:      metrics.TableIters,
		WALFiles:                metrics.WAL.Files,
		WALBytes:                metrics.WAL.PhysicalSize,
	}, nil
}

func (d *DB) CompactAll(parallel bool) error {
	if d == nil || d.db == nil {
		return errors.New("db not initialized")
	}
	if err := d.db.Flush(); err != nil {
		return err
	}
	// MSC database keys are textual protocol prefixes. This range covers the
	// complete application keyspace while satisfying Pebble's start < end
	// requirement.
	start := []byte{}
	end := bytes.Repeat([]byte{0xff}, 32)
	return d.db.Compact(start, end, parallel)
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
