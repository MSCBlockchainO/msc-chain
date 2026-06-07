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
	nodeProcessLockFile = "node.process.lock"
	nodeProcessLockInfo = "node.process.lock.json"
)

type nodeProcessLock struct {
	nodeID string
	path   string
	lock   *flock.Flock
}

type nodeProcessLockMetadata struct {
	NodeID    string `json:"node_id"`
	PID       int    `json:"pid"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
	DataPath  string `json:"data_path"`
	StartedAt string `json:"started_at"`
}

func acquireNodeProcessLock(dataDir, nodeID string) (*nodeProcessLock, error) {
	id := strings.TrimSpace(nodeID)
	if id == "" {
		return nil, fmt.Errorf("node id is required for process locking")
	}

	nodePath := nodeDataPath(dataDir, id)
	if abs, err := filepath.Abs(nodePath); err == nil {
		nodePath = abs
	}
	if err := ensurePrivateDirectory(nodePath); err != nil {
		return nil, fmt.Errorf("prepare node lock directory: %w", err)
	}

	lockPath := filepath.Join(nodePath, nodeProcessLockFile)
	fileLock := flock.New(lockPath)
	locked, err := fileLock.TryLock()
	if err != nil {
		return nil, fmt.Errorf("acquire node process lock %s: %w", lockPath, err)
	}
	if !locked {
		return nil, fmt.Errorf("node %s is already running for data path %s; stop the existing process or use a different --id/--datadir", id, nodePath)
	}

	info := nodeProcessLockMetadata{
		NodeID:    id,
		PID:       os.Getpid(),
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
		DataPath:  nodePath,
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if raw, err := json.MarshalIndent(info, "", "  "); err == nil {
		_ = writePrivateFile(filepath.Join(nodePath, nodeProcessLockInfo), raw)
	}

	return &nodeProcessLock{
		nodeID: id,
		path:   lockPath,
		lock:   fileLock,
	}, nil
}

func (l *nodeProcessLock) Release() {
	if l == nil || l.lock == nil {
		return
	}
	_ = l.lock.Unlock()
}
