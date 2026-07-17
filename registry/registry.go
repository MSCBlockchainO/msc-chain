// Package registry is the independent validator-registry boundary between
// state/execution and consensus.
package registry

import (
	"errors"
	"sort"
	"strings"
)

type Status string

const (
	Pending Status = "pending"
	Active  Status = "active"
	Jailed  Status = "jailed"
	Exited  Status = "exited"
)

type Record struct {
	ID               string
	PublicKey        []byte
	Stake            uint64
	Status           Status
	ActivationHeight uint64
	ExitHeight       uint64
}

type Snapshot interface {
	Height() uint64
	Version() uint64
	Hash() string
	Get(id string) (Record, bool)
	ActiveIDs() []string
}

type Reader interface {
	AtHeight(height uint64) (Snapshot, error)
}

var ErrSnapshotNotFound = errors.New("registry: snapshot not found")

type immutableSnapshot struct {
	height  uint64
	version uint64
	hash    string
	records map[string]Record
}

// NewSnapshot copies all records and returns an immutable registry capability.
func NewSnapshot(height uint64, version uint64, hash string, records map[string]Record) Snapshot {
	copyRecords := make(map[string]Record, len(records))
	for rawID, record := range records {
		id := strings.TrimSpace(rawID)
		if id == "" {
			id = strings.TrimSpace(record.ID)
		}
		if id == "" {
			continue
		}
		record.ID = id
		record.PublicKey = append([]byte(nil), record.PublicKey...)
		copyRecords[id] = record
	}
	return immutableSnapshot{
		height:  height,
		version: version,
		hash:    strings.TrimSpace(hash),
		records: copyRecords,
	}
}

func (s immutableSnapshot) Height() uint64  { return s.height }
func (s immutableSnapshot) Version() uint64 { return s.version }
func (s immutableSnapshot) Hash() string    { return s.hash }
func (s immutableSnapshot) Get(id string) (Record, bool) {
	record, ok := s.records[strings.TrimSpace(id)]
	record.PublicKey = append([]byte(nil), record.PublicKey...)
	return record, ok
}
func (s immutableSnapshot) ActiveIDs() []string {
	ids := make([]string, 0, len(s.records))
	for id, record := range s.records {
		if record.Status == Active {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// StaticReader is an immutable height-indexed registry reader for replay and
// consensus adapters.
type StaticReader struct{ snapshots map[uint64]Snapshot }

func NewStaticReader(snapshots ...Snapshot) StaticReader {
	byHeight := make(map[uint64]Snapshot, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot != nil && snapshot.Height() > 0 {
			byHeight[snapshot.Height()] = snapshot
		}
	}
	return StaticReader{snapshots: byHeight}
}

func (r StaticReader) AtHeight(height uint64) (Snapshot, error) {
	snapshot, ok := r.snapshots[height]
	if !ok {
		return nil, ErrSnapshotNotFound
	}
	return snapshot, nil
}
