package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

const (
	// `nodeProcessLockFile` defines the constant value used by this package.
	nodeProcessLockFile = "node.process.lock"
	// `nodeProcessLockInfo` defines the constant value used by this package.
	nodeProcessLockInfo = "node.process.lock.json"
)

type nodeProcessLock struct {
	// `nodeID` stores the value associated with this record.
	nodeID string
	// `path` stores the value associated with this record.
	path   string
	// `lock` stores the synchronization state protecting shared data.
	lock   *flock.Flock
}

type nodeProcessLockMetadata struct {
	// `NodeID` stores the value associated with this record.
	NodeID    string `json:"node_id"`
	// `PID` stores the value associated with this record.
	PID       int    `json:"pid"`
	// `GOOS` stores the value associated with this record.
	GOOS      string `json:"goos"`
	// `GOARCH` stores the value associated with this record.
	GOARCH    string `json:"goarch"`
	// `DataPath` stores the value associated with this record.
	DataPath  string `json:"data_path"`
	// `StartedAt` stores the value associated with this record.
	StartedAt string `json:"started_at"`
}

// acquireNodeProcessLock implements the acquire node process lock helper.
func acquireNodeProcessLock(dataDir, nodeID string) (*nodeProcessLock, error) {
	// `id` stores the current position in the related collection.
	id := strings.TrimSpace(nodeID)
	if id == "" {
		return nil, fmt.Errorf("node id is required for process locking")
	}

	// `nodePath` stores the value produced by this operation.
	nodePath := nodeDataPath(dataDir, id)
	// `abs` and `err` store the error produced by this operation.
	if abs, err := filepath.Abs(nodePath); err == nil {
		nodePath = abs
	}
	// `err` stores the error produced by this operation.
	if err := ensurePrivateDirectory(nodePath); err != nil {
		return nil, fmt.Errorf("prepare node lock directory: %w", err)
	}

	// `lockPath` stores the synchronization state protecting shared data.
	lockPath := filepath.Join(nodePath, nodeProcessLockFile)
	// `fileLock` stores the synchronization state protecting shared data.
	fileLock := flock.New(lockPath)
	// `locked` and `err` store the error produced by this operation.
	locked, err := fileLock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("acquire node process lock %s: %w", lockPath, err)
	}
	if !locked {
		return nil, fmt.Errorf("node %s is already running for data path %s; stop the existing process or use a different --id/--datadir", id, nodePath)
	}

	// `info` stores the current position in the related collection.
	info := nodeProcessLockMetadata{
		NodeID:    id,
		PID:       os.Getpid(),
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
		DataPath:  nodePath,
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	// `raw` and `err` store the error produced by this operation.
	if raw, err := json.MarshalIndent(info, "", "  "); err == nil {
		_ = writePrivateFile(filepath.Join(nodePath, nodeProcessLockInfo), raw)
	}

	return &nodeProcessLock{
		nodeID: id,
		path:   lockPath,
		lock:   fileLock,
	}, nil
}

// Release implements the release helper.
func (l *nodeProcessLock) Release() {
	if l == nil || l.lock == nil {
		return
	}
	_ = l.lock.Unlock()
}
