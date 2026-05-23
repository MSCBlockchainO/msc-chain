package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	syncpipeline "msc-chain/sync"
)

type snapshotMetaCandidate struct {
	meta      *SnapshotMetaResponse
	providers map[string]struct{}
}

type nodeSnapshotSyncAdapter struct {
	node                       *Node
	minHeight                  uint64
	requiredProofs             int
	downloaded                 *StateSnapshot
	selectedProviders          []string
	selectedProviderValidators map[string]string
}

func (a *nodeSnapshotSyncAdapter) proofBackedSnapshotMeta(targetHeight uint64, validatorProviders map[string]string) (syncpipeline.SnapshotMeta, bool) {
	if a == nil || a.node == nil || targetHeight == 0 {
		return syncpipeline.SnapshotMeta{}, false
	}
	totalValidators := len(validatorProviders)
	if totalValidators == 0 {
		totalValidators = len(a.node.validatorAuthorityIDsForQuorum(targetHeight))
	}
	if totalValidators == 0 {
		totalValidators = len(a.node.GetConsensusValidators(int(targetHeight)))
	}
	required := RequiredSnapshotProofs(totalValidators)
	if required <= 0 {
		required = 1
	}
	observations, votes := a.node.cachedSnapshotProofObservations(targetHeight, a.minHeight, validatorProviders, required)
	for _, vote := range votes {
		_, _, _ = a.node.updateSnapshotSessionVote(vote)
	}
	quorum, _ := selectStrictSnapshotMetaCandidate(observations, required)
	if quorum == nil || quorum.Height == 0 {
		return syncpipeline.SnapshotMeta{}, false
	}
	providers := make([]string, 0, len(quorum.Providers))
	providerValidators := make(map[string]string, len(quorum.Providers))
	selectedKey := strictSnapshotMetaCandidateKey(quorum)
	for _, observation := range observations {
		if observation.Candidate == nil || strictSnapshotMetaCandidateKey(observation.Candidate) != selectedKey {
			continue
		}
		providerID := strings.TrimSpace(observation.Provider)
		validatorID := normalizeValidatorID(observation.ValidatorID)
		if providerID == "" || validatorID == "" {
			continue
		}
		providerValidators[providerID] = validatorID
	}
	for _, provider := range quorum.Providers {
		if strings.TrimSpace(provider) == "" {
			continue
		}
		providerID := strings.TrimSpace(provider)
		providers = append(providers, providerID)
		if _, ok := providerValidators[providerID]; ok {
			continue
		}
		for validatorID, mappedProvider := range validatorProviders {
			if strings.TrimSpace(mappedProvider) == providerID {
				if id := normalizeValidatorID(validatorID); id != "" {
					providerValidators[providerID] = id
				}
				break
			}
		}
	}
	if len(providers) == 0 {
		return syncpipeline.SnapshotMeta{}, false
	}
	sort.Strings(providers)
	validators := make([]string, 0, len(quorum.Validators))
	for validatorID := range quorum.Validators {
		if id := normalizeValidatorID(validatorID); id != "" {
			validators = append(validators, id)
		}
	}
	sort.Strings(validators)
	a.requiredProofs = required
	a.selectedProviders = append(a.selectedProviders[:0], providers...)
	a.selectedProviderValidators = providerValidators
	a.node.updateSnapshotCatalogMeta(quorum.Height, strings.TrimSpace(quorum.StateRoot), 1, providers)
	a.node.updateSnapshotCatalogProofSet(quorum.Height, validators)
	if len(providers) > 0 {
		a.node.updateSnapshotCatalogAvailability(quorum.Height, 1)
	}
	return syncpipeline.SnapshotMeta{
		Height:           quorum.Height,
		Chunks:           1,
		StateRoot:        strings.TrimSpace(quorum.StateRoot),
		SnapshotHash:     strings.TrimSpace(quorum.SnapshotHash),
		CheckpointHeight: quorum.CheckpointHeight,
		Providers:        providers,
	}, true
}

func (a *nodeSnapshotSyncAdapter) RequestSnapshotMeta(ctx context.Context, localHeight uint64, networkHeight uint64) (syncpipeline.SnapshotMeta, error) {
	_ = ctx
	if a == nil || a.node == nil || a.node.Host == nil {
		return syncpipeline.SnapshotMeta{}, errors.New("snapshot sync adapter unavailable")
	}
	if networkHeight == 0 {
		return syncpipeline.SnapshotMeta{}, errors.New("network height unavailable")
	}
	targetHeight := networkHeight
	peers := a.node.SelectSnapshotPeers(targetHeight, true)
	if len(peers) == 0 {
		if meta, ok := a.proofBackedSnapshotMeta(targetHeight, nil); ok {
			return meta, nil
		}
		return syncpipeline.SnapshotMeta{}, errors.New("snapshot peers unavailable")
	}
	validatorProviders := make(map[string]string, len(peers))
	validatorIDs := make(map[string]string, len(peers))
	candidates := make(map[string]*snapshotMetaCandidate)

	addCandidate := func(provider string, validatorID string, meta *SnapshotMetaResponse) {
		if meta == nil || !meta.Available || meta.Height == 0 {
			return
		}
		if meta.Height > targetHeight {
			return
		}
		if a.minHeight > 0 && meta.Height < a.minHeight {
			return
		}
		observation, _ := a.node.strictSnapshotMetaObservationForTarget(meta, provider, validatorID, targetHeight, a.minHeight, false)
		if observation == nil || observation.Candidate == nil {
			return
		}
		key := strictSnapshotMetaCandidateKey(observation.Candidate)
		if key == "" {
			return
		}
		entry := candidates[key]
		if entry == nil {
			copyMeta := *meta
			entry = &snapshotMetaCandidate{
				meta:      &copyMeta,
				providers: make(map[string]struct{}),
			}
			candidates[key] = entry
		}
		entry.providers[strings.TrimSpace(provider)] = struct{}{}
	}

	for _, info := range peers {
		validatorID := normalizeValidatorID(info.ValidatorID)
		provider := strings.TrimSpace(info.PeerID.String())
		if validatorID == "" || provider == "" {
			continue
		}
		validatorProviders[validatorID] = provider
		validatorIDs[provider] = validatorID
	}

	for _, availability := range a.node.cachedSnapshotMetaAvailabilities(targetHeight, a.minHeight, validatorProviders) {
		if availability.Meta == nil {
			continue
		}
		addCandidate(availability.Provider, availability.ValidatorID, availability.Meta)
	}

	if meta, ok := a.proofBackedSnapshotMeta(targetHeight, validatorProviders); ok {
		return meta, nil
	}

	for _, info := range peers {
		provider := strings.TrimSpace(info.PeerID.String())
		validatorID := validatorIDs[provider]
		meta, err := a.node.requestSnapshotMetaFromPeer(info.PeerID, targetHeight)
		if err != nil || meta == nil || !meta.Available {
			meta, err = a.node.requestSnapshotMetaFromPeer(info.PeerID, 0)
			if err != nil || meta == nil || !meta.Available {
				continue
			}
		}
		addCandidate(provider, validatorID, meta)
	}

	if len(candidates) == 0 {
		if meta, ok := a.proofBackedSnapshotMeta(targetHeight, validatorProviders); ok {
			return meta, nil
		}
		return syncpipeline.SnapshotMeta{}, errors.New("snapshot metadata unavailable from peers")
	}

	keys := make([]string, 0, len(candidates))
	for key := range candidates {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		left := candidates[keys[i]]
		right := candidates[keys[j]]
		if left == nil || left.meta == nil {
			return false
		}
		if right == nil || right.meta == nil {
			return true
		}
		if left.meta.Height != right.meta.Height {
			return left.meta.Height > right.meta.Height
		}
		if len(left.providers) != len(right.providers) {
			return len(left.providers) > len(right.providers)
		}
		return keys[i] < keys[j]
	})
	best := candidates[keys[0]]
	if best == nil || best.meta == nil {
		return syncpipeline.SnapshotMeta{}, errors.New("snapshot metadata candidate unavailable")
	}
	providers := make([]string, 0, len(best.providers))
	providerValidators := make(map[string]string, len(best.providers))
	for provider := range best.providers {
		if strings.TrimSpace(provider) == "" {
			continue
		}
		providerID := strings.TrimSpace(provider)
		providers = append(providers, providerID)
		if id := normalizeValidatorID(validatorIDs[providerID]); id != "" {
			providerValidators[providerID] = id
		}
	}
	sort.Strings(providers)
	a.selectedProviders = append(a.selectedProviders[:0], providers...)
	a.selectedProviderValidators = providerValidators
	replicationTargets := a.snapshotReplicationTargetIDs(best.meta.Height, best.meta.SnapshotHash)
	catalogProviders := append([]string{}, providers...)
	catalogProviders = append(catalogProviders, replicationTargets...)
	a.node.updateSnapshotCatalogMeta(best.meta.Height, strings.TrimSpace(best.meta.StateRoot), best.meta.TotalChunks, catalogProviders)
	switch {
	case len(replicationTargets) > 0:
		available := len(providers)
		if available > len(replicationTargets) {
			available = len(replicationTargets)
		}
		a.node.updateSnapshotCatalogAvailability(best.meta.Height, float64(available)/float64(len(replicationTargets)))
	case len(providers) > 0:
		a.node.updateSnapshotCatalogAvailability(best.meta.Height, 1)
	}

	return syncpipeline.SnapshotMeta{
		Height:           best.meta.Height,
		Chunks:           int(best.meta.TotalChunks),
		StateRoot:        strings.TrimSpace(best.meta.StateRoot),
		SnapshotHash:     strings.TrimSpace(best.meta.SnapshotHash),
		CheckpointHeight: snapshotCheckpointHeightFor(best.meta.Height),
		Providers:        providers,
	}, nil
}

func (a *nodeSnapshotSyncAdapter) CollectSnapshotProofs(ctx context.Context, meta syncpipeline.SnapshotMeta) ([]syncpipeline.SnapshotProof, error) {
	_ = ctx
	if a == nil || a.node == nil {
		return nil, errors.New("snapshot sync adapter unavailable")
	}
	collector := SnapshotProofCollector{
		Node:             a.node,
		TargetHeight:     meta.Height,
		MinHeight:        a.minHeight,
		CheckpointHeight: meta.CheckpointHeight,
		StrictCoreQuorum: true,
	}
	quorum, err := collector.Collect()
	if err != nil {
		return nil, err
	}
	if quorum == nil {
		return nil, nil
	}
	if quorum.Required > 0 {
		a.requiredProofs = quorum.Required
	}
	proofs := quorum.Proofs
	if len(proofs) == 0 {
		proofs = a.node.snapshotProofsForCandidate(quorum.Candidate)
	}
	out := make([]syncpipeline.SnapshotProof, 0, len(proofs))
	for validatorID, proof := range proofs {
		sigHex := strings.TrimSpace(proof.SignatureHex)
		sig, err := hex.DecodeString(sigHex)
		if err != nil || len(sig) == 0 {
			continue
		}
		out = append(out, syncpipeline.SnapshotProof{
			Height:                proof.Height,
			CheckpointHeight:      proof.CheckpointHeight,
			BlockHash:             strings.TrimSpace(proof.BlockHash),
			SnapshotHash:          strings.TrimSpace(proof.SnapshotHash),
			StateRoot:             strings.TrimSpace(proof.StateRoot),
			StateMerkleRoot:       strings.TrimSpace(proof.StateMerkleRoot),
			LedgerHash:            strings.TrimSpace(proof.LedgerHash),
			ValidatorSetHash:      strings.TrimSpace(proof.ValidatorSetHash),
			ValidatorSetRoot:      strings.TrimSpace(proof.ValidatorSetRoot),
			ValidatorRegistryHash: strings.TrimSpace(proof.ValidatorRegistryHash),
			CheckpointDomain:      strings.TrimSpace(proof.CheckpointDomain),
			Validator:             normalizeValidatorID(validatorID),
			Signature:             sig,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Validator < out[j].Validator
	})
	proofValidators := make([]string, 0, len(out))
	for _, proof := range out {
		if id := normalizeValidatorID(proof.Validator); id != "" {
			proofValidators = append(proofValidators, id)
		}
	}
	a.node.updateSnapshotCatalogProofSet(meta.Height, proofValidators)
	return out, nil
}

func (a *nodeSnapshotSyncAdapter) RequiredProofs(ctx context.Context, meta syncpipeline.SnapshotMeta) int {
	_ = ctx
	if a != nil && a.requiredProofs > 0 {
		return a.requiredProofs
	}
	if a == nil || a.node == nil {
		return 1
	}
	required := execQuorumRequired(len(a.node.validatorAuthorityIDsForQuorum(meta.Height)))
	if required <= 0 {
		required = 1
	}
	return required
}

func (a *nodeSnapshotSyncAdapter) VerifyProof(ctx context.Context, proof syncpipeline.SnapshotProof) bool {
	_ = ctx
	if a == nil || a.node == nil {
		return false
	}
	wire := SnapshotProof{
		Height:                proof.Height,
		CheckpointHeight:      proof.CheckpointHeight,
		BlockHash:             strings.TrimSpace(proof.BlockHash),
		SnapshotHash:          strings.TrimSpace(proof.SnapshotHash),
		StateRoot:             strings.TrimSpace(proof.StateRoot),
		StateMerkleRoot:       strings.TrimSpace(proof.StateMerkleRoot),
		LedgerHash:            strings.TrimSpace(proof.LedgerHash),
		ValidatorSetHash:      strings.TrimSpace(proof.ValidatorSetHash),
		ValidatorSetRoot:      strings.TrimSpace(proof.ValidatorSetRoot),
		ValidatorRegistryHash: strings.TrimSpace(proof.ValidatorRegistryHash),
		CheckpointDomain:      strings.TrimSpace(proof.CheckpointDomain),
		Validator:             normalizeValidatorID(proof.Validator),
		SignatureHex:          hex.EncodeToString(proof.Signature),
	}
	normalizeSnapshotProof(&wire)
	return a.node.verifySnapshotProof(&wire)
}

func (a *nodeSnapshotSyncAdapter) VerifyStateRoot(ctx context.Context, meta syncpipeline.SnapshotMeta) error {
	_ = ctx
	if a == nil || a.node == nil {
		return errors.New("snapshot sync adapter unavailable")
	}
	snapshot := a.downloaded
	if snapshot == nil {
		if snap, _, _, ok := a.node.ResolveCommittedStateSnapshot(meta.Height); ok && snap != nil {
			snapshot = snap
		}
	}
	if snapshot == nil {
		return errors.New("downloaded snapshot unavailable for verification")
	}
	if strings.TrimSpace(meta.StateRoot) != "" &&
		!strings.EqualFold(strings.TrimSpace(snapshot.StateRoot), strings.TrimSpace(meta.StateRoot)) {
		return fmt.Errorf("state root mismatch snapshot=%s meta=%s",
			ShortHash(snapshot.StateRoot),
			ShortHash(meta.StateRoot),
		)
	}
	if strings.TrimSpace(meta.SnapshotHash) != "" &&
		!strings.EqualFold(strings.TrimSpace(snapshot.SnapshotHash), strings.TrimSpace(meta.SnapshotHash)) {
		return fmt.Errorf("snapshot hash mismatch snapshot=%s meta=%s",
			ShortHash(snapshot.SnapshotHash),
			ShortHash(meta.SnapshotHash),
		)
	}
	if ok, reason := a.node.verifySnapshotAgainstLocalBlockDetailed(snapshot); !ok {
		if strings.TrimSpace(reason) != "" && reason != "anchor_block_unavailable" {
			return fmt.Errorf("local anchor verification failed: %s", reason)
		}
	}
	return nil
}

func snapshotMatchesSyncPipelineMeta(snapshot *StateSnapshot, meta syncpipeline.SnapshotMeta) bool {
	if snapshot == nil || meta.Height == 0 || snapshot.Height != meta.Height {
		return false
	}
	if strings.TrimSpace(meta.StateRoot) != "" &&
		!strings.EqualFold(strings.TrimSpace(snapshot.StateRoot), strings.TrimSpace(meta.StateRoot)) {
		return false
	}
	if strings.TrimSpace(meta.SnapshotHash) != "" &&
		!strings.EqualFold(strings.TrimSpace(snapshot.SnapshotHash), strings.TrimSpace(meta.SnapshotHash)) {
		return false
	}
	return true
}

func (a *nodeSnapshotSyncAdapter) DownloadSnapshotChunks(ctx context.Context, meta syncpipeline.SnapshotMeta) error {
	_ = ctx
	if a == nil || a.node == nil {
		return errors.New("snapshot sync adapter unavailable")
	}
	a.hydrateSelectedSnapshotPeerValidators()
	result, err := a.node.downloadTrustedSnapshotAndStore(meta.Height, meta.Height, true, false, false)
	if err != nil {
		return err
	}
	if result == nil || result.Snapshot == nil {
		return errors.New("snapshot download returned empty result")
	}
	if !snapshotMatchesSyncPipelineMeta(result.Snapshot, meta) {
		if result.Snapshot.Height == meta.Height {
			_ = a.node.deleteStoredSnapshotHeight(result.Snapshot.Height)
			_ = a.node.refreshLatestSnapshotPointer()
		}
		result, err = a.node.downloadTrustedSnapshotAndStore(meta.Height, meta.Height, true, false, false)
		if err != nil {
			return err
		}
		if result == nil || result.Snapshot == nil {
			return errors.New("snapshot download retry returned empty result")
		}
		if !snapshotMatchesSyncPipelineMeta(result.Snapshot, meta) {
			return fmt.Errorf("snapshot identity mismatch snapshot_h=%d snapshot=%s/%s meta_h=%d meta=%s/%s",
				result.Snapshot.Height,
				ShortHash(result.Snapshot.SnapshotHash),
				ShortHash(result.Snapshot.StateRoot),
				meta.Height,
				ShortHash(meta.SnapshotHash),
				ShortHash(meta.StateRoot),
			)
		}
	}
	a.downloaded = result.Snapshot
	return nil
}

func (a *nodeSnapshotSyncAdapter) hydrateSelectedSnapshotPeerValidators() {
	if a == nil || a.node == nil || len(a.selectedProviderValidators) == 0 {
		return
	}
	a.node.peerStateMu.Lock()
	if a.node.peerToValidator == nil {
		a.node.peerToValidator = make(map[string]string)
	}
	for provider, validatorID := range a.selectedProviderValidators {
		provider = strings.TrimSpace(provider)
		validatorID = normalizeValidatorID(validatorID)
		if provider == "" || validatorID == "" {
			continue
		}
		if normalizeNodeRole(a.node.peerRole[provider]) != "validator" {
			continue
		}
		if strings.TrimSpace(a.node.peerToValidator[provider]) == "" {
			a.node.peerToValidator[provider] = validatorID
		}
	}
	a.node.peerStateMu.Unlock()
}

func (a *nodeSnapshotSyncAdapter) ParallelChunks() int {
	return syncSnapshotParallelChunks()
}

func (a *nodeSnapshotSyncAdapter) ApplySnapshot(ctx context.Context, meta syncpipeline.SnapshotMeta) error {
	_ = ctx
	if a == nil || a.node == nil {
		return errors.New("snapshot sync adapter unavailable")
	}
	snapshot := a.downloaded
	if snapshot == nil {
		if snap, _, _, ok := a.node.ResolveCommittedStateSnapshot(meta.Height); ok && snap != nil {
			snapshot = snap
		}
	}
	if snapshot == nil {
		return errors.New("snapshot unavailable for apply")
	}
	a.node.updateSnapshotCatalogMeta(snapshot.Height, strings.TrimSpace(snapshot.StateRoot), uint64(meta.Chunks), a.selectedProviders)
	a.node.updateSnapshotCatalogAvailability(snapshot.Height, 1)
	a.node.ApplySnapshotForSync(*snapshot)
	a.node.noteSnapshotApplied(snapshot.Height)
	a.node.markSnapshotSessionApplied(snapshot, a.requiredProofs)
	return nil
}

func (a *nodeSnapshotSyncAdapter) DeltaReplay(ctx context.Context, fromHeight uint64, toHeight uint64) error {
	_ = ctx
	_ = fromHeight
	if a == nil || a.node == nil || toHeight == 0 {
		return nil
	}
	if a.node.Blockchain != nil && a.node.Blockchain.Height() >= toHeight {
		return nil
	}
	if !a.node.syncDeltaReplayFromPeers(toHeight) {
		return fmt.Errorf("delta replay to height %d failed", toHeight)
	}
	return nil
}

func (a *nodeSnapshotSyncAdapter) snapshotReplicationPeers(height uint64) []syncpipeline.Peer {
	if a == nil || a.node == nil || a.node.Host == nil {
		return nil
	}
	peers := a.node.Host.Network().Peers()
	if len(peers) == 0 {
		return nil
	}
	out := make([]syncpipeline.Peer, 0, len(peers))
	for _, pid := range peers {
		peerID := strings.TrimSpace(pid.String())
		if peerID == "" {
			continue
		}
		a.node.peerStateMu.Lock()
		validatorID := normalizeValidatorID(a.node.peerToValidator[peerID])
		peerRole := normalizeNodeRole(a.node.peerRole[peerID])
		a.node.peerStateMu.Unlock()
		isValidator := false
		if validatorID != "" {
			isValidator = true
			if height > 0 {
				isValidator = a.node.isValidatorInSetForHeight(validatorID, height) ||
					a.node.isValidatorInSetForHeight(validatorID, height+1)
			}
		}
		isArchival := peerRole == "full" || strings.EqualFold(peerRole, "archival")
		out = append(out, syncpipeline.Peer{
			ID:          peerID,
			IsValidator: isValidator,
			IsArchival:  isArchival,
			Score:       a.node.syncPeerScoreValue(peerID),
		})
	}
	return out
}

func (a *nodeSnapshotSyncAdapter) snapshotReplicationTargetIDs(height uint64, snapshotHash string) []string {
	peers := a.snapshotReplicationPeers(height)
	if len(peers) == 0 {
		return nil
	}
	selected := syncpipeline.SelectSnapshotReplicationPeers(
		peers,
		syncSnapshotReplicationMinCopies(),
		strings.TrimSpace(snapshotHash),
	)
	if len(selected) == 0 {
		return nil
	}
	out := make([]string, 0, len(selected))
	for _, peer := range selected {
		id := strings.TrimSpace(peer.ID)
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	if DebugSync || DebugConsensus {
		fmt.Printf("[SNAPSHOT-REPLICATION] policy=validator+archival+random target_count=%d targets=%v\n", len(out), out)
	}
	return out
}

func (n *Node) runEnterpriseSnapshotPipeline(targetHeight uint64, allowReapply bool) bool {
	if n == nil || n.Blockchain == nil || targetHeight == 0 {
		return false
	}
	localHeight := n.Blockchain.Height()
	if localHeight >= targetHeight {
		return true
	}
	minHeight := localHeight + 1
	if allowReapply {
		minHeight = localHeight
	}
	adapter := &nodeSnapshotSyncAdapter{
		node:      n,
		minHeight: minHeight,
	}
	manager := &syncpipeline.SnapshotManager{
		Meta:     adapter,
		Verifier: adapter,
		Chunks:   adapter,
		Applier:  adapter,
		PeerSource: func() []syncpipeline.Peer {
			return adapter.snapshotReplicationPeers(targetHeight)
		},
	}

	result, err := manager.StartSync(context.Background(), syncpipeline.SyncInput{
		LocalHeight:   localHeight,
		NetworkHeight: targetHeight,
	})
	if err != nil {
		if DebugSync || DebugConsensus {
			fmt.Printf("[SYNC-PIPELINE] enterprise_snapshot failed local=%d target=%d err=%v\n",
				localHeight, targetHeight, err)
		}
		return false
	}
	if !result.UsedSnapshot {
		return false
	}
	if DebugSync || DebugConsensus {
		fmt.Printf("[SYNC-PIPELINE] enterprise_snapshot applied snapshot_h=%d proofs=%d/%d delta=%d->%d\n",
			result.SnapshotHeight,
			result.ProofVotes,
			result.ProofRequired,
			result.DeltaReplayFrom,
			result.DeltaReplayTo,
		)
	}
	return n.Blockchain.Height() >= result.SnapshotHeight
}
