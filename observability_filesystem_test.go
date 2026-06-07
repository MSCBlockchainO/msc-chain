package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeObservabilityTestFile(t *testing.T, path string, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create observability test dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatalf("write observability test file: %v", err)
	}
}

func TestFilesystemObservabilityUsesBackgroundCachedScans(t *testing.T) {
	n := &Node{ID: "OBS", DataDir: t.TempDir()}
	base := nodeDataPath(n.DataDir, n.ID)
	writeObservabilityTestFile(t, filepath.Join(base, finalityCertificatesDir, "1.json"), "certificate")
	writeObservabilityTestFile(t, filepath.Join(base, finalityEpochAnchorsDir, "1.json"), "anchor")
	writeObservabilityTestFile(t, filepath.Join(base, finalityValidatorCommitmentsDir, "1.json"), "commitment")
	writeObservabilityTestFile(t, filepath.Join(base, finalityIrreversibleRootsDir, "1.json"), "root")
	writeObservabilityTestFile(t, filepath.Join(base, "state_checkpoints", "1.json"), "checkpoint")
	writeObservabilityTestFile(t, filepath.Join(base, "state_checkpoints", "2.json"), "checkpoint")
	writeObservabilityTestFile(t, filepath.Join(base, "cold-storage", "cold.bin"), "cold")

	start := time.Now()
	firstFinality := n.finalityArtifactObservability()
	firstStorage, firstCold := n.storageDirectorySizeSnapshot()
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("filesystem observability blocked initial scrape: %s", elapsed)
	}
	if firstFinality != (finalityArtifactObservability{}) || firstStorage != 0 || firstCold != 0 {
		t.Fatalf("expected empty initial cache, finality=%+v storage=%d cold=%d", firstFinality, firstStorage, firstCold)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		n.observabilityMu.RLock()
		ready := n.observability.FinalityArtifactsScannedAt > 0 &&
			n.observability.StorageSizeScannedAtUnix > 0 &&
			!n.observability.FinalityScanInProgress &&
			!n.observability.StorageSizeScanInProgress
		n.observabilityMu.RUnlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background filesystem observability scan did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}

	finality := n.finalityArtifactObservability()
	if finality.Certificates != 1 || finality.Anchors != 1 || finality.ValidatorCommits != 1 ||
		finality.IrreversibleRoots != 1 || finality.StateCheckpoints != 2 {
		t.Fatalf("unexpected cached finality artifact counts: %+v", finality)
	}
	storage, cold := n.storageDirectorySizeSnapshot()
	if storage == 0 || cold == 0 || storage < cold {
		t.Fatalf("unexpected cached storage sizes: storage=%d cold=%d", storage, cold)
	}
}

func TestFilesystemObservabilityReturnsCachedValuesWhileScanRuns(t *testing.T) {
	n := &Node{}
	n.observability = observabilityStats{
		StorageSizeBytes:          123,
		ColdStorageSizeBytes:      45,
		StorageSizeScanInProgress: true,
		FinalityCertificates:      7,
		FinalityAnchors:           6,
		FinalityValidatorCommits:  5,
		FinalityIrreversibleRoots: 4,
		FinalityStateCheckpoints:  3,
		FinalityScanInProgress:    true,
	}

	start := time.Now()
	finality := n.finalityArtifactObservability()
	storage, cold := n.storageDirectorySizeSnapshot()
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("cached filesystem observability blocked: %s", elapsed)
	}
	if finality.Certificates != 7 || finality.Anchors != 6 || finality.ValidatorCommits != 5 ||
		finality.IrreversibleRoots != 4 || finality.StateCheckpoints != 3 {
		t.Fatalf("unexpected cached finality values: %+v", finality)
	}
	if storage != 123 || cold != 45 {
		t.Fatalf("unexpected cached storage values: storage=%d cold=%d", storage, cold)
	}
}
