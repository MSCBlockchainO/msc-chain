package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNodeProcessLockRejectsDuplicateAndReleases(t *testing.T) {
	root := t.TempDir()
	first, err := acquireNodeProcessLock(root, "LOCKTEST")
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	defer first.Release()

	infoPath := filepath.Join(nodeDataPath(root, "LOCKTEST"), nodeProcessLockInfo)
	raw, err := os.ReadFile(infoPath)
	if err != nil {
		t.Fatalf("read lock metadata: %v", err)
	}
	var info nodeProcessLockMetadata
	if err := json.Unmarshal(raw, &info); err != nil {
		t.Fatalf("decode lock metadata: %v", err)
	}
	if info.NodeID != "LOCKTEST" || info.PID == 0 || info.DataPath == "" {
		t.Fatalf("unexpected lock metadata: %+v", info)
	}

	second, err := acquireNodeProcessLock(root, "LOCKTEST")
	if err == nil {
		second.Release()
		t.Fatal("expected duplicate lock acquisition to fail")
	}

	first.Release()
	third, err := acquireNodeProcessLock(root, "LOCKTEST")
	if err != nil {
		t.Fatalf("reacquire after release: %v", err)
	}
	third.Release()
}

func TestNodeProcessLockRequiresNodeID(t *testing.T) {
	if lock, err := acquireNodeProcessLock(t.TempDir(), " "); err == nil {
		if lock != nil {
			lock.Release()
		}
		t.Fatal("expected empty node id to fail")
	}
}
