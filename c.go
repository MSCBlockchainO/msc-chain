package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// LastBlock implements the last block helper.
func (bc *Blockchain) LastBlock() Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if len(bc.Blocks) == 0 {
		return Block{}
	}
	return bc.Blocks[len(bc.Blocks)-1]
}

// SignBlock signs block.
func (n *Node) SignBlock(block *Block) {
	if block == nil {
		return
	}
	n.applyBlockQuorumPolicyMetadata(block)
	// `hash` stores the digest used to identify or verify the related data.
	hash := HashBlock(*block)
	block.BlockHash = hash
	if n == nil || !isValidatorSigningKeyUsable(n.ValidatorKey) {
		return
	}
	// Network node IDs and consensus validator IDs are separate for
	// auto-generated identities. Select the signer from the active validator
	// set so a fresh msc_* node can sign its VAL_* proposer block.
	signerID := n.localConsensusValidatorIDForHeight(block.ID)
	if signerID == "" {
		signerID = normalizeValidatorID(n.ValidatorKey.ID)
	}
	// `proposerID` stores the value produced by this operation.
	proposerID := normalizeValidatorID(block.Proposer)
	if proposerID == "" {
		proposerID = signerID
		block.Proposer = signerID
	}
	// Only proposer can sign proposer-auth block.
	if signerID == "" || proposerID != signerID {
		return
	}
	if len(n.ValidatorKey.PublicKey) == ed25519.PublicKeySize {
		validatorPubKeysMu.Lock()
		ValidatorPubKeys[signerID] = append(ed25519.PublicKey(nil), n.ValidatorKey.PublicKey...)
		validatorPubKeysMu.Unlock()
	}
	// `sig` and `ok` store whether the related condition is satisfied.
	sig, ok := n.signValidatorPayload([]byte(hash))
	if !ok {
		return
	}
	block.Signature = append([]byte(nil), sig...)
}

// applyBlockQuorumPolicyMetadata applies block quorum policy metadata.
func (n *Node) applyBlockQuorumPolicyMetadata(block *Block) {
	if n == nil || block == nil || block.ID == 0 {
		return
	}
	// `validators` and `ok` store whether the related condition is satisfied.
	validators, _, ok := n.deterministicCommitteeValidatorsForHeight(block.ID)
	if !ok || len(validators) == 0 {
		return
	}
	// `required` stores the request data being processed.
	required := execQuorumRequired(len(validators))
	block.ConsensusMode = "NORMAL"
	block.QuorumPolicyVersion = quorumPolicyVersionV1
	block.ActiveReadyCount = len(validators)
	block.RequiredQuorum = required
	block.StrictQuorum = required
}

// `executionStateRootVersionV1` defines the constant value used by this package.
const executionStateRootVersionV1 = "v1"

type executionParentLedgerContext struct {
	// `ParentHeight` stores the value associated with this record.
	ParentHeight uint64
	// `ParentSource` stores the value associated with this record.
	ParentSource string
	// `ParentHash` stores the digest used to identify or verify the related data.
	ParentHash string
	// `RuntimeLedgerHash` stores the digest used to identify or verify the related data.
	RuntimeLedgerHash string
	// `ExecutionLedgerHash` stores the digest used to identify or verify the related data.
	ExecutionLedgerHash string
}

type executionRootContext struct {
	// `ParentHeight` stores the value associated with this record.
	ParentHeight uint64
	// `ParentSource` stores the value associated with this record.
	ParentSource string
	// `ParentHash` stores the digest used to identify or verify the related data.
	ParentHash string
	// `ParentLedgerHash` stores the digest used to identify or verify the related data.
	ParentLedgerHash string
	// `RuntimeLedgerHash` stores the digest used to identify or verify the related data.
	RuntimeLedgerHash string
	// `ExecutionLedgerHash` stores the digest used to identify or verify the related data.
	ExecutionLedgerHash string
	// `RootVersion` stores the digest used to identify or verify the related data.
	RootVersion string
	// Execution commitments are produced by the Node-free engine and are the
	// only execution facts consumed by consensus verification.
	StateRoot       string
	ReceiptsRoot    string
	EventRoot       string
	ExecutionHash   string
	FeeRoot         string
	DTLStateRoot    string
	DTLReceiptsRoot string
}

func (ctx executionRootContext) commitments() ExecutionCommitments {
	return ExecutionCommitments{
		StateRoot:       ctx.StateRoot,
		ReceiptsRoot:    ctx.ReceiptsRoot,
		EventRoot:       ctx.EventRoot,
		ExecutionHash:   ctx.ExecutionHash,
		FeeRoot:         ctx.FeeRoot,
		DTLStateRoot:    ctx.DTLStateRoot,
		DTLReceiptsRoot: ctx.DTLReceiptsRoot,
	}
}

func executionRootDeferReason(ctx executionRootContext) string {
	source := strings.TrimSpace(ctx.ParentSource)
	switch source {
	case "parent_state_unavailable", "genesis_state_unavailable", "execution_snapshot_unavailable":
		return source
	}
	if strings.Contains(source, "unavailable") {
		return source
	}
	return ""
}

type ExecutionSandbox struct {
	// `Ledger` stores the value associated with this record.
	Ledger Ledger
}

// NewExecutionSandbox creates a new execution sandbox.
func NewExecutionSandbox(parent Ledger) *ExecutionSandbox {
	return &ExecutionSandbox{Ledger: parent.Clone()}
}

// ApplyBlock crosses the Node-free deterministic execution boundary.
func (s *ExecutionSandbox) ApplyBlock(input BlockExecutionInput) (BlockExecutionResult, error) {
	if s == nil {
		return BlockExecutionResult{}, errors.New("nil execution sandbox")
	}
	finishObservation := beginExecutionObservation(input.Block.ID)
	input.Context = newExecutionStateContext(s.Ledger)
	result, err := (DeterministicBlockExecutionEngine{}).ExecuteBlock(input)
	finishObservation(err)
	if err != nil {
		return BlockExecutionResult{}, err
	}
	s.Ledger = result.NextLedger.Clone()
	result.NextLedger = s.Ledger.Clone()
	return result, nil
}

// executionStateRootVersionForHeight implements the execution state root version for height helper.
func executionStateRootVersionForHeight(height uint64) string {
	_ = height
	return executionStateRootVersionV1
}

// ledgerHasInitializedBacking implements the ledger has initialized backing helper.
func ledgerHasInitializedBacking(ledger Ledger) bool {
	return ledger.Balances != nil ||
		ledger.Nonces != nil ||
		ledger.Stakes != nil ||
		ledger.ValidatorRewardWallets != nil ||
		ledger.DTL != nil ||
		ledger.UsedValidatorUpdateCerts != nil
}

// committedBlockForLedgerHeight implements the committed block for ledger height helper.
func (n *Node) committedBlockForLedgerHeight(height uint64) (Block, bool) {
	if n == nil || height == 0 {
		return Block{}, false
	}
	if n.Blockchain != nil {
		// `block` and `ok` store whether the related condition is satisfied.
		if block, ok := n.Blockchain.GetBlock(height); ok {
			return block, true
		}
	}
	return n.LoadBlock(int(height))
}

// executionSnapshotLedgerMatchesBlock implements the execution snapshot ledger matches block helper.
func (n *Node) executionSnapshotLedgerMatchesBlock(height uint64, ledger Ledger) (bool, string, string) {
	if !ledgerHasInitializedBacking(ledger) {
		return false, "", ""
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.committedBlockForLedgerHeight(height)
	if !ok || strings.TrimSpace(block.StateRoot) == "" {
		return true, "", strings.TrimSpace(HashLedger(ledger))
	}
	// `ledgerHash` stores the digest used to identify or verify the related data.
	ledgerHash := strings.TrimSpace(HashLedger(ledger))
	// `expectedRoot` stores the digest used to identify or verify the related data.
	expectedRoot := strings.TrimSpace(ComputeExecHashVersioned(block, ledgerHash, executionStateRootVersionForHeight(block.ID)))
	return expectedRoot != "" && strings.EqualFold(expectedRoot, strings.TrimSpace(block.StateRoot)), expectedRoot, ledgerHash
}

// evictExecutionSnapshotLedger implements the evict execution snapshot ledger helper.
func (n *Node) evictExecutionSnapshotLedger(height uint64) {
	if n == nil || height == 0 {
		return
	}
	n.snapshotExecutionLedgerMu.Lock()
	if n.snapshotExecutionLedgerByHeight != nil {
		delete(n.snapshotExecutionLedgerByHeight, height)
	}
	n.snapshotExecutionLedgerMu.Unlock()
}

// evictPostCommitLedger implements the evict post commit ledger helper.
func (n *Node) evictPostCommitLedger(height uint64) {
	if n == nil || height == 0 {
		return
	}
	n.postCommitLedgerMu.Lock()
	if n.postCommitLedgerByHeight != nil {
		delete(n.postCommitLedgerByHeight, height)
	}
	n.postCommitLedgerMu.Unlock()
}

// beginExecutionLedgerConsistencyCheck implements the begin execution ledger consistency check helper.
func (n *Node) beginExecutionLedgerConsistencyCheck(height uint64) (uint64, bool) {
	if n == nil || height == 0 {
		return 0, false
	}
	n.executionLedgerConsistencyMu.Lock()
	defer n.executionLedgerConsistencyMu.Unlock()
	// `generation` stores the value produced by this operation.
	generation := n.executionLedgerGeneration
	if n.executionLedgerConsistencyHeight == height &&
		n.executionLedgerConsistencyGeneration == generation {
		return generation, false
	}
	if n.executionLedgerConsistencyCheckingHeight == height &&
		n.executionLedgerConsistencyCheckingGeneration == generation {
		// The authoritative cache is safe to return while another caller
		// performs the one strict live-ledger reconciliation for this height.
		return generation, false
	}
	n.executionLedgerConsistencyCheckingHeight = height
	n.executionLedgerConsistencyCheckingGeneration = generation
	n.executionLedgerConsistencyChecks++
	return generation, true
}

// finishExecutionLedgerConsistencyCheck implements the finish execution ledger consistency check helper.
func (n *Node) finishExecutionLedgerConsistencyCheck(height uint64, generation uint64) {
	if n == nil || height == 0 {
		return
	}
	n.executionLedgerConsistencyMu.Lock()
	defer n.executionLedgerConsistencyMu.Unlock()
	if n.executionLedgerConsistencyCheckingHeight == height &&
		n.executionLedgerConsistencyCheckingGeneration == generation {
		n.executionLedgerConsistencyCheckingHeight = 0
		n.executionLedgerConsistencyCheckingGeneration = 0
	}
	if n.executionLedgerGeneration != generation {
		return
	}
	n.executionLedgerConsistencyHeight = height
	n.executionLedgerConsistencyGeneration = generation
}

// cancelExecutionLedgerConsistencyCheck implements the cancel execution ledger consistency check helper.
func (n *Node) cancelExecutionLedgerConsistencyCheck(height uint64, generation uint64) {
	if n == nil || height == 0 {
		return
	}
	n.executionLedgerConsistencyMu.Lock()
	if n.executionLedgerConsistencyCheckingHeight == height &&
		n.executionLedgerConsistencyCheckingGeneration == generation {
		n.executionLedgerConsistencyCheckingHeight = 0
		n.executionLedgerConsistencyCheckingGeneration = 0
	}
	n.executionLedgerConsistencyMu.Unlock()
}

// markExecutionLedgerConsistent implements the mark execution ledger consistent helper.
func (n *Node) markExecutionLedgerConsistent(height uint64) {
	if n == nil || height == 0 {
		return
	}
	n.executionLedgerConsistencyMu.Lock()
	n.executionLedgerConsistencyHeight = height
	n.executionLedgerConsistencyGeneration = n.executionLedgerGeneration
	n.executionLedgerConsistencyCheckingHeight = 0
	n.executionLedgerConsistencyCheckingGeneration = 0
	n.executionLedgerConsistencyMu.Unlock()
}

// blockExecutionLedgerRepair implements the block execution ledger repair helper.
func (n *Node) blockExecutionLedgerRepair(height uint64) {
	if n == nil || height == 0 {
		return
	}
	n.executionLedgerConsistencyMu.Lock()
	n.executionLedgerRepairBlockedHeight = height
	n.executionLedgerConsistencyMu.Unlock()
}

// executionLedgerRepairBlocked implements the execution ledger repair blocked helper.
func (n *Node) executionLedgerRepairBlocked(height uint64) bool {
	if n == nil || height == 0 {
		return false
	}
	n.executionLedgerConsistencyMu.Lock()
	defer n.executionLedgerConsistencyMu.Unlock()
	if n.executionLedgerRepairBlockedHeight != height {
		return false
	}
	// `nextHeight` stores the value produced by this operation.
	nextHeight := height
	if height < ^uint64(0) {
		nextHeight = height + 1
	}
	// `allowed` stores whether the related condition is satisfied.
	if allowed, _ := n.allowExecutionLedgerDriftRepair(nextHeight); allowed {
		n.executionLedgerRepairBlockedHeight = 0
		return false
	}
	return true
}

// currentExecutionLedgerFromAuthoritative returns current execution ledger from authoritative.
func (n *Node) currentExecutionLedgerFromAuthoritative(height uint64, authoritative Ledger, reason string) Ledger {
	if n == nil || height == 0 || !ledgerHasInitializedBacking(authoritative) {
		return authoritative
	}
	// `generation` and `shouldCheck` store the value produced by this operation.
	generation, shouldCheck := n.beginExecutionLedgerConsistencyCheck(height)
	if !shouldCheck {
		return authoritative
	}

	// `liveLedger` stores the value produced by this operation.
	liveLedger := n.ExecutionLedger.Clone()
	// `liveHash` stores the digest used to identify or verify the related data.
	liveHash := ""
	// `authoritativeHash` stores the digest used to identify or verify the related data.
	authoritativeHash := ""
	// `mismatch` stores the value produced by this operation.
	mismatch := !ledgerHasInitializedBacking(liveLedger)
	if !mismatch {
		liveHash = HashLedger(liveLedger)
		authoritativeHash = HashLedger(authoritative)
		mismatch = !strings.EqualFold(liveHash, authoritativeHash)
	}
	if mismatch {
		if liveHash == "" && ledgerHasInitializedBacking(liveLedger) {
			liveHash = HashLedger(liveLedger)
		}
		if authoritativeHash == "" {
			authoritativeHash = HashLedger(authoritative)
		}
		if n.executionLedgerRepairBlocked(height) {
			n.cancelExecutionLedgerConsistencyCheck(height, generation)
			if n.shouldLogLivenessReason(fmt.Sprintf("execution_ledger_authority_mismatch:%d", height), livenessReasonLogCooldown) {
				log.Printf("[LEDGER-DRIFT-CRITICAL] reason=%s height=%d mode=fail_closed repair_state=explicit_live_drift_lock live_ledger=%s authoritative_ledger=%s",
					strings.TrimSpace(reason),
					height,
					ShortHash(liveHash),
					ShortHash(authoritativeHash),
				)
			}
			return liveLedger
		}
		n.setExecutionLedger(authoritative)
		n.markExecutionLedgerConsistent(height)
		log.Printf("[LEDGER-REBUILD] reason=%s height=%d live_ledger=%s restored_ledger=%s",
			strings.TrimSpace(reason),
			height,
			ShortHash(liveHash),
			ShortHash(authoritativeHash),
		)
		return authoritative
	}
	n.finishExecutionLedgerConsistencyCheck(height, generation)
	return authoritative
}

// currentExecutionLedgerClone returns current execution ledger clone.
func (n *Node) currentExecutionLedgerClone() Ledger {
	if n == nil {
		return Ledger{}.Clone()
	}
	// `tipHeight` stores the value produced by this operation.
	tipHeight := uint64(0)
	if n.Blockchain != nil {
		tipHeight = n.Blockchain.Height()
	}
	n.commitMu.Lock()
	if n.committedHeight > tipHeight {
		tipHeight = n.committedHeight
	}
	n.commitMu.Unlock()
	// Blocks without a state root predate persisted execution authority. Their
	// in-process execution ledger is the only unambiguous current source and
	// must not be overwritten by an execution-stage cache.
	if tipHeight > 0 {
		_, hasPostCommitAuthority := n.cachedPostCommitLedger(tipHeight)
		if tip, ok := n.committedBlockForLedgerHeight(tipHeight); !hasPostCommitAuthority &&
			(!ok || strings.TrimSpace(tip.StateRoot) == "") &&
			ledgerHasInitializedBacking(n.ExecutionLedger) {
			cloned := n.ExecutionLedger.Clone()
			if !strings.EqualFold(HashLedger(n.Ledger.Clone()), HashLedger(cloned)) {
				n.Ledger = cloned.Clone()
			}
			return cloned
		}
	}
	if tipHeight > 0 {
		// `cachedPostCommitLedger` and `ok` store whether the related condition is satisfied.
		if cachedPostCommitLedger, ok := n.cachedPostCommitLedger(tipHeight); ok && ledgerHasInitializedBacking(cachedPostCommitLedger) {
			return n.currentExecutionLedgerFromAuthoritative(tipHeight, cachedPostCommitLedger, "current_execution_snapshot_preferred")
		}
		// `restored` and `ok` store whether the related condition is satisfied.
		if restored, ok := n.committedTipLedgerFromExecutionSnapshot(tipHeight); ok && ledgerHasInitializedBacking(restored) {
			return n.currentExecutionLedgerFromAuthoritative(tipHeight, restored, "current_execution_tip_replay")
		}
	}
	if ledgerHasInitializedBacking(n.ExecutionLedger) {
		// `cloned` stores the value produced by this operation.
		cloned := n.ExecutionLedger.Clone()
		if !strings.EqualFold(HashLedger(n.Ledger.Clone()), HashLedger(cloned)) {
			n.Ledger = cloned.Clone()
		}
		return cloned
	}
	if tipHeight > 0 {
		// `restored` and `ok` store whether the related condition is satisfied.
		if restored, ok := n.committedTipLedgerFromExecutionSnapshot(tipHeight); ok && ledgerHasInitializedBacking(restored) {
			n.setExecutionLedger(restored)
			return restored.Clone()
		}
		if n.shouldLogLivenessReason(fmt.Sprintf("execution_ledger_runtime_fallback:%d", tipHeight), livenessReasonLogCooldown) {
			log.Printf("[EXEC-LEDGER-FALLBACK] height=%d source=runtime runtime_ledger=%s",
				tipHeight,
				ShortHash(HashLedger(n.Ledger.Clone())),
			)
		}
	}
	return n.Ledger.Clone()
}

type recoveryRejoinGateStatus struct {
	Ready               bool
	Reason              string
	RuntimeLedgerHash   string
	ExecutionLedgerHash string
	StateRoot           string
	RegistryHash        string
	ParentHash          string
	TipHash             string
}

func (n *Node) recoveryVotingRejoinGate(height uint64) recoveryRejoinGateStatus {
	status := recoveryRejoinGateStatus{Ready: true, Reason: "verified"}
	if n == nil || height == 0 {
		status.Reason = "genesis_or_empty"
		return status
	}
	if n.Blockchain == nil {
		status.Ready = false
		status.Reason = "blockchain_unavailable"
		return status
	}
	localHeight := n.Blockchain.Height()
	if localHeight != height {
		status.Ready = false
		status.Reason = "last_applied_height_mismatch"
		return status
	}

	executionLedger := n.currentExecutionLedgerClone()
	runtimeLedger := n.Ledger.Clone()
	status.RuntimeLedgerHash = strings.TrimSpace(HashLedger(runtimeLedger))
	status.ExecutionLedgerHash = strings.TrimSpace(HashLedger(executionLedger))
	if status.RuntimeLedgerHash == "" || status.ExecutionLedgerHash == "" {
		status.Ready = false
		status.Reason = "ledger_hash_unavailable"
		return status
	}
	if !strings.EqualFold(status.RuntimeLedgerHash, status.ExecutionLedgerHash) {
		status.Ready = false
		status.Reason = "execution_ledger_divergence"
		return status
	}

	block, ok := n.LoadBlock(int(height))
	if !ok && n.Blockchain != nil {
		block, ok = n.Blockchain.GetBlock(height)
	}
	if !ok || block.ID != height {
		status.Ready = false
		status.Reason = "tip_block_unavailable"
		return status
	}
	status.StateRoot = strings.TrimSpace(block.StateRoot)
	status.RegistryHash = strings.TrimSpace(block.ValidatorRegistryHash)
	status.ParentHash = strings.TrimSpace(block.PrevHash)
	status.TipHash = strings.TrimSpace(block.BlockHash)
	if status.StateRoot == "" {
		status.Reason = "legacy_state_root_unavailable"
		return status
	}
	if status.TipHash == "" {
		status.Ready = false
		status.Reason = "tip_hash_missing"
		return status
	}
	if !recoveryStateRootLooksVerifiable(status.StateRoot) {
		status.Reason = "legacy_state_root_unverifiable"
		return status
	}

	expectedRoot, _, rootOK := n.executionStateRootForBlock(block)
	rootMatches := rootOK &&
		strings.TrimSpace(expectedRoot) != "" &&
		strings.EqualFold(strings.TrimSpace(expectedRoot), status.StateRoot)
	if !rootMatches {
		if cached, cachedOK := n.cachedExecutionSnapshotLedger(height); cachedOK {
			if matched, cachedRoot, _ := n.executionSnapshotLedgerMatchesBlock(height, cached); matched {
				expectedRoot = cachedRoot
				rootOK = true
				rootMatches = true
			}
		}
	}
	if !rootMatches {
		if snap, _, found := n.resolveTrustedExecutionSnapshotFromStorage(height); found && snap != nil &&
			strings.EqualFold(strings.TrimSpace(snap.BlockHash), status.TipHash) &&
			strings.EqualFold(strings.TrimSpace(snap.StateRoot), status.StateRoot) {
			if matched, trustedRoot, _ := n.executionSnapshotLedgerMatchesBlock(height, snap.Ledger); matched {
				expectedRoot = trustedRoot
				rootOK = true
				rootMatches = true
				n.cacheExecutionSnapshotLedger(height, snap.Ledger)
				n.markExecutionSnapshotReadyHeight(height)
			}
		}
	}
	if !rootOK || strings.TrimSpace(expectedRoot) == "" || !rootMatches {
		status.Ready = false
		status.Reason = "state_root_mismatch"
		return status
	}
	if expectedRegistry, _ := n.expectedValidatorRegistryHashWithSource(height + 1); strings.TrimSpace(expectedRegistry) != "" &&
		status.RegistryHash != "" &&
		!strings.EqualFold(strings.TrimSpace(expectedRegistry), status.RegistryHash) {
		status.Ready = false
		status.Reason = "registry_hash_mismatch"
		return status
	}
	return status
}

func recoveryStateRootLooksVerifiable(root string) bool {
	root = strings.TrimSpace(root)
	if len(root) != 64 {
		return false
	}
	for _, ch := range root {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
			continue
		}
		return false
	}
	return true
}

// executionLiveTipTrusted reports whether the current-process execution tip
// has an authority trail suitable for consensus execution.
func (n *Node) executionLiveTipTrusted(height uint64) bool {
	if n == nil || height == 0 {
		return false
	}
	block, blockOK := n.committedBlockForLedgerHeight(height)
	if !blockOK || strings.TrimSpace(block.StateRoot) == "" {
		return true
	}
	if _, ok := n.cachedPostCommitLedger(height); ok {
		return true
	}
	n.executionSnapshotRebuildMu.Lock()
	liveCommitHeight := n.executionSnapshotLiveCommitHeight
	n.executionSnapshotRebuildMu.Unlock()
	if liveCommitHeight >= height {
		return true
	}
	if _, _, ok := n.resolveTrustedExecutionSnapshotFromStorage(height); ok {
		return true
	}
	if n.executionLedgerRepairBlocked(height) {
		return false
	}
	if n.Blockchain != nil && n.Blockchain.Height() == height && ledgerHasInitializedBacking(n.ExecutionLedger) {
		runtimeHash := HashLedger(n.Ledger.Clone())
		executionHash := HashLedger(n.ExecutionLedger.Clone())
		if runtimeHash != "" && strings.EqualFold(strings.TrimSpace(runtimeHash), strings.TrimSpace(executionHash)) {
			parentLedger := n.ExecutionLedger.Clone()
			source := "durable_runtime_execution"
			expectedRoot := ComputeExecHashVersioned(block, HashLedger(parentLedger), executionStateRootVersionForHeight(block.ID))
			if expectedRoot != "" && strings.EqualFold(strings.TrimSpace(expectedRoot), strings.TrimSpace(block.StateRoot)) {
				parentLedger = n.replayPostBlockEffectsToLedger(block, parentLedger)
				n.setExecutionLedger(parentLedger)
				source = "durable_runtime_execution_post_effects"
			}
			n.cachePostCommitLedger(height, parentLedger)
			if n.shouldLogLivenessReason(fmt.Sprintf("execution_live_tip_trusted:%d", height), livenessReasonLogCooldown) {
				log.Printf("[EXEC-LIVE-TIP-TRUST] height=%d source=%s ledger=%s",
					height,
					source,
					ShortHash(HashLedger(parentLedger)),
				)
			}
			return true
		}
	}
	return false
}

// committedExecutionLedgerForPostBlockEffects implements the committed execution ledger for post block effects helper.
func (n *Node) committedExecutionLedgerForPostBlockEffects(block Block) Ledger {
	if n == nil {
		return Ledger{}.Clone()
	}
	// `matchesCommittedRoot` stores the digest used to identify or verify the related data.
	matchesCommittedRoot := func(ledger Ledger) bool {
		if !ledgerHasInitializedBacking(ledger) {
			return false
		}
		if block.ID == 0 || strings.TrimSpace(block.StateRoot) == "" {
			return true
		}
		// `expectedRoot` stores the digest used to identify or verify the related data.
		expectedRoot := ComputeExecHashVersioned(block, HashLedger(ledger), executionStateRootVersionForHeight(block.ID))
		return expectedRoot != "" && strings.EqualFold(strings.TrimSpace(expectedRoot), strings.TrimSpace(block.StateRoot))
	}

	// `cachedLedger` and `ok` store whether the related condition is satisfied.
	if cachedLedger, ok := n.cachedExecutionSnapshotLedger(block.ID); ok && matchesCommittedRoot(cachedLedger) {
		return cachedLedger.Clone()
	}
	// `snap` and `ok` store whether the related condition is satisfied.
	if snap, _, ok := n.resolveTrustedExecutionSnapshotFromStorage(block.ID); ok && snap != nil && matchesCommittedRoot(snap.Ledger) {
		return snap.Ledger.Clone()
	}
	if matchesCommittedRoot(n.ExecutionLedger) {
		return n.ExecutionLedger.Clone()
	}
	if matchesCommittedRoot(n.Ledger) {
		return n.Ledger.Clone()
	}

	// `fallback` stores the value produced by this operation.
	fallback := n.currentExecutionLedgerClone()
	if !matchesCommittedRoot(fallback) && n.shouldLogLivenessReason(fmt.Sprintf("post_block_effects_execution_snapshot_unmatched:%d", block.ID), livenessReasonLogCooldown) {
		log.Printf("[LEDGER-REBUILD] reason=post_block_effects_execution_snapshot_unmatched height=%d fallback_ledger=%s block_root=%s",
			block.ID,
			ShortHash(HashLedger(fallback)),
			ShortHash(block.StateRoot),
		)
	}
	return fallback.Clone()
}

// committedTipLedgerFromExecutionSnapshot implements the committed tip ledger from execution snapshot helper.
func (n *Node) committedTipLedgerFromExecutionSnapshot(height uint64) (Ledger, bool) {
	if n == nil || height == 0 {
		return Ledger{}, false
	}
	var (
		// `authoritative` stores the value used by this operation.
		authoritative Ledger
		// `ok` stores whether the related condition is satisfied.
		ok bool
	)
	// `cachedPostCommitLedger` and `found` store whether the related condition is satisfied.
	if cachedPostCommitLedger, found := n.cachedPostCommitLedger(height); found {
		return cachedPostCommitLedger.Clone(), true
	}
	// `cachedLedger` and `found` store whether the related condition is satisfied.
	if cachedLedger, found := n.cachedExecutionSnapshotLedger(height); found {
		// `matched`, `expectedRoot`, and `ledgerHash` store the digest used to identify or verify the related data.
		if matched, expectedRoot, ledgerHash := n.executionSnapshotLedgerMatchesBlock(height, cachedLedger); matched {
			authoritative = cachedLedger
			ok = true
		} else {
			n.evictExecutionSnapshotLedger(height)
			n.evictPostCommitLedger(height)
			if n.shouldLogLivenessReason(fmt.Sprintf("execution_snapshot_cache_mismatch:%d", height), livenessReasonLogCooldown) {
				// `block` stores the synchronization state protecting shared data.
				block, _ := n.committedBlockForLedgerHeight(height)
				log.Printf("[LEDGER-REBUILD-EVICT] reason=execution_snapshot_cache_mismatch height=%d snapshot_ledger=%s expected_root=%s block_root=%s",
					height,
					ShortHash(ledgerHash),
					ShortHash(expectedRoot),
					ShortHash(block.StateRoot),
				)
			}
		}
	}
	if !ok {
		// `snap` and `found` store whether the related condition is satisfied.
		if snap, _, found := n.resolveTrustedExecutionSnapshotFromStorage(height); found && snap != nil {
			// `matched`, `expectedRoot`, and `ledgerHash` store the digest used to identify or verify the related data.
			if matched, expectedRoot, ledgerHash := n.executionSnapshotLedgerMatchesBlock(height, snap.Ledger); matched {
				authoritative = snap.Ledger.Clone()
				ok = true
				n.cacheExecutionSnapshotLedger(height, authoritative)
			} else if n.shouldLogLivenessReason(fmt.Sprintf("execution_snapshot_storage_mismatch:%d", height), livenessReasonLogCooldown) {
				// `block` stores the synchronization state protecting shared data.
				block, _ := n.committedBlockForLedgerHeight(height)
				log.Printf("[LEDGER-REBUILD-REJECT] reason=execution_snapshot_storage_mismatch height=%d snapshot_ledger=%s expected_root=%s block_root=%s",
					height,
					ShortHash(ledgerHash),
					ShortHash(expectedRoot),
					ShortHash(block.StateRoot),
				)
			}
		}
	}
	if !ok || !ledgerHasInitializedBacking(authoritative) {
		return Ledger{}, false
	}
	if n.Blockchain == nil {
		return authoritative.Clone(), true
	}
	// `block` and `found` store whether the related condition is satisfied.
	block, found := n.Blockchain.GetBlock(height)
	if !found || strings.TrimSpace(block.StateRoot) == "" {
		return authoritative.Clone(), true
	}
	// `restored` stores the result produced by this operation.
	restored := n.replayPostBlockEffectsToLedger(block, authoritative)
	n.cachePostCommitLedger(height, restored)
	return restored.Clone(), true
}

// cachePostCommitLedger implements the cache post commit ledger helper.
func (n *Node) cachePostCommitLedger(height uint64, ledger Ledger) {
	if n == nil || height == 0 {
		return
	}
	n.postCommitLedgerMu.Lock()
	defer n.postCommitLedgerMu.Unlock()
	if n.postCommitLedgerByHeight == nil {
		n.postCommitLedgerByHeight = make(map[uint64]Ledger)
	}
	n.postCommitLedgerByHeight[height] = ledger.Clone()
	// `cacheDepth` stores the value produced by this operation.
	cacheDepth := n.ledgerMemoryCacheDepth()
	// `removed` stores the value produced by this operation.
	removed := 0
	// `h` tracks the current values while iterating.
	for h := range n.postCommitLedgerByHeight {
		if h+cacheDepth <= height {
			delete(n.postCommitLedgerByHeight, h)
			removed++
		}
	}
	maybeReleaseMemoryAfterLedgerCachePrune(removed, height)
}

// cachedPostCommitLedger implements the cached post commit ledger helper.
func (n *Node) cachedPostCommitLedger(height uint64) (Ledger, bool) {
	if n == nil || height == 0 {
		return Ledger{}, false
	}
	n.postCommitLedgerMu.Lock()
	defer n.postCommitLedgerMu.Unlock()
	if n.postCommitLedgerByHeight == nil {
		return Ledger{}, false
	}
	// `ledger` and `ok` store whether the related condition is satisfied.
	ledger, ok := n.postCommitLedgerByHeight[height]
	if !ok {
		return Ledger{}, false
	}
	return ledger.Clone(), true
}

// setExecutionLedger implements the set execution ledger helper.
func (n *Node) setExecutionLedger(ledger Ledger) {
	if n == nil {
		return
	}
	// `cloned` stores the value produced by this operation.
	cloned := ledger.Clone()
	n.ExecutionLedger = cloned
	n.Ledger = cloned.Clone()
	n.executionLedgerConsistencyMu.Lock()
	n.executionLedgerGeneration++
	n.executionLedgerConsistencyHeight = 0
	n.executionLedgerConsistencyGeneration = 0
	n.executionLedgerRepairBlockedHeight = 0
	n.executionLedgerConsistencyMu.Unlock()
}

// mutateAuthoritativeLedger implements the mutate authoritative ledger helper.
func (n *Node) mutateAuthoritativeLedger(mutator func(*Ledger)) Ledger {
	if n == nil {
		return Ledger{}.Clone()
	}
	// `ledger` stores the value produced by this operation.
	ledger := n.currentExecutionLedgerClone()
	if mutator != nil {
		mutator(&ledger)
	}
	n.setExecutionLedger(ledger)
	return ledger.Clone()
}

// observeConsensusExecutionDrift implements the observe consensus execution drift helper.
func (n *Node) observeConsensusExecutionDrift(height uint64, reason string, parentLedgerHash string, runtimeLedgerHash string, executionLedgerHash string) {
	if n == nil {
		return
	}
	if strings.EqualFold(strings.TrimSpace(parentLedgerHash), strings.TrimSpace(runtimeLedgerHash)) &&
		strings.EqualFold(strings.TrimSpace(parentLedgerHash), strings.TrimSpace(executionLedgerHash)) {
		return
	}
	log.Printf("[EXEC-CHECK] height=%d reason=%s parent_ledger=%s runtime_ledger=%s execution_ledger=%s",
		height,
		reason,
		ShortHash(parentLedgerHash),
		ShortHash(runtimeLedgerHash),
		ShortHash(executionLedgerHash),
	)
}

// resetRuntimeLedgerToExecution implements the reset runtime ledger to execution helper.
func (n *Node) resetRuntimeLedgerToExecution(reason string, height uint64) bool {
	if n == nil {
		return false
	}
	// `execLedger` stores the value produced by this operation.
	execLedger := n.currentExecutionLedgerClone()
	if !ledgerHasInitializedBacking(execLedger) {
		return false
	}
	// `runtimeHash` stores the digest used to identify or verify the related data.
	runtimeHash := HashLedger(n.Ledger.Clone())
	// `execHash` stores the digest used to identify or verify the related data.
	execHash := HashLedger(execLedger)
	if strings.EqualFold(runtimeHash, execHash) {
		return false
	}
	n.setExecutionLedger(execLedger)
	log.Printf("[LEDGER-RESET] reason=%s height=%d runtime_ledger=%s execution_ledger=%s",
		reason,
		height,
		ShortHash(runtimeHash),
		ShortHash(execHash),
	)
	return true
}

// executionRestoreReasonTrustsCommittedTipLedger reports whether recovery is
// correcting a live committed tip instead of rebuilding historical post-effects.
func executionRestoreReasonTrustsCommittedTipLedger(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return false
	}
	return strings.Contains(reason, "autoheal_") ||
		strings.Contains(reason, "consensus_detector_halted") ||
		strings.Contains(reason, "height_stuck") ||
		strings.Contains(reason, "snapshot_sync") ||
		strings.Contains(reason, "execution_snapshot_rebuild_failed")
}

// restoreLedgersFromAuthoritativeExecution implements the restore ledgers from authoritative execution helper.
func (n *Node) restoreLedgersFromAuthoritativeExecution(height uint64, reason string) bool {
	if n == nil || height == 0 {
		return false
	}
	// `runtimeHash` stores the digest used to identify or verify the related data.
	runtimeHash := HashLedger(n.Ledger.Clone())
	// `executionHash` stores the digest used to identify or verify the related data.
	executionHash := HashLedger(n.currentExecutionLedgerClone())
	var (
		// `authoritative` stores the value used by this operation.
		authoritative Ledger
		// `source` stores the value used by this operation.
		source string
		// `ok` stores whether the related condition is satisfied.
		ok bool
	)
	// `loadAuthoritative` stores the value produced by this operation.
	loadAuthoritative := func() bool {
		// `cachedLedger` and `found` store whether the related condition is satisfied.
		if cachedLedger, found := n.cachedExecutionSnapshotLedger(height); found {
			// `matched`, `expectedRoot`, and `ledgerHash` store the digest used to identify or verify the related data.
			matched, expectedRoot, ledgerHash := n.executionSnapshotLedgerMatchesBlock(height, cachedLedger)
			if matched {
				authoritative = cachedLedger
				source = "execution_cache"
				ok = true
				return true
			}
			n.evictExecutionSnapshotLedger(height)
			n.evictPostCommitLedger(height)
			if n.shouldLogLivenessReason(fmt.Sprintf("restore_execution_snapshot_cache_mismatch:%d", height), livenessReasonLogCooldown) {
				// `block` stores the synchronization state protecting shared data.
				block, _ := n.committedBlockForLedgerHeight(height)
				log.Printf("[LEDGER-REBUILD-EVICT] reason=execution_snapshot_cache_mismatch height=%d source=execution_cache snapshot_ledger=%s expected_root=%s block_root=%s",
					height,
					ShortHash(ledgerHash),
					ShortHash(expectedRoot),
					ShortHash(block.StateRoot),
				)
			}
		}
		// `snap` and `found` store whether the related condition is satisfied.
		if snap, _, found := n.resolveTrustedExecutionSnapshotFromStorage(height); found && snap != nil {
			// `matched`, `expectedRoot`, and `ledgerHash` store the digest used to identify or verify the related data.
			matched, expectedRoot, ledgerHash := n.executionSnapshotLedgerMatchesBlock(height, snap.Ledger)
			if matched {
				authoritative = snap.Ledger.Clone()
				source = "trusted_snapshot"
				ok = true
				n.cacheExecutionSnapshotLedger(height, authoritative)
				return true
			}
			if n.shouldLogLivenessReason(fmt.Sprintf("restore_execution_snapshot_storage_mismatch:%d", height), livenessReasonLogCooldown) {
				// `block` stores the synchronization state protecting shared data.
				block, _ := n.committedBlockForLedgerHeight(height)
				log.Printf("[LEDGER-REBUILD-REJECT] reason=execution_snapshot_storage_mismatch height=%d source=trusted_snapshot snapshot_ledger=%s expected_root=%s block_root=%s",
					height,
					ShortHash(ledgerHash),
					ShortHash(expectedRoot),
					ShortHash(block.StateRoot),
				)
			}
		}
		return false
	}
	loadAuthoritative()
	if !ok && n.startupExecutionSnapshotCanRebuildLocally(height) {
		// `err` stores the error produced by this operation.
		if err := n.rebuildTrustedExecutionSnapshotsUpTo(height); err != nil {
			if n.shouldLogLivenessReason(fmt.Sprintf("restore_execution_snapshot_rebuild_failed:%d:%s", height, err.Error()), livenessReasonLogCooldown) {
				log.Printf("[LEDGER-REBUILD-REJECT] reason=execution_snapshot_rebuild_failed height=%d err=%v", height, err)
			}
			if startupExecutionSnapshotFailureNeedsNetworkRecovery(err.Error()) ||
				syncRecoveryReasonNeedsTrustedStateRepair(reason) {
				go n.forceSnapshotResyncNow(height, "execution_snapshot_rebuild_failed_"+strings.TrimSpace(reason))
			}
		} else {
			loadAuthoritative()
		}
	}
	if !ok || !ledgerHasInitializedBacking(authoritative) {
		return false
	}
	// `authoritativeHash` stores the digest used to identify or verify the related data.
	authoritativeHash := HashLedger(authoritative)
	// `restored` stores the result produced by this operation.
	restored := authoritative.Clone()
	// `restoredHash` stores the digest used to identify or verify the related data.
	restoredHash := authoritativeHash
	trustCommittedTipLedger := executionRestoreReasonTrustsCommittedTipLedger(reason)
	if trustCommittedTipLedger {
		n.evictPostCommitLedger(height)
		source += "_committed_tip"
	}
	if n.Blockchain != nil && !trustCommittedTipLedger && !strings.Contains(strings.ToLower(strings.TrimSpace(reason)), "corrupt_trie") {
		// `block` and `found` store whether the related condition is satisfied.
		if block, found := n.Blockchain.GetBlock(height); found && strings.TrimSpace(block.StateRoot) != "" {
			restored = n.replayPostBlockEffectsToLedger(block, authoritative)
			restoredHash = HashLedger(restored)
			source += "_post_effects"
		}
	}
	n.cachePostCommitLedger(height, restored)
	n.setExecutionLedger(restored)
	log.Printf("[LEDGER-REBUILD] reason=%s height=%d source=%s runtime_ledger=%s execution_ledger=%s authoritative_ledger=%s restored_ledger=%s",
		reason,
		height,
		source,
		ShortHash(runtimeHash),
		ShortHash(executionHash),
		ShortHash(authoritativeHash),
		ShortHash(restoredHash),
	)
	return true
}

// authoritativeExecutionSnapshotLedger implements the authoritative execution snapshot ledger helper.
func (n *Node) authoritativeExecutionSnapshotLedger(height uint64) (Ledger, string, bool) {
	if n == nil || height == 0 {
		return Ledger{}, "", false
	}
	// `cachedLedger` and `found` store whether the related condition is satisfied.
	if cachedLedger, found := n.cachedExecutionSnapshotLedger(height); found && ledgerHasInitializedBacking(cachedLedger) {
		// `matched` stores the value produced by this operation.
		if matched, _, _ := n.executionSnapshotLedgerMatchesBlock(height, cachedLedger); matched {
			return cachedLedger.Clone(), "execution_cache", true
		}
		n.evictExecutionSnapshotLedger(height)
		n.evictPostCommitLedger(height)
	}
	// `snap` and `found` store whether the related condition is satisfied.
	if snap, _, found := n.resolveTrustedExecutionSnapshotFromStorage(height); found && snap != nil && ledgerHasInitializedBacking(snap.Ledger) {
		// `matched` stores the value produced by this operation.
		if matched, _, _ := n.executionSnapshotLedgerMatchesBlock(height, snap.Ledger); matched {
			return snap.Ledger.Clone(), "trusted_snapshot", true
		}
	}
	return Ledger{}, "", false
}

// legacyExecutionSnapshotStateRootForBlock implements the legacy execution snapshot state root for block helper.
func (n *Node) legacyExecutionSnapshotStateRootForBlock(block Block, reason string) (string, executionRootContext, bool) {
	// `ctx` stores the context controlling this operation.
	ctx := executionRootContext{RootVersion: executionStateRootVersionForHeight(block.ID)}
	if n == nil || block.ID <= 1 {
		return "", ctx, false
	}
	// `parentHeight` stores the value produced by this operation.
	parentHeight := block.ID - 1
	// `parentLedger`, `source`, and `ok` store whether the related condition is satisfied.
	parentLedger, source, ok := n.authoritativeExecutionSnapshotLedger(parentHeight)
	if !ok {
		ctx.ParentSource = "execution_snapshot_unavailable"
		return "", ctx, false
	}
	ctx.ParentHeight = parentHeight
	ctx.ParentSource = source + "_legacy_parent"
	ctx.ParentLedgerHash = HashLedger(parentLedger)
	ctx.RuntimeLedgerHash = HashLedger(n.Ledger.Clone())
	ctx.ExecutionLedgerHash = HashLedger(n.currentExecutionLedgerClone())
	// `parentBlock` and `found` store whether the related condition is satisfied.
	if parentBlock, found := n.LoadBlock(int(parentHeight)); found {
		ctx.ParentHash = strings.TrimSpace(parentBlock.BlockHash)
		if strings.TrimSpace(block.PrevHash) != "" && ctx.ParentHash != "" &&
			!strings.EqualFold(strings.TrimSpace(block.PrevHash), ctx.ParentHash) {
			ctx.ParentSource = "legacy_parent_hash_mismatch"
			return "", ctx, false
		}
	} else if n.Blockchain != nil {
		// `parentBlock` and `found` store whether the related condition is satisfied.
		if parentBlock, found := n.Blockchain.GetBlock(parentHeight); found {
			ctx.ParentHash = strings.TrimSpace(parentBlock.BlockHash)
			if strings.TrimSpace(block.PrevHash) != "" && ctx.ParentHash != "" &&
				!strings.EqualFold(strings.TrimSpace(block.PrevHash), ctx.ParentHash) {
				ctx.ParentSource = "legacy_parent_hash_mismatch"
				return "", ctx, false
			}
		}
	}

	// `sandbox` stores the value produced by this operation.
	sandbox := NewExecutionSandbox(parentLedger)
	// `newLedger` and `err` store the error produced by this operation.
	executionResult, err := sandbox.ApplyBlock(BlockExecutionInput{
		Block:     block,
		Authority: n.prepareBlockExecutionAuthority(block),
	})
	if err != nil {
		ctx.ParentSource = "legacy_parent_apply_failed"
		return "", ctx, false
	}
	// `expectedRoot` stores the digest used to identify or verify the related data.
	expectedRoot := executionResult.Commitments.StateRoot
	ctx.StateRoot = executionResult.Commitments.StateRoot
	ctx.ReceiptsRoot = executionResult.Commitments.ReceiptsRoot
	ctx.EventRoot = executionResult.Commitments.EventRoot
	ctx.ExecutionHash = executionResult.Commitments.ExecutionHash
	ctx.FeeRoot = executionResult.Commitments.FeeRoot
	ctx.DTLStateRoot = executionResult.Commitments.DTLStateRoot
	ctx.DTLReceiptsRoot = executionResult.Commitments.DTLReceiptsRoot
	if expectedRoot == "" || !strings.EqualFold(strings.TrimSpace(block.StateRoot), strings.TrimSpace(expectedRoot)) {
		return expectedRoot, ctx, false
	}
	// The committed chain may contain blocks sealed before post-commit reward
	// replay completed on the proposer. Cache this verified parent choice so the
	// immediately following ProcessBlock execution uses the same deterministic
	// parent ledger that passed state-root verification.
	n.cachePostCommitLedger(parentHeight, parentLedger)
	n.setExecutionLedger(parentLedger)
	if n.shouldLogLivenessReason(fmt.Sprintf("verify_state_root_legacy_parent:%d:%s", block.ID, strings.TrimSpace(block.BlockHash)), livenessReasonLogCooldown) {
		log.Printf("[VERIFY-STATE-ROOT-LEGACY-PARENT] height=%d reason=%s block=%s parent_height=%d parent_source=%s parent_ledger=%s expected_root=%s block_root=%s",
			block.ID,
			strings.TrimSpace(reason),
			ShortHash(block.BlockHash),
			parentHeight,
			ctx.ParentSource,
			ShortHash(ctx.ParentLedgerHash),
			ShortHash(expectedRoot),
			ShortHash(block.StateRoot),
		)
	}
	return expectedRoot, ctx, true
}

// snapshotAnchorBlockForLedgerReplay implements the snapshot anchor block for ledger replay helper.
func (n *Node) snapshotAnchorBlockForLedgerReplay(snapshot StateSnapshot) (Block, bool) {
	if n == nil || snapshot.Height == 0 {
		return Block{}, false
	}
	// `matches` stores the value produced by this operation.
	matches := func(block Block) bool {
		if block.ID != snapshot.Height {
			return false
		}
		if strings.TrimSpace(block.BlockHash) == "" || strings.TrimSpace(block.StateRoot) == "" {
			return false
		}
		if strings.TrimSpace(snapshot.BlockHash) != "" &&
			!strings.EqualFold(strings.TrimSpace(block.BlockHash), strings.TrimSpace(snapshot.BlockHash)) {
			return false
		}
		return true
	}
	// `block` and `ok` store whether the related condition is satisfied.
	if block, ok := n.LoadBlock(int(snapshot.Height)); ok && matches(block) {
		return block, true
	}
	if n.Blockchain != nil {
		// `block` and `ok` store whether the related condition is satisfied.
		if block, ok := n.Blockchain.GetBlock(snapshot.Height); ok && matches(block) {
			return block, true
		}
	}
	return Block{}, false
}

// applySnapshotExecutionTipLedger applies snapshot execution tip ledger.
func (n *Node) applySnapshotExecutionTipLedger(snapshot StateSnapshot, reason string) Ledger {
	if n == nil || snapshot.Height == 0 {
		return snapshot.Ledger.Clone()
	}
	// `executionLedger` stores the value produced by this operation.
	executionLedger := snapshot.Ledger.Clone()
	n.cacheExecutionSnapshotLedger(snapshot.Height, executionLedger)
	n.markExecutionSnapshotReadyHeight(snapshot.Height)

	// `resumeLedger` stores the result produced by this operation.
	resumeLedger := executionLedger.Clone()
	// `source` stores the value produced by this operation.
	source := "snapshot_execution"
	// `block` and `ok` store whether the related condition is satisfied.
	if block, ok := n.snapshotAnchorBlockForLedgerReplay(snapshot); ok {
		// `executionHash` stores the digest used to identify or verify the related data.
		executionHash := HashLedger(executionLedger)
		// `expectedRoot` stores the digest used to identify or verify the related data.
		expectedRoot := ComputeExecHashVersioned(block, executionHash, executionStateRootVersionForHeight(block.ID))
		if strings.EqualFold(strings.TrimSpace(expectedRoot), strings.TrimSpace(block.StateRoot)) {
			resumeLedger = n.startupExecutionParentLedgerAfterBlock(block, executionLedger, snapshot.Height+1)
			n.cachePostCommitLedger(snapshot.Height, resumeLedger)
			source = "snapshot_post_commit"
		} else if n.shouldLogLivenessReason(fmt.Sprintf("snapshot_execution_root_mismatch:%d:%s", snapshot.Height, reason), livenessReasonLogCooldown) {
			log.Printf("[LEDGER-REBUILD-REJECT] reason=snapshot_execution_root_mismatch height=%d source=%s snapshot_ledger=%s expected_root=%s block_root=%s",
				snapshot.Height,
				strings.TrimSpace(reason),
				ShortHash(executionHash),
				ShortHash(expectedRoot),
				ShortHash(block.StateRoot),
			)
		}
	}
	n.setExecutionLedger(resumeLedger)
	if n.shouldLogLivenessReason(fmt.Sprintf("snapshot_execution_tip:%d:%s", snapshot.Height, reason), livenessReasonLogCooldown) {
		log.Printf("[LEDGER-REBUILD] reason=%s height=%d source=%s execution_ledger=%s resume_ledger=%s",
			strings.TrimSpace(reason),
			snapshot.Height,
			source,
			ShortHash(HashLedger(executionLedger)),
			ShortHash(HashLedger(resumeLedger)),
		)
	}
	return resumeLedger.Clone()
}

// advanceConsensusToCommittedTip implements the advance consensus to committed tip helper.
func (n *Node) advanceConsensusToCommittedTip(reason string) bool {
	if n == nil || n.isShuttingDown() {
		return false
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "committed_tip"
	}

	// `committedHeight` stores the value produced by this operation.
	committedHeight := uint64(0)
	if n.Blockchain != nil {
		committedHeight = n.Blockchain.Height()
	}

	n.commitMu.Lock()
	if n.committedHeight > committedHeight {
		committedHeight = n.committedHeight
	}
	if committedHeight > n.committedHeight {
		n.committedHeight = committedHeight
	}
	if committedHeight > 0 && (n.lastCommitHeight < committedHeight || n.lastCommitAt.IsZero()) {
		n.lastCommitHeight = committedHeight
		n.lastCommitAt = time.Now()
	}
	n.commitMu.Unlock()

	if committedHeight == 0 {
		return false
	}

	if !n.restoreLedgersFromAuthoritativeExecution(committedHeight, reason) {
		// `repaired` stores the value produced by this operation.
		if _, repaired := n.ensureCommittedTipStateSnapshot(committedHeight, reason); repaired {
			_ = n.restoreLedgersFromAuthoritativeExecution(committedHeight, reason)
		}
	}
	// `parentLedger` stores the value produced by this operation.
	parentLedger := n.currentExecutionLedgerClone()
	n.markConsensusCommittedHeight(committedHeight)

	n.clearAcceptedProposal(committedHeight)
	n.clearLeaderBlock(committedHeight)
	n.requestHeartbeatBroadcast(true)

	if !ResultGossipOnly {
		if n.Consensus != nil {
			n.Consensus.FinalizeHeight(committedHeight)
		}
		return true
	}

	n.startNextRoundImmediatelyWithReason(committedHeight+1, parentLedger, "committed_tip")
	return true
}

// allowExecutionLedgerDriftRepair implements the allow execution ledger drift repair helper.
func (n *Node) allowExecutionLedgerDriftRepair(height uint64) (bool, string) {
	if n == nil {
		return false, "node_unavailable"
	}
	if n.consensusRecomputePauseActive() {
		return true, "recompute_pause"
	}
	if n.Consensus != nil {
		n.Consensus.mu.Lock()
		// `syncing` stores the value produced by this operation.
		syncing := n.Consensus.Syncing
		// `paused` stores the value produced by this operation.
		paused := n.Consensus.Paused
		n.Consensus.mu.Unlock()
		if syncing {
			return true, "syncing"
		}
		if paused {
			return true, "consensus_paused"
		}
	}
	if strings.TrimSpace(n.Role) != "validator" {
		return true, "non_validator"
	}
	// `currentEpoch` stores the value produced by this operation.
	currentEpoch := n.currentEpoch()
	if height > 0 && height >= currentEpoch {
		return false, "validator_live_consensus"
	}
	return true, "historical_replay"
}

// enforceRuntimeLedgerMatchesExecution implements the enforce runtime ledger matches execution helper.
func (n *Node) enforceRuntimeLedgerMatchesExecution(height uint64, ctx *executionRootContext) bool {
	if n == nil || ctx == nil {
		return false
	}
	// `runtimeHash` stores the digest used to identify or verify the related data.
	runtimeHash := strings.TrimSpace(ctx.RuntimeLedgerHash)
	// `executionHash` stores the digest used to identify or verify the related data.
	executionHash := strings.TrimSpace(ctx.ExecutionLedgerHash)
	if runtimeHash == "" || executionHash == "" || strings.EqualFold(runtimeHash, executionHash) {
		return true
	}
	// `allowed` and `mode` store whether the related condition is satisfied.
	if allowed, mode := n.allowExecutionLedgerDriftRepair(height); !allowed {
		n.blockExecutionLedgerRepair(ctx.ParentHeight)
		log.Printf("[LEDGER-DRIFT-CRITICAL] reason=before_execution_drift height=%d mode=fail_closed repair_state=%s parent_height=%d runtime_ledger=%s execution_ledger=%s",
			height,
			mode,
			ctx.ParentHeight,
			ShortHash(runtimeHash),
			ShortHash(executionHash),
		)
		return false
	}
	if ctx.ParentHeight > 0 {
		if !n.restoreLedgersFromAuthoritativeExecution(ctx.ParentHeight, "before_execution_drift") {
			return false
		}
	} else if !n.resetRuntimeLedgerToExecution("before_execution_drift", height) {
		return false
	}
	ctx.RuntimeLedgerHash = HashLedger(n.Ledger.Clone())
	ctx.ExecutionLedgerHash = HashLedger(n.currentExecutionLedgerClone())
	return strings.EqualFold(strings.TrimSpace(ctx.RuntimeLedgerHash), strings.TrimSpace(ctx.ExecutionLedgerHash))
}

// recordLocalExecutionMismatch implements the record local execution mismatch helper.
func (n *Node) recordLocalExecutionMismatch(height uint64, blockHash string) int {
	if n == nil {
		return 0
	}
	n.localExecMismatchMu.Lock()
	defer n.localExecMismatchMu.Unlock()
	blockHash = strings.TrimSpace(blockHash)
	if n.localExecMismatchHeight != height || !strings.EqualFold(strings.TrimSpace(n.localExecMismatchBlockHash), blockHash) {
		n.localExecMismatchCount = 0
	}
	n.localExecMismatchHeight = height
	n.localExecMismatchBlockHash = blockHash
	n.localExecMismatchCount++
	return n.localExecMismatchCount
}

// clearLocalExecutionMismatch implements the clear local execution mismatch helper.
func (n *Node) clearLocalExecutionMismatch() {
	if n == nil {
		return
	}
	n.localExecMismatchMu.Lock()
	n.localExecMismatchCount = 0
	n.localExecMismatchHeight = 0
	n.localExecMismatchBlockHash = ""
	n.localExecMismatchMu.Unlock()
}

// applyLocalExecutionSafetyLock applies local execution safety lock.
func (n *Node) applyLocalExecutionSafetyLock(block Block, ctx executionRootContext, expectedRoot string, reason string) {
	if n == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "state_root_mismatch"
	}
	// `strikes` stores the value produced by this operation.
	strikes := n.recordLocalExecutionMismatch(block.ID, block.BlockHash)
	if strikes < 3 {
		return
	}
	// `runtimeReset` stores the value produced by this operation.
	runtimeReset := n.resetRuntimeLedgerToExecution(reason, block.ID)
	n.clearLeaderBlock(block.ID)
	log.Printf("[EXEC-SAFETY-LOCK] height=%d round=%d block=%s strikes=%d runtime_reset=%t parent_ledger=%s runtime_ledger=%s execution_ledger=%s expected_root=%s block_root=%s reason=%s",
		block.ID,
		block.Round,
		ShortHash(block.BlockHash),
		strikes,
		runtimeReset,
		ShortHash(ctx.ParentLedgerHash),
		ShortHash(ctx.RuntimeLedgerHash),
		ShortHash(ctx.ExecutionLedgerHash),
		ShortHash(expectedRoot),
		ShortHash(block.StateRoot),
		reason,
	)
	n.scheduleTrustedStateRootMismatchRepair(block, ctx, expectedRoot, strikes, reason)
	n.clearLocalExecutionMismatch()
}

func (n *Node) scheduleTrustedStateRootMismatchRepair(block Block, ctx executionRootContext, expectedRoot string, strikes int, reason string) {
	if n == nil || n.Blockchain == nil || block.ID == 0 || strikes < 3 {
		return
	}
	localHeight := n.Blockchain.Height()
	if block.ID <= localHeight {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "state_root_mismatch"
	}
	repairReason := "state_root_mismatch_trusted_repair_" + reason
	throttleKey := fmt.Sprintf("state_root_mismatch_trusted_repair:%d:%s", block.ID, strings.TrimSpace(block.BlockHash))
	if !n.shouldLogLivenessReason(throttleKey, 5*time.Second) {
		return
	}
	log.Printf("[EXEC-STATE-REPAIR] height=%d local=%d round=%d block=%s parent_source=%s parent_ledger=%s expected_root=%s block_root=%s strikes=%d action=trusted_snapshot reason=%s",
		block.ID,
		localHeight,
		block.Round,
		ShortHash(block.BlockHash),
		ctx.ParentSource,
		ShortHash(ctx.ParentLedgerHash),
		ShortHash(expectedRoot),
		ShortHash(block.StateRoot),
		strikes,
		repairReason,
	)
	go n.forceSnapshotResyncNow(block.ID, repairReason)
}

// deterministicExecutionParentForEpoch implements the deterministic execution parent for epoch helper.
func (n *Node) deterministicExecutionParentForEpoch(epoch uint64) (Ledger, executionParentLedgerContext, bool) {
	if n == nil || n.Blockchain == nil {
		return Ledger{}, executionParentLedgerContext{}, false
	}
	// `last` stores the value produced by this operation.
	last := n.Blockchain.LastBlock()
	// `nextID` stores the value produced by this operation.
	nextID := last.ID + 1
	if epoch == 0 || epoch != nextID {
		epoch = nextID
	}
	return n.executionParentLedgerForBlock(Block{
		ID:       epoch,
		PrevHash: last.BlockHash,
	})
}

// executionLedgerForBlock implements the execution ledger for block helper.
func (n *Node) executionLedgerForBlock(block Block) (Ledger, executionRootContext, bool) {
	// `ctx` stores the context controlling this operation.
	ctx := executionRootContext{
		RootVersion: executionStateRootVersionForHeight(block.ID),
	}
	// `parentLedger`, `parentCtx`, and `ok` store whether the related condition is satisfied.
	parentLedger, parentCtx, ok := n.executionParentLedgerForBlock(block)
	ctx.ParentHeight = parentCtx.ParentHeight
	ctx.ParentSource = parentCtx.ParentSource
	ctx.ParentHash = parentCtx.ParentHash
	ctx.RuntimeLedgerHash = parentCtx.RuntimeLedgerHash
	ctx.ExecutionLedgerHash = parentCtx.ExecutionLedgerHash
	if !ok {
		if n != nil {
			log.Printf("[EXEC-PARENT] unavailable height=%d prev=%s parent_height=%d parent_hash=%s source=%s runtime_ledger=%s execution_ledger=%s root_version=%s",
				block.ID,
				ShortHash(block.PrevHash),
				ctx.ParentHeight,
				ShortHash(ctx.ParentHash),
				ctx.ParentSource,
				ShortHash(ctx.RuntimeLedgerHash),
				ShortHash(ctx.ExecutionLedgerHash),
				ctx.RootVersion,
			)
		}
		return Ledger{}, ctx, false
	}

	ctx.ParentLedgerHash = HashLedger(parentLedger)
	n.observeConsensusExecutionDrift(block.ID, "before_execution", ctx.ParentLedgerHash, ctx.RuntimeLedgerHash, ctx.ExecutionLedgerHash)
	// `err` stores the error produced by this operation.
	if err := n.deterministicEnsureExecutionLedgerAligned(block.ID, &ctx); err != nil {
		log.Printf("[EXEC-CHECK] height=%d reason=runtime_execution_mismatch_unresolved parent_ledger=%s runtime_ledger=%s execution_ledger=%s",
			block.ID,
			ShortHash(ctx.ParentLedgerHash),
			ShortHash(ctx.RuntimeLedgerHash),
			ShortHash(ctx.ExecutionLedgerHash),
		)
		return Ledger{}, ctx, false
	}
	// `sandbox` stores the value produced by this operation.
	sandbox := NewExecutionSandbox(parentLedger)
	// `newLedger` and `err` store the error produced by this operation.
	executionInput := BlockExecutionInput{
		Block:     block,
		Authority: n.prepareBlockExecutionAuthority(block),
	}
	executionResult, err := sandbox.ApplyBlock(executionInput)
	if err != nil {
		log.Printf("[EXEC-APPLY] height=%d round=%d block=%s parent_source=%s parent_height=%d parent_ledger=%s reason=%s",
			block.ID,
			block.Round,
			ShortHash(block.BlockHash),
			ctx.ParentSource,
			ctx.ParentHeight,
			ShortHash(ctx.ParentLedgerHash),
			err.Error(),
		)
		return Ledger{}, ctx, false
	}
	newLedger := executionResult.NextLedger
	ctx.StateRoot = executionResult.Commitments.StateRoot
	ctx.ReceiptsRoot = executionResult.Commitments.ReceiptsRoot
	ctx.EventRoot = executionResult.Commitments.EventRoot
	ctx.ExecutionHash = executionResult.Commitments.ExecutionHash
	ctx.FeeRoot = executionResult.Commitments.FeeRoot
	ctx.DTLStateRoot = executionResult.Commitments.DTLStateRoot
	ctx.DTLReceiptsRoot = executionResult.Commitments.DTLReceiptsRoot
	// `ledgerHash` stores the digest used to identify or verify the related data.
	ledgerHash := HashLedger(newLedger)
	// `stateMerkleRoot` stores the digest used to identify or verify the related data.
	stateMerkleRoot := LedgerStateMerkleRoot(newLedger)
	if !ExecutionDeterminismGuardEnabled {
		return newLedger, ctx, true
	}
	// `replaySandbox` stores the value produced by this operation.
	replaySandbox := NewExecutionSandbox(parentLedger)
	// `replayLedger` and `err` store the error produced by this operation.
	replayResult, err := replaySandbox.ApplyBlock(executionInput)
	if err != nil {
		log.Printf("[EXEC-APPLY] height=%d round=%d block=%s parent_source=%s parent_height=%d parent_ledger=%s reason=replay_%s",
			block.ID,
			block.Round,
			ShortHash(block.BlockHash),
			ctx.ParentSource,
			ctx.ParentHeight,
			ShortHash(ctx.ParentLedgerHash),
			err.Error(),
		)
		return Ledger{}, ctx, false
	}
	replayLedger := replayResult.NextLedger
	// `replayLedgerHash` stores the digest used to identify or verify the related data.
	replayLedgerHash := HashLedger(replayLedger)
	// `replayStateMerkleRoot` stores the digest used to identify or verify the related data.
	replayStateMerkleRoot := LedgerStateMerkleRoot(replayLedger)
	// `replayExecHash` stores the digest used to identify or verify the related data.
	replayExecHash := replayResult.Commitments.StateRoot
	// `execHash` stores the digest used to identify or verify the related data.
	execHash := executionResult.Commitments.StateRoot
	if !strings.EqualFold(ledgerHash, replayLedgerHash) ||
		!strings.EqualFold(stateMerkleRoot, replayStateMerkleRoot) ||
		!strings.EqualFold(execHash, replayExecHash) ||
		executionResult.Commitments != replayResult.Commitments {
		log.Printf("[EXEC-DETERMINISM] mismatch height=%d round=%d proposer=%s parent_source=%s parent_height=%d parent_hash=%s parent_ledger=%s runtime_ledger=%s execution_ledger=%s root_version=%s ledger_hash_a=%s ledger_hash_b=%s state_merkle_a=%s state_merkle_b=%s exec_a=%s exec_b=%s",
			block.ID,
			block.Round,
			ShortID(block.Proposer),
			ctx.ParentSource,
			ctx.ParentHeight,
			ShortHash(ctx.ParentHash),
			ShortHash(ctx.ParentLedgerHash),
			ShortHash(ctx.RuntimeLedgerHash),
			ShortHash(ctx.ExecutionLedgerHash),
			ctx.RootVersion,
			ShortHash(ledgerHash),
			ShortHash(replayLedgerHash),
			ShortHash(stateMerkleRoot),
			ShortHash(replayStateMerkleRoot),
			ShortHash(execHash),
			ShortHash(replayExecHash),
		)
		return Ledger{}, ctx, false
	}
	return newLedger, ctx, true
}

// computeExecHashV1 computes exec hash v1.
func computeExecHashV1(block Block, ledgerHash string) string {
	if ledgerHash == "" || block.ID == 0 {
		return ""
	}
	// `epoch` stores the value produced by this operation.
	epoch := block.BlockTime.Epoch
	if epoch == 0 {
		epoch = block.ID
	}
	return HashStrings([]string{
		fmt.Sprintf("%d", block.ID),
		block.Proposer,
		block.MempoolRoot,
		block.PrevHash,
		fmt.Sprintf("%d", epoch),
		ledgerHash,
	})
}

// ComputeExecHashVersioned computes exec hash versioned.
func ComputeExecHashVersioned(block Block, ledgerHash string, version string) string {
	switch strings.TrimSpace(version) {
	case "", executionStateRootVersionV1:
		return computeExecHashV1(block, ledgerHash)
	default:
		return ""
	}
}

// executionParentLedgerForBlock implements the execution parent ledger for block helper.
func (n *Node) executionParentLedgerForBlock(block Block) (Ledger, executionParentLedgerContext, bool) {
	// `ctx` stores the context controlling this operation.
	ctx := executionParentLedgerContext{}
	if n == nil || block.ID == 0 {
		return Ledger{}, ctx, false
	}

	// `parentHeight` stores the value produced by this operation.
	parentHeight := block.ID - 1
	// `liveExecutionLedger` stores the value produced by this operation.
	liveExecutionLedger := n.currentExecutionLedgerClone()
	ctx.ParentHeight = parentHeight
	ctx.RuntimeLedgerHash = HashLedger(n.Ledger.Clone())
	ctx.ExecutionLedgerHash = HashLedger(liveExecutionLedger)

	// `expectedPrevHash` stores the digest used to identify or verify the related data.
	expectedPrevHash := strings.TrimSpace(block.PrevHash)
	// `parentBlock` and `ok` store whether the related condition is satisfied.
	if parentBlock, ok := n.LoadBlock(int(parentHeight)); ok {
		ctx.ParentHash = strings.TrimSpace(parentBlock.BlockHash)
	}
	if expectedPrevHash != "" && ctx.ParentHash != "" && !strings.EqualFold(expectedPrevHash, ctx.ParentHash) {
		ctx.ParentSource = "parent_hash_mismatch"
		return Ledger{}, ctx, false
	}

	if parentHeight == 0 {
		if n.Blockchain != nil && n.Blockchain.Height() == 0 {
			// `last` stores the value produced by this operation.
			last := n.Blockchain.LastBlock()
			if ctx.ParentHash == "" {
				ctx.ParentHash = strings.TrimSpace(last.BlockHash)
			}
			if expectedPrevHash != "" && ctx.ParentHash != "" && !strings.EqualFold(expectedPrevHash, ctx.ParentHash) {
				ctx.ParentSource = "parent_hash_mismatch"
				return Ledger{}, ctx, false
			}
			ctx.ParentSource = "runtime_genesis"
			return n.currentExecutionLedgerClone(), ctx, true
		}
		// `snapshot` and `err` store the error produced by this operation.
		if snapshot, err := n.GetSnapshot(0); err == nil && snapshot != nil {
			if ctx.ParentHash == "" {
				ctx.ParentHash = strings.TrimSpace(snapshot.BlockHash)
			}
			if expectedPrevHash != "" && ctx.ParentHash != "" && !strings.EqualFold(expectedPrevHash, ctx.ParentHash) {
				ctx.ParentSource = "parent_hash_mismatch"
				return Ledger{}, ctx, false
			}
			ctx.ParentSource = "snapshot_genesis"
			return snapshot.Ledger.Clone(), ctx, true
		}
		ctx.ParentSource = "genesis_state_unavailable"
		return Ledger{}, ctx, false
	}

	if n.Blockchain != nil {
		// `liveTip` stores the value produced by this operation.
		liveTip := n.Blockchain.Height()
		n.commitMu.Lock()
		if n.committedHeight > liveTip {
			liveTip = n.committedHeight
		}
		n.commitMu.Unlock()
		if parentHeight == liveTip &&
			ledgerHasInitializedBacking(liveExecutionLedger) &&
			n.executionLiveTipTrusted(parentHeight) {
			ctx.ParentSource = "live_execution_tip"
			if cached, ok := n.cachedPostCommitLedger(parentHeight); ok && ledgerHasInitializedBacking(cached) {
				ctx.RuntimeLedgerHash = HashLedger(n.Ledger.Clone())
				ctx.ExecutionLedgerHash = HashLedger(n.currentExecutionLedgerClone())
				return cached, ctx, true
			}
			refreshed := n.currentExecutionLedgerClone()
			ctx.RuntimeLedgerHash = HashLedger(n.Ledger.Clone())
			ctx.ExecutionLedgerHash = HashLedger(refreshed)
			return refreshed, ctx, true
		}
	}

	// `restored` and `ok` store whether the related condition is satisfied.
	if restored, ok := n.committedTipLedgerFromExecutionSnapshot(parentHeight); ok && ledgerHasInitializedBacking(restored) {
		ctx.ParentSource = "post_commit_execution_snapshot"
		return restored, ctx, true
	}
	// `cachedLedger` and `ok` store whether the related condition is satisfied.
	if cachedLedger, ok := n.cachedExecutionSnapshotLedger(parentHeight); ok {
		// `parentBlock` and `found` store whether the related condition is satisfied.
		if parentBlock, found := n.LoadBlock(int(parentHeight)); !found || strings.TrimSpace(parentBlock.StateRoot) == "" {
			ctx.ParentSource = "execution_cache_legacy"
			return cachedLedger, ctx, true
		}
	}
	// `snapshot` and `ok` store whether the related condition is satisfied.
	if snapshot, _, ok := n.resolveTrustedExecutionSnapshotFromStorage(parentHeight); ok && snapshot != nil {
		if ctx.ParentHash == "" {
			ctx.ParentHash = strings.TrimSpace(snapshot.BlockHash)
		}
		if expectedPrevHash != "" && ctx.ParentHash != "" && !strings.EqualFold(expectedPrevHash, ctx.ParentHash) {
			ctx.ParentSource = "parent_hash_mismatch"
			return Ledger{}, ctx, false
		}
		// `parentBlock` and `found` store whether the related condition is satisfied.
		if parentBlock, found := n.LoadBlock(int(parentHeight)); !found || strings.TrimSpace(parentBlock.StateRoot) == "" {
			ctx.ParentSource = "trusted_snapshot_legacy"
			return snapshot.Ledger.Clone(), ctx, true
		}
	}

	ctx.ParentSource = "parent_state_unavailable"
	return Ledger{}, ctx, false
}

// executionStateRootForBlock implements the execution state root for block helper.
func (n *Node) executionStateRootForBlock(block Block) (string, executionRootContext, bool) {
	// `newLedger`, `ctx`, and `ok` store whether the related condition is satisfied.
	_, ctx, ok := n.executionLedgerForBlock(block)
	if !ok {
		return "", ctx, false
	}
	return ctx.StateRoot, ctx, strings.TrimSpace(ctx.StateRoot) != ""
}

// ExecuteBlockAndGetStateRoot implements the execute block and get state root helper.
func (n *Node) ExecuteBlockAndGetStateRoot(block Block) string {
	if strings.TrimSpace(block.StateRoot) == "" && block.ID > 1 {
		parentHeight := block.ID - 1
		if parentLedger, _, ok := n.authoritativeExecutionSnapshotLedger(parentHeight); ok {
			if parentBlock, found := n.LoadBlock(int(parentHeight)); found {
				parentHash := strings.TrimSpace(parentBlock.BlockHash)
				if strings.TrimSpace(block.PrevHash) != "" && parentHash != "" &&
					!strings.EqualFold(strings.TrimSpace(block.PrevHash), parentHash) {
					return ""
				}
			}
			if root := ComputeExecHashVersioned(block, HashLedger(parentLedger), executionStateRootVersionForHeight(block.ID)); strings.TrimSpace(root) != "" {
				return root
			}
		}
	}
	// `execHash` and `ok` store whether the related condition is satisfied.
	execHash, _, ok := n.executionStateRootForBlock(block)
	if !ok {
		return ""
	}
	return execHash
}

// verifyExecutionStateRootWithAuthoritativeRepair verifies execution state root with authoritative repair.
func (n *Node) verifyExecutionStateRootWithAuthoritativeRepair(block Block, reason string) (string, executionRootContext, bool) {
	// `expectedRoot`, `execCtx`, and `ok` store whether the related condition is satisfied.
	expectedRoot, execCtx, ok := n.executionStateRootForBlock(block)
	if ok && expectedRoot != "" && strings.EqualFold(strings.TrimSpace(block.StateRoot), strings.TrimSpace(expectedRoot)) {
		return expectedRoot, execCtx, true
	}
	if n == nil || block.ID <= 1 {
		return expectedRoot, execCtx, false
	}

	// `parentHeight` stores the value produced by this operation.
	parentHeight := block.ID - 1
	if !n.restoreLedgersFromAuthoritativeExecution(parentHeight, reason) {
		return expectedRoot, execCtx, false
	}

	expectedRoot, execCtx, ok = n.executionStateRootForBlock(block)
	// `matched` stores the value produced by this operation.
	matched := ok && expectedRoot != "" && strings.EqualFold(strings.TrimSpace(block.StateRoot), strings.TrimSpace(expectedRoot))
	if matched && n.shouldLogLivenessReason(fmt.Sprintf("verify_state_root_repair:%d:%s", block.ID, strings.TrimSpace(reason)), livenessReasonLogCooldown) {
		log.Printf("[VERIFY-STATE-ROOT-REPAIR] height=%d reason=%s block=%s parent_height=%d parent_source=%s parent_ledger=%s expected_root=%s block_root=%s",
			block.ID,
			strings.TrimSpace(reason),
			ShortHash(block.BlockHash),
			execCtx.ParentHeight,
			execCtx.ParentSource,
			ShortHash(execCtx.ParentLedgerHash),
			ShortHash(expectedRoot),
			ShortHash(block.StateRoot),
		)
	}
	if !matched {
		// `legacyRoot`, `legacyCtx`, and `legacyOK` store whether the related condition is satisfied.
		if legacyRoot, legacyCtx, legacyOK := n.legacyExecutionSnapshotStateRootForBlock(block, reason); legacyOK {
			return legacyRoot, legacyCtx, true
		}
	}
	return expectedRoot, execCtx, matched
}

// executionTraceContext implements the execution trace context helper.
func (n *Node) executionTraceContext() (runtimeLedgerHash string, executionLedgerHash string, tipHeight uint64, tipHash string) {
	if n == nil {
		return "", "", 0, ""
	}
	if n.Blockchain != nil {
		// `last` stores the value produced by this operation.
		last := n.Blockchain.LastBlock()
		tipHeight = last.ID
		tipHash = strings.TrimSpace(last.BlockHash)
	}
	executionLedgerHash = strings.TrimSpace(HashLedger(n.currentExecutionLedgerClone()))
	runtimeLedgerHash = strings.TrimSpace(HashLedger(n.Ledger.Clone()))
	return runtimeLedgerHash, executionLedgerHash, tipHeight, tipHash
}

// preflightOwnLeaderBlock enforces strict local validity before a node
// can publish a leader block. If this fails, the block is never broadcast.
func (n *Node) preflightOwnLeaderBlock(block Block) error {
	if n == nil {
		return errors.New("nil node")
	}
	if block.ID == 0 {
		return errors.New("zero height block")
	}
	// `last` stores the value produced by this operation.
	last := n.Blockchain.LastBlock()
	if block.ID != last.ID+1 {
		return fmt.Errorf("stale/future proposal: got=%d want=%d", block.ID, last.ID+1)
	}
	if block.PrevHash != last.BlockHash {
		return errors.New("prev hash mismatch")
	}
	if ConsensusProposeRequiresSyncReady {
		// `ready` and `reason` store the value produced by this operation.
		if ready, reason := n.syncReadyForConsensus(block.ID); !ready {
			return fmt.Errorf("proposal gated: %s", reason)
		}
	}
	if ConsensusPostBlockSafeModeEnabled {
		// `active` stores the value produced by this operation.
		if active, _, _ := n.postBlockSafeModeState(block.ID); active {
			return errors.New("proposal gated: safe_mode_active")
		}
	}

	// `validators` stores whether the related condition is satisfied.
	validators := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID)))
	// `expectedLeader` stores the value produced by this operation.
	expectedLeader := n.consensusLeaderForHeightRound(block.ID, block.Round, validators)
	if expectedLeader != "" && block.Proposer != expectedLeader {
		if n.syncExecutionResultQuorumFallback(block, validators) {
			if DebugConsensus || DebugSync {
				fmt.Printf("[SYNC-LEADER-FALLBACK] height=%d source=block_quorum_metadata validators=%d hash=%s\n",
					block.ID, len(validators), ShortHash(block.ValidatorSetHash))
			}
		} else {
			return fmt.Errorf("unexpected proposer: got=%s want=%s", block.Proposer, expectedLeader)
		}
	}
	// `expectedHash` and `source` store the digest used to identify or verify the related data.
	expectedHash, source := n.expectedValidatorSetHashWithSource(block.ID)
	// `hashMode` stores the digest used to identify or verify the related data.
	hashMode := validatorSetHashModeForHeight(block.ID)
	if validatorSetSourceIsChainAuthoritative(source) && strings.TrimSpace(expectedHash) == "" {
		if DebugConsensus {
			fmt.Printf("[SET-COMMITMENT-REJECT] height=%d reason=missing_parent_commitment source=%s mode=%s got=%s\n",
				block.ID, source, hashMode, ShortHash(block.ValidatorSetHash))
		}
		return errors.New("validator_set_hash_expected_from_parent_missing")
	}
	if strings.TrimSpace(expectedHash) != "" {
		if block.ValidatorSetHash == "" || !strings.EqualFold(strings.TrimSpace(block.ValidatorSetHash), strings.TrimSpace(expectedHash)) {
			if validatorSetSourceIsChainAuthoritative(source) {
				if DebugConsensus {
					fmt.Printf("[SET-COMMITMENT-REJECT] height=%d source=%s mode=%s expected=%s got=%s\n",
						block.ID, source, hashMode, ShortHash(expectedHash), ShortHash(block.ValidatorSetHash))
				}
				return fmt.Errorf("validator set hash mismatch: got=%s want=%s", ShortHash(block.ValidatorSetHash), ShortHash(expectedHash))
			}
			if DebugConsensus {
				fmt.Printf("[SET-COMMITMENT-SOURCE] weak-source active-set mismatch accepted height=%d source=%s mode=%s got=%s want=%s\n",
					block.ID, source, hashMode, ShortHash(block.ValidatorSetHash), ShortHash(expectedHash))
			}
		} else if DebugConsensus {
			fmt.Printf("[SET-COMMITMENT-APPLY] height=%d source=%s mode=%s hash=%s\n",
				block.ID, source, hashMode, ShortHash(block.ValidatorSetHash))
		}
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockValidatorSetHashHeaderCommitment(block); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockNextValidatorSetCommitment(block); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockNextValidatorSetRootCommitment(block); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockValidatorSetRootCommitment(block); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := n.validateBlockValidatorRegistryCommitment(block); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := verifyBlockQuorumMetadata(block, len(validators)); err != nil {
		return err
	}

	// `err` stores the error produced by this operation.
	if err := VerifyMempoolRoot(block); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := VerifyReceiptRoot(block); err != nil {
		return err
	}
	// `expectedRoot`, `execCtx`, and `rootOK` store whether the related condition is satisfied.
	expectedRoot, execCtx, rootOK := n.verifyExecutionStateRootWithAuthoritativeRepair(block, "preflight_state_root_mismatch")
	if !rootOK {
		if deferReason := executionRootDeferReason(execCtx); deferReason != "" {
			log.Printf("[EXEC-PREFLIGHT-DEFER] height=%d round=%d proposer=%s prev=%s block=%s parent_source=%s parent_height=%d parent_hash=%s runtime_ledger=%s execution_ledger=%s reason=%s",
				block.ID,
				block.Round,
				ShortID(block.Proposer),
				ShortHash(block.PrevHash),
				ShortHash(block.BlockHash),
				execCtx.ParentSource,
				execCtx.ParentHeight,
				ShortHash(execCtx.ParentHash),
				ShortHash(execCtx.RuntimeLedgerHash),
				ShortHash(execCtx.ExecutionLedgerHash),
				deferReason,
			)
			return errors.New(deferReason)
		}
		// `clearedProposal` stores the value produced by this operation.
		clearedProposal := n.clearAcceptedProposalIfBlock(block.ID, block, "state_root_mismatch")
		// `currentRuntimeLedgerHash`, `currentExecutionLedgerHash`, `tipHeight`, and `tipHash` store the digest used to identify or verify the related data.
		currentRuntimeLedgerHash, currentExecutionLedgerHash, tipHeight, tipHash := n.executionTraceContext()
		log.Printf("[EXEC-PREFLIGHT] height=%d round=%d proposer=%s prev=%s tx_count=%d block=%s block_root=%s expected_root=%s parent_source=%s parent_height=%d parent_hash=%s parent_ledger=%s runtime_ledger=%s execution_ledger=%s root_version=%s current_runtime=%s current_execution=%s current_tip=%d/%s proposal=%s cleared_proposal=%t reason=state_root_mismatch",
			block.ID,
			block.Round,
			ShortID(block.Proposer),
			ShortHash(block.PrevHash),
			len(block.Transactions),
			ShortHash(block.BlockHash),
			ShortHash(block.StateRoot),
			ShortHash(expectedRoot),
			execCtx.ParentSource,
			execCtx.ParentHeight,
			ShortHash(execCtx.ParentHash),
			ShortHash(execCtx.ParentLedgerHash),
			ShortHash(execCtx.RuntimeLedgerHash),
			ShortHash(execCtx.ExecutionLedgerHash),
			execCtx.RootVersion,
			ShortHash(currentRuntimeLedgerHash),
			ShortHash(currentExecutionLedgerHash),
			tipHeight,
			ShortHash(tipHash),
			proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot),
			clearedProposal,
		)
		n.applyLocalExecutionSafetyLock(block, execCtx, expectedRoot, "preflight_state_root_mismatch")
		return errors.New("state root mismatch")
	}
	n.clearLocalExecutionMismatch()
	if block.BlockHash == "" || HashBlock(block) != block.BlockHash {
		return errors.New("block hash mismatch")
	}
	if len(block.Signature) == 0 || !VerifyBlockSignature(block) {
		return errors.New("invalid proposer signature")
	}
	return nil
}

// enterProposePhase runs the leader's propose step for a locally-built block:
// validate it, install it as the active candidate, then publish it.
func (n *Node) enterProposePhase(block Block, voteTrigger string) bool {
	if n == nil || block.ID == 0 || n.isShuttingDown() {
		return false
	}
	// `err` stores the error produced by this operation.
	if err := n.preflightOwnLeaderBlock(block); err != nil {
		if DebugConsensus {
			fmt.Printf("Skipping leader proposal @ epoch %d: %v\n", block.ID, err)
		}
		return false
	}
	if !n.storeLeaderBlock(block) {
		return false
	}
	n.processQueuedExecutionVotesForProposal(block)
	if n.executionResultAlreadyCommitted(block.ID) {
		return true
	}
	if n.tryFinalizeProposalIfQuorum(block, "proposal_existing_quorum") {
		return true
	}
	if n.isShuttingDown() {
		return false
	}
	n.setLogicalTick(block.ID, TickExec)
	n.broadcastLeaderBlockUnchecked(block)
	if n.isShuttingDown() {
		return false
	}
	n.maybeBroadcastCurrentLeaderExecutionVote(voteTrigger)
	return true
}

// proposalDeadlineGuardDuration implements the proposal deadline guard duration helper.
func proposalDeadlineGuardDuration() time.Duration {
	if ConsensusProposalDeadlineGuard <= 0 {
		return 200 * time.Millisecond
	}
	return ConsensusProposalDeadlineGuard
}

// consensusRoundSnapshot implements the consensus round snapshot helper.
func (n *Node) consensusRoundSnapshot(height uint64) (uint32, time.Time, bool) {
	if n == nil || n.Consensus == nil || height == 0 {
		return 0, time.Time{}, false
	}
	n.Consensus.mu.Lock()
	defer n.Consensus.mu.Unlock()
	if n.Consensus.Height != height {
		return 0, time.Time{}, false
	}
	return n.Consensus.Round, n.Consensus.RoundStart, true
}

// realignConsensusHeightToEpoch implements the realign consensus height to epoch helper.
func (n *Node) realignConsensusHeightToEpoch(epoch uint64, reason string) bool {
	if n == nil || n.Consensus == nil || epoch == 0 || n.isShuttingDown() {
		return false
	}
	n.Consensus.mu.Lock()
	// `current` stores the value produced by this operation.
	current := n.Consensus.Height
	// `syncing` stores the value produced by this operation.
	syncing := n.Consensus.Syncing || n.Consensus.syncInFlight
	// `paused` stores the value produced by this operation.
	paused := n.Consensus.Paused
	n.Consensus.mu.Unlock()
	if syncing || paused || current == epoch {
		return false
	}
	n.clearImmediateRoundStart(epoch)
	n.hardResetConsensus(epoch)
	if DebugConsensus {
		fmt.Printf("[CONSENSUS-REALIGN] reason=%s from=%d to=%d\n", strings.TrimSpace(reason), current, epoch)
	}
	return true
}

// startConsensusRound implements the start consensus round helper.
func (n *Node) startConsensusRound(height uint64, round uint32) bool {
	if n == nil || n.Consensus == nil || height == 0 || n.isShuttingDown() {
		return false
	}
	round = clampProposerRound(round)
	// `now` stores the value produced by this operation.
	now := time.Now()
	n.Consensus.mu.Lock()
	defer n.Consensus.mu.Unlock()
	if n.Consensus.Height != height {
		return false
	}
	if round < n.Consensus.Round {
		return false
	}
	if round > n.Consensus.Round || n.Consensus.RoundStart.IsZero() {
		n.Consensus.Round = round
		n.Consensus.RoundStart = now
	}
	n.Consensus.Phase = PhasePropose
	n.Consensus.Committed = false
	return true
}

// isRoundLeader implements the is round leader helper.
func (n *Node) isRoundLeader(height uint64, round uint32) bool {
	if n == nil || height == 0 {
		return false
	}
	// `validators` stores whether the related condition is satisfied.
	validators := n.freezeValidatorSetForHeight(height, n.GetConsensusValidators(int(height)))
	if len(validators) == 0 {
		return false
	}
	// `leaderID` stores the value produced by this operation.
	leaderID := normalizeValidatorID(n.consensusLeaderForHeightRound(height, round, validators))
	return leaderID != "" && leaderID == n.localConsensusValidatorIDForSet(validators)
}

// markLeaderProposalSent implements the mark leader proposal sent helper.
func (n *Node) markLeaderProposalSent(height uint64, round uint32) {
	if n == nil || height == 0 {
		return
	}
	// `nowNs` stores the value produced by this operation.
	nowNs := time.Now().UnixNano()
	n.leaderMu.Lock()
	n.lastLeaderEpoch = height
	n.lastLeaderRound = round
	n.lastLeaderSlot = nowNs
	n.leaderMu.Unlock()
}

// leaderProposalRetryState implements the leader proposal retry state helper.
func (n *Node) leaderProposalRetryState(height uint64, round uint32, retry time.Duration) (sameEpoch bool, sameRound bool, throttle bool) {
	if n == nil || height == 0 {
		return false, false, false
	}
	n.leaderMu.Lock()
	defer n.leaderMu.Unlock()
	if n.lastLeaderEpoch != height {
		return false, false, false
	}
	sameEpoch = true
	sameRound = n.lastLeaderRound == round
	if !sameRound {
		return sameEpoch, sameRound, false
	}
	if retry <= 0 || n.lastLeaderSlot <= 0 {
		return sameEpoch, sameRound, false
	}
	// `lastSent` stores the value produced by this operation.
	lastSent := time.Unix(0, n.lastLeaderSlot)
	if time.Since(lastSent) < retry {
		return sameEpoch, sameRound, true
	}
	return sameEpoch, sameRound, false
}

// reuseLeaderProposalForRound implements the reuse leader proposal for round helper.
func (n *Node) reuseLeaderProposalForRound(height uint64, round uint32, trigger string) bool {
	if n == nil || height == 0 {
		return false
	}
	// `existing` and `ok` store whether the related condition is satisfied.
	existing, ok := n.getLeaderBlock(height)
	if !ok || existing.Round != round || existing.BlockHash == "" {
		return false
	}
	validators := n.freezeValidatorSetForHeight(height, n.GetConsensusValidators(int(height)))
	if normalizeValidatorID(existing.Proposer) != n.localConsensusValidatorIDForSet(validators) {
		return false
	}
	n.setLogicalTick(existing.ID, TickExec)
	n.broadcastLeaderBlockUnchecked(existing)
	n.maybeBroadcastCurrentLeaderExecutionVote(strings.TrimSpace(trigger) + "_rebroadcast")
	n.markLeaderProposalSent(height, round)
	return true
}

// forceRoundProposal implements the force round proposal helper.
func (n *Node) forceRoundProposal(height uint64, round uint32, parentLedger Ledger, trigger string) bool {
	if n == nil || height == 0 || n.isShuttingDown() || !ResultGossipOnly {
		return false
	}
	if n.currentEpoch() != height {
		return false
	}
	if !n.startConsensusRound(height, round) {
		return false
	}
	// `ready` stores the value produced by this operation.
	if ready, _ := n.validatorParticipationGateStatus(height); !ready {
		return false
	}
	if ConsensusProposeRequiresSyncReady {
		// `ready` stores the value produced by this operation.
		if ready, _ := n.syncReadyForConsensus(height); !ready {
			return false
		}
	}
	if ConsensusPostBlockSafeModeEnabled && !n.tryExitPostBlockSafeMode(height) {
		return false
	}
	// `blocked` stores the block data handled by this operation.
	if blocked, _, _ := n.consensusSyncGateForHeight(height); blocked {
		return false
	}
	if !n.isRoundLeader(height, round) {
		return false
	}
	// `lockedBlock` and `locked` store the synchronization state protecting shared data.
	if lockedBlock, _, locked, _ := n.acceptedProposalVoteLockForRound(height, round); locked {
		n.maybeBroadcastExecutionVoteForBlock(lockedBlock, strings.TrimSpace(trigger)+"_accepted_vote_lock")
		return false
	}
	// `existing` and `ok` store whether the related condition is satisfied.
	if existing, ok := n.getLeaderBlock(height); ok {
		if existing.Round == round && n.reuseLeaderProposalForRound(height, round, trigger) {
			return true
		}
		if existing.Round < round {
			n.clearLeaderBlock(height)
		}
	}
	if ledgerHasInitializedBacking(parentLedger) {
		n.setExecutionLedger(parentLedger)
	}
	n.setProposedRound(height, round)
	// `block` stores the synchronization state protecting shared data.
	block := n.BuildLeaderBlock(height)
	if block.StateRoot == "" || block.Round != round || n.isShuttingDown() {
		return false
	}
	if ledgerHasInitializedBacking(parentLedger) {
		n.setExecutionLedger(parentLedger)
	}
	if !n.enterProposePhase(block, trigger) {
		return false
	}
	n.markLeaderProposalSent(height, round)
	return true
}

// forceRoundProposalIfLate implements the force round proposal if late helper.
func (n *Node) forceRoundProposalIfLate(height uint64, round uint32, parentLedger Ledger, trigger string) bool {
	if n == nil || height == 0 || n.isShuttingDown() {
		return false
	}
	round = clampProposerRound(round)
	// `activeRound`, `roundStart`, and `ok` store whether the related condition is satisfied.
	activeRound, roundStart, ok := n.consensusRoundSnapshot(height)
	if !ok || activeRound != round || roundStart.IsZero() {
		return false
	}
	if time.Since(roundStart) < proposalDeadlineGuardDuration() {
		return false
	}
	// `existing` and `ok` store whether the related condition is satisfied.
	if existing, ok := n.getLeaderBlock(height); ok && existing.Round == round && existing.BlockHash != "" {
		return false
	}
	return n.forceRoundProposal(height, round, parentLedger, trigger)
}

// forceRoundZeroProposalIfLate implements the force round zero proposal if late helper.
func (n *Node) forceRoundZeroProposalIfLate(height uint64, parentLedger Ledger, trigger string) bool {
	return n.forceRoundProposalIfLate(height, 0, parentLedger, trigger)
}

// scheduleProposalDeadlineGuard implements the schedule proposal deadline guard helper.
func (n *Node) scheduleProposalDeadlineGuard(height uint64, round uint32, parentLedger Ledger) {
	if n == nil || height == 0 || n.isShuttingDown() {
		return
	}
	round = clampProposerRound(round)
	// `deadline` stores the value produced by this operation.
	deadline := proposalDeadlineGuardDuration()
	if deadline <= 0 {
		return
	}
	// `queuedLedger` stores the value produced by this operation.
	queuedLedger := parentLedger.Clone()
	// `ctx` stores the context controlling this operation.
	ctx := n.RootContext()
	n.SafeGo(fmt.Sprintf("proposal_deadline_guard_%d_%d", height, round), func() {
		// `timer` stores the value produced by this operation.
		timer := time.NewTimer(deadline)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		_ = n.scheduleConsensusPriorityTask(func() {
			_ = n.forceRoundProposalIfLate(height, round, queuedLedger, "round_start_deadline")
		})
	})
}

// scheduleRoundZeroProposalDeadlineGuard implements the schedule round zero proposal deadline guard helper.
func (n *Node) scheduleRoundZeroProposalDeadlineGuard(height uint64, parentLedger Ledger) {
	n.scheduleProposalDeadlineGuard(height, 0, parentLedger)
}

// startNextRoundImmediately implements the start next round immediately helper.
func (n *Node) startNextRoundImmediately(nextHeight uint64, parentLedger Ledger) {
	n.startNextRoundImmediatelyWithReason(nextHeight, parentLedger, "direct")
}

// startNextRoundImmediatelyWithReason implements the start next round immediately with reason helper.
func (n *Node) startNextRoundImmediatelyWithReason(nextHeight uint64, parentLedger Ledger, reason string) {
	if n == nil || nextHeight == 0 || !ResultGossipOnly {
		return
	}
	if !n.tryScheduleImmediateRoundStart(nextHeight) {
		return
	}
	// `queuedLedger` stores the value produced by this operation.
	queuedLedger := parentLedger.Clone()
	if !n.scheduleConsensusPriorityTask(func() {
		// `started` stores the value produced by this operation.
		started := n.startNextRoundImmediatelyNow(nextHeight, queuedLedger, reason)
		n.finishImmediateRoundStart(nextHeight, started)
	}) {
		n.clearImmediateRoundStart(nextHeight)
		return
	}
}

// startNextRoundImmediatelyNow implements the start next round immediately now helper.
func (n *Node) startNextRoundImmediatelyNow(nextHeight uint64, parentLedger Ledger, reason string) bool {
	if n == nil || nextHeight == 0 || !ResultGossipOnly || n.isShuttingDown() {
		return false
	}
	if n.immediateRoundStartAlreadyHandled(nextHeight) {
		return false
	}
	if n.currentEpoch() != nextHeight {
		return false
	}
	if ledgerHasInitializedBacking(parentLedger) {
		n.setExecutionLedger(parentLedger)
	}

	if n.Consensus != nil {
		n.Consensus.mu.Lock()
		if !n.Consensus.Syncing {
			n.Consensus.Paused = false
		}
		n.Consensus.mu.Unlock()
	}

	n.hardResetConsensus(nextHeight)
	if !n.startConsensusRound(nextHeight, 0) {
		return false
	}
	n.setLogicalTick(nextHeight, TickExec)
	n.scheduleProposalDeadlineGuard(nextHeight, 0, parentLedger)
	if reason == "" {
		reason = "immediate_round_start"
	}
	_ = n.forceRoundProposal(nextHeight, 0, parentLedger, reason)
	return true
}

// ComputeExecHash computes exec hash.
func ComputeExecHash(block Block, ledgerHash string) string {
	return ComputeExecHashVersioned(block, ledgerHash, executionStateRootVersionForHeight(block.ID))
}

// enterProposerRoundRecoveryMode implements the enter proposer round recovery mode helper.
func (n *Node) enterProposerRoundRecoveryMode(height uint64, round uint32, maxRounds uint32) {
	if n == nil || height == 0 {
		return
	}
	if maxRounds == 0 {
		maxRounds = proposerRoundRecoveryCap()
	}
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("round_recovery:%d:%d", height, maxRounds)
	if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
		log.Printf("[ROUND-RECOVERY] height=%d round=%d max_round=%d action=recompute_sync",
			height,
			round,
			maxRounds,
		)
	}
	n.requestConsensusRecomputePause(height, "round_cap_exceeded")
	n.maybeSyncToBestObservedHeight("round_cap_exceeded")
}

// pauseConsensusForLivenessShortfall implements the pause consensus for liveness shortfall helper.
func (n *Node) pauseConsensusForLivenessShortfall(height uint64, required int, snap CommitteeLivenessSnapshot) {
	if n == nil || height == 0 || required <= 0 {
		return
	}
	if snap.Live >= required {
		return
	}
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("liveness_pause:%d:%d", height, required)
	if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
		log.Printf("[LIVENESS-PAUSE] height=%d live=%d required=%d action=recompute_pause",
			height,
			snap.Live,
			required,
		)
	}
	n.requestConsensusRecomputePause(height, "live_quorum_unavailable")
}

// ActivateConsensus implements the activate consensus helper.
func (n *Node) ActivateConsensus(ctx context.Context) error {
	// Run the consensus loop for validator/full nodes so observer mode still
	// verifies/executes and tracks network progress. Propose/vote remains gated
	// by canParticipateInConsensusNow inside the loop.
	if n == nil {
		return errors.New("node unavailable")
	}
	if n.Role != "validator" && normalizeNodeRole(n.Role) != "full" {
		return nil
	}
	if !ResultGossipOnly {
		return errors.New("legacy consensus removed")
	}
	if !consensusStarted.CompareAndSwap(false, true) {
		return errors.New("consensus already running")
	}
	go func() {
		defer consensusStarted.Store(false)

		// `realTick` stores the value produced by this operation.
		realTick := GlobalConfig.RealTick
		if realTick <= 0 {
			realTick = 2 * time.Second
		}
		// `minBlockInterval` stores the value currently being processed.
		minBlockInterval := ConsensusMinBlockInterval
		if minBlockInterval <= 0 {
			minBlockInterval = 4 * time.Second
		}
		// `ticker` stores the value produced by this operation.
		ticker := time.NewTicker(realTick)
		defer ticker.Stop()

		// `lastEpoch` stores the value used by this operation.
		var lastEpoch uint64
		// `lastEpochAt` stores the value used by this operation.
		var lastEpochAt time.Time
		// `lastFallbackEpoch` stores the value used by this operation.
		var lastFallbackEpoch uint64
		// `lastRound` stores the value used by this operation.
		var lastRound uint32
		// `lastParticipationGateEpoch` stores the value used by this operation.
		var lastParticipationGateEpoch uint64
		// `lastParticipationGateReason` stores the value used by this operation.
		var lastParticipationGateReason string
		// `lastProposalGateEpoch` stores the value used by this operation.
		var lastProposalGateEpoch uint64
		// `lastProposalGateReason` stores the value used by this operation.
		var lastProposalGateReason string
		// `lastStartupGateEpoch` stores the value used by this operation.
		var lastStartupGateEpoch uint64
		// `lastStartupGateReason` stores the value used by this operation.
		var lastStartupGateReason string
		// `lastRoundGateEpoch` stores the value used by this operation.
		var lastRoundGateEpoch uint64
		// `lastRoundGateAt` stores the value used by this operation.
		var lastRoundGateAt time.Time
		// `holdRoundClock` stores the synchronization state protecting shared data.
		holdRoundClock := func(epoch uint64) {
			if epoch == 0 {
				return
			}
			lastRoundGateEpoch = epoch
			lastRoundGateAt = time.Now()
		}

		// Round 0 must not wait for the first consensus ticker pulse. Kick the
		// current epoch onto the consensus lane immediately on loop startup, then
		// let the regular ticker handle retries/failover afterward.
		n.startNextRoundImmediatelyWithReason(n.currentEpoch(), n.currentExecutionLedgerClone(), "activate_consensus")

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n.consensusRecomputePauseActive() {
					holdRoundClock(n.currentEpoch())
					continue
				}
				// `epoch` stores the value produced by this operation.
				epoch := n.currentEpoch()
				// `ready` and `reason` store the value produced by this operation.
				if ready, reason := n.validatorParticipationGateStatus(epoch); !ready {
					if DebugConsensus && (lastParticipationGateEpoch != epoch || lastParticipationGateReason != reason) {
						fmt.Printf("[PARTICIPATION-GATE] validator=%s height=%d reason=%s\n", ShortID(n.ID), epoch, reason)
						lastParticipationGateEpoch = epoch
						lastParticipationGateReason = reason
					}
					holdRoundClock(epoch)
					continue
				}
				// `blocked` stores the block data handled by this operation.
				if blocked, _, _ := n.consensusSyncGateForHeight(epoch); blocked {
					holdRoundClock(epoch)
					continue
				}
				// `ok` and `reason` store whether the related condition is satisfied.
				if ok, reason := n.startupValidatorSetSelfCheck(); !ok {
					if DebugConsensus && (lastStartupGateEpoch != epoch || lastStartupGateReason != reason) {
						fmt.Printf("[STARTUP-GATE] validator=%s height=%d reason=%s\n", ShortID(n.ID), epoch, reason)
						lastStartupGateEpoch = epoch
						lastStartupGateReason = reason
					}
					holdRoundClock(epoch)
					continue
				}
				if n.realignConsensusHeightToEpoch(epoch, "consensus_tick") {
					lastEpoch = 0
					lastEpochAt = time.Time{}
					lastFallbackEpoch = 0
					lastRound = 0
					lastRoundGateEpoch = 0
					lastRoundGateAt = time.Time{}
				}
				if epoch != lastEpoch {
					lastEpoch = epoch
					lastEpochAt = time.Now()
					lastFallbackEpoch = 0
					lastRound = 0
					lastParticipationGateEpoch = 0
					lastParticipationGateReason = ""
					lastProposalGateEpoch = 0
					lastProposalGateReason = ""
					lastStartupGateEpoch = 0
					lastStartupGateReason = ""
					lastRoundGateEpoch = 0
					lastRoundGateAt = time.Time{}
					// `epochStartHeight` stores the value produced by this operation.
					epochStartHeight := epoch
					// `epochStartLedger` stores the value produced by this operation.
					epochStartLedger := n.currentExecutionLedgerClone()
					_ = n.scheduleConsensusPriorityTask(func() {
						n.startConsensusRound(epochStartHeight, 0)
						_ = n.forceRoundProposal(epochStartHeight, 0, epochStartLedger, "consensus_epoch_start")
					})
					n.scheduleProposalDeadlineGuard(epochStartHeight, 0, epochStartLedger)
				}
				if !lastEpochAt.IsZero() && time.Since(lastEpochAt) < minBlockInterval {
					continue
				}
				if ConsensusPostBlockSafeModeEnabled {
					if !n.tryExitPostBlockSafeMode(epoch) {
						continue
					}
				}
				// `validators` stores whether the related condition is satisfied.
				validators := n.freezeValidatorSetForHeight(epoch, n.GetConsensusValidators(int(epoch)))
				// `total` stores the measured quantity used by this operation.
				total := len(validators)
				// `required` stores the request data being processed.
				required := n.executionQuorumRequiredForEpoch(epoch)
				if required == 0 {
					required = execQuorumRequired(total)
				}
				if total == 0 || required == 0 {
					continue
				}
				// `observedRound` and `observedAt` store the value produced by this operation.
				observedRound, observedAt := n.proposedRoundAnchorForHeight(epoch)
				// `roundEpochStartedAt` stores the value produced by this operation.
				roundEpochStartedAt := lastEpochAt
				if lastRoundGateEpoch == epoch {
					roundEpochStartedAt, observedAt = consensusRoundAnchorsWithGateHold(roundEpochStartedAt, observedAt, lastRoundGateAt)
				}
				// `rawRound` stores the value produced by this operation.
				rawRound := computeConsensusRound(time.Now(), roundEpochStartedAt, observedRound, observedAt, minBlockInterval, ProposerRoundTimeout, realTick)
				// `maxRounds` stores the value produced by this operation.
				if maxRounds := proposerRoundRecoveryCap(); maxRounds > 0 && rawRound > maxRounds {
					n.enterProposerRoundRecoveryMode(epoch, rawRound, maxRounds)
					continue
				}
				// `round` stores the value produced by this operation.
				round := clampProposerRound(rawRound)
				if ConsensusProposeRequiresSyncReady {
					// `ready` and `reason` store the value produced by this operation.
					if ready, reason := n.syncReadyForConsensus(epoch); !ready {
						if DebugConsensus && (lastProposalGateEpoch != epoch || lastProposalGateReason != reason) {
							fmt.Printf("[PROPOSAL-GATE] skipped lagging validator=%s height=%d reason=%s\n", ShortID(n.ID), epoch, reason)
							lastProposalGateEpoch = epoch
							lastProposalGateReason = reason
						}
						holdRoundClock(epoch)
						continue
					}
				}
				// `leaderID` stores the value produced by this operation.
				leaderID, _, _ := n.selectLiveLeaderForHeightRound(epoch, round, validators)
				if leaderID == "" {
					continue
				}
				if round != lastRound {
					if DebugConsensus && round > 0 {
						fmt.Printf("[ROUND-FAILOVER] height=%d round=%d leader=%s\n", epoch, round, ShortID(leaderID))
					}
					lastRound = round
					// `roundHeight` stores the value produced by this operation.
					roundHeight := epoch
					// `roundToStart` stores the value produced by this operation.
					roundToStart := round
					// `roundLedger` stores the value produced by this operation.
					roundLedger := n.currentExecutionLedgerClone()
					_ = n.scheduleConsensusPriorityTask(func() {
						n.startConsensusRound(roundHeight, roundToStart)
					})
					n.scheduleProposalDeadlineGuard(roundHeight, roundToStart, roundLedger)
				}
				// `snap` stores the value produced by this operation.
				snap := n.committeeLivenessSnapshot(epoch)
				// `live` stores the value produced by this operation.
				live := snap.Live
				if live < required {
					if DebugConsensus {
						n.logLivenessReasonSummary("consensus", epoch, required, snap)
					}
				}
				n.setProposedRound(epoch, round)

				// Leader-stall fallback: broadcast a deterministic empty block if the leader stalls.
				if !lastEpochAt.IsZero() && lastFallbackEpoch != epoch && leaderID == n.ID {
					// `timeout` stores the result produced by this operation.
					timeout := LeaderStallTimeout
					if timeout <= 0 {
						timeout = 3 * realTick
					}
					// `minTimeout` stores the result produced by this operation.
					minTimeout := 3 * realTick
					if timeout < minTimeout {
						timeout = minTimeout
					}
					if time.Since(lastEpochAt) >= timeout {
						// Keep fallback block construction/publication on the
						// consensus lane so stalled recovery cannot compete with
						// normal proposal work on arbitrary goroutines.
						fallbackHeight := epoch
						// `fallbackRound` stores the value produced by this operation.
						fallbackRound := round
						if n.scheduleConsensusPriorityTask(func() {
							// `currentRound` stores the value produced by this operation.
							if currentRound := n.proposedRoundForHeight(fallbackHeight); currentRound > fallbackRound {
								return
							}
							if !n.isRoundLeader(fallbackHeight, fallbackRound) {
								return
							}
							n.setProposedRound(fallbackHeight, fallbackRound)
							if n.reuseLeaderProposalForRound(fallbackHeight, fallbackRound, "fallback_block") {
								return
							}
							n.clearLeaderBlock(fallbackHeight)
							// `fallback` stores the value produced by this operation.
							fallback := n.BuildFallbackBlock(fallbackHeight)
							if fallback.StateRoot != "" && n.enterProposePhase(fallback, "fallback_block") {
								n.markLeaderProposalSent(fallbackHeight, fallback.Round)
							}
						}) {
							lastFallbackEpoch = epoch
						}
					}
				}

				if leaderID == "" || leaderID != n.ID {
					continue
				}

				// `proposeRetry` stores the value produced by this operation.
				proposeRetry := LeaderStallTimeout
				if proposeRetry <= 0 {
					proposeRetry = 3 * realTick
				}
				if proposeRetry < realTick {
					proposeRetry = realTick
				}

				// `trigger` stores the value produced by this operation.
				trigger := "built_block"
				// `sameEpoch`, `sameRound`, and `throttle` store the value produced by this operation.
				sameEpoch, sameRound, throttle := n.leaderProposalRetryState(epoch, round, proposeRetry)
				if throttle {
					continue
				}
				if sameEpoch && sameRound {
					trigger = "rebroadcast_block"
				}
				// `proposalHeight` stores the value produced by this operation.
				proposalHeight := epoch
				// `proposalRound` stores the value produced by this operation.
				proposalRound := round
				// `proposalLedger` stores the value produced by this operation.
				proposalLedger := n.currentExecutionLedgerClone()
				_ = n.scheduleConsensusPriorityTask(func() {
					_ = n.forceRoundProposal(proposalHeight, proposalRound, proposalLedger, trigger)
				})
			}
		}
	}()
	return nil
}

// initPubSubTopics implements the init pub sub topics helper.
func (n *Node) initPubSubTopics() error {

	// =====================================================
	// ÃƒÂ°Ã…Â¸Ã¢â‚¬ÂÃ¢â‚¬â„¢ HARD GUARD ÃƒÂ¢Ã¢â€šÂ¬Ã¢â‚¬Â PubSub must exist
	// =====================================================
	if n.PubSub == nil {
		return fmt.Errorf("pubsub not initialized")
	}

	// `err` stores the error produced by this operation.
	var err error

	// =====================================================
	// ÃƒÂ°Ã…Â¸Ã‚Â§Ã‚Â± BLOCKS TOPIC (AUTHORITATIVE STATE)
	// =====================================================
	if n.BlockTopic == nil {
		n.BlockTopic, err = n.PubSub.Join(TopicBlock)
		if err != nil {
			return err
		}
	}
	// Legacy block topic (best-effort)
	if n.TopicBlocks == nil {
		// `legacy` and `legacyErr` store the error produced by this operation.
		if legacy, legacyErr := n.PubSub.Join(TopicBlocksLegacy); legacyErr == nil {
			n.TopicBlocks = legacy
		} else if DebugNet {
			fmt.Printf("ÃƒÂ¢Ã‚ÂÃ…â€™ Legacy block topic join failed: %v\n", legacyErr)
		}
	}

	// =====================================================
	// ÃƒÂ°Ã…Â¸Ã¢â‚¬â„¢Ã‚Â¸ TRANSACTIONS TOPIC (MEMPOOL)
	// =====================================================
	if n.TxTopic == nil {
		n.TxTopic, err = n.PubSub.Join(TopicTx)
		if err != nil {
			return err
		}
	}

	// =====================================================
	// ÃƒÂ°Ã…Â¸Ã‚Â¤Ã‚Â VALIDATORS TOPIC (CONSENSUS MEMBERSHIP) ÃƒÂ°Ã…Â¸Ã¢â‚¬ÂÃ‚Â¥ FIX
	// =====================================================
	if n.ValidatorTopic == nil {
		n.ValidatorTopic, err = n.PubSub.Join(TopicValidator)
		if err != nil {
			return fmt.Errorf("validator topic join failed: %w", err)
		}
	}
	if n.ValidatorSub == nil {
		n.ValidatorSub, err = n.ValidatorTopic.Subscribe()
		if err != nil {
			return fmt.Errorf("validator subscription failed: %w", err)
		}
	}

	// =====================================================
	// ÃƒÆ’Ã‚Â°Ãƒâ€¦Ã‚Â¸ÃƒÂ¢Ã¢â€šÂ¬Ã‚ÂºÃƒâ€šÃ‚Â¤ CONSENSUS TOPIC (EXEC/COMMIT/SNAPSHOTS)
	// =====================================================
	if n.ConsensusTopic == nil {
		n.ConsensusTopic, err = n.PubSub.Join(TopicConsensus)
		if err != nil {
			return fmt.Errorf("consensus topic join failed: %w", err)
		}
	}
	if n.ConsensusSub == nil {
		n.ConsensusSub, err = n.ConsensusTopic.Subscribe()
		if err != nil {
			return fmt.Errorf("consensus subscription failed: %w", err)
		}
	}

	if n.SnapshotMetaTopic == nil {
		n.SnapshotMetaTopic, err = n.PubSub.Join(TopicSnapshotMeta)
		if err != nil {
			return fmt.Errorf("snapshot meta topic join failed: %w", err)
		}
	}
	if n.SnapshotMetaSub == nil {
		n.SnapshotMetaSub, err = n.SnapshotMetaTopic.Subscribe()
		if err != nil {
			return fmt.Errorf("snapshot meta subscription failed: %w", err)
		}
	}

	if n.SnapshotChunkTopic == nil {
		n.SnapshotChunkTopic, err = n.PubSub.Join(TopicSnapshotChunk)
		if err != nil {
			return fmt.Errorf("snapshot chunk topic join failed: %w", err)
		}
	}
	if n.SnapshotChunkSub == nil {
		n.SnapshotChunkSub, err = n.SnapshotChunkTopic.Subscribe()
		if err != nil {
			return fmt.Errorf("snapshot chunk subscription failed: %w", err)
		}
	}

	if n.SnapshotProofTopic == nil {
		n.SnapshotProofTopic, err = n.PubSub.Join(TopicSnapshotProof)
		if err != nil {
			return fmt.Errorf("snapshot proof topic join failed: %w", err)
		}
	}
	if n.SnapshotProofSub == nil {
		n.SnapshotProofSub, err = n.SnapshotProofTopic.Subscribe()
		if err != nil {
			return fmt.Errorf("snapshot proof subscription failed: %w", err)
		}
	}

	// =====================================================
	// ÃƒÂ°Ã…Â¸Ã¢â‚¬Å“Ã‚Â¡ DEBUG LOGS
	// =====================================================
	if DebugNet {
		fmt.Println("ÃƒÂ°Ã…Â¸Ã¢â‚¬Å“Ã‚Â¡ PubSub topics initialized (MSC)")
		fmt.Println("   msc-block        block propagation")
		fmt.Println("   msc-tx           mempool ingress")
		fmt.Println("   msc-consensus    exec/commit/snapshot")
		fmt.Println("   msc-validator    validator gossip")
		fmt.Println("   msc-snapshot-meta snapshot manifest metadata")
		fmt.Println("   msc-snapshot-chunk snapshot chunk availability")
		fmt.Println("   msc-snapshot-proof snapshot anchor proofs")
	}

	return nil
}

// Height implements the height helper.
func (bc *Blockchain) Height() uint64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if len(bc.Blocks) == 0 {
		return 0
	}
	return bc.Blocks[len(bc.Blocks)-1].ID
}

// FinalizedHeight implements the finalized height helper.
func (bc *Blockchain) FinalizedHeight() uint64 {
	// In this codebase, the canonical chain tip is the finalized height.
	return bc.Height()
}

// GetBlock returns block.
func (bc *Blockchain) GetBlock(height uint64) (Block, bool) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if height == 0 || len(bc.Blocks) == 0 {
		return Block{}, false
	}
	// `idx` stores the current position in the related collection.
	idx := int(height - 1)
	if idx >= 0 && idx < len(bc.Blocks) {
		if bc.Blocks[idx].ID == height {
			return bc.Blocks[idx], true
		}
	}
	// Fallback for safety if the slice index doesn't align.
	for i := len(bc.Blocks) - 1; i >= 0; i-- {
		if bc.Blocks[i].ID == height {
			return bc.Blocks[i], true
		}
		if bc.Blocks[i].ID < height {
			break
		}
	}
	return Block{}, false
}

// GetActiveValidators returns active validators.
func (n *Node) GetActiveValidators() []string {
	// `consensusHeight` stores the value produced by this operation.
	consensusHeight := n.currentEpoch()
	// `consensus` stores the value produced by this operation.
	consensus := canonicalValidatorIDs(n.GetConsensusValidators(int(consensusHeight)))
	if len(consensus) == 0 {
		return nil
	}

	// `now` stores the value produced by this operation.
	now := time.Now()
	// `localFinalized` stores the value produced by this operation.
	localFinalized := n.getFinalizedHeight()
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(consensus))

	n.validatorMu.RLock()
	// `id` tracks the current position in the related collection.
	for _, id := range consensus {
		// `normID` stores the value produced by this operation.
		normID := normalizeValidatorID(id)
		if normID == "" {
			continue
		}
		// `st` stores the value produced by this operation.
		st := n.validatorStatus[normID]
		if !n.isValidatorLiveForConsensusLocked(normID, st, now, localFinalized) {
			continue
		}
		out = append(out, id)
	}
	n.validatorMu.RUnlock()
	sort.Strings(out)

	if DebugConsensus {
		// `chainHeight` stores the value produced by this operation.
		chainHeight := uint64(0)
		if n.Blockchain != nil {
			chainHeight = n.Blockchain.Height()
		}
		fmt.Printf(
			"[LIVENESS] live_validators=%d height=%d local_finalized=%d drift_limit=%d ids=%v\n",
			len(out),
			chainHeight,
			localFinalized,
			validatorLivenessMaxHeightDriftBlocks(),
			out,
		)
	}

	return out
}

// validatorInAnyHeartbeatSet implements the validator in any heartbeat set helper.
func (n *Node) validatorInAnyHeartbeatSet(id string, reportedHeight uint64, finalizedHeight uint64, execEpoch uint64, validatorSetHeight uint64) bool {
	id = normalizeValidatorID(id)
	if id == "" || n == nil {
		return false
	}
	// `heights` stores the value produced by this operation.
	heights := []uint64{
		finalizedHeight,
		reportedHeight,
		execEpoch,
		validatorSetHeight,
		n.currentEpoch(),
	}
	// `checked` stores the value produced by this operation.
	checked := make(map[uint64]struct{}, len(heights)*2)
	// `h` tracks the current values while iterating.
	for _, h := range heights {
		if h == 0 {
			continue
		}
		// `candidates` stores the value produced by this operation.
		candidates := []uint64{h}
		if h > 1 {
			candidates = append(candidates, h-1)
		}
		// `target` tracks the current values while iterating.
		for _, target := range candidates {
			if target == 0 {
				continue
			}
			// `ok` stores whether the related condition is satisfied.
			if _, ok := checked[target]; ok {
				continue
			}
			checked[target] = struct{}{}
			// `inSet` and `ok` store whether the related condition is satisfied.
			if inSet, _, ok := n.authoritativeHeartbeatMembershipAtHeight(id, target); ok {
				if inSet {
					return true
				}
				continue
			}
			if n.isValidatorInSetForHeight(id, target) {
				return true
			}
		}
	}
	// Bootstrap-only fallback: once chain history exists, sync must converge via
	// committed validator authority rather than core membership.
	if validatorSetAutohealStrictCoreQuorum() && n.coreBootstrapAuthorityAllowed() {
		// `coreID` tracks the current values while iterating.
		for _, coreID := range n.activeCoreAuthorityIDs() {
			if normalizeValidatorID(coreID) == id {
				return true
			}
		}
	}
	return false
}

// authoritativeHeartbeatMembershipForAnnouncement implements the authoritative heartbeat membership for announcement helper.
func (n *Node) authoritativeHeartbeatMembershipForAnnouncement(id string, reportedHeight uint64, finalizedHeight uint64, execEpoch uint64, validatorSetHeight uint64) (bool, string) {
	id = normalizeValidatorID(id)
	if id == "" || n == nil {
		return false, "none"
	}
	// `heights` stores the value produced by this operation.
	heights := []uint64{
		finalizedHeight,
		reportedHeight,
		execEpoch,
		validatorSetHeight,
		n.currentEpoch(),
	}
	// `checked` stores the value produced by this operation.
	checked := make(map[uint64]struct{}, len(heights)*2)
	// `h` tracks the current values while iterating.
	for _, h := range heights {
		if h == 0 {
			continue
		}
		// `candidates` stores the value produced by this operation.
		candidates := []uint64{h}
		if h > 1 {
			candidates = append(candidates, h-1)
		}
		// `target` tracks the current values while iterating.
		for _, target := range candidates {
			if target == 0 {
				continue
			}
			// `ok` stores whether the related condition is satisfied.
			if _, ok := checked[target]; ok {
				continue
			}
			checked[target] = struct{}{}
			// `inSet`, `source`, and `ok` store whether the related condition is satisfied.
			if inSet, source, ok := n.authoritativeHeartbeatMembershipAtHeight(id, target); ok {
				if inSet {
					return true, source
				}
				continue
			}
		}
	}
	return false, "none"
}

// handleValidatorAnnouncement handles validator announcement.
func (n *Node) handleValidatorAnnouncement(data []byte) {
	// `ann` stores the value used by this operation.
	var ann ValidatorAnnouncement
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(data, &ann); err != nil {
		return
	}
	ann.NodeID = normalizeValidatorID(ann.NodeID)

	// Ignore self & invalid
	if ann.NodeID == "" || containsNormalizedValidatorID(n.localConsensusValidatorIDCandidates(), ann.NodeID) {
		return
	}

	if ann.PubKey == "" {
		return
	}
	// `pkBytes` and `err` store the error produced by this operation.
	pkBytes, err := hex.DecodeString(ann.PubKey)
	if err != nil || len(pkBytes) != ed25519.PublicKeySize {
		return
	}

	if ann.Signature != "" {
		// `sigBytes` and `err` store the error produced by this operation.
		sigBytes, err := hex.DecodeString(ann.Signature)
		if err != nil {
			return
		}
		// `reported` stores the value produced by this operation.
		reported := ann.ReportedHeight
		if reported == 0 {
			reported = ann.Height
		}
		// `finalized` stores the value produced by this operation.
		finalized := ann.FinalizedHeight
		if finalized == 0 {
			finalized = reported
		}
		// `execEpoch` stores the value produced by this operation.
		execEpoch := ann.ExecEpoch
		if execEpoch == 0 {
			execEpoch = finalized + 1
		}
		// `validatorSetHeight` stores whether the related condition is satisfied.
		validatorSetHeight := validatorAnnouncementActivationHeight(ann)
		if validatorSetHeight == 0 {
			validatorSetHeight = execEpoch
		}
		// `validatorSetHash` stores whether the related condition is satisfied.
		validatorSetHash := strings.ToLower(strings.TrimSpace(ann.ValidatorSetHash))
		// `nextActivationHeight` stores the value produced by this operation.
		nextActivationHeight := ann.NextActivationHeight
		if nextActivationHeight == 0 && validatorSetHeight > 0 {
			nextActivationHeight = validatorSetHeight + 1
		}
		// `nextValidatorSetHash` stores the digest used to identify or verify the related data.
		nextValidatorSetHash := strings.ToLower(strings.TrimSpace(ann.NextValidatorSetHash))
		if nextValidatorSetHash == "" {
			nextValidatorSetHash = validatorSetHash
		}
		// `v5OK` stores whether the related condition is satisfied.
		v5OK := false
		if ann.ConsensusReadySet {
			v5OK = ed25519.Verify(
				ed25519.PublicKey(pkBytes),
				validatorAnnounceSignBytesV5(
					ann.NodeID,
					ann.PubKey,
					ann.P2PAddr,
					reported,
					finalized,
					execEpoch,
					validatorSetHeight,
					validatorSetHash,
					nextValidatorSetHash,
					nextActivationHeight,
					ann.ConsensusReadySet,
					ann.ConsensusReady,
					ann.IsValidator,
				),
				sigBytes,
			)
		}
		// `v4OK` stores whether the related condition is satisfied.
		v4OK := false
		v4OK = ed25519.Verify(
			ed25519.PublicKey(pkBytes),
			validatorAnnounceSignBytesV4(
				ann.NodeID,
				ann.PubKey,
				ann.P2PAddr,
				reported,
				finalized,
				execEpoch,
				validatorSetHeight,
				validatorSetHash,
				nextValidatorSetHash,
				nextActivationHeight,
				ann.IsValidator,
			),
			sigBytes,
		)
		// `v3OK` stores whether the related condition is satisfied.
		v3OK := false
		v3OK = ed25519.Verify(
			ed25519.PublicKey(pkBytes),
			validatorAnnounceSignBytesV3(
				ann.NodeID,
				ann.PubKey,
				ann.P2PAddr,
				reported,
				finalized,
				execEpoch,
				validatorSetHeight,
				validatorSetHash,
				ann.IsValidator,
			),
			sigBytes,
		)
		if !v5OK && !v4OK && !v3OK && !ed25519.Verify(
			ed25519.PublicKey(pkBytes),
			validatorAnnounceSignBytesV2(ann.NodeID, ann.PubKey, ann.P2PAddr, reported, finalized, execEpoch, ann.IsValidator),
			sigBytes,
		) {
			if !ed25519.Verify(
				ed25519.PublicKey(pkBytes),
				validatorAnnounceSignBytes(ann.NodeID, ann.PubKey, reported, finalized, execEpoch, ann.IsValidator),
				sigBytes,
			) {
				if !ed25519.Verify(
					ed25519.PublicKey(pkBytes),
					validatorAnnounceSignBytesLegacy(ann.NodeID, ann.PubKey, ann.Height, ann.IsValidator),
					sigBytes,
				) {
					if DebugConsensus {
						fmt.Printf("Invalid validator announce signature: %s\n", ShortID(ann.NodeID))
					}
					return
				}
			}
		}
	}

	// Store pubkey (protocol testnet allows override; protocol mainnet rejects mismatches)
	validatorPubKeysMu.RLock()
	// `existing` and `existingOK` store whether the related condition is satisfied.
	existing, existingOK := ValidatorPubKeys[ann.NodeID]
	validatorPubKeysMu.RUnlock()
	// `pubKeyUpdated` stores the value produced by this operation.
	pubKeyUpdated := !existingOK || !bytes.Equal(existing, pkBytes)
	if existingOK && len(existing) == ed25519.PublicKeySize {
		if !bytes.Equal(existing, pkBytes) {
			if !protocolIsTestnet() {
				if DebugConsensus {
					fmt.Printf("Pubkey mismatch for validator %s\n", ShortID(ann.NodeID))
				}
				return
			}
			if DebugConsensus {
				fmt.Printf("Pubkey override (testnet) for validator %s\n", ShortID(ann.NodeID))
			}
		}
	}
	validatorPubKeysMu.Lock()
	ValidatorPubKeys[ann.NodeID] = ed25519.PublicKey(pkBytes)
	validatorPubKeysMu.Unlock()
	if pubKeyUpdated {
		// A queued block may have failed signature verification before this
		// validator key refresh landed; retry immediately.
		go n.ProcessQueuedBlocks()
	}

	// Normalize heights for logging/state.
	reported := ann.ReportedHeight
	if reported == 0 {
		reported = ann.Height
	}
	// `finalized` stores the value produced by this operation.
	finalized := ann.FinalizedHeight
	if finalized == 0 {
		finalized = reported
	}
	// `execEpoch` stores the value produced by this operation.
	execEpoch := ann.ExecEpoch
	if execEpoch == 0 {
		execEpoch = finalized + 1
	}
	// `setHeight` stores the value produced by this operation.
	setHeight := validatorAnnouncementActivationHeight(ann)
	if setHeight == 0 {
		setHeight = execEpoch
	}

	if ann.P2PAddr != "" {
		n.HandlePeerHello("", ann.NodeID, ann.P2PAddr)
		if n.Host != nil && n.canDialPeer() {
			// `maddr` and `err` store the error produced by this operation.
			if maddr, err := ma.NewMultiaddr(ann.P2PAddr); err == nil {
				// `info` and `err` store the error produced by this operation.
				if info, err := peer.AddrInfoFromP2pAddr(maddr); err == nil {
					if info.ID != n.Host.ID() && len(n.Host.Network().ConnsToPeer(info.ID)) == 0 {
						n.connectToPeersAsync([]string{ann.P2PAddr}, 12*time.Second)
					}
				}
			}
		}
	}

	// `setHash` stores the digest used to identify or verify the related data.
	setHash := strings.ToLower(strings.TrimSpace(ann.ValidatorSetHash))
	// `inSet` stores the current position in the related collection.
	inSet := n.validatorInAnyHeartbeatSet(ann.NodeID, reported, finalized, execEpoch, setHeight)
	if !ann.IsValidator {
		// `authInSet` and `source` store the value produced by this operation.
		if authInSet, source := n.authoritativeHeartbeatMembershipForAnnouncement(ann.NodeID, reported, finalized, execEpoch, setHeight); authInSet {
			ann.IsValidator = true
			inSet = true
			if DebugConsensus {
				if n.shouldLogLivenessReason(fmt.Sprintf("heartbeat_fallback:%s:%d", ann.NodeID, reported), livenessReasonLogCooldown) {
					fmt.Printf("[HEARTBEAT-FALLBACK] id=%s height=%d source=%s reason=peer_advertised_candidate_but_in_set\n",
						ShortID(ann.NodeID), reported, source)
				}
			}
		}
	}

	// Candidate heartbeats (permissionless, no owner approval).
	// Strict activation relies on IsValidator=false to keep join/rejoin nodes
	// in candidate lane until frozen-set activation.
	if !ann.IsValidator || !inSet {
		n.registerCandidateHeartbeat(ann, ed25519.PublicKey(pkBytes), reported, finalized, execEpoch, setHeight, setHash)
		n.maybeOfferSnapshotToValidator(ann.NodeID, reported)
		if DebugConsensus {
			fmt.Printf(
				"Candidate heartbeat received: %s | reported_height=%d | finalized_height=%d | local_exec_epoch=%d\n",
				ShortID(ann.NodeID),
				reported,
				finalized,
				execEpoch,
			)
		}
		return
	}

	// Ignore only deeply stale heartbeats; allow small drift so quorum does not
	// collapse while lagging validators catch up.
	const staleHeartbeatIgnoreDrift uint64 = 8
	// `localFinalized` stores the value produced by this operation.
	localFinalized := uint64(0)
	localFinalized = n.getFinalizedHeight()
	if localFinalized > 0 && reported > 0 && localFinalized > reported+staleHeartbeatIgnoreDrift {
		n.maybeOfferSnapshotToValidator(ann.NodeID, reported)
		if DebugConsensus {
			fmt.Printf("Ignoring stale heartbeat: %s reported=%d local_finalized=%d\n",
				ShortID(ann.NodeID), reported, localFinalized)
		}
		return
	}

	// Register heartbeat + activation
	n.RegisterValidator(ann.NodeID, reported, finalized, execEpoch, setHeight, setHash)
	if ann.ConsensusReadySet {
		n.setValidatorConsensusReady(ann.NodeID, ann.ConsensusReady)
	}
	n.recordHeightReport(ann.NodeID, finalized)
	n.recomputeFinalizedHeight()
	n.addPendingValidator(ann.NodeID)
	// `currentEpoch` stores the value produced by this operation.
	currentEpoch := n.currentEpoch()
	// `syncing` stores the value produced by this operation.
	syncing, _, _ := n.effectiveConsensusSyncState(n.getFinalizedHeight())
	if !syncing {
		// `safeModeProgressed` stores the value produced by this operation.
		safeModeProgressed := false
		if ConsensusPostBlockSafeModeEnabled && currentEpoch > 0 {
			// `active` and `until` store the value produced by this operation.
			if active, until, _ := n.postBlockSafeModeState(currentEpoch); active || !until.IsZero() {
				safeModeProgressed = n.tryExitPostBlockSafeMode(currentEpoch)
			}
		}
		n.replayQueuedExecutionVotes()
		if safeModeProgressed {
			n.maybeBroadcastCurrentLeaderExecutionVote("validator_heartbeat")
		}
	}
	n.maybeRequestSyncFromHeartbeats()
	n.maybeOfferSnapshotToValidator(ann.NodeID, reported)
	n.maybeTriggerAdaptiveValidatorSnapshotPublish("validator_heartbeat")

	if DebugConsensus {
		fmt.Printf(
			"Validator heartbeat received: %s | reported_height=%d | finalized_height=%d | local_exec_epoch=%d\n",
			ShortID(ann.NodeID),
			reported,
			finalized,
			execEpoch,
		)
	}
}

// isSelfCandidateForHeight implements the is self candidate for height helper.
func (n *Node) isSelfCandidateForHeight(height uint64) bool {
	n.candidateMu.RLock()
	// `cand` stores the value produced by this operation.
	cand := n.candidates[n.ID]
	n.candidateMu.RUnlock()
	if cand == nil || cand.PermanentBan {
		return false
	}
	if cand.BanUntil > 0 && height < cand.BanUntil {
		return false
	}
	if height > 0 {
		if cand.LastFinalizedHeight < height && cand.LastReportedHeight < height {
			return false
		}
	}
	return true
}

// registerCandidateHeartbeat implements the register candidate heartbeat helper.
func (n *Node) registerCandidateHeartbeat(
	ann ValidatorAnnouncement,
	pk ed25519.PublicKey,
	reported uint64,
	finalized uint64,
	execEpoch uint64,
	validatorSetHeight uint64,
	validatorSetHash string,
) {
	n.candidateMu.Lock()
	defer n.candidateMu.Unlock()

	// `cand` and `ok` store whether the related condition is satisfied.
	cand, ok := n.candidates[ann.NodeID]
	if !ok {
		cand = &CandidateStatus{
			ID:         ann.NodeID,
			PubKey:     pk,
			ExecHashes: make(map[uint64]string),
		}
		cand.FirstSeenHeight = n.Blockchain.Height()
		n.candidates[ann.NodeID] = cand
	}

	if len(cand.PubKey) == 0 {
		cand.PubKey = pk
	} else if !bytes.Equal(cand.PubKey, pk) {
		if !protocolIsTestnet() {
			return
		}
		cand.PubKey = pk
	}

	cand.LastReportedHeight = reported
	cand.LastFinalizedHeight = finalized
	cand.HeartbeatTotal++
	if cand.LastHeartbeatEpoch == 0 || execEpoch >= cand.LastHeartbeatEpoch {
		cand.HeartbeatGood++
	}
	cand.LastHeartbeatEpoch = execEpoch
	if validatorSetHeight == 0 {
		validatorSetHeight = execEpoch
	}
	cand.LastValidatorSetHeight = validatorSetHeight
	cand.LastValidatorSetHash = strings.ToLower(strings.TrimSpace(validatorSetHash))
	cand.LastHeartbeatAt = time.Now()

	if !protocolDeterministicValidatorSelectionEnabled() {
		GlobalValidatorRegistry.Ensure(ann.NodeID, n.Blockchain.Height())
	}
	if len(pk) == ed25519.PublicKeySize {
		GlobalValidatorRegistry.mu.Lock()
		if rec := GlobalValidatorRegistry.records[ann.NodeID]; rec != nil && normalizeConsensusPubKeyHex(rec.ConsensusPubKey) == "" {
			rec.ConsensusPubKey = strings.ToLower(hex.EncodeToString(pk))
		}
		GlobalValidatorRegistry.mu.Unlock()
	}

	if cand.ObservationStartHeight == 0 {
		if reported > 0 || finalized > 0 {
			// `start` stores the value produced by this operation.
			start := reported
			if finalized > 0 && (start == 0 || finalized < start) {
				start = finalized
			}
			if start > 0 {
				cand.ObservationStartHeight = start
			}
		}
	}

	if cand.BanUntil > 0 && n.Blockchain.Height() >= cand.BanUntil {
		cand.BanUntil = 0
	}
}

// WaitForValidatorQuorum implements the wait for validator quorum helper.
func (n *Node) WaitForValidatorQuorum(min int, timeout time.Duration) bool {
	// `deadline` stores the value produced by this operation.
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if n.countLiveValidators() >= min {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// WaitForWalletAuth implements the wait for wallet auth helper.
func (n *Node) WaitForWalletAuth(ctx context.Context, timeout time.Duration) bool {
	// Stake gate for validator activation. Network startup is handled separately.
	if n.Role != "validator" {
		return true
	}
	// Core validators can be exempt from stake gate by policy.
	if ValidatorCoreStakeExempt && n.isCoreValidatorCurrent(n.localConsensusValidatorID()) {
		return true
	}
	if !ValidatorRequireStake {
		return true
	}
	// `deadline` stores the value produced by this operation.
	deadline := time.Now().Add(timeout)
	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		if n.hasRequiredValidatorStake() {
			return true
		}
		if timeout > 0 && time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// canParticipateAsValidator implements the can participate as validator helper.
func (n *Node) canParticipateAsValidator() bool {
	if n == nil {
		return false
	}
	if n.Role != "validator" {
		return false
	}
	if !isValidatorKeyUsable(n.ValidatorKey) {
		return false
	}
	// `height` stores the value produced by this operation.
	height := n.currentEpoch()
	localID := n.localConsensusValidatorIDForHeight(height)
	// Core validators can be held in pending phase and excluded from proposer/committee.
	if n.isCoreValidatorCurrent(localID) {
		if !n.coreEligibleForConsensus(height) {
			return false
		}
		return n.hasRequiredValidatorStake()
	}
	return n.hasRequiredValidatorStake()
}

// hasWalletLoginForValidator implements the has wallet login for validator helper.
func (n *Node) hasWalletLoginForValidator() bool {
	if n == nil {
		return false
	}
	localID := n.localConsensusValidatorID()
	if !n.requiresWalletAuthCurrent(localID) {
		return true
	}
	if n.bootstrapGenesisWalletAuthSatisfied(localID) {
		return true
	}
	authMu.Lock()
	// `walletAddr` stores the address used by this operation.
	walletAddr := authWalletAddr
	// `nodeID` stores the value produced by this operation.
	nodeID := authNodeID
	authMu.Unlock()
	return walletAddr != "" && nodeIdentityMapKey(nodeID) == nodeIdentityMapKey(n.ID)
}

// bootstrapGenesisWalletAuthSatisfied implements the bootstrap genesis wallet auth satisfied helper.
func (n *Node) bootstrapGenesisWalletAuthSatisfied(nodeID string) bool {
	if n == nil {
		return false
	}
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return false
	}
	return genesisWalletAuthExemptValidator(id)
}

// canParticipateInConsensusNow implements the can participate in consensus now helper.
func (n *Node) canParticipateInConsensusNow() bool {
	// `ready` stores the value produced by this operation.
	ready, _ := n.validatorParticipationGateStatus(0)
	return ready
}

// validatorParticipationGateStatus implements the validator participation gate status helper.
func (n *Node) validatorParticipationGateStatus(height uint64) (bool, string) {
	if n == nil {
		return false, "node_unavailable"
	}
	if height == 0 {
		height = n.currentEpoch()
	}
	// `isolated` and `reason` store the current position in the related collection.
	if isolated, reason := n.validatorSyncIsolationState(height); isolated {
		if strings.TrimSpace(reason) == "" {
			reason = "syncing"
		}
		return false, reason
	}
	if n.Role != "validator" {
		return false, "role_not_validator"
	}
	if !isValidatorKeyUsable(n.ValidatorKey) {
		return false, "validator_key_unavailable"
	}
	localID := n.localConsensusValidatorIDForHeight(height)
	if n.isCoreValidatorCurrent(localID) && !n.coreEligibleForConsensus(height) {
		return false, "core_pending_activation"
	}
	if !n.hasRequiredValidatorStake() {
		return false, "validator_stake_required"
	}
	if !n.isCoreRegistryTrustReadyForValidatorParticipation() {
		return false, "core_registry_unverified"
	}
	if !n.hasWalletLoginForValidator() {
		return false, "wallet_auth_required"
	}
	if validatorOnboardingStrictActivationEnabled() {
		// `active` and `reason` store the value produced by this operation.
		if active, reason := n.selfActiveValidatorAt(height); !active {
			if strings.TrimSpace(reason) == "" {
				reason = "activation_pending_not_in_frozen_set"
			}
			return false, reason
		}
	}
	return true, "ready"
}

// hasRequiredValidatorStake implements the has required validator stake helper.
func (n *Node) hasRequiredValidatorStake() bool {
	if n == nil {
		return false
	}
	validatorID := n.localConsensusValidatorID()
	// Core validators can be exempt from stake gate by policy.
	if ValidatorCoreStakeExempt && n.isCoreValidatorCurrent(validatorID) {
		return true
	}
	if !ValidatorRequireStake {
		return true
	}
	if validatorID == "" {
		return false
	}

	// `required` stores the request data being processed.
	required := int(ConsensusValidatorMinStake)
	// `ledgers` stores the value produced by this operation.
	ledgers := []Ledger{
		n.currentExecutionLedgerClone(),
		n.Ledger.Clone(),
		n.ExecutionLedger.Clone(),
	}
	// `ledger` tracks the current values while iterating.
	for _, ledger := range ledgers {
		// `total` stores the measured quantity used by this operation.
		total := 0
		// `amount` tracks the current values while iterating.
		for _, amount := range validatorStakeTotals(&ledger, validatorID) {
			total += amount
		}
		if total >= required {
			return true
		}
	}

	return false
}

// WaitForWalletLogin blocks until a wallet authentication is completed for this node.
// Unlike WaitForWalletAuth, it does NOT require stake eligibility.
func (n *Node) WaitForWalletLogin(ctx context.Context, timeout time.Duration) bool {
	if !n.requiresWalletAuthCurrent(n.localConsensusValidatorID()) {
		return true
	}
	// `deadline` stores the value produced by this operation.
	deadline := time.Now().Add(timeout)
	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		if n.hasWalletLoginForValidator() {
			return true
		}
		if timeout > 0 && time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// handleConsensusEnvelope handles consensus envelope.
func (n *Node) handleConsensusEnvelope(data []byte) bool {
	return n.handleConsensusEnvelopeFromPeer(data, "")
}

// handleConsensusEnvelopeFromPeer handles consensus envelope from peer.
func (n *Node) handleConsensusEnvelopeFromPeer(data []byte, peerID string) bool {
	// `wrapped` stores the value used by this operation.
	var wrapped Message
	// `err` stores the error produced by this operation.
	if err := UnmarshalP2PMessage(data, &wrapped); err != nil || wrapped.Type == "" {
		return false
	}
	switch wrapped.Type {
	case MsgExecutionResult:
		// `res` stores the result produced by this operation.
		var res ExecutionResultMsg
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(wrapped.Data, &res); err == nil {
			// `allowed` and `reason` store whether the related condition is satisfied.
			if allowed, reason := n.allowExecutionVoteNetworkIngress(res); !allowed {
				n.logExecutionVoteIngressDrop(reason, res, "consensus_gossip")
				return true
			}
			_ = n.submitExecutionResultOnConsensusLane(res, true)
			return true
		}
	case MsgLeaderBlock:
		// `block` stores the synchronization state protecting shared data.
		var block Block
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(wrapped.Data, &block); err == nil {
			_ = n.submitLeaderBlockOnConsensusLane(block, "")
			return true
		}
	case MsgValidatorAnnounce:
		n.handleValidatorAnnouncement(wrapped.Data)
		return true
	case MsgCommit:
		// `cm` stores the value used by this operation.
		var cm CommitMsg
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(wrapped.Data, &cm); err == nil {
			_ = n.submitCommitMsgOnConsensusLane(cm)
			return true
		}
	case MsgSnapshotOffer:
		// `offer` stores the value used by this operation.
		var offer SnapshotOffer
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(wrapped.Data, &offer); err == nil {
			n.handleSnapshotOffer(offer)
			return true
		}
	case MsgSnapshotMeta:
		// `meta` stores the value used by this operation.
		var meta SnapshotMetaGossip
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(wrapped.Data, &meta); err == nil {
			n.handleSnapshotMetaGossipMessage(meta)
			return true
		}
	case MsgSnapshotChunk:
		// `chunk` stores the value used by this operation.
		var chunk SnapshotChunkGossip
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(wrapped.Data, &chunk); err == nil {
			n.handleSnapshotChunkGossipMessage(chunk)
			return true
		}
	case MsgSnapshotProof:
		// `proof` stores the value used by this operation.
		var proof SnapshotProof
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(wrapped.Data, &proof); err == nil {
			n.handleSnapshotProofFromPeer(proof, peerID)
			return true
		}
	}
	return false
}

// listenConsensus implements the listen consensus helper.
func (n *Node) listenConsensus(ctx context.Context) {
	// `sub` stores the value produced by this operation.
	sub := n.ConsensusSub
	if sub == nil {
		if n.PubSub == nil {
			log.Println("ConsensusSub is nil")
			return
		}
		// `err` stores the error produced by this operation.
		var err error
		if n.ConsensusTopic == nil {
			n.ConsensusTopic, err = n.PubSub.Join(TopicConsensus)
			if err != nil {
				log.Printf("consensus topic rejoin failed: %v", err)
				return
			}
		}
		sub, err = n.ConsensusTopic.Subscribe()
		if err != nil {
			log.Printf("consensus subscription rejoin failed: %v", err)
			return
		}
		n.ConsensusSub = sub
		log.Println("[GOSSIP-REPAIR] consensus subscription restored")
	}
	defer func() {
		if n.ConsensusSub == sub {
			n.ConsensusSub = nil
		}
		sub.Cancel()
	}()

	for {
		// `msg` and `err` store the error produced by this operation.
		msg, err := sub.Next(ctx)
		if err != nil {
			log.Println("consensus listener stopped:", err)
			return
		}
		if n.Host != nil && msg.ReceivedFrom == n.Host.ID() {
			continue
		}
		_ = n.handleConsensusEnvelope(msg.Data)
	}
}

// listenValidators implements the listen validators helper.
func (n *Node) listenValidators(ctx context.Context) {
	// `sub` stores the value produced by this operation.
	sub := n.ValidatorSub
	if sub == nil {
		if n.PubSub == nil {
			log.Println("ValidatorSub is nil")
			return
		}
		// `err` stores the error produced by this operation.
		var err error
		if n.ValidatorTopic == nil {
			n.ValidatorTopic, err = n.PubSub.Join(TopicValidator)
			if err != nil {
				log.Printf("validator topic rejoin failed: %v", err)
				return
			}
		}
		sub, err = n.ValidatorTopic.Subscribe()
		if err != nil {
			log.Printf("validator subscription rejoin failed: %v", err)
			return
		}
		n.ValidatorSub = sub
		log.Println("[GOSSIP-REPAIR] validator subscription restored")
	}
	defer func() {
		if n.ValidatorSub == sub {
			n.ValidatorSub = nil
		}
		sub.Cancel()
	}()

	for {
		// `msg` and `err` store the error produced by this operation.
		msg, err := sub.Next(ctx)
		if err != nil {
			log.Println("validator listener stopped:", err)
			return
		}

		if n.handleConsensusEnvelope(msg.Data) {
			continue
		}

		// Support both raw ValidatorInfo and Message wrappers
		var wrapped Message
		// `err` stores the error produced by this operation.
		if err := UnmarshalP2PMessage(msg.Data, &wrapped); err == nil && wrapped.Type != "" {
			switch wrapped.Type {
			case MsgValidatorAnnounce:
				n.handleValidatorAnnouncement(wrapped.Data)
			case MsgValidatorSetUpdate:
				// `update` stores the value used by this operation.
				var update ValidatorSetUpdate
				// `err` stores the error produced by this operation.
				if err := json.Unmarshal(wrapped.Data, &update); err == nil {
					n.handleValidatorSetUpdate(update)
				}
			}
			continue
		}

		// `info` stores the current position in the related collection.
		var info ValidatorInfo
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(msg.Data, &info); err != nil {
			continue
		}
		info.ID = normalizeValidatorID(info.ID)
		if info.ID == "" {
			continue
		}

		n.validatorMu.Lock()

		// `st` and `ok` store whether the related condition is satisfied.
		st, ok := n.validatorStatus[info.ID]
		if !ok {
			st = &ValidatorStatus{}
			n.validatorStatus[info.ID] = st
		}

		st.Height = info.Height
		st.LastSeen = time.Now()

		participationMu.Lock()
		// `ok` stores whether the related condition is satisfied.
		if _, ok := Participation[info.ID]; !ok {
			Participation[info.ID] = &ParticipationScore{
				ValidBlocks:   1,
				InvalidBlocks: 0,
				LastSeen:      time.Now(),
				CooldownUntil: 0,
				Reputation:    100,
			}
		}
		participationMu.Unlock()

		n.validatorMu.Unlock()
	}
}

// BuildGenesisBlock builds genesis block.
func BuildGenesisBlock(g Genesis) (Block, error) {

	if g.ChainID == "" {
		return Block{}, errors.New("genesis missing chain_id")
	}
	if !isProtocolChainID(g.ChainID) {
		return Block{}, fmt.Errorf("genesis chain_id mismatch: got=%s want=%s", strings.TrimSpace(g.ChainID), protocolChainID())
	}
	g.ChainID = protocolChainID()
	if len(g.Validators) == 0 {
		return Block{}, errors.New("genesis has no validators")
	}

	// ÃƒÂ°Ã…Â¸Ã¢â‚¬ÂÃ¢â‚¬â„¢ deterministic payload (NO maps in block)
	payload := struct {
		// `ChainID` stores the value associated with this record.
		ChainID string
		// `Validators` stores whether the related condition is satisfied.
		Validators map[string]string
	}{
		ChainID:    g.ChainID,
		Validators: g.Validators,
	}

	// `payloadBytes` stores the value produced by this operation.
	payloadBytes, _ := json.Marshal(payload)
	// `stateHash` stores the digest used to identify or verify the related data.
	stateHash := sha256.Sum256(payloadBytes)

	// `block` stores the synchronization state protecting shared data.
	block := Block{
		ID:        0,
		Type:      BlockTypeGenesis,
		PrevHash:  "",
		Proposer:  "genesis",
		Timestamp: 0, // ÃƒÂ°Ã…Â¸Ã¢â‚¬ÂÃ¢â‚¬â„¢ MUST be deterministic
		BlockTime: LogicalTimeForEpoch(0),
		StateRoot: hex.EncodeToString(stateHash[:]),
	}

	block.BlockHash = HashBlock(block)
	return block, nil
}

// CalculateHash implements the calculate hash helper.
func (b Block) CalculateHash() string {
	// `data` stores the value produced by this operation.
	data := ""
	if validatorSetCommitmentV2EnabledAt(b.ID) {
		// `activationHeight` stores the value produced by this operation.
		activationHeight := canonicalActivationHeight(b.NextValidatorSetHeight, b.ActivationHeight)
		data = fmt.Sprintf(
			"%d|%s|%d|%s|%s|%s|%s|%d|%s|%x",
			b.ID,
			b.PrevHash,
			SystemTimeUnits(b.BlockTime),
			b.Proposer,
			b.StateRoot,
			b.MempoolRoot,
			b.ValidatorSetHash,
			activationHeight,
			strings.TrimSpace(b.NextValidatorSetHash),
			b.Payload,
		)
	} else {
		data = fmt.Sprintf(
			"%d|%s|%d|%s|%s|%s|%s|%x",
			b.ID,
			b.PrevHash,
			SystemTimeUnits(b.BlockTime),
			b.Proposer,
			b.StateRoot,
			b.MempoolRoot,
			b.ValidatorSetHash,
			b.Payload,
		)
	}
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// StoreBlock stores block.
func (db *NodeDB) StoreBlock(block Block) error {
	// `err` stores the error produced by this operation.
	err := db.Blocks.Update(func(txn *Txn) error {
		// `height` stores the value produced by this operation.
		height := block.ID
		if height == 0 && block.Height != 0 {
			height = block.Height
		}
		// `key` stores the key used to access the related value.
		key := []byte(fmt.Sprintf("block:%d", height))
		// `val` stores the value currently being processed.
		val, _ := json.Marshal(block)
		// `enc` and `err` store the error produced by this operation.
		enc, err := encryptDBValue(val)
		if err != nil {
			return err
		}
		return txn.Set(key, enc)
	})
	if err != nil {
		return err
	}
	return db.StoreTxRecords(block)
}

// LoadGenesisFromFile loads genesis from file.
func LoadGenesisFromFile(
	db *NodeDB,
	bc *Blockchain,
	path string,
) (*Genesis, error) {

	// `g` and `err` store the error produced by this operation.
	g, err := loadGenesisFromDisk(path)
	if err != nil {
		return nil, fmt.Errorf("invalid genesis json: %w", err)
	}
	// `block` and `err` store the error produced by this operation.
	block, err := BuildGenesisBlock(*g)
	if err != nil {
		return nil, err
	}
	ChainID = protocolChainID()

	// `err` stores the error produced by this operation.
	if err := db.StoreBlock(block); err != nil {
		return nil, err
	}

	bc.Blocks = []Block{block}

	log.Println("ÃƒÂ¢Ã…â€œÃ¢â‚¬Â¦ Genesis loaded from genesis.json (authoritative)")
	return g, nil
}

// At startup ONLY
func LoadGenesisValidators(node *Node) {
	// `genesis` stores the value produced by this operation.
	genesis := LoadGenesisFile("genesis.json")

	node.validatorMu.Lock()
	defer node.validatorMu.Unlock()

	node.validatorStatus = make(map[string]*ValidatorStatus)

	// `id` tracks the current position in the related collection.
	for id := range genesis.Validators {
		id = normalizeValidatorID(id)
		if id == "" {
			continue
		}
		node.validatorStatus[id] = &ValidatorStatus{
			Height:   0,
			LastSeen: time.Unix(0, 0), // ÃƒÂ°Ã…Â¸Ã¢â‚¬ÂÃ¢â‚¬â„¢ deterministic
		}

		Participation[id] = &ParticipationScore{
			ValidBlocks:   1,
			InvalidBlocks: 0,
			LastSeen:      time.Unix(0, 0),
			CooldownUntil: 0,
			Reputation:    100,
		}
	}
}

// LoadGenesisFile loads genesis file.
func LoadGenesisFile(path string) *GenesisFile {
	// `data` and `err` store the error produced by this operation.
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[WARN] Failed to read genesis file %s: %v", path, err)
		return &GenesisFile{
			ChainID:    protocolChainID(),
			Validators: map[string]string{},
		}
	}

	// `g` stores the value used by this operation.
	var g GenesisFile
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(data, &g); err != nil {
		log.Printf("[WARN] Invalid genesis format in %s: %v", path, err)
		return &GenesisFile{
			ChainID:    protocolChainID(),
			Validators: map[string]string{},
		}
	}
	if !isProtocolChainID(g.ChainID) {
		log.Printf("[WARN] Genesis chain_id mismatch in %s: got=%s want=%s", path, strings.TrimSpace(g.ChainID), protocolChainID())
		return &GenesisFile{
			ChainID:    protocolChainID(),
			Validators: map[string]string{},
		}
	}
	g.ChainID = protocolChainID()
	ChainID = protocolChainID()

	if len(g.Validators) == 0 {
		log.Printf("[WARN] Genesis has zero validators (%s). Node will continue in degraded/sync mode.", path)
	}

	return &g
}

// keys implements the keys helper.
func keys[K comparable, V any](m map[K]V) []K {
	// `out` stores the result produced by this operation.
	out := make([]K, 0, len(m))
	// `k` tracks the current values while iterating.
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprint(out[i]) < fmt.Sprint(out[j])
	})
	return out
}

// RegisterValidator implements the register validator helper.
func (n *Node) RegisterValidator(addr string, reportedHeight uint64, finalizedHeight uint64, execEpoch uint64, validatorSetHeight uint64, validatorSetHash string) {
	addr = normalizeValidatorID(addr)
	if addr == "" {
		return
	}
	if !n.validatorInAnyHeartbeatSet(addr, reportedHeight, finalizedHeight, execEpoch, validatorSetHeight) {
		// Anti-flap grace: keep refreshing recently live validators during
		// transient set-height races around epoch transitions.
		st, ok := n.validatorStatusSnapshot(addr)
		if !ok || st.LastSeen.IsZero() || time.Since(st.LastSeen) > 20*time.Second {
			return
		}
	}
	n.validatorMu.Lock()
	defer n.validatorMu.Unlock()

	// `st` and `exists` store whether the related condition is satisfied.
	st, exists := n.validatorStatus[addr]
	if !exists {
		st = &ValidatorStatus{}
		n.validatorStatus[addr] = st
	}

	st.LastSeen = time.Now()
	st.ReportedHeight = reportedHeight
	if finalizedHeight == 0 {
		finalizedHeight = reportedHeight
	}
	st.FinalizedHeight = finalizedHeight
	st.ExecEpoch = execEpoch
	st.Height = finalizedHeight
	if validatorSetHeight == 0 {
		validatorSetHeight = execEpoch
	}
	st.ValidatorSetHeight = validatorSetHeight
	st.ValidatorSetHash = strings.ToLower(strings.TrimSpace(validatorSetHash))
	st.Active = true
	n.recordValidatorRejoinHeartbeatLocked(addr)

	participationMu.Lock()
	// `ok` stores whether the related condition is satisfied.
	if _, ok := Participation[addr]; !ok {
		Participation[addr] = &ParticipationScore{
			ValidBlocks:   1,
			InvalidBlocks: 0,
			LastSeen:      time.Now(),
			CooldownUntil: 0,
			Reputation:    100,
		}
	}
	participationMu.Unlock()

	if DebugConsensus {
		fmt.Printf("Validator heartbeat: %s | reported_height=%d | finalized_height=%d | local_exec_epoch=%d\n",
			ShortID(addr), reportedHeight, finalizedHeight, execEpoch)
	}
}

// setValidatorConsensusReady implements the set validator consensus ready helper.
func (n *Node) setValidatorConsensusReady(addr string, ready bool) {
	if n == nil {
		return
	}
	addr = normalizeValidatorID(addr)
	if addr == "" {
		return
	}
	n.validatorMu.Lock()
	defer n.validatorMu.Unlock()
	// `st` and `ok` store whether the related condition is satisfied.
	st, ok := n.validatorStatus[addr]
	if !ok || st == nil {
		return
	}
	st.Enabled = ready
	st.ConsensusReadyKnown = true
}
