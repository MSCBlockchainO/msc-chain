package main

import (
	"os"
	"path/filepath"
	"time"
)

type observabilityStats struct {
	// `AutoHealActionsTotal` counts non-noop auto-heal actions attempted.
	AutoHealActionsTotal uint64
	// `SnapshotCreateTotal` stores the measured quantity used by this operation.
	SnapshotCreateTotal uint64
	// `SnapshotCreateFailures` stores the result produced by this operation.
	SnapshotCreateFailures uint64
	// `SnapshotCreateHeight` stores the value associated with this record.
	SnapshotCreateHeight uint64
	// `SnapshotCreateDurationMs` stores the value associated with this record.
	SnapshotCreateDurationMs uint64

	// `SnapshotLoadTotal` stores the measured quantity used by this operation.
	SnapshotLoadTotal uint64
	// `SnapshotLoadFailures` stores the result produced by this operation.
	SnapshotLoadFailures uint64
	// `SnapshotLoadHeight` stores the value associated with this record.
	SnapshotLoadHeight uint64
	// `SnapshotLoadDurationMs` stores the value associated with this record.
	SnapshotLoadDurationMs uint64

	// `SnapshotApplyTotal` stores the measured quantity used by this operation.
	SnapshotApplyTotal uint64
	// `SnapshotApplyFailures` stores the result produced by this operation.
	SnapshotApplyFailures uint64
	// `SnapshotApplyHeight` stores the value associated with this record.
	SnapshotApplyHeight uint64
	// `SnapshotApplyDurationMs` stores the value associated with this record.
	SnapshotApplyDurationMs uint64

	// `ReplayTotal` stores the measured quantity used by this operation.
	ReplayTotal uint64
	// `ReplayFailures` stores the result produced by this operation.
	ReplayFailures uint64
	// `ReplayHeight` stores the value associated with this record.
	ReplayHeight uint64
	// `ReplayBlocks` stores the value associated with this record.
	ReplayBlocks uint64
	// `ReplayBlocksTotal` stores the measured quantity used by this operation.
	ReplayBlocksTotal uint64
	// `ReplayDurationMs` stores the value associated with this record.
	ReplayDurationMs uint64

	// `StorageGCCyclesTotal` stores the measured quantity used by this operation.
	StorageGCCyclesTotal uint64
	// `StorageGCFailuresTotal` stores the measured quantity used by this operation.
	StorageGCFailuresTotal uint64
	// `StorageGCDurationMs` stores the value associated with this record.
	StorageGCDurationMs uint64
	// `StoragePrunedSnapshotsTotal` stores the measured quantity used by this operation.
	StoragePrunedSnapshotsTotal uint64
	// `StoragePrunedStatesTotal` stores the measured quantity used by this operation.
	StoragePrunedStatesTotal uint64
	// `StorageSizeBytes` stores the value associated with this record.
	StorageSizeBytes uint64
	// `ColdStorageSizeBytes` stores the value associated with this record.
	ColdStorageSizeBytes uint64
	// `StorageSizeScannedAtUnix` stores the value associated with this record.
	StorageSizeScannedAtUnix int64
	// `StorageSizeScanInProgress` stores the value associated with this record.
	StorageSizeScanInProgress bool
	// `FinalityCertificates` stores the value associated with this record.
	FinalityCertificates uint64
	// `FinalityAnchors` stores the value associated with this record.
	FinalityAnchors uint64
	// `FinalityValidatorCommits` stores the value associated with this record.
	FinalityValidatorCommits uint64
	// `FinalityIrreversibleRoots` stores the value associated with this record.
	FinalityIrreversibleRoots uint64
	// `FinalityStateCheckpoints` stores the value associated with this record.
	FinalityStateCheckpoints uint64
	// `FinalityArtifactsScannedAt` stores the value associated with this record.
	FinalityArtifactsScannedAt int64
	// `FinalityScanInProgress` stores the value associated with this record.
	FinalityScanInProgress bool
	// `ColdExportsTotal` stores the measured quantity used by this operation.
	ColdExportsTotal uint64

	// `SyncModeSwitchTotal` stores the measured quantity used by this operation.
	SyncModeSwitchTotal uint64
	// `PeerDisconnectTotal` stores the measured quantity used by this operation.
	PeerDisconnectTotal uint64
	// `PeerDisconnectFirstUnix` stores the value associated with this record.
	PeerDisconnectFirstUnix int64
	// `PeerDisconnectLastUnix` stores the value associated with this record.
	PeerDisconnectLastUnix int64
	// `PeerDiversityRejectTotal` stores the measured quantity used by this operation.
	PeerDiversityRejectTotal uint64
	// `PeerDiversityOutboundRejectTotal` stores the measured quantity used by this operation.
	PeerDiversityOutboundRejectTotal uint64
	// `PeerResourceDropTotal` stores the measured quantity used by this operation.
	PeerResourceDropTotal uint64
	// `PeerConnectionFloodTotal` stores the measured quantity used by this operation.
	PeerConnectionFloodTotal uint64
	// `PeerConnectedMax` stores the value associated with this record.
	PeerConnectedMax uint64
	// `PeerConnectedLast` stores the value associated with this record.
	PeerConnectedLast uint64

	// `BlockGossipReceivedTotal` stores the block data handled by this operation.
	BlockGossipReceivedTotal uint64
	// `BlockPropagationLastMs` stores the block data handled by this operation.
	BlockPropagationLastMs uint64
	// `BlockPropagationMaxMs` stores the block data handled by this operation.
	BlockPropagationMaxMs uint64
	// `BlockPropagationHeight` stores the block data handled by this operation.
	BlockPropagationHeight uint64

	// `RPCRequestsTotal` stores the measured quantity used by this operation.
	RPCRequestsTotal uint64
	// `RPCRateLimitedTotal` stores the measured quantity used by this operation.
	RPCRateLimitedTotal uint64
	// `RPCBodyRejectedTotal` stores the measured quantity used by this operation.
	RPCBodyRejectedTotal uint64
	// `RPCConcurrentRejectedTotal` stores the measured quantity used by this operation.
	RPCConcurrentRejectedTotal uint64
	// `RPCUnauthorizedTotal` stores the measured quantity used by this operation.
	RPCUnauthorizedTotal uint64
	// `RPCInflight` stores the value associated with this record.
	RPCInflight int64
}

func (n *Node) observeAutoHealAction() {
	if n == nil {
		return
	}
	n.observabilityMu.Lock()
	n.observability.AutoHealActionsTotal++
	n.observabilityMu.Unlock()
}

// durationMillisForMetrics implements the duration millis for metrics helper.
func durationMillisForMetrics(elapsed time.Duration) uint64 {
	if elapsed <= 0 {
		return 0
	}
	// `ms` stores the value produced by this operation.
	ms := uint64(elapsed / time.Millisecond)
	if ms == 0 {
		return 1
	}
	return ms
}

// observeSnapshotOperation implements the observe snapshot operation helper.
func (n *Node) observeSnapshotOperation(kind string, height uint64, elapsed time.Duration, success bool) {
	if n == nil {
		return
	}
	// `ms` stores the value produced by this operation.
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

// observeReplayOperation implements the observe replay operation helper.
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

// observeStorageManagerRun implements the observe storage manager run helper.
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
		// `prunedThrough` stores the value produced by this operation.
		if prunedThrough := report.RetainFromHeight - 1; prunedThrough > n.observability.StoragePrunedStatesTotal {
			n.observability.StoragePrunedStatesTotal = prunedThrough
		}
	}
	if report.ColdStorageExported > 0 {
		n.observability.ColdExportsTotal += uint64(report.ColdStorageExported)
	}
}

// observeSyncModeSwitch implements the observe sync mode switch helper.
func (n *Node) observeSyncModeSwitch(oldMode string, newMode string) {
	if n == nil || oldMode == "" || oldMode == newMode {
		return
	}
	n.observabilityMu.Lock()
	n.observability.SyncModeSwitchTotal++
	n.observabilityMu.Unlock()
}

// observePeerDisconnect implements the observe peer disconnect helper.
func (n *Node) observePeerDisconnect(reason string) {
	if n == nil {
		return
	}
	_ = reason
	// `now` stores the value produced by this operation.
	now := time.Now().Unix()
	n.observabilityMu.Lock()
	if n.observability.PeerDisconnectFirstUnix == 0 {
		n.observability.PeerDisconnectFirstUnix = now
	}
	n.observability.PeerDisconnectLastUnix = now
	n.observability.PeerDisconnectTotal++
	n.observabilityMu.Unlock()
}

// observePeerDiversityReject implements the observe peer diversity reject helper.
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

// observePeerResourceDrop implements the observe peer resource drop helper.
func (n *Node) observePeerResourceDrop(reason string) {
	if n == nil {
		return
	}
	_ = reason
	n.observabilityMu.Lock()
	n.observability.PeerResourceDropTotal++
	n.observabilityMu.Unlock()
}

// observePeerConnectionFlood implements the observe peer connection flood helper.
func (n *Node) observePeerConnectionFlood(reason string) {
	if n == nil {
		return
	}
	_ = reason
	n.observabilityMu.Lock()
	n.observability.PeerConnectionFloodTotal++
	n.observabilityMu.Unlock()
}

// observePeerConnectivityGauge implements the observe peer connectivity gauge helper.
func (n *Node) observePeerConnectivityGauge(peers int) {
	if n == nil || peers < 0 {
		return
	}
	n.observabilityMu.Lock()
	// `value` stores the value currently being processed.
	value := uint64(peers)
	n.observability.PeerConnectedLast = value
	if value > n.observability.PeerConnectedMax {
		n.observability.PeerConnectedMax = value
	}
	n.observabilityMu.Unlock()
}

// observeBlockPropagation implements the observe block propagation helper.
func (n *Node) observeBlockPropagation(block Block, receivedAt time.Time) {
	if n == nil || block.ID == 0 {
		return
	}
	// `elapsed` stores the value produced by this operation.
	elapsed := time.Since(receivedAt)
	if receivedAt.IsZero() || elapsed < 0 {
		elapsed = 0
	}
	// `ms` stores the value produced by this operation.
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

// observeRPCRequestStart implements the observe rpc request start helper.
func (n *Node) observeRPCRequestStart() {
	if n == nil {
		return
	}
	n.observabilityMu.Lock()
	n.observability.RPCRequestsTotal++
	n.observability.RPCInflight++
	n.observabilityMu.Unlock()
}

// observeRPCRequestFinish implements the observe rpc request finish helper.
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

// observeRPCRateLimited implements the observe rpc rate limited helper.
func (n *Node) observeRPCRateLimited() {
	if n == nil {
		return
	}
	n.observabilityMu.Lock()
	n.observability.RPCRateLimitedTotal++
	n.observabilityMu.Unlock()
}

// observeRPCBodyRejected implements the observe rpc body rejected helper.
func (n *Node) observeRPCBodyRejected() {
	if n == nil {
		return
	}
	n.observabilityMu.Lock()
	n.observability.RPCBodyRejectedTotal++
	n.observabilityMu.Unlock()
}

// observeRPCConcurrentRejected implements the observe rpc concurrent rejected helper.
func (n *Node) observeRPCConcurrentRejected() {
	if n == nil {
		return
	}
	n.observabilityMu.Lock()
	n.observability.RPCConcurrentRejectedTotal++
	n.observabilityMu.Unlock()
}

// observabilityStatsSnapshot implements the observability stats snapshot helper.
func (n *Node) observabilityStatsSnapshot() observabilityStats {
	if n == nil {
		return observabilityStats{}
	}
	n.observabilityMu.RLock()
	defer n.observabilityMu.RUnlock()
	return n.observability
}

type peerObservabilitySnapshot struct {
	// `RateLimitDropsTotal` stores the measured quantity used by this operation.
	RateLimitDropsTotal uint64
	// `InvalidProofsTotal` stores the measured quantity used by this operation.
	InvalidProofsTotal uint64
	// `SecurityFaultsTotal` stores the measured quantity used by this operation.
	SecurityFaultsTotal uint64
	// `AverageReputation` stores the value associated with this record.
	AverageReputation float64
	// `AverageLatencyMs` stores the value associated with this record.
	AverageLatencyMs float64
	// `MaxLatencyMs` stores the value associated with this record.
	MaxLatencyMs float64
}

// peerObservabilitySnapshot implements the peer observability snapshot helper.
func (n *Node) peerObservabilitySnapshot() peerObservabilitySnapshot {
	if n == nil {
		return peerObservabilitySnapshot{AverageReputation: 1}
	}
	n.syncPeerScoreMu.Lock()
	defer n.syncPeerScoreMu.Unlock()
	if len(n.syncPeerScores) == 0 {
		return peerObservabilitySnapshot{AverageReputation: 1}
	}
	// `out` stores the result produced by this operation.
	var out peerObservabilitySnapshot
	// `reputationTotal` stores the measured quantity used by this operation.
	var reputationTotal float64
	// `latencyTotal` stores the measured quantity used by this operation.
	var latencyTotal float64
	// `latencySamples` stores the value used by this operation.
	var latencySamples float64
	// `score` tracks the current values while iterating.
	for _, score := range n.syncPeerScores {
		if score == nil {
			continue
		}
		out.RateLimitDropsTotal += score.RateLimitDropCount + score.TrustedRateLimitDropCount
		out.InvalidProofsTotal += score.InvalidProofCount
		out.SecurityFaultsTotal += score.SecurityFaultCount + score.TrustedSecurityFaultCount
		// `reputation` stores the value produced by this operation.
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

// peerDisconnectRatePerMinute implements the peer disconnect rate per minute helper.
func peerDisconnectRatePerMinute(obs observabilityStats) float64 {
	if obs.PeerDisconnectTotal == 0 {
		return 0
	}
	if obs.PeerDisconnectFirstUnix <= 0 || obs.PeerDisconnectLastUnix <= obs.PeerDisconnectFirstUnix {
		return float64(obs.PeerDisconnectTotal)
	}
	// `minutes` stores the value produced by this operation.
	minutes := float64(obs.PeerDisconnectLastUnix-obs.PeerDisconnectFirstUnix) / 60.0
	if minutes <= 0 {
		return float64(obs.PeerDisconnectTotal)
	}
	return float64(obs.PeerDisconnectTotal) / minutes
}

// validatorStatusSnapshotMap implements the validator status snapshot map helper.
func (n *Node) validatorStatusSnapshotMap() map[string]ValidatorStatus {
	// `out` stores the result produced by this operation.
	out := make(map[string]ValidatorStatus)
	if n == nil {
		return out
	}
	n.validatorMu.RLock()
	defer n.validatorMu.RUnlock()
	// `id` and `st` track the current position in the related collection.
	for id, st := range n.validatorStatus {
		if st == nil {
			continue
		}
		out[id] = *st
	}
	return out
}

// mempoolObservabilitySnapshot implements the mempool observability snapshot helper.
func (n *Node) mempoolObservabilitySnapshot() (depth int, bytes uint64, oldestTxAgeSeconds uint64) {
	if n == nil {
		return 0, 0, 0
	}
	n.Mempool.mu.Lock()
	defer n.Mempool.mu.Unlock()
	// `now` stores the value produced by this operation.
	now := time.Now().Unix()
	// `oldestExpiryAge` stores the value produced by this operation.
	oldestExpiryAge := int64(0)
	// `tx` tracks the transaction data handled by this operation.
	for _, tx := range n.Mempool.Transactions {
		depth++
		bytes += uint64(len(tx.ID) + len(tx.From) + len(tx.To) + len(tx.PublicKey) + len(tx.Signature) + len(tx.DTLTokenID) + len(tx.DTLPayload) + len(tx.DTLGovernanceCert) + 96)
		if tx.Expiry > 0 {
			// `age` stores the value produced by this operation.
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
	// `Certificates` stores the value associated with this record.
	Certificates uint64
	// `Anchors` stores the value associated with this record.
	Anchors uint64
	// `ValidatorCommits` stores whether the related condition is satisfied.
	ValidatorCommits uint64
	// `IrreversibleRoots` stores the current position in the related collection.
	IrreversibleRoots uint64
	// `StateCheckpoints` stores the value associated with this record.
	StateCheckpoints uint64
}

// `filesystemObservabilityCacheTTL` defines the constant value used by this package.
const filesystemObservabilityCacheTTL = 5 * time.Minute

// finalityArtifactObservability implements the finality artifact observability helper.
func (n *Node) finalityArtifactObservability() finalityArtifactObservability {
	if n == nil {
		return finalityArtifactObservability{}
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.observabilityMu.Lock()
	// `cached` stores the value produced by this operation.
	cached := finalityArtifactObservability{
		Certificates:      n.observability.FinalityCertificates,
		Anchors:           n.observability.FinalityAnchors,
		ValidatorCommits:  n.observability.FinalityValidatorCommits,
		IrreversibleRoots: n.observability.FinalityIrreversibleRoots,
		StateCheckpoints:  n.observability.FinalityStateCheckpoints,
	}
	// `stale` stores the value produced by this operation.
	stale := n.observability.FinalityArtifactsScannedAt == 0 ||
		now.Unix()-n.observability.FinalityArtifactsScannedAt >= int64(filesystemObservabilityCacheTTL/time.Second)
	if stale && !n.observability.FinalityScanInProgress {
		n.observability.FinalityScanInProgress = true
		go n.refreshFinalityArtifactObservability()
	}
	n.observabilityMu.Unlock()
	return cached
}

// refreshFinalityArtifactObservability implements the refresh finality artifact observability helper.
func (n *Node) refreshFinalityArtifactObservability() {
	if n == nil {
		return
	}
	// `base` stores the value produced by this operation.
	base := nodeDataPath(n.DataDir, n.ID)
	// `latest` stores the value produced by this operation.
	latest := finalityArtifactObservability{
		Certificates:      countRegularFiles(filepath.Join(base, finalityCertificatesDir)),
		Anchors:           countRegularFiles(filepath.Join(base, finalityEpochAnchorsDir)),
		ValidatorCommits:  countRegularFiles(filepath.Join(base, finalityValidatorCommitmentsDir)),
		IrreversibleRoots: countRegularFiles(filepath.Join(base, finalityIrreversibleRootsDir)),
		StateCheckpoints:  countRegularFiles(filepath.Join(base, "state_checkpoints")),
	}
	n.observabilityMu.Lock()
	n.observability.FinalityCertificates = latest.Certificates
	n.observability.FinalityAnchors = latest.Anchors
	n.observability.FinalityValidatorCommits = latest.ValidatorCommits
	n.observability.FinalityIrreversibleRoots = latest.IrreversibleRoots
	n.observability.FinalityStateCheckpoints = latest.StateCheckpoints
	n.observability.FinalityArtifactsScannedAt = time.Now().Unix()
	n.observability.FinalityScanInProgress = false
	n.observabilityMu.Unlock()
}

// storageDirectorySizeSnapshot implements the storage directory size snapshot helper.
func (n *Node) storageDirectorySizeSnapshot() (storageSizeBytes uint64, coldStorageSizeBytes uint64) {
	if n == nil {
		return 0, 0
	}
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.observabilityMu.Lock()
	storageSizeBytes = n.observability.StorageSizeBytes
	coldStorageSizeBytes = n.observability.ColdStorageSizeBytes
	// `stale` stores the value produced by this operation.
	stale := n.observability.StorageSizeScannedAtUnix == 0 ||
		now.Unix()-n.observability.StorageSizeScannedAtUnix >= int64(filesystemObservabilityCacheTTL/time.Second)
	if stale && !n.observability.StorageSizeScanInProgress {
		n.observability.StorageSizeScanInProgress = true
		go n.refreshStorageDirectorySizeSnapshot()
	}
	n.observabilityMu.Unlock()
	return storageSizeBytes, coldStorageSizeBytes
}

// refreshStorageDirectorySizeSnapshot implements the refresh storage directory size snapshot helper.
func (n *Node) refreshStorageDirectorySizeSnapshot() {
	if n == nil {
		return
	}
	// `nodeRoot` stores the digest used to identify or verify the related data.
	nodeRoot := nodeDataPath(n.DataDir, n.ID)
	// `storageSizeBytes` stores the value produced by this operation.
	storageSizeBytes := directorySizeBytes(nodeRoot)
	// `coldStorageSizeBytes` stores the value produced by this operation.
	coldStorageSizeBytes := directorySizeBytes(filepath.Join(nodeRoot, "cold-storage"))

	n.observabilityMu.Lock()
	n.observability.StorageSizeBytes = storageSizeBytes
	n.observability.ColdStorageSizeBytes = coldStorageSizeBytes
	n.observability.StorageSizeScannedAtUnix = time.Now().Unix()
	n.observability.StorageSizeScanInProgress = false
	n.observabilityMu.Unlock()
}

// countRegularFiles implements the count regular files helper.
func countRegularFiles(root string) uint64 {
	if root == "" {
		return 0
	}
	// `count` stores the measured quantity used by this operation.
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

// directorySizeBytes implements the directory size bytes helper.
func directorySizeBytes(root string) uint64 {
	if root == "" {
		return 0
	}
	// `size` stores the measured quantity used by this operation.
	var size uint64
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		// `info` and `err` store the error produced by this operation.
		info, err := d.Info()
		if err == nil && info.Size() > 0 {
			size += uint64(info.Size())
		}
		return nil
	})
	return size
}
