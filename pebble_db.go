package main

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"
)

// `ErrKeyNotFound` stores the error produced by this operation.
var ErrKeyNotFound = pebble.ErrNotFound

type DB struct {
	// `db` stores the value associated with this record.
	db *pebble.DB
}

type DBMetricsSummary struct {
	// `DiskSpaceBytes` stores the value associated with this record.
	DiskSpaceBytes          uint64 `json:"disk_space_bytes"`
	// `EstimatedCompactionDebt` stores the value associated with this record.
	EstimatedCompactionDebt uint64 `json:"estimated_compaction_debt_bytes"`
	// `CompactionsInProgress` stores the value associated with this record.
	CompactionsInProgress   int64  `json:"compactions_in_progress"`
	// `TotalFiles` stores the measured quantity used by this operation.
	TotalFiles              int64  `json:"total_files"`
	// `L0Files` stores the value associated with this record.
	L0Files                 int64  `json:"l0_files"`
	// `L0Sublevels` stores the value associated with this record.
	L0Sublevels             int32  `json:"l0_sublevels"`
	// `ReadAmplification` stores the value associated with this record.
	ReadAmplification       int    `json:"read_amplification"`
	// `ObsoleteTables` stores the value associated with this record.
	ObsoleteTables          int64  `json:"obsolete_tables"`
	// `ObsoleteTableBytes` stores the value associated with this record.
	ObsoleteTableBytes      uint64 `json:"obsolete_table_bytes"`
	// `ZombieTables` stores the value associated with this record.
	ZombieTables            int64  `json:"zombie_tables"`
	// `ZombieTableBytes` stores the value associated with this record.
	ZombieTableBytes        uint64 `json:"zombie_table_bytes"`
	// `OpenSnapshots` stores the value associated with this record.
	OpenSnapshots           int    `json:"open_snapshots"`
	// `OpenTableIterators` stores the value associated with this record.
	OpenTableIterators      int64  `json:"open_table_iterators"`
	// `WALFiles` stores the value associated with this record.
	WALFiles                int64  `json:"wal_files"`
	// `WALBytes` stores the value associated with this record.
	WALBytes                uint64 `json:"wal_bytes"`
}

// openPebbleDB implements the open pebble db helper.
func openPebbleDB(path string) (*DB, error) {
	// `opts` stores the value produced by this operation.
	opts := &pebble.Options{}
	// `db` and `err` store the error produced by this operation.
	db, err := pebble.Open(path, opts)
	if err != nil {
		return nil, err
	}
	return &DB{db: db}, nil
}

// MetricsSummary implements the metrics summary helper.
func (d *DB) MetricsSummary() (DBMetricsSummary, error) {
	if d == nil || d.db == nil {
		return DBMetricsSummary{}, errors.New("db not initialized")
	}
	// `metrics` stores the value produced by this operation.
	metrics := d.db.Metrics()
	// `total` stores the measured quantity used by this operation.
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

// CompactAll implements the compact all helper.
func (d *DB) CompactAll(parallel bool) error {
	if d == nil || d.db == nil {
		return errors.New("db not initialized")
	}
	// `err` stores the error produced by this operation.
	if err := d.db.Flush(); err != nil {
		return err
	}
	// MSC database keys are textual protocol prefixes. This range covers the
	// complete application keyspace while satisfying Pebble's start < end
	// requirement.
	start := []byte{}
	// `end` stores the value produced by this operation.
	end := bytes.Repeat([]byte{0xff}, 32)
	return d.db.Compact(start, end, parallel)
}

// Close implements the close helper.
func (d *DB) Close() error {
	if d == nil || d.db == nil {
		return nil
	}
	return d.db.Close()
}

// View implements the view helper.
func (d *DB) View(fn func(txn *Txn) error) (err error) {
	defer func() {
		// `r` stores the value produced by this operation.
		if r := recover(); r != nil {
			err = fmt.Errorf("db view failed: %v", r)
		}
	}()
	if d == nil || d.db == nil {
		return errors.New("db not initialized")
	}
	// `snap` stores the value produced by this operation.
	snap := d.db.NewSnapshot()
	defer snap.Close()
	// `txn` stores the transaction data handled by this operation.
	txn := &Txn{
		db:       d.db,
		snap:     snap,
		readOnly: true,
	}
	return fn(txn)
}

// Update implements the update helper.
func (d *DB) Update(fn func(txn *Txn) error) (err error) {
	defer func() {
		// `r` stores the value produced by this operation.
		if r := recover(); r != nil {
			err = fmt.Errorf("db update failed: %v", r)
		}
	}()
	if d == nil || d.db == nil {
		return errors.New("db not initialized")
	}
	// `snap` stores the value produced by this operation.
	snap := d.db.NewSnapshot()
	defer snap.Close()
	// `batch` stores the value produced by this operation.
	batch := d.db.NewBatch()
	// `txn` stores the transaction data handled by this operation.
	txn := &Txn{
		db:    d.db,
		snap:  snap,
		batch: batch,
	}
	// `err` stores the error produced by this operation.
	if err := fn(txn); err != nil {
		_ = batch.Close()
		return err
	}
	// `err` stores the error produced by this operation.
	if err := batch.Commit(pebble.Sync); err != nil {
		_ = batch.Close()
		return err
	}
	return batch.Close()
}

type Txn struct {
	// `db` stores the value associated with this record.
	db       *pebble.DB
	// `snap` stores the value associated with this record.
	snap     *pebble.Snapshot
	// `batch` stores the value associated with this record.
	batch    *pebble.Batch
	// `readOnly` stores the value associated with this record.
	readOnly bool
}

// Get implements the get helper.
func (t *Txn) Get(key []byte) (*Item, error) {
	if t == nil || t.snap == nil {
		return nil, errors.New("transaction not initialized")
	}
	// `val`, `closer`, and `err` store the error produced by this operation.
	val, closer, err := t.snap.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, ErrKeyNotFound
		}
		return nil, err
	}
	defer closer.Close()
	// `out` stores the result produced by this operation.
	out := append([]byte{}, val...)
	return &Item{
		key: append([]byte{}, key...),
		val: out,
	}, nil
}

// Set implements the set helper.
func (t *Txn) Set(key, val []byte) error {
	if t == nil || t.batch == nil {
		return errors.New("read-only transaction")
	}
	return t.batch.Set(key, val, nil)
}

// Delete implements the delete helper.
func (t *Txn) Delete(key []byte) error {
	if t == nil || t.batch == nil {
		return errors.New("read-only transaction")
	}
	return t.batch.Delete(key, nil)
}

// NewIterator creates a new iterator.
func (t *Txn) NewIterator(opts IteratorOptions) *Iterator {
	if t == nil || t.snap == nil {
		return &Iterator{}
	}
	// `iterOpts` stores the current position in the related collection.
	iterOpts := &pebble.IterOptions{}
	if len(opts.Prefix) > 0 {
		iterOpts.LowerBound = append([]byte{}, opts.Prefix...)
		iterOpts.UpperBound = prefixUpperBound(opts.Prefix)
	}
	// `iter` and `err` store the error produced by this operation.
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
	// `Prefix` stores the value associated with this record.
	Prefix         []byte
	// `PrefetchValues` stores the value associated with this record.
	PrefetchValues bool
}

// `DefaultIteratorOptions` stores the value used by this operation.
var DefaultIteratorOptions = IteratorOptions{}

type Iterator struct {
	// `iter` stores the current position in the related collection.
	iter   *pebble.Iterator
	// `prefix` stores the value associated with this record.
	prefix []byte
}

// Close implements the close helper.
func (it *Iterator) Close() error {
	if it == nil || it.iter == nil {
		return nil
	}
	return it.iter.Close()
}

// Rewind implements the rewind helper.
func (it *Iterator) Rewind() {
	if it == nil || it.iter == nil {
		return
	}
	it.iter.First()
}

// Seek implements the seek helper.
func (it *Iterator) Seek(prefix []byte) {
	if it == nil || it.iter == nil {
		return
	}
	it.iter.SeekGE(prefix)
}

// Valid implements the valid helper.
func (it *Iterator) Valid() bool {
	if it == nil || it.iter == nil {
		return false
	}
	return it.iter.Valid()
}

// ValidForPrefix implements the valid for prefix helper.
func (it *Iterator) ValidForPrefix(prefix []byte) bool {
	if it == nil || it.iter == nil {
		return false
	}
	if !it.iter.Valid() {
		return false
	}
	return bytes.HasPrefix(it.iter.Key(), prefix)
}

// Next implements the next helper.
func (it *Iterator) Next() {
	if it == nil || it.iter == nil {
		return
	}
	it.iter.Next()
}

// Item implements the item helper.
func (it *Iterator) Item() *Item {
	if it == nil || it.iter == nil || !it.iter.Valid() {
		return nil
	}
	// `key` stores the key used to access the related value.
	key := append([]byte{}, it.iter.Key()...)
	// `val` stores the value currently being processed.
	val := append([]byte{}, it.iter.Value()...)
	return &Item{
		key: key,
		val: val,
	}
}

type Item struct {
	// `key` stores the key used to access the related value.
	key []byte
	// `val` stores the value currently being processed.
	val []byte
}

// Key implements the key helper.
func (i *Item) Key() []byte {
	if i == nil {
		return nil
	}
	return i.key
}

// Value implements the value helper.
func (i *Item) Value(fn func(val []byte) error) error {
	if i == nil {
		return ErrKeyNotFound
	}
	return fn(append([]byte{}, i.val...))
}

// prefixUpperBound implements the prefix upper bound helper.
func prefixUpperBound(prefix []byte) []byte {
	if len(prefix) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := append([]byte{}, prefix...)
	// `i` stores the current position in the related collection.
	for i := len(out) - 1; i >= 0; i-- {
		if out[i] != 0xFF {
			out[i]++
			return out[:i+1]
		}
	}
	return nil
}
