// Package state defines the only state capabilities available to deterministic
// execution code. Implementations may be backed by an overlay, a database
// transaction, or an immutable snapshot.
package state

import (
	"bytes"
	"errors"
	"sort"
	"sync"
)

var ErrNotFound = errors.New("state: key not found")

// StateReader is a read-only deterministic state capability.
type StateReader interface {
	Get(key []byte) ([]byte, error)
}

// StateWriter is the mutation capability granted to execution. Persistence and
// commit authority intentionally live outside this interface.
type StateWriter interface {
	Set(key, value []byte) error
	Delete(key []byte) error
}

// Reader and Writer retain concise compatibility names for adapters.
type Reader = StateReader
type Writer = StateWriter

type ReadWriter interface {
	Reader
	Writer
}

// Overlay buffers a transition atomically. Failed execution can discard it;
// successful execution exports a deterministically ordered write set.
type Overlay struct {
	parent  Reader
	writes  map[string][]byte
	deletes map[string]struct{}
}

func NewOverlay(parent Reader) *Overlay {
	return &Overlay{parent: parent, writes: map[string][]byte{}, deletes: map[string]struct{}{}}
}

func (o *Overlay) Get(key []byte) ([]byte, error) {
	if o == nil {
		return nil, ErrNotFound
	}
	k := string(key)
	if _, deleted := o.deletes[k]; deleted {
		return nil, ErrNotFound
	}
	if value, ok := o.writes[k]; ok {
		return bytes.Clone(value), nil
	}
	if o.parent == nil {
		return nil, ErrNotFound
	}
	value, err := o.parent.Get(key)
	return bytes.Clone(value), err
}

func (o *Overlay) Set(key, value []byte) error {
	if o == nil {
		return errors.New("state: nil overlay")
	}
	k := string(bytes.Clone(key))
	o.writes[k] = bytes.Clone(value)
	delete(o.deletes, k)
	return nil
}

func (o *Overlay) Delete(key []byte) error {
	if o == nil {
		return errors.New("state: nil overlay")
	}
	k := string(bytes.Clone(key))
	delete(o.writes, k)
	o.deletes[k] = struct{}{}
	return nil
}

type Mutation struct {
	Key    []byte
	Value  []byte
	Delete bool
}

// Mutations returns a stable byte-lexicographic write set.
func (o *Overlay) Mutations() []Mutation {
	if o == nil {
		return nil
	}
	keys := make([]string, 0, len(o.writes)+len(o.deletes))
	for key := range o.writes {
		keys = append(keys, key)
	}
	for key := range o.deletes {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare([]byte(keys[i]), []byte(keys[j])) < 0 })
	out := make([]Mutation, 0, len(keys))
	for _, key := range keys {
		value, ok := o.writes[key]
		out = append(out, Mutation{Key: []byte(key), Value: bytes.Clone(value), Delete: !ok})
	}
	return out
}

// Memory is a concurrency-safe capability implementation for replay and tests.
type Memory struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func NewMemory() *Memory { return &Memory{data: map[string][]byte{}} }

func (m *Memory) Get(key []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.data[string(key)]
	if !ok {
		return nil, ErrNotFound
	}
	return bytes.Clone(value), nil
}

func (m *Memory) Set(key, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[string(bytes.Clone(key))] = bytes.Clone(value)
	return nil
}

func (m *Memory) Delete(key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, string(key))
	return nil
}
