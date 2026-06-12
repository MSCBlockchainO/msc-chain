package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

type blockVerificationContext struct {
	Last                   Block
	Validators             []string
	ExpectedLeader         string
	SyncContinuityFallback bool
}

func (n *Node) verifyBlockProductionEnvelope(block Block, bc *Blockchain) (blockVerificationContext, error) {
	ctx := blockVerificationContext{}
	if n == nil {
		return ctx, errors.New("nil_node")
	}
	if bc == nil {
		return ctx, errors.New("nil_blockchain")
	}
	if err := verifyBlockStructure(block); err != nil {
		return ctx, err
	}

	last := bc.LastBlock()
	ctx.Last = last
	if err := n.verifyBlockContinuity(block, last); err != nil {
		return ctx, err
	}
	if err := verifyBlockTimestampEnvelope(block, last); err != nil {
		return ctx, err
	}
	if err := verifyBlockHashEnvelope(block); err != nil {
		return ctx, err
	}
	if err := verifyBlockProposerSignatureEnvelope(block); err != nil {
		return ctx, err
	}

	validators, fallback := n.resolveBlockVerificationValidators(block)
	if len(validators) == 0 {
		return ctx, errors.New("validator_set_unresolved")
	}
	ctx.Validators = validators
	ctx.SyncContinuityFallback = fallback
	ctx.ExpectedLeader = n.consensusLeaderForHeightRound(block.ID, block.Round, validators)

	if !fallback {
		if err := verifyBlockProposerMembership(block, validators); err != nil {
			return ctx, err
		}
	}
	if err := verifyBlockDuplicateEvidence(block); err != nil {
		return ctx, err
	}
	if err := n.verifyBlockConsensusEvidence(block, validators); err != nil {
		return ctx, err
	}
	if err := n.verifyFinalityCommitments(block, validators); err != nil {
		return ctx, err
	}
	if err := verifyBlockTransactionAuthenticity(block); err != nil {
		return ctx, err
	}
	return ctx, nil
}

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
	if strings.TrimSpace(block.Hash) != "" && !strings.EqualFold(strings.TrimSpace(block.Hash), strings.TrimSpace(block.BlockHash)) {
		return errors.New("legacy_hash_mismatch")
	}
	if GlobalConfig.MaxTxPerBlock > 0 && len(block.Transactions) > GlobalConfig.MaxTxPerBlock {
		return errors.New("too_many_transactions")
	}
	return nil
}

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

func verifyBlockHashEnvelope(block Block) error {
	if !strings.EqualFold(strings.TrimSpace(HashBlock(block)), strings.TrimSpace(block.BlockHash)) {
		return errors.New("hash_mismatch")
	}
	return nil
}

func verifyBlockProposerSignatureEnvelope(block Block) error {
	if len(block.Signature) == 0 {
		if !IsTestnet {
			return errors.New("invalid_block_signature")
		}
		return nil
	}
	validSig := VerifyBlockSignature(block)
	if !validSig && IsTestnet {
		deadline := time.Now().Add(1200 * time.Millisecond)
		for !validSig && time.Now().Before(deadline) {
			time.Sleep(120 * time.Millisecond)
			validSig = VerifyBlockSignature(block)
		}
	}
	if !validSig {
		if block.BlockTime.Tick == TickFinalize && len(block.ExecutionResults) > 0 && block.RequiredQuorum > 0 {
			return nil
		}
		return errors.New("invalid_block_signature")
	}
	return nil
}

func (n *Node) resolveBlockVerificationValidators(block Block) ([]string, bool) {
	if n == nil {
		return nil, false
	}
	validators := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID)))
	fallback := false
	if len(validators) == 0 {
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

func verifyBlockProposerMembership(block Block, validators []string) error {
	if !containsNormalizedValidatorID(validators, block.Proposer) {
		return errors.New("invalid_proposer")
	}
	return nil
}

func verifyBlockDuplicateEvidence(block Block) error {
	seenSigners := make(map[string]struct{}, len(block.Signatures))
	for _, signer := range block.Signatures {
		id := normalizeValidatorID(signer)
		if id == "" {
			return errors.New("empty_block_signature_signer")
		}
		if _, exists := seenSigners[id]; exists {
			return errors.New("duplicate_block_signature_signer")
		}
		seenSigners[id] = struct{}{}
	}

	seenResults := make(map[string]struct{}, len(block.ExecutionResults))
	for _, result := range block.ExecutionResults {
		id := normalizeValidatorID(result.Signer)
		if id == "" {
			return errors.New("empty_execution_result_signer")
		}
		if _, exists := seenResults[id]; exists {
			return errors.New("duplicate_execution_result_signer")
		}
		seenResults[id] = struct{}{}
	}
	return nil
}

func (n *Node) verifyBlockConsensusEvidence(block Block, validators []string) error {
	validatorSet := make(map[string]struct{}, len(validators))
	for _, id := range canonicalValidatorIDs(validators) {
		validatorSet[id] = struct{}{}
	}
	if len(validatorSet) == 0 {
		return errors.New("validator_set_unresolved")
	}

	for _, signer := range block.Signatures {
		id := normalizeValidatorID(signer)
		if _, ok := validatorSet[id]; !ok {
			return fmt.Errorf("signature_signer_not_validator: %s", id)
		}
	}

	executionSigners := make(map[string]struct{}, len(block.ExecutionResults))
	for _, result := range block.ExecutionResults {
		signer := normalizeValidatorID(result.Signer)
		if _, ok := validatorSet[signer]; !ok {
			return fmt.Errorf("execution_signer_not_validator: %s", signer)
		}
		signatureVerified := false
		if result.Height != 0 && result.Height != block.ID {
			return errors.New("execution_result_height_mismatch")
		}
		if resultBlockHash := strings.TrimSpace(result.BlockHash); resultBlockHash != "" {
			finalHash := strings.TrimSpace(block.BlockHash)
			proposalHash := executionVoteProposalHashForFinalBlock(block)
			if !strings.EqualFold(resultBlockHash, finalHash) && (proposalHash == "" || !strings.EqualFold(resultBlockHash, proposalHash)) {
				if !verifyBlockExecutionResultSignatureForHint(result, block, resultBlockHash) {
					// Legacy v1 votes omit block-hash hints; accept them when the
					// exec/merkle signature still matches under the canonical paths.
					if err := verifyBlockExecutionResultSignature(result, block); err != nil {
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
		if !IsTestnet && block.RequiredQuorum > 0 && strings.TrimSpace(result.Signature) == "" {
			return errors.New("execution_result_signature_missing")
		}
		if !signatureVerified {
			if err := verifyBlockExecutionResultSignature(result, block); err != nil {
				return err
			}
		}
		executionSigners[signer] = struct{}{}
	}

	if err := verifyBlockQuorumMetadata(block, len(validatorSet)); err != nil {
		return err
	}
	if err := verifyBlockExecutionQuorumEvidence(block, executionSigners); err != nil {
		return err
	}
	return n.validateCommittedBlockQuorumEvidence(block)
}

func verifyBlockExecutionQuorumEvidence(block Block, executionSigners map[string]struct{}) error {
	if len(block.ExecutionResults) == 0 || block.RequiredQuorum <= 0 {
		return nil
	}
	if len(executionSigners) < block.RequiredQuorum {
		mode := strings.ToUpper(strings.TrimSpace(block.ConsensusMode))
		if mode == "" {
			mode = "NORMAL"
		}
		return fmt.Errorf("execution_quorum_evidence_shortfall: signers=%d required=%d mode=%s", len(executionSigners), block.RequiredQuorum, mode)
	}
	return nil
}

func verifyBlockExecutionResultSignatureForHint(result ExecutionResult, block Block, blockHashHint string) bool {
	sigHex := strings.TrimSpace(result.Signature)
	blockHashHint = strings.TrimSpace(blockHashHint)
	if sigHex == "" || blockHashHint == "" {
		return false
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	candidates := execResultPubKeyCandidates(result.Signer)
	if len(candidates) == 0 {
		return false
	}
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

func verifyBlockQuorumMetadata(block Block, validatorCount int) error {
	mode := strings.ToUpper(strings.TrimSpace(block.ConsensusMode))
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
	if !IsTestnet && mode != "NORMAL" && block.StrictQuorum >= 3 && block.RequiredQuorum < 3 {
		return fmt.Errorf("quorum_metadata_below_mainnet_floor: required=%d strict=%d", block.RequiredQuorum, block.StrictQuorum)
	}
	return nil
}

func recoveryQuorumMetadataAllowsOmittedCounts(mode string, block Block) bool {
	return mode == "RECOVERY" &&
		block.ActiveReadyCount == 0 &&
		block.RequiredQuorum == 0 &&
		block.StrictQuorum == 0
}

func verifyBlockExecutionResultSignature(result ExecutionResult, block Block) error {
	sigHex := strings.TrimSpace(result.Signature)
	if sigHex == "" {
		return nil
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("invalid_execution_result_signature_encoding")
	}
	candidates := execResultPubKeyCandidates(result.Signer)
	if len(candidates) == 0 {
		return errors.New("execution_result_pubkey_missing")
	}
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
	blockHashHints := []string{
		strings.TrimSpace(result.BlockHash),
		strings.TrimSpace(block.BlockHash),
		executionVoteProposalHashForFinalBlock(block),
	}
	seen := make(map[string]struct{}, len(blockHashHints))
	for _, hint := range blockHashHints {
		hint = strings.TrimSpace(hint)
		if hint == "" {
			continue
		}
		if _, ok := seen[hint]; ok {
			continue
		}
		seen[hint] = struct{}{}
		candidate := base
		candidate.BlockHashHint = hint
		candidate.SigVersion = execResultSigVersionV2
		if verifyExecutionResultSignature(candidate, candidates, sig) {
			return nil
		}
	}
	legacy := base
	legacy.BlockHashHint = ""
	legacy.SigVersion = execResultSigVersionV1
	if verifyExecutionResultSignature(legacy, candidates, sig) {
		return nil
	}
	return errors.New("invalid_execution_result_signature")
}

func executionVoteProposalHashForFinalBlock(block Block) string {
	if block.BlockTime.Tick != TickFinalize || block.BlockTime.Epoch == 0 {
		return ""
	}
	proposal := block
	proposal.BlockTime = LogicalTimeForEpochTick(block.BlockTime.Epoch, TickExec)
	proposal.Timestamp = int64(SystemTimeUnits(proposal.BlockTime))
	clearFinalityCommitments(&proposal)
	return HashBlock(proposal)
}

func verifyBlockTransactionAuthenticity(block Block) error {
	seen := make(map[string]struct{}, len(block.Transactions))
	for _, raw := range block.Transactions {
		tx := raw
		normalizeIncomingTx(&tx)
		if err := validateTransactionShape(tx); err != nil {
			return fmt.Errorf("tx_shape_invalid: %w", err)
		}
		canonicalID := ComputeTxID(tx)
		legacyID := ComputeTxIDLegacy(tx)
		if strings.TrimSpace(tx.ID) != "" &&
			!strings.EqualFold(tx.ID, canonicalID) &&
			!strings.EqualFold(tx.ID, legacyID) &&
			!matchesLegacySignedTxID(tx, tx.ID) {
			return errors.New("tx_id_mismatch")
		}
		id := canonicalID
		if id == "" {
			return errors.New("tx_id_missing")
		}
		if _, exists := seen[id]; exists {
			return errors.New("duplicate_tx_in_block")
		}
		seen[id] = struct{}{}
		if tx.ChainID != ChainID {
			return fmt.Errorf("invalid_chain_id: %s", tx.ChainID)
		}
		if tx.Type == TxFaucet {
			if !IsTestnet {
				return errors.New("faucet disabled on mainnet")
			}
			if strings.TrimSpace(tx.PublicKey) != "" || strings.TrimSpace(tx.Signature) != "" {
				return errors.New("faucet tx must not include signature")
			}
			continue
		}
		if tx.Type == TxEVM {
			return errors.New("evm/vm removed permanently")
		}
		if err := verifySignedBlockTransaction(tx); err != nil {
			return err
		}
	}
	return nil
}

func verifySignedBlockTransaction(tx Transaction) error {
	pubKeyBytes, err := hex.DecodeString(tx.PublicKey)
	if err != nil {
		return errors.New("invalid public key encoding")
	}
	if !AddressMatchesPublicKey(tx.From, ed25519.PublicKey(pubKeyBytes)) {
		return errors.New("address/public key mismatch")
	}
	pubKey, err := DecodePublicKey(tx.PublicKey)
	if err != nil {
		return errors.New("invalid public key")
	}
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
