package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

type blockVerificationContext struct {
	// `Last` stores the value associated with this record.
	Last Block
	// `Validators` stores whether the related condition is satisfied.
	Validators []string
	// `ExpectedLeader` stores the value associated with this record.
	ExpectedLeader string
	// `SyncContinuityFallback` stores the value associated with this record.
	SyncContinuityFallback bool
}

// verifyBlockProductionEnvelope validates a proposed block's structure, continuity, signatures, evidence, and finality.
func (n *Node) verifyBlockProductionEnvelope(block Block, bc *Blockchain) (blockVerificationContext, error) {
	// `ctx` stores the context controlling this operation.
	ctx := blockVerificationContext{}
	if n == nil {
		return ctx, errors.New("nil_node")
	}
	if bc == nil {
		return ctx, errors.New("nil_blockchain")
	}
	// `err` stores the error produced by this operation.
	if err := verifyBlockStructure(block); err != nil {
		return ctx, err
	}

	// `last` stores the value produced by this operation.
	last := bc.LastBlock()
	ctx.Last = last
	// `err` stores the error produced by this operation.
	if err := n.verifyBlockContinuity(block, last); err != nil {
		return ctx, err
	}
	if err := validateBlockProtocolTransition(last, block); err != nil {
		return ctx, err
	}
	// `err` stores the error produced by this operation.
	if err := verifyBlockTimestampEnvelope(block, last); err != nil {
		return ctx, err
	}
	// `err` stores the error produced by this operation.
	if err := verifyBlockHashEnvelope(block); err != nil {
		return ctx, err
	}
	// `validators` and `fallback` store whether the related condition is satisfied.
	validators, fallback := n.resolveBlockVerificationValidators(block)
	if len(validators) == 0 {
		return ctx, errors.New("validator_set_unresolved")
	}
	ctx.Validators = validators
	ctx.SyncContinuityFallback = fallback
	ctx.ExpectedLeader = n.consensusLeaderForHeightRound(block.ID, block.Round, validators)

	if !fallback {
		// `err` stores the error produced by this operation.
		if err := verifyBlockProposerMembership(block, validators); err != nil {
			return ctx, err
		}
	}
	// `err` stores the error produced by this operation.
	if err := n.verifyBlockProposerSignatureEnvelope(block, validators); err != nil {
		return ctx, err
	}
	// `err` stores the error produced by this operation.
	if err := verifyBlockDuplicateEvidence(block); err != nil {
		return ctx, err
	}
	// `err` stores the error produced by this operation.
	if err := n.verifyBlockConsensusEvidence(block, validators); err != nil {
		return ctx, err
	}
	// `err` stores the error produced by this operation.
	if err := n.verifyFinalityCommitments(block, validators); err != nil {
		return ctx, err
	}
	// `err` stores the error produced by this operation.
	if err := verifyBlockTransactionAuthenticity(block); err != nil {
		return ctx, err
	}
	return ctx, nil
}

// verifyBlockStructure checks the required block fields and protocol transaction limit.
func verifyBlockStructure(block Block) error {
	if block.ID == 0 {
		return errors.New("invalid_height")
	}
	if block.Height != 0 && block.Height != block.ID {
		return errors.New("invalid_height_alias")
	}
	if strings.TrimSpace(block.BlockHash) == "" {
		return errors.New("block_hash_missing")
	}
	if strings.TrimSpace(block.PrevHash) == "" {
		return errors.New("prev_hash_missing")
	}
	if strings.TrimSpace(block.Proposer) == "" {
		return errors.New("proposer_missing")
	}
	if strings.TrimSpace(block.StateRoot) == "" {
		return errors.New("state_root_missing")
	}
	if err := validateBlockProtocolEnvelope(block); err != nil {
		return err
	}
	if strings.TrimSpace(block.Hash) != "" && !strings.EqualFold(strings.TrimSpace(block.Hash), strings.TrimSpace(block.BlockHash)) {
		return errors.New("legacy_hash_mismatch")
	}
	if len(block.Transactions) > ConsensusMaxTxPerBlock {
		return errors.New("too_many_transactions")
	}
	return nil
}

// verifyBlockContinuity verifies block continuity.
func (n *Node) verifyBlockContinuity(block Block, last Block) error {
	if block.ID != last.ID+1 {
		return errors.New("invalid_height")
	}
	if !strings.EqualFold(strings.TrimSpace(block.PrevHash), strings.TrimSpace(last.BlockHash)) {
		return errors.New("prev_hash_mismatch")
	}
	if n != nil && n.hasCommittedDifferentHash(block.ID, block.BlockHash) {
		n.recordFinalizedHashConflictEvidence(block.ID, block.Round, "", block.BlockHash, "verification_continuity")
		return errors.New("committed_different_hash")
	}
	return nil
}

// verifyBlockTimestampEnvelope verifies block timestamp envelope.
func verifyBlockTimestampEnvelope(block Block, parent Block) error {
	if block.BlockTime.Epoch != block.ID {
		return errors.New("invalid_epoch")
	}
	if block.Timestamp != int64(SystemTimeUnits(block.BlockTime)) {
		return errors.New("invalid_timestamp")
	}
	if parent.ID > 0 && block.Timestamp <= parent.Timestamp {
		return errors.New("non_monotonic_timestamp")
	}
	return nil
}

// verifyBlockHashEnvelope verifies block hash envelope.
func verifyBlockHashEnvelope(block Block) error {
	if !strings.EqualFold(strings.TrimSpace(HashBlock(block)), strings.TrimSpace(block.BlockHash)) {
		return errors.New("hash_mismatch")
	}
	return nil
}

// verifyBlockProposerSignatureEnvelope verifies block proposer signature envelope
// against deterministic validator-registry key material when it is available.
func (n *Node) verifyBlockProposerSignatureEnvelope(block Block, validators []string) error {
	if len(block.Signature) == 0 {
		if protocolRequiresStrictConsensusSignatures() {
			return errors.New("invalid_block_signature")
		}
		return nil
	}
	// `validSig` stores whether the related condition is satisfied.
	validSig := false
	// `candidates` stores the deterministic public keys accepted for the proposer.
	candidates := n.blockProposerSignatureCandidates(block, validators)
	// `strictRegistry` stores whether mutable/runtime key fallback is forbidden.
	strictRegistry := strings.TrimSpace(block.ValidatorRegistryHash) != "" ||
		(n != nil && n.validatorRegistryCommitmentRequiredAt(block.ID))
	if len(candidates) > 0 {
		validSig = verifyBlockSignatureWithCandidates(block, candidates)
	} else if !strictRegistry {
		// Legacy/bootstrap compatibility only: before validator-registry
		// commitments are active, older blocks may be verifiable only through
		// genesis/runtime key maps. Committed-registry blocks must not fall back
		// to mutable local peer/key state.
		validSig = VerifyBlockSignature(block)
	}
	if !validSig {
		if block.BlockTime.Tick == TickFinalize && len(block.ExecutionResults) > 0 && block.RequiredQuorum > 0 {
			return nil
		}
		return errors.New("invalid_block_signature")
	}
	return nil
}

// blockProposerSignatureCandidates returns deterministic proposer public-key
// candidates from the validator-registry snapshot committed for this height.
func (n *Node) blockProposerSignatureCandidates(block Block, validators []string) []ed25519.PublicKey {
	if n == nil || block.ID == 0 {
		return nil
	}
	// `proposerID` stores the value produced by this operation.
	proposerID := normalizeValidatorID(block.Proposer)
	if proposerID == "" {
		return nil
	}
	if len(validators) > 0 && !containsValidatorID(validators, proposerID) {
		return nil
	}
	// `snapshot` stores the value produced by this operation.
	snapshot := n.committedProposerRegistrySnapshot(block)
	if len(snapshot) == 0 {
		return nil
	}
	// `rec` and `ok` store whether the related condition is satisfied.
	rec, ok := validatorRecordFromStakeSnapshot(snapshot, proposerID)
	if !ok {
		return nil
	}
	// `pubHex` stores the value produced by this operation.
	pubHex := normalizeConsensusPubKeyHex(rec.ConsensusPubKey)
	if pubHex == "" {
		return nil
	}
	// `pubRaw` and `err` store the error produced by this operation.
	pubRaw, err := hex.DecodeString(pubHex)
	if err != nil || len(pubRaw) != ed25519.PublicKeySize {
		return nil
	}
	return []ed25519.PublicKey{ed25519.PublicKey(append([]byte(nil), pubRaw...))}
}

// committedProposerRegistrySnapshot resolves the chain-committed registry
// snapshot that authorizes the proposer for the block height.
func (n *Node) committedProposerRegistrySnapshot(block Block) map[string]ValidatorRecord {
	if n == nil || block.ID == 0 {
		return nil
	}
	// Validator set and proposer authority for block H are derived from the
	// committed parent state. Sparse anchors can also carry a current-height
	// registry snapshot, so check both deterministic sources.
	if block.ID > 1 {
		// `parentHeight` stores the value produced by this operation.
		parentHeight := block.ID - 1
		// `snapshot` and `ok` store whether the related condition is satisfied.
		if snapshot, _, _, ok := n.resolveCommittedValidatorRegistrySnapshot(parentHeight); ok && len(snapshot) > 0 {
			return snapshot
		}
	}
	// `snapshot` and `ok` store whether the related condition is satisfied.
	if snapshot, _, _, ok := n.resolveCommittedValidatorRegistrySnapshot(block.ID); ok && len(snapshot) > 0 {
		return snapshot
	}
	return nil
}

// resolveBlockVerificationValidators implements the resolve block verification validators helper.
func (n *Node) resolveBlockVerificationValidators(block Block) ([]string, bool) {
	if n == nil {
		return nil, false
	}
	// `validators` stores whether the related condition is satisfied.
	validators := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID)))
	// `fallback` stores the value produced by this operation.
	fallback := false
	// `ok` stores whether the related condition is satisfied.
	if len(validators) == 0 {
		// `recovered` stores the value produced by this operation.
		if recovered := n.syncContinuityValidatorFallback(block); len(recovered) > 0 {
			validators = recovered
			fallback = true
			if DebugConsensus || DebugSync {
				fmt.Printf("[SYNC-VALIDATOR-FALLBACK] height=%d source=queued_child_continuity validators=%d hash=%s\n",
					block.ID, len(validators), ShortHash(block.ValidatorSetHash))
			}
		}
	} else if _, ok := n.queuedChildExtendsBlockDuringSync(block); ok {
		fallback = true
		if DebugConsensus || DebugSync {
			fmt.Printf("[SYNC-LEADER-FALLBACK] height=%d source=queued_child_continuity validators=%d hash=%s\n",
				block.ID, len(validators), ShortHash(block.ValidatorSetHash))
		}
	}
	return canonicalValidatorIDs(validators), fallback
}

// verifyBlockProposerMembership verifies block proposer membership.
func verifyBlockProposerMembership(block Block, validators []string) error {
	if !containsNormalizedValidatorID(validators, block.Proposer) {
		return errors.New("invalid_proposer")
	}
	return nil
}

// verifyBlockDuplicateEvidence verifies block duplicate evidence.
func verifyBlockDuplicateEvidence(block Block) error {
	// `seenSigners` stores the value produced by this operation.
	seenSigners := make(map[string]struct{}, len(block.Signatures))
	// `signer` tracks the current values while iterating.
	for _, signer := range block.Signatures {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(signer)
		if id == "" {
			return errors.New("empty_block_signature_signer")
		}
		// `exists` stores whether the related condition is satisfied.
		if _, exists := seenSigners[id]; exists {
			return errors.New("duplicate_block_signature_signer")
		}
		seenSigners[id] = struct{}{}
	}

	// `seenResults` stores the value produced by this operation.
	seenResults := make(map[string]struct{}, len(block.ExecutionResults))
	// `result` tracks the result produced by this operation.
	for _, result := range block.ExecutionResults {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(result.Signer)
		if id == "" {
			return errors.New("empty_execution_result_signer")
		}
		// `exists` stores whether the related condition is satisfied.
		if _, exists := seenResults[id]; exists {
			return errors.New("duplicate_execution_result_signer")
		}
		seenResults[id] = struct{}{}
	}
	return nil
}

// verifyBlockConsensusEvidence verifies block consensus evidence.
func (n *Node) verifyBlockConsensusEvidence(block Block, validators []string) error {
	// `validatorSet` stores whether the related condition is satisfied.
	validatorSet := make(map[string]struct{}, len(validators))
	// `id` tracks the current position in the related collection.
	for _, id := range canonicalValidatorIDs(validators) {
		validatorSet[id] = struct{}{}
	}
	if len(validatorSet) == 0 {
		return errors.New("validator_set_unresolved")
	}

	// `signer` tracks the current values while iterating.
	for _, signer := range block.Signatures {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(signer)
		// `ok` stores whether the related condition is satisfied.
		if _, ok := validatorSet[id]; !ok {
			return fmt.Errorf("signature_signer_not_validator: %s", id)
		}
	}

	// `executionSigners` stores the value produced by this operation.
	executionSigners := make(map[string]struct{}, len(block.ExecutionResults))
	// `result` tracks the result produced by this operation.
	for _, result := range block.ExecutionResults {
		// `signer` stores the value produced by this operation.
		signer := normalizeValidatorID(result.Signer)
		// `ok` stores whether the related condition is satisfied.
		if _, ok := validatorSet[signer]; !ok {
			return fmt.Errorf("execution_signer_not_validator: %s", signer)
		}
		// `signatureVerified` stores the value produced by this operation.
		signatureVerified := false
		if result.Height != 0 && result.Height != block.ID {
			return errors.New("execution_result_height_mismatch")
		}
		// `resultBlockHash` stores the digest used to identify or verify the related data.
		if resultBlockHash := strings.TrimSpace(result.BlockHash); resultBlockHash != "" {
			// `finalHash` stores the digest used to identify or verify the related data.
			finalHash := strings.TrimSpace(block.BlockHash)
			// `proposalHash` stores the digest used to identify or verify the related data.
			proposalHash := executionVoteProposalHashForFinalBlock(block)
			if !strings.EqualFold(resultBlockHash, finalHash) && (proposalHash == "" || !strings.EqualFold(resultBlockHash, proposalHash)) {
				if !n.verifyBlockExecutionResultSignatureForHint(result, block, resultBlockHash) {
					// Legacy v1 votes omit block-hash hints; accept them when the
					// exec/merkle signature still matches under the canonical paths.
					if err := n.verifyBlockExecutionResultSignature(result, block); err != nil {
						return errors.New("execution_result_block_mismatch")
					}
				}
				signatureVerified = true
			}
		}
		if strings.TrimSpace(result.ResultHash) == "" {
			return errors.New("execution_result_hash_missing")
		}
		if !strings.EqualFold(strings.TrimSpace(result.ResultHash), strings.TrimSpace(block.StateRoot)) {
			return errors.New("execution_result_state_root_mismatch")
		}
		if strings.TrimSpace(result.TxMerkle) != "" && !strings.EqualFold(strings.TrimSpace(result.TxMerkle), strings.TrimSpace(block.MempoolRoot)) {
			return errors.New("execution_result_tx_merkle_mismatch")
		}
		if !executionResultHashMatches(result.ExecutionResultHash, executionResultHashFromBlockResult(result, block)) {
			return errors.New("execution_result_hash_mismatch")
		}
		if protocolRequiresStrictConsensusSignatures() && block.RequiredQuorum > 0 && strings.TrimSpace(result.Signature) == "" {
			return errors.New("execution_result_signature_missing")
		}
		if !signatureVerified {
			// `err` stores the error produced by this operation.
			if err := n.verifyBlockExecutionResultSignature(result, block); err != nil {
				return err
			}
		}
		executionSigners[signer] = struct{}{}
	}

	// `err` stores the error produced by this operation.
	if err := verifyBlockQuorumMetadata(block, len(validatorSet)); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := verifyBlockExecutionQuorumEvidence(block, executionSigners); err != nil {
		return err
	}
	return n.validateCommittedBlockQuorumEvidence(block)
}

// verifyBlockExecutionQuorumEvidence verifies block execution quorum evidence.
func verifyBlockExecutionQuorumEvidence(block Block, executionSigners map[string]struct{}) error {
	if len(block.ExecutionResults) == 0 || block.RequiredQuorum <= 0 {
		return nil
	}
	if len(executionSigners) < block.RequiredQuorum {
		// `mode` stores the value produced by this operation.
		mode := strings.ToUpper(strings.TrimSpace(block.ConsensusMode))
		if mode == "" {
			mode = "NORMAL"
		}
		return fmt.Errorf("execution_quorum_evidence_shortfall: signers=%d required=%d mode=%s", len(executionSigners), block.RequiredQuorum, mode)
	}
	return nil
}

// verifyBlockExecutionResultSignatureForHint verifies block execution result signature for hint.
func verifyBlockExecutionResultSignatureForHint(result ExecutionResult, block Block, blockHashHint string) bool {
	return (*Node)(nil).verifyBlockExecutionResultSignatureForHint(result, block, blockHashHint)
}

// verifyBlockExecutionResultSignatureForHint verifies block execution result signature for hint.
func (n *Node) verifyBlockExecutionResultSignatureForHint(result ExecutionResult, block Block, blockHashHint string) bool {
	// `sigHex` stores the value produced by this operation.
	sigHex := strings.TrimSpace(result.Signature)
	blockHashHint = strings.TrimSpace(blockHashHint)
	if sigHex == "" || blockHashHint == "" {
		return false
	}
	// `sig` and `err` store the error produced by this operation.
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	// `candidates` stores the value produced by this operation.
	candidates := n.execResultPubKeyCandidatesForHeight(result.Signer, block.ID)
	if len(candidates) == 0 {
		return false
	}
	// `msg` stores the value produced by this operation.
	msg := ExecutionResultMsg{
		HeightHint:    block.ID,
		RoundHint:     block.Round,
		BlockHashHint: blockHashHint,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      strings.TrimSpace(result.ResultHash),
		TxMerkle:      strings.TrimSpace(result.TxMerkle),
		Signer:        strings.TrimSpace(result.Signer),
		Signature:     sigHex,
	}
	if result.Round > 0 {
		msg.RoundHint = result.Round
	}
	return verifyExecutionResultSignature(msg, candidates, sig)
}

// verifyBlockQuorumMetadata verifies block quorum metadata.
func verifyBlockQuorumMetadata(block Block, validatorCount int) error {
	// `mode` stores the value produced by this operation.
	mode := strings.ToUpper(strings.TrimSpace(block.ConsensusMode))
	// `hasMetadata` stores the value produced by this operation.
	hasMetadata := mode != "" ||
		strings.TrimSpace(block.QuorumPolicyVersion) != "" ||
		block.ActiveReadyCount > 0 ||
		block.RequiredQuorum > 0 ||
		block.StrictQuorum > 0
	if !hasMetadata {
		return nil
	}
	if mode == "" {
		mode = "NORMAL"
	}
	switch mode {
	case "NORMAL", "DEGRADED", "RECOVERY":
	default:
		return fmt.Errorf("quorum_metadata_unknown_mode: %s", mode)
	}
	if strings.TrimSpace(block.QuorumPolicyVersion) == "" {
		return errors.New("quorum_policy_version_missing")
	}
	if recoveryQuorumMetadataAllowsOmittedCounts(mode, block) {
		return nil
	}
	if block.RequiredQuorum <= 0 {
		return errors.New("required_quorum_missing")
	}
	if block.StrictQuorum <= 0 {
		return errors.New("strict_quorum_missing")
	}
	if block.ActiveReadyCount > 0 && block.ActiveReadyCount > validatorCount {
		return fmt.Errorf("active_ready_exceeds_validator_set: ready=%d validators=%d", block.ActiveReadyCount, validatorCount)
	}
	if block.ActiveReadyCount > 0 && block.RequiredQuorum > block.ActiveReadyCount {
		return fmt.Errorf("required_quorum_exceeds_active_ready: required=%d ready=%d", block.RequiredQuorum, block.ActiveReadyCount)
	}
	if block.RequiredQuorum > block.StrictQuorum {
		return fmt.Errorf("required_quorum_exceeds_strict: required=%d strict=%d", block.RequiredQuorum, block.StrictQuorum)
	}
	if block.RequiredQuorum < block.StrictQuorum {
		if mode == "NORMAL" {
			return fmt.Errorf("quorum_metadata_weak_normal: required=%d strict=%d", block.RequiredQuorum, block.StrictQuorum)
		}
		return fmt.Errorf("quorum_metadata_below_strict: required=%d strict=%d mode=%s", block.RequiredQuorum, block.StrictQuorum, mode)
	}
	if mode != "NORMAL" && block.ActiveReadyCount > 1 && block.RequiredQuorum < 2 {
		return fmt.Errorf("quorum_metadata_weak_degraded: required=%d active_ready=%d", block.RequiredQuorum, block.ActiveReadyCount)
	}
	if protocolRequiresStrictConsensusSignatures() && mode != "NORMAL" && block.StrictQuorum >= 3 && block.RequiredQuorum < 3 {
		return fmt.Errorf("quorum_metadata_below_mainnet_floor: required=%d strict=%d", block.RequiredQuorum, block.StrictQuorum)
	}
	return nil
}

// recoveryQuorumMetadataAllowsOmittedCounts implements the recovery quorum metadata allows omitted counts helper.
func recoveryQuorumMetadataAllowsOmittedCounts(mode string, block Block) bool {
	return mode == "RECOVERY" &&
		block.ActiveReadyCount == 0 &&
		block.RequiredQuorum == 0 &&
		block.StrictQuorum == 0
}

// verifyBlockExecutionResultSignature verifies block execution result signature.
func verifyBlockExecutionResultSignature(result ExecutionResult, block Block) error {
	return (*Node)(nil).verifyBlockExecutionResultSignature(result, block)
}

// verifyBlockExecutionResultSignature verifies block execution result signature.
func (n *Node) verifyBlockExecutionResultSignature(result ExecutionResult, block Block) error {
	// `sigHex` stores the value produced by this operation.
	sigHex := strings.TrimSpace(result.Signature)
	if sigHex == "" {
		return nil
	}
	// `sig` and `err` store the error produced by this operation.
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("invalid_execution_result_signature_encoding")
	}
	// `candidates` stores the value produced by this operation.
	candidates := n.execResultPubKeyCandidatesForHeight(result.Signer, block.ID)
	if len(candidates) == 0 {
		return errors.New("execution_result_pubkey_missing")
	}
	// `base` stores the value produced by this operation.
	base := ExecutionResultMsg{
		HeightHint: block.ID,
		RoundHint:  block.Round,
		SigVersion: execResultSigVersionV2,
		ExecHash:   strings.TrimSpace(result.ResultHash),
		TxMerkle:   strings.TrimSpace(result.TxMerkle),
		Signer:     strings.TrimSpace(result.Signer),
		Signature:  sigHex,
	}
	if result.Round > 0 {
		base.RoundHint = result.Round
	}
	// `blockHashHints` stores the block data handled by this operation.
	blockHashHints := []string{
		strings.TrimSpace(result.BlockHash),
		strings.TrimSpace(block.BlockHash),
		executionVoteProposalHashForFinalBlock(block),
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(blockHashHints))
	// `hint` tracks the current values while iterating.
	for _, hint := range blockHashHints {
		hint = strings.TrimSpace(hint)
		if hint == "" {
			continue
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[hint]; ok {
			continue
		}
		seen[hint] = struct{}{}
		// `candidate` stores the value produced by this operation.
		candidate := base
		candidate.BlockHashHint = hint
		candidate.SigVersion = execResultSigVersionV2
		if verifyExecutionResultSignature(candidate, candidates, sig) {
			return nil
		}
	}
	// `legacy` stores the value produced by this operation.
	legacy := base
	legacy.BlockHashHint = ""
	legacy.SigVersion = execResultSigVersionV1
	if verifyExecutionResultSignature(legacy, candidates, sig) {
		return nil
	}
	if block.BlockTime.Tick == TickFinalize {
		// `hints` stores the value produced by this operation.
		hints := []string{
			strings.TrimSpace(result.BlockHash),
			executionVoteProposalHashForFinalBlock(block),
		}
		// `seenHints` stores the value produced by this operation.
		seenHints := make(map[string]struct{}, len(hints))
		// `proposalHash` tracks the digest used to identify or verify the related data.
		for _, proposalHash := range hints {
			proposalHash = strings.TrimSpace(proposalHash)
			if proposalHash == "" {
				continue
			}
			// `ok` stores whether the related condition is satisfied.
			if _, ok := seenHints[proposalHash]; ok {
				continue
			}
			seenHints[proposalHash] = struct{}{}
			if n.verifyCommitVoteSignatureForHeight(CommitMsg{
				Height:                  block.ID,
				Hash:                    proposalHash,
				ExecHash:                strings.TrimSpace(result.ResultHash),
				TxMerkle:                strings.TrimSpace(result.TxMerkle),
				ExecutionCommitmentHash: executionCommitmentHashForBlock(block),
				From:                    strings.TrimSpace(result.Signer),
				Signature:               sigHex,
			}) {
				return nil
			}
		}
	}
	return errors.New("invalid_execution_result_signature")
}

// executionVoteProposalHashForFinalBlock implements the execution vote proposal hash for final block helper.
func executionVoteProposalHashForFinalBlock(block Block) string {
	candidates := executionVoteProposalHashCandidatesForFinalBlock(block)
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

// executionVoteProposalHashCandidatesForFinalBlock returns canonical proposal
// hashes accepted for a finalized envelope. Older/current proposal builders can
// sign before deterministic quorum metadata is stamped during finalization.
func executionVoteProposalHashCandidatesForFinalBlock(block Block) []string {
	if block.BlockTime.Tick != TickFinalize || block.BlockTime.Epoch == 0 {
		return nil
	}
	// `proposal` stores the value produced by this operation.
	proposal := block
	proposal.BlockTime = LogicalTimeForEpochTick(block.BlockTime.Epoch, TickExec)
	proposal.Timestamp = int64(SystemTimeUnits(proposal.BlockTime))
	clearFinalityCommitments(&proposal)
	candidates := []string{HashBlock(proposal)}

	// Finalization may add deterministic policy metadata that was absent from
	// the signed proposal. The policy itself is still verified independently.
	policyless := proposal
	policyless.ConsensusMode = ""
	policyless.QuorumPolicyVersion = ""
	policyless.ActiveReadyCount = 0
	policyless.RequiredQuorum = 0
	policyless.StrictQuorum = 0
	policylessHash := HashBlock(policyless)
	if policylessHash != candidates[0] {
		candidates = append(candidates, policylessHash)
	}
	return candidates
}

// verifyBlockTransactionAuthenticity verifies block transaction authenticity.
func verifyBlockTransactionAuthenticity(block Block) error {
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(block.Transactions))
	// `firstErrByIndex` stores per-transaction errors while preserving the
	// original deterministic first-error return order.
	firstErrByIndex := make([]error, len(block.Transactions))
	// `signedTxs` stores transactions whose independent signatures can be
	// checked by a worker pool after structural checks finish.
	signedTxs := make([]Transaction, 0, len(block.Transactions))
	// `signedIndexes` maps signedTxs positions back to block transaction indexes.
	signedIndexes := make([]int, 0, len(block.Transactions))
	// `raw` tracks the current values while iterating.
	for i, raw := range block.Transactions {
		// `tx` stores the transaction data handled by this operation.
		tx := raw
		normalizeIncomingTx(&tx)
		if err := validateRemovedVMEnvelope(tx); err != nil {
			firstErrByIndex[i] = err
			continue
		}
		// `err` stores the error produced by this operation.
		if err := validateTransactionShape(tx); err != nil {
			firstErrByIndex[i] = fmt.Errorf("tx_shape_invalid: %w", err)
			continue
		}
		// `canonicalID` stores the value produced by this operation.
		canonicalID := ComputeTxID(tx)
		// `legacyID` stores the value produced by this operation.
		legacyID := ComputeTxIDLegacy(tx)
		// `providedID` stores the value produced by this operation.
		providedID := strings.TrimSpace(tx.ID)
		if providedID == "" {
			firstErrByIndex[i] = errors.New("tx_id_missing")
			continue
		}
		if tx.Type == TxDTL && raw.ID != canonicalID {
			firstErrByIndex[i] = errors.New("dtl_tx_id_mismatch")
			continue
		}
		if !strings.EqualFold(providedID, canonicalID) &&
			!strings.EqualFold(providedID, legacyID) &&
			!matchesLegacySignedTxID(tx, providedID) {
			firstErrByIndex[i] = errors.New("tx_id_mismatch")
			continue
		}
		// `id` stores the current position in the related collection.
		id := canonicalID
		if id == "" {
			firstErrByIndex[i] = errors.New("tx_id_missing")
			continue
		}
		// `exists` stores whether the related condition is satisfied.
		if _, exists := seen[id]; exists {
			firstErrByIndex[i] = errors.New("duplicate_tx_in_block")
			continue
		}
		seen[id] = struct{}{}
		if !isProtocolChainID(tx.ChainID) {
			firstErrByIndex[i] = fmt.Errorf("invalid_chain_id: %s", tx.ChainID)
			continue
		}
		if tx.Type == TxFaucet {
			if !protocolFaucetEnabled() {
				firstErrByIndex[i] = errors.New("faucet disabled on mainnet")
				continue
			}
			if strings.TrimSpace(tx.PublicKey) != "" || strings.TrimSpace(tx.Signature) != "" {
				firstErrByIndex[i] = errors.New("faucet tx must not include signature")
			}
			continue
		}
		signedIndexes = append(signedIndexes, i)
		signedTxs = append(signedTxs, tx)
	}
	// `signatureResults` stores independent signature checks from the worker pool.
	signatureResults := ParallelVerifySignedBlockTransactions(signedTxs, 0)
	for resultIndex, result := range signatureResults {
		if result.Valid || resultIndex >= len(signedIndexes) {
			continue
		}
		blockIndex := signedIndexes[resultIndex]
		if firstErrByIndex[blockIndex] == nil {
			firstErrByIndex[blockIndex] = errors.New(result.Error)
		}
	}
	for _, err := range firstErrByIndex {
		if err != nil {
			return err
		}
	}
	return nil
}

// verifySignedBlockTransaction verifies signed block transaction.
func verifySignedBlockTransaction(tx Transaction) error {
	// `pubKeyBytes` and `err` store the error produced by this operation.
	pubKeyBytes, err := hex.DecodeString(tx.PublicKey)
	if err != nil {
		return errors.New("invalid public key encoding")
	}
	if !AddressMatchesPublicKey(tx.From, ed25519.PublicKey(pubKeyBytes)) {
		return errors.New("address/public key mismatch")
	}
	// `pubKey` and `err` store the error produced by this operation.
	pubKey, err := DecodePublicKey(tx.PublicKey)
	if err != nil {
		return errors.New("invalid public key")
	}
	// `sig` and `err` store the error produced by this operation.
	sig, err := DecodeSignature(tx.Signature)
	if err != nil {
		return errors.New("invalid signature encoding")
	}
	if ed25519.Verify(pubKey, TxPayload(tx), sig) {
		return nil
	}
	if ed25519.Verify(pubKey, TxPayloadLegacy(tx), sig) {
		return nil
	}
	return errors.New("signature verification failed")
}
