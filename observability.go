package main

import (
	"os"
	"path/filepath"
	"time"
)

type observabilityStats struct {
	SnapshotCreateTotal      uint64
	SnapshotCreateFailures   uint64
	SnapshotCreateHeight     uint64
	SnapshotCreateDurationMs uint64

	SnapshotLoadTotal      uint64
	SnapshotLoadFailures   uint64
	SnapshotLoadHeight     uint64
	SnapshotLoadDurationMs uint64

	SnapshotApplyTotal      uint64
	SnapshotApplyFailures   uint64
	SnapshotApplyHeight     uint64
	SnapshotApplyDurationMs uint64

	ReplayTotal       uint64
	ReplayFailures    uint64
	ReplayHeight      uint64
	ReplayBlocks      uint64
	ReplayBlocksTotal uint64
	ReplayDurationMs  uint64

	StorageGCCyclesTotal        uint64
	StorageGCFailuresTotal      uint64
	StorageGCDurationMs         uint64
	StoragePrunedSnapshotsTotal uint64
	StoragePrunedStatesTotal    uint64
	StorageSizeBytes            uint64
	ColdStorageSizeBytes        uint64
	StorageSizeScannedAtUnix    int64
	ColdExportsTotal            uint64

	SyncModeSwitchTotal              uint64
	PeerDisconnectTotal              uint64
	PeerDisconnectFirstUnix          int64
	PeerDisconnectLastUnix           int64
	PeerDiversityRejectTotal         uint64
	PeerDiversityOutboundRejectTotal uint64
	PeerResourceDropTotal            uint64
	PeerConnectionFloodTotal         uint64
	PeerConnectedMax                 uint64
	PeerConnectedLast                uint64

	BlockGossipReceivedTotal uint64
	BlockPropagationLastMs   uint64
	BlockPropagationMaxMs    uint64
	BlockPropagationHeight   uint64

	RPCRequestsTotal           uint64
	RPCRateLimitedTotal        uint64
	RPCBodyRejectedTotal       uint64
	RPCConcurrentRejectedTotal uint64
	RPCUnauthorizedTotal       uint64
	RPCInflight                int64
}

func durationMillisForMetrics(elapsed time.Duration) uint64 {
	if elapsed <= 0 {
		return 0
	}
	ms := uint64(elapsed / time.Millisecond)
	if ms == 0 {
		return 1
	}
	return ms
}

func (n *Node) observeSnapshotOperation(kind string, height uint64, elapsed time.Duration, success bool) {
	if n == nil {
		return
	}
	ms := durationMillisForMetrics(elapsed)
	n.observabilityMu.Lock()
	defer n.observabilityMu.Unlock()
	switch kind {
	case "create":
		n.observability.SnapshotCreateTotal++
		if !success {
			n.observability.SnapshotCreateFailures++
		}
		n.observability.SnapshotCreateHeight = height
		n.observability.SnapshotCreateDurationMs = ms
	case "load":
		n.observability.SnapshotLoadTotal++
		if !success {
			n.observability.SnapshotLoadFailures++
		}
		n.observability.SnapshotLoadHeight = height
		n.observability.SnapshotLoadDurationMs = ms
	case "apply":
		n.observability.SnapshotApplyTotal++
		if !success {
			n.observability.SnapshotApplyFailures++
		}
		n.observability.SnapshotApplyHeight = height
		n.observability.SnapshotApplyDurationMs = ms
	}
}

func (n *Node) observeReplayOperation(height uint64, blocks uint64, elapsed time.Duration, success bool) {
	if n == nil {
		return
	}
	n.observabilityMu.Lock()
	defer n.observabilityMu.Unlock()
	n.observability.ReplayTotal++
	if !success {
		n.observability.ReplayFailures++
	}
	n.observability.ReplayHeight = height
	n.observability.ReplayBlocks = blocks
	n.observability.ReplayBlocksTotal += blocks
	n.observability.ReplayDurationMs = durationMillisForMetrics(elapsed)
}

func (n *Node) observeStorageManagerRun(report StorageManagerReport, elapsed time.Duration, success bool) {
	if n == nil {
		return
	}
	n.observabilityMu.Lock()
	defer n.observabilityMu.Unlock()
	n.observability.StorageGCCyclesTotal++
	if !success {
		n.observability.StorageGCFailuresTotal++
	}
	n.observability.StorageGCDurationMs = durationMillisForMetrics(elapsed)
	if report.SnapshotsPruned > 0 {
		n.observability.StoragePrunedSnapshotsTotal += uint64(report.SnapshotsPruned)
	}
	if report.RetainFromHeight > 1 {
		if prunedThrough := report.RetainFromHeight - 1; prunedThrough > n.observability.StoragePrunedStatesTotal {
			n.observability.StoragePrunedStatesTotal = prunedThrough
		}
	}
	if report.ColdStorageExported > 0 {
		n.observability.ColdExportsTotal += uint64(report.ColdStorageExported)
	}
}

func (n *Node) observeSyncModeSwitch(oldMode string, newMode string) {
	if n == nil || oldMode == "" || oldMode == newMode {
		return
	}
	n.observabilityMu.Lock()
	n.observability.SyncModeSwitchTotal++
	n.observabilityMu.Unlock()
}

func (n *Node) observePeerDisconnect(reason string) {
	if n == nil {
		return
	}
	_ = reason
	now := time.Now().Unix()
	n.observabilityMu.Lock()
	if n.observability.PeerDisconnectFirstUnix == 0 {
		n.observability.PeerDisconnectFirstUnix = now
	}
	n.observability.PeerDisconnectLastUnix = now
	n.observability.PeerDisconnectTotal++
	n.observabilityMu.Unlock()
}

func (n *Node) observePeerDiversityReject(outbound bool) {
	if n == nil {
		return
	}
	n.observabilityMu.Lock()
	n.observability.PeerDiversityRejectTotal++
	if outbound {
		n.observability.PeerDiversityOutboundRejectTotal++
	}
	n.observabilityMu.Unlock()
}

func (n *Node) observePeerResourceDrop(reason string) {
	if n == nil {
		return
	}
	_ = reason
	n.observabilityMu.Lock()
	n.observability.PeerResourceDropTotal++
	n.observabilityMu.Unlock()
}

func (n *Node) observePeerConnectionFlood(reason string) {
	if n == nil {
		return
	}
	_ = reason
	n.observabilityMu.Lock()
	n.observability.PeerConnectionFloodTotal++
	n.observabilityMu.Unlock()
}

func (n *Node) observePeerConnectivityGauge(peers int) {
	if n == nil || peers < 0 {
		return
	}
	n.observabilityMu.Lock()
	value := uint64(peers)
	n.observability.PeerConnectedLast = value
	if value > n.observability.PeerConnectedMax {
		n.observability.PeerConnectedMax = value
	}
	n.observabilityMu.Unlock()
}

func (n *Node) observeBlockPropagation(block Block, receivedAt time.Time) {
	if n == nil || block.ID == 0 {
		return
	}
	elapsed := time.Since(receivedAt)
	if receivedAt.IsZero() || elapsed < 0 {
		elapsed = 0
	}
	ms := durationMillisForMetrics(elapsed)
	n.observabilityMu.Lock()
	n.observability.BlockGossipReceivedTotal++
	n.observability.BlockPropagationLastMs = ms
	if ms > n.observability.BlockPropagationMaxMs {
		n.observability.BlockPropagationMaxMs = ms
	}
	n.observability.BlockPropagationHeight = block.ID
	n.observabilityMu.Unlock()
}

func (n *Node) observeRPCRequestStart() {
	if n == nil {
		return
	}
	n.observabilityMu.Lock()
	n.observability.RPCRequestsTotal++
	n.observability.RPCInflight++
	n.observabilityMu.Unlock()
}

func (n *Node) observeRPCRequestFinish(status int) {
	if n == nil {
		return
	}
	n.observabilityMu.Lock()
	if n.observability.RPCInflight > 0 {
		n.observability.RPCInflight--
	}
	if status == 0 {
		status = 200
	}
	if status == 401 {
		n.observability.RPCUnauthorizedTotal++
	}
	n.observabilityMu.Unlock()
}

func (n *Node) observeRPCRateLimited() {
	if n == nil {
		return
	}
	n.observabilityMu.Lock()
	n.observability.RPCRateLimitedTotal++
	n.observabilityMu.Unlock()
}

func (n *Node) observeRPCBodyRejected() {
	if n == nil {
		return
	}
	n.observabilityMu.Lock()
	n.observability.RPCBodyRejectedTotal++
	n.observabilityMu.Unlock()
}

func (n *Node) observeRPCConcurrentRejected() {
	if n == nil {
		return
	}
	n.observabilityMu.Lock()
	n.observability.RPCConcurrentRejectedTotal++
	n.observabilityMu.Unlock()
}

func (n *Node) observabilityStatsSnapshot() observabilityStats {
	if n == nil {
		return observabilityStats{}
	}
	n.observabilityMu.RLock()
	defer n.observabilityMu.RUnlock()
	return n.observability
}

type peerObservabilitySnapshot struct {
	RateLimitDropsTotal uint64
	InvalidProofsTotal  uint64
	SecurityFaultsTotal uint64
	AverageReputation   float64
	AverageLatencyMs    float64
	MaxLatencyMs        float64
}

func (n *Node) peerObservabilitySnapshot() peerObservabilitySnapshot {
	if n == nil {
		return peerObservabilitySnapshot{AverageReputation: 1}
	}
	n.syncPeerScoreMu.Lock()
	defer n.syncPeerScoreMu.Unlock()
	if len(n.syncPeerScores) == 0 {
		return peerObservabilitySnapshot{AverageReputation: 1}
	}
	var out peerObservabilitySnapshot
	var reputationTotal float64
	var latencyTotal float64
	var latencySamples float64
	for _, score := range n.syncPeerScores {
		if score == nil {
			continue
		}
		out.RateLimitDropsTotal += score.RateLimitDropCount
		out.InvalidProofsTotal += score.InvalidProofCount
		out.SecurityFaultsTotal += score.SecurityFaultCount
		reputation, _ := peerReputationValue(score)
		reputationTotal += reputation
		if score.AvgLatencyMs > 0 {
			latencyTotal += score.AvgLatencyMs
			latencySamples++
			if score.AvgLatencyMs > out.MaxLatencyMs {
				out.MaxLatencyMs = score.AvgLatencyMs
			}
		}
	}
	out.AverageReputation = reputationTotal / float64(len(n.syncPeerScores))
	if latencySamples > 0 {
		out.AverageLatencyMs = latencyTotal / latencySamples
	}
	return out
}

func peerDisconnectRatePerMinute(obs observabilityStats) float64 {
	if obs.PeerDisconnectTotal == 0 {
		return 0
	}
	if obs.PeerDisconnectFirstUnix <= 0 || obs.PeerDisconnectLastUnix <= obs.PeerDisconnectFirstUnix {
		return float64(obs.PeerDisconnectTotal)
	}
	minutes := float64(obs.PeerDisconnectLastUnix-obs.PeerDisconnectFirstUnix) / 60.0
	if minutes <= 0 {
		return float64(obs.PeerDisconnectTotal)
	}
	return float64(obs.PeerDisconnectTotal) / minutes
}

func (n *Node) validatorStatusSnapshotMap() map[string]ValidatorStatus {
	out := make(map[string]ValidatorStatus)
	if n == nil {
		return out
	}
	n.validatorMu.RLock()
	defer n.validatorMu.RUnlock()
	for id, st := range n.validatorStatus {
		if st == nil {
			continue
		}
		out[id] = *st
	}
	return out
}

func (n *Node) mempoolObservabilitySnapshot() (depth int, bytes uint64, oldestTxAgeSeconds uint64) {
	if n == nil {
		return 0, 0, 0
	}
	n.Mempool.mu.Lock()
	defer n.Mempool.mu.Unlock()
	now := time.Now().Unix()
	oldestExpiryAge := int64(0)
	for _, tx := range n.Mempool.Transactions {
		depth++
		bytes += uint64(len(tx.ID) + len(tx.From) + len(tx.To) + len(tx.PublicKey) + len(tx.Signature) + len(tx.DTLTokenID) + len(tx.DTLPayload) + len(tx.DTLGovernanceCert) + 96)
		if tx.Expiry > 0 {
			age := now - tx.Expiry
			if age < 0 {
				age = 0
			}
			if age > oldestExpiryAge {
				oldestExpiryAge = age
			}
		}
	}
	if oldestExpiryAge > 0 {
		oldestTxAgeSeconds = uint64(oldestExpiryAge)
	}
	return depth, bytes, oldestTxAgeSeconds
}

type finalityArtifactObservability struct {
	Certificates      uint64
	Anchors           uint64
	ValidatorCommits  uint64
	IrreversibleRoots uint64
	StateCheckpoints  uint64
}

func (n *Node) finalityArtifactObservability() finalityArtifactObservability {
	if n == nil {
		return finalityArtifactObservability{}
	}
	base := nodeDataPath(n.DataDir, n.ID)
	return finalityArtifactObservability{
		Certificates:      countRegularFiles(filepath.Join(base, finalityCertificatesDir)),
		Anchors:           countRegularFiles(filepath.Join(base, finalityEpochAnchorsDir)),
		ValidatorCommits:  countRegularFiles(filepath.Join(base, finalityValidatorCommitmentsDir)),
		IrreversibleRoots: countRegularFiles(filepath.Join(base, finalityIrreversibleRootsDir)),
		StateCheckpoints:  countRegularFiles(filepath.Join(base, "state_checkpoints")),
	}
}

func (n *Node) storageDirectorySizeSnapshot() (storageSizeBytes uint64, coldStorageSizeBytes uint64) {
	if n == nil {
		return 0, 0
	}
	now := time.Now()
	n.observabilityMu.RLock()
	if n.observability.StorageSizeScannedAtUnix > 0 && now.Unix()-n.observability.StorageSizeScannedAtUnix < 60 {
		storageSizeBytes = n.observability.StorageSizeBytes
		coldStorageSizeBytes = n.observability.ColdStorageSizeBytes
		n.observabilityMu.RUnlock()
		return storageSizeBytes, coldStorageSizeBytes
	}
	n.observabilityMu.RUnlock()

	nodeRoot := nodeDataPath(n.DataDir, n.ID)
	storageSizeBytes = directorySizeBytes(nodeRoot)
	coldStorageSizeBytes = directorySizeBytes(filepath.Join(nodeRoot, "cold-storage"))

	n.observabilityMu.Lock()
	n.observability.StorageSizeBytes = storageSizeBytes
	n.observability.ColdStorageSizeBytes = coldStorageSizeBytes
	n.observability.StorageSizeScannedAtUnix = now.Unix()
	n.observabilityMu.Unlock()
	return storageSizeBytes, coldStorageSizeBytes
}

func countRegularFiles(root string) uint64 {
	if root == "" {
		return 0
	}
	var count uint64
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		count++
		return nil
	})
	return count
}

func directorySizeBytes(root string) uint64 {
	if root == "" {
		return 0
	}
	var size uint64
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil && info.Size() > 0 {
			size += uint64(info.Size())
		}
		return nil
	})
	return size
}
