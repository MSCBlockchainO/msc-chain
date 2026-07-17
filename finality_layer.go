package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// `finalityCertificateVersionV1` defines the constant value used by this package.
	finalityCertificateVersionV1 = "finality_epoch_v1"
	// `finalityCertificateDomainV1` defines the constant value used by this package.
	finalityCertificateDomainV1 = "MSC_FINALIZED_EPOCH_V1"
	// `finalityCertificateVersionV2` defines the constant value used by this package.
	finalityCertificateVersionV2 = "finality_epoch_v2"
	// `finalityCertificateDomainV2` defines the constant value used by this package.
	finalityCertificateDomainV2 = "MSC_FINALIZED_EPOCH_V2"
	// `finalityAnchorDBPrefix` defines the constant value used by this package.
	finalityAnchorDBPrefix = "finality_anchor:"
	// `finalityCertificateDBPrefix` defines the constant value used by this package.
	finalityCertificateDBPrefix = "finality_cert:"
	// `finalityCheckpointDBPrefix` defines the constant value used by this package.
	finalityCheckpointDBPrefix = "finality_checkpoint:"

	// `finalityCertificatesDir` defines the constant value used by this package.
	finalityCertificatesDir = "finalized_epoch_certificates"
	// `finalityEpochAnchorsDir` defines the constant value used by this package.
	finalityEpochAnchorsDir = "epoch_anchor_hashes"
	// `finalityValidatorCommitmentsDir` defines the constant value used by this package.
	finalityValidatorCommitmentsDir = "validator_commitments"
	// `finalityIrreversibleRootsDir` defines the constant value used by this package.
	finalityIrreversibleRootsDir = "irreversible_roots"
)

// finalityCertificateVersionDomain implements the finality certificate version domain helper.
func finalityCertificateVersionDomain(block Block) (string, string) {
	if block.FinalityCertificate != nil {
		switch strings.TrimSpace(block.FinalityCertificate.Version) {
		case finalityCertificateVersionV2:
			return finalityCertificateVersionV2, finalityCertificateDomainV2
		case finalityCertificateVersionV1:
			return finalityCertificateVersionV1, finalityCertificateDomainV1
		}
	}
	return finalityCertificateVersionV1, finalityCertificateDomainV1
}

type finalityCheckpointRecord struct {
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `Domain` stores the value associated with this record.
	Domain string `json:"domain"`
	// `Epoch` stores the value associated with this record.
	Epoch uint64 `json:"epoch"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string `json:"block_hash"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash"`
	// `ValidatorSetRoot` stores whether the related condition is satisfied.
	ValidatorSetRoot string `json:"validator_set_root,omitempty"`
	// `FinalizedValidatorSetHash` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetHash string `json:"finalized_validator_set_hash"`
	// `FinalizedValidatorSetRoot` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetRoot string `json:"finalized_validator_set_root,omitempty"`
	// `EpochAnchorHash` stores the digest used to identify or verify the related data.
	EpochAnchorHash string `json:"epoch_anchor_hash"`
	// `PreviousEpochAnchorHash` stores the digest used to identify or verify the related data.
	PreviousEpochAnchorHash string `json:"previous_epoch_anchor_hash,omitempty"`
	// `FinalityRoot` stores the digest used to identify or verify the related data.
	FinalityRoot string `json:"finality_root"`
}

type EpochAnchorRecord struct {
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `Domain` stores the value associated with this record.
	Domain string `json:"domain"`
	// `Epoch` stores the value associated with this record.
	Epoch uint64 `json:"epoch"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `AnchorHash` stores the digest used to identify or verify the related data.
	AnchorHash string `json:"anchor_hash"`
	// `PreviousAnchorHash` stores the digest used to identify or verify the related data.
	PreviousAnchorHash string `json:"previous_anchor_hash,omitempty"`
	// `FinalizedHash` stores the digest used to identify or verify the related data.
	FinalizedHash string `json:"finalized_hash"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash"`
	// `ValidatorSetRoot` stores whether the related condition is satisfied.
	ValidatorSetRoot string `json:"validator_set_root,omitempty"`
	// `FinalizedValidatorSetHash` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetHash string `json:"finalized_validator_set_hash"`
	// `FinalizedValidatorSetRoot` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetRoot string `json:"finalized_validator_set_root,omitempty"`
	// `FinalityRoot` stores the digest used to identify or verify the related data.
	FinalityRoot string `json:"finality_root"`
}

type ValidatorCommitmentRecord struct {
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `Domain` stores the value associated with this record.
	Domain string `json:"domain"`
	// `Epoch` stores the value associated with this record.
	Epoch uint64 `json:"epoch"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash"`
	// `ValidatorSetRoot` stores whether the related condition is satisfied.
	ValidatorSetRoot string `json:"validator_set_root,omitempty"`
	// `FinalizedValidatorSetHash` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetHash string `json:"finalized_validator_set_hash"`
	// `FinalizedValidatorSetRoot` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetRoot string `json:"finalized_validator_set_root,omitempty"`
}

type IrreversibleRoot struct {
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `Domain` stores the value associated with this record.
	Domain string `json:"domain"`
	// `Epoch` stores the value associated with this record.
	Epoch uint64 `json:"epoch"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `FinalizedHash` stores the digest used to identify or verify the related data.
	FinalizedHash string `json:"finalized_hash"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root"`
	// `FinalityRoot` stores the digest used to identify or verify the related data.
	FinalityRoot string `json:"finality_root"`
	// `EpochAnchorHash` stores the digest used to identify or verify the related data.
	EpochAnchorHash string `json:"epoch_anchor_hash"`
}

// blockFinalityHashData implements the block finality hash data helper.
func blockFinalityHashData(block Block) string {
	if !blockFinalityCommitmentsPresent(block) {
		return ""
	}
	return fmt.Sprintf(
		"|finality_epoch=%d|finalized_height=%d|finalized_state=%s|finalized_vset=%s|finalized_vset_root=%s|epoch_anchor=%s|prev_epoch_anchor=%s|finality_root=%s",
		block.FinalizedEpoch,
		block.FinalizedHeight,
		strings.TrimSpace(block.FinalizedStateRoot),
		strings.TrimSpace(block.FinalizedValidatorSetHash),
		strings.TrimSpace(block.FinalizedValidatorSetRoot),
		strings.TrimSpace(block.EpochAnchorHash),
		strings.TrimSpace(block.PreviousEpochAnchorHash),
		strings.TrimSpace(block.FinalityRoot),
	)
}

// blockFinalityCommitmentsPresent implements the block finality commitments present helper.
func blockFinalityCommitmentsPresent(block Block) bool {
	return block.FinalizedEpoch > 0 ||
		block.FinalizedHeight > 0 ||
		strings.TrimSpace(block.FinalizedStateRoot) != "" ||
		strings.TrimSpace(block.FinalizedValidatorSetHash) != "" ||
		strings.TrimSpace(block.FinalizedValidatorSetRoot) != "" ||
		strings.TrimSpace(block.EpochAnchorHash) != "" ||
		strings.TrimSpace(block.PreviousEpochAnchorHash) != "" ||
		strings.TrimSpace(block.FinalityRoot) != "" ||
		block.FinalityCertificate != nil
}

// clearFinalityCommitments implements the clear finality commitments helper.
func clearFinalityCommitments(block *Block) {
	if block == nil {
		return
	}
	block.FinalizedEpoch = 0
	block.FinalizedHeight = 0
	block.FinalizedStateRoot = ""
	block.FinalizedValidatorSetHash = ""
	block.FinalizedValidatorSetRoot = ""
	block.EpochAnchorHash = ""
	block.PreviousEpochAnchorHash = ""
	block.FinalityRoot = ""
	block.FinalityCertificate = nil
}

// finalitySignersForBlock implements the finality signers for block helper.
func finalitySignersForBlock(block Block) []string {
	if len(block.Signatures) > 0 {
		return canonicalValidatorIDs(block.Signatures)
	}
	if block.FinalityCertificate != nil && len(block.FinalityCertificate.Signers) > 0 {
		return canonicalValidatorIDs(block.FinalityCertificate.Signers)
	}
	// `derived` stores the value produced by this operation.
	derived := make([]string, 0, len(block.ExecutionResults))
	// `result` tracks the result produced by this operation.
	for _, result := range block.ExecutionResults {
		derived = append(derived, result.Signer)
	}
	return canonicalValidatorIDs(derived)
}

// computeFinalityRoot computes finality root.
func computeFinalityRoot(block Block, _ []string) string {
	// The exact quorum witnesses are certificate evidence, not canonical chain
	// identity. Binding local signer subsets into the block hash lets two honest
	// nodes finalize the same state with different hashes if they observe
	// different valid quorum subsets.
	_, domain := finalityCertificateVersionDomain(block)
	return HashStrings([]string{
		domain,
		"root",
		strconv.FormatUint(block.ID, 10),
		strconv.FormatUint(block.BlockTime.Epoch, 10),
		strings.TrimSpace(block.PrevHash),
		strings.TrimSpace(block.StateRoot),
		strings.TrimSpace(block.MempoolRoot),
		strings.TrimSpace(block.ValidatorSetHash),
		strings.TrimSpace(block.ValidatorSetRoot),
		strings.TrimSpace(block.FinalizedValidatorSetRoot),
		strings.TrimSpace(block.ValidatorRegistryHash),
		strings.TrimSpace(block.NextValidatorSetHash),
		strings.TrimSpace(block.NextValidatorSetRoot),
		strconv.FormatUint(blockActivationHeight(block), 10),
		strings.ToUpper(strings.TrimSpace(block.ConsensusMode)),
		strings.TrimSpace(block.QuorumPolicyVersion),
		strconv.Itoa(block.ActiveReadyCount),
		strconv.Itoa(block.RequiredQuorum),
		strconv.Itoa(block.StrictQuorum),
	})
}

// computeEpochAnchorHash computes epoch anchor hash.
func computeEpochAnchorHash(block Block, previousAnchor string, _ []string) string {
	// Keep the anchor deterministic across equivalent quorum subsets. The
	// certificate still carries and verifies the actual signers.
	_, domain := finalityCertificateVersionDomain(block)
	return HashStrings([]string{
		domain,
		"epoch_anchor",
		strconv.FormatUint(block.FinalizedEpoch, 10),
		strconv.FormatUint(block.FinalizedHeight, 10),
		strings.TrimSpace(block.PrevHash),
		strings.TrimSpace(block.FinalizedStateRoot),
		strings.TrimSpace(block.FinalizedValidatorSetHash),
		strings.TrimSpace(block.FinalizedValidatorSetRoot),
		strings.TrimSpace(block.FinalityRoot),
		strings.TrimSpace(previousAnchor),
	})
}

// attachFinalityCommitments implements the attach finality commitments helper.
func (n *Node) attachFinalityCommitments(block *Block) {
	n.attachFinalityCommitmentsForProposal(block, "")
}

func (n *Node) attachFinalityCommitmentsForProposal(block *Block, proposalHash string) {
	if block == nil || block.ID == 0 || block.BlockTime.Tick != TickFinalize {
		return
	}
	// `signers` stores the value produced by this operation.
	signers := finalitySignersForBlock(*block)
	if len(signers) == 0 {
		return
	}
	// `proposalHash` stores the digest used to identify or verify the related data.
	proposalHash = strings.TrimSpace(proposalHash)
	if proposalHash == "" {
		proposalHash = executionVoteProposalHashForFinalBlock(*block)
	}
	// `count` and `required` store the measured quantity used by this operation.
	if _, _, count, required := n.commitVoteEvidence(block.ID, proposalHash); required > 0 && count >= required {
		block.FinalityCertificate = &FinalizedEpochCertificate{
			Version: finalityCertificateVersionV2,
			Domain:  finalityCertificateDomainV2,
		}
	}
	// `previousAnchor` stores the value produced by this operation.
	previousAnchor := ""
	if n != nil && block.ID > 1 {
		// `anchor` and `ok` store whether the related condition is satisfied.
		if anchor, ok, _ := n.finalityAnchorHashForHeight(block.ID - 1); ok {
			previousAnchor = anchor
		}
	}
	block.FinalizedEpoch = block.ID
	block.FinalizedHeight = block.ID
	block.FinalizedStateRoot = strings.TrimSpace(block.StateRoot)
	block.FinalizedValidatorSetHash = strings.TrimSpace(block.ValidatorSetHash)
	block.FinalizedValidatorSetRoot = strings.TrimSpace(block.ValidatorSetRoot)
	block.PreviousEpochAnchorHash = previousAnchor
	block.FinalityRoot = computeFinalityRoot(*block, signers)
	block.EpochAnchorHash = computeEpochAnchorHash(*block, previousAnchor, signers)
}

// finalityValidatorSignatures implements the finality validator signatures helper.
func finalityValidatorSignatures(signers []string, signatureMap map[string]string) []ValidatorSignature {
	if len(signers) == 0 || len(signatureMap) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make([]ValidatorSignature, 0, len(signers))
	// `signer` tracks the current values while iterating.
	for _, signer := range canonicalValidatorIDs(signers) {
		// `sig` stores the value produced by this operation.
		sig := strings.TrimSpace(signatureMap[normalizeValidatorID(signer)])
		if sig == "" {
			continue
		}
		out = append(out, ValidatorSignature{Validator: signer, Signature: sig})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// attachFinalityCertificate implements the attach finality certificate helper.
func (n *Node) attachFinalityCertificate(block *Block) {
	n.attachFinalityCertificateForProposal(block, "")
}

func (n *Node) attachFinalityCertificateForProposal(block *Block, proposalHash string) {
	if block == nil || block.ID == 0 || strings.TrimSpace(block.BlockHash) == "" {
		return
	}
	if !blockFinalityCommitmentsPresent(*block) {
		return
	}
	// `signers` stores the value produced by this operation.
	signers := finalitySignersForBlock(*block)
	if len(signers) == 0 {
		return
	}
	// `signatureMap` stores the value produced by this operation.
	signatureMap := make(map[string]string)
	// `result` tracks the result produced by this operation.
	for _, result := range block.ExecutionResults {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(result.Signer)
		// `sig` stores the value produced by this operation.
		sig := strings.TrimSpace(result.Signature)
		if id == "" || sig == "" {
			continue
		}
		signatureMap[id] = sig
	}
	if len(signatureMap) == 0 {
		signatureMap = nil
	}
	// `version` and `domain` store the value produced by this operation.
	version, domain := finalityCertificateVersionDomain(*block)
	// `proposalHash` stores the digest used to identify or verify the related data.
	proposalHash = strings.TrimSpace(proposalHash)
	if proposalHash == "" {
		proposalHash = executionVoteProposalHashForFinalBlock(*block)
	}
	// `commitSigners`, `commitWitnesses`, `commitCount`, and `commitRequired` store the measured quantity used by this operation.
	commitSigners, commitWitnesses, commitCount, commitRequired := n.commitVoteEvidence(block.ID, proposalHash)
	// `commitSignatureMap` stores the value produced by this operation.
	commitSignatureMap := make(map[string]string, len(commitWitnesses))
	// `witness` tracks the current values while iterating.
	for _, witness := range commitWitnesses {
		// `signer` stores the value produced by this operation.
		if signer := normalizeValidatorID(witness.Validator); signer != "" && strings.TrimSpace(witness.Signature) != "" {
			commitSignatureMap[signer] = strings.TrimSpace(witness.Signature)
		}
	}
	if version == finalityCertificateVersionV2 {
		if commitRequired <= 0 || commitCount < commitRequired {
			return
		}
		signers = commitSigners
	} else {
		commitWitnesses = nil
		commitSignatureMap = nil
		proposalHash = ""
	}
	block.FinalityCertificate = &FinalizedEpochCertificate{
		Version:                   version,
		Domain:                    domain,
		Epoch:                     block.FinalizedEpoch,
		Height:                    block.FinalizedHeight,
		BlockHash:                 strings.TrimSpace(block.BlockHash),
		StateRoot:                 strings.TrimSpace(block.FinalizedStateRoot),
		ValidatorSetHash:          strings.TrimSpace(block.ValidatorSetHash),
		ValidatorSetRoot:          strings.TrimSpace(block.ValidatorSetRoot),
		EpochAnchorHash:           strings.TrimSpace(block.EpochAnchorHash),
		PreviousEpochAnchorHash:   strings.TrimSpace(block.PreviousEpochAnchorHash),
		FinalityRoot:              strings.TrimSpace(block.FinalityRoot),
		ConsensusMode:             strings.TrimSpace(block.ConsensusMode),
		QuorumPolicyVersion:       strings.TrimSpace(block.QuorumPolicyVersion),
		ActiveReadyCount:          block.ActiveReadyCount,
		RequiredQuorum:            block.RequiredQuorum,
		StrictQuorum:              block.StrictQuorum,
		FinalizedValidatorSetHash: strings.TrimSpace(block.FinalizedValidatorSetHash),
		FinalizedValidatorSetRoot: strings.TrimSpace(block.FinalizedValidatorSetRoot),
		Signers:                   signers,
		Signatures:                commitWitnesses,
		ExecutionResultSignatures: signatureMap,
		CommitVoteProposalHash:    proposalHash,
		ExecutionCommitmentHash:   executionCommitmentHashForBlock(*block),
		CommitVoteSignatures:      commitSignatureMap,
	}
	if version == finalityCertificateVersionV1 {
		block.FinalityCertificate.Signatures = finalityValidatorSignatures(signers, signatureMap)
	}
}

// finalityRequiredQuorum returns the signer count a finalized block must satisfy.
func finalityRequiredQuorum(block Block, validators []string) int {
	if block.RequiredQuorum > 0 {
		return block.RequiredQuorum
	}
	if block.StrictQuorum > 0 {
		return block.StrictQuorum
	}
	// `fallback` stores the value produced by this operation.
	fallback := strictExecSupermajority(len(canonicalValidatorIDs(validators)))
	if fallback > 0 {
		return fallback
	}
	if len(validators) > 0 {
		return len(validators)
	}
	return 1
}

// verifyFinalityCommitments checks finalized block roots, signers, quorum, and persisted checkpoints.
func (n *Node) verifyFinalityCommitments(block Block, validators []string) error {
	if !blockFinalityCommitmentsPresent(block) {
		return nil
	}
	if block.BlockTime.Tick != TickFinalize {
		return errors.New("finality_commitment_on_non_final_block")
	}
	if block.FinalizedEpoch != block.ID || block.FinalizedHeight != block.ID {
		return errors.New("finality_height_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(block.FinalizedStateRoot), strings.TrimSpace(block.StateRoot)) {
		return errors.New("finality_state_root_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(block.FinalizedValidatorSetHash), strings.TrimSpace(block.ValidatorSetHash)) {
		return errors.New("finality_validator_set_hash_mismatch")
	}
	if strings.TrimSpace(block.FinalizedValidatorSetRoot) != "" &&
		!strings.EqualFold(strings.TrimSpace(block.FinalizedValidatorSetRoot), strings.TrimSpace(block.ValidatorSetRoot)) {
		return errors.New("finality_validator_set_root_mismatch")
	}
	// `signers` stores the value produced by this operation.
	signers := finalitySignersForBlock(block)
	if len(signers) == 0 {
		return errors.New("finality_signers_missing")
	}
	// `validatorSet` stores whether the related condition is satisfied.
	validatorSet := make(map[string]struct{}, len(validators))
	// `id` tracks the current position in the related collection.
	for _, id := range canonicalValidatorIDs(validators) {
		validatorSet[id] = struct{}{}
	}
	// `signer` tracks the current values while iterating.
	for _, signer := range signers {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := validatorSet[signer]; !ok {
			return fmt.Errorf("finality_signer_not_validator: %s", signer)
		}
	}
	// `required` stores the request data being processed.
	required := finalityRequiredQuorum(block, validators)
	if len(signers) < required {
		return fmt.Errorf("finality_quorum_shortfall: signers=%d required=%d", len(signers), required)
	}
	if n != nil {
		// `err` stores the error produced by this operation.
		if err := n.verifyPersistedFinalityCheckpoint(block); err != nil {
			return err
		}
	}
	// `expectedRoot` stores the digest used to identify or verify the related data.
	expectedRoot := computeFinalityRoot(block, signers)
	if !strings.EqualFold(strings.TrimSpace(block.FinalityRoot), expectedRoot) {
		return errors.New("finality_root_mismatch")
	}
	// `previousAnchor` stores the value produced by this operation.
	previousAnchor := strings.TrimSpace(block.PreviousEpochAnchorHash)
	if n != nil && block.ID > 1 {
		// `persistedPrevious`, `ok`, and `err` store the error produced by this operation.
		if persistedPrevious, ok, err := n.finalityAnchorHashForHeight(block.ID - 1); err != nil {
			return err
		} else if ok && strings.TrimSpace(persistedPrevious) != "" {
			if previousAnchor == "" {
				return errors.New("finality_previous_anchor_missing")
			}
			if !strings.EqualFold(previousAnchor, strings.TrimSpace(persistedPrevious)) {
				return errors.New("finality_previous_anchor_mismatch")
			}
		}
	}
	// `expectedAnchor` stores the value produced by this operation.
	expectedAnchor := computeEpochAnchorHash(block, previousAnchor, signers)
	if !strings.EqualFold(strings.TrimSpace(block.EpochAnchorHash), expectedAnchor) {
		return errors.New("epoch_anchor_mismatch")
	}
	// `err` stores the error produced by this operation.
	if err := n.verifyFinalityCertificate(block, signers, required); err != nil {
		return err
	}
	if err := verifyExecutionCommitmentSignerQuorum(block, signers, required); err != nil {
		return err
	}
	return nil
}

// verifyFinalityCertificate validates the embedded finality certificate against block commitments.
func verifyFinalityCertificate(block Block, signers []string, required int) error {
	return (*Node)(nil).verifyFinalityCertificate(block, signers, required)
}

// verifyFinalityCertificate validates the embedded finality certificate using
// height-bound committed validator keys when a node context is available.
func (n *Node) verifyFinalityCertificate(block Block, signers []string, required int) error {
	// `cert` stores the value produced by this operation.
	cert := block.FinalityCertificate
	if cert == nil {
		return errors.New("finality_certificate_missing")
	}
	// `version` stores the value produced by this operation.
	version := strings.TrimSpace(cert.Version)
	// `domain` stores the value produced by this operation.
	domain := strings.TrimSpace(cert.Domain)
	switch version {
	case finalityCertificateVersionV1:
		if domain != finalityCertificateDomainV1 {
			return errors.New("finality_certificate_domain_mismatch")
		}
	case finalityCertificateVersionV2:
		if domain != finalityCertificateDomainV2 {
			return errors.New("finality_certificate_domain_mismatch")
		}
	default:
		return errors.New("finality_certificate_version_mismatch")
	}
	if block.ProtocolVersion > 0 && version != finalityCertificateVersionV2 {
		return errors.New("finality_certificate_execution_commitment_signature_missing")
	}
	// `expectedDomain` stores the value produced by this operation.
	if _, expectedDomain := finalityCertificateVersionDomain(block); domain != expectedDomain {
		return errors.New("finality_certificate_domain_mismatch")
	}
	if cert.Epoch != block.FinalizedEpoch || cert.Height != block.FinalizedHeight {
		return errors.New("finality_certificate_height_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(cert.BlockHash), strings.TrimSpace(block.BlockHash)) {
		return errors.New("finality_certificate_block_hash_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(cert.StateRoot), strings.TrimSpace(block.FinalizedStateRoot)) {
		return errors.New("finality_certificate_state_root_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(cert.ValidatorSetHash), strings.TrimSpace(block.ValidatorSetHash)) ||
		!strings.EqualFold(strings.TrimSpace(cert.FinalizedValidatorSetHash), strings.TrimSpace(block.FinalizedValidatorSetHash)) {
		return errors.New("finality_certificate_validator_set_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(cert.ValidatorSetRoot), strings.TrimSpace(block.ValidatorSetRoot)) ||
		!strings.EqualFold(strings.TrimSpace(cert.FinalizedValidatorSetRoot), strings.TrimSpace(block.FinalizedValidatorSetRoot)) {
		return errors.New("finality_certificate_validator_set_root_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(cert.EpochAnchorHash), strings.TrimSpace(block.EpochAnchorHash)) ||
		!strings.EqualFold(strings.TrimSpace(cert.PreviousEpochAnchorHash), strings.TrimSpace(block.PreviousEpochAnchorHash)) {
		return errors.New("finality_certificate_anchor_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(cert.FinalityRoot), strings.TrimSpace(block.FinalityRoot)) {
		return errors.New("finality_certificate_root_mismatch")
	}
	expectedExecutionCommitmentHash := executionCommitmentHashForBlock(block)
	if !strings.EqualFold(strings.TrimSpace(cert.ExecutionCommitmentHash), expectedExecutionCommitmentHash) {
		return errors.New("finality_certificate_execution_commitment_mismatch")
	}
	if cert.RequiredQuorum != block.RequiredQuorum ||
		cert.StrictQuorum != block.StrictQuorum ||
		cert.ActiveReadyCount != block.ActiveReadyCount ||
		!strings.EqualFold(strings.TrimSpace(cert.ConsensusMode), strings.TrimSpace(block.ConsensusMode)) ||
		strings.TrimSpace(cert.QuorumPolicyVersion) != strings.TrimSpace(block.QuorumPolicyVersion) {
		return errors.New("finality_certificate_quorum_metadata_mismatch")
	}
	// `certSigners` stores the value produced by this operation.
	certSigners := canonicalValidatorIDs(cert.Signers)
	if !sameStringSlice(certSigners, signers) {
		return errors.New("finality_certificate_signer_mismatch")
	}
	if len(certSigners) < required {
		return fmt.Errorf("finality_certificate_quorum_shortfall: signers=%d required=%d", len(certSigners), required)
	}
	// `resultSigs` stores the result produced by this operation.
	resultSigs := make(map[string]string, len(block.ExecutionResults))
	// `result` tracks the result produced by this operation.
	for _, result := range block.ExecutionResults {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(result.Signer)
		// `sig` stores the value produced by this operation.
		sig := strings.TrimSpace(result.Signature)
		if id != "" && sig != "" {
			resultSigs[id] = sig
		}
	}
	// `rawSigner` and `rawSig` track the current values while iterating.
	for rawSigner, rawSig := range cert.ExecutionResultSignatures {
		// `signer` stores the value produced by this operation.
		signer := normalizeValidatorID(rawSigner)
		// `sig` stores the value produced by this operation.
		sig := strings.TrimSpace(rawSig)
		if signer == "" || sig == "" {
			return errors.New("finality_certificate_signature_empty")
		}
		if version == finalityCertificateVersionV1 && !containsNormalizedValidatorID(certSigners, signer) {
			return fmt.Errorf("finality_certificate_signature_signer_mismatch: %s", signer)
		}
		// `expected` stores the value produced by this operation.
		if expected := strings.TrimSpace(resultSigs[signer]); expected != "" && !strings.EqualFold(expected, sig) {
			return fmt.Errorf("finality_certificate_signature_mismatch: %s", signer)
		}
	}
	if version == finalityCertificateVersionV2 {
		// `proposalHash` stores the digest used to identify or verify the related data.
		proposalHash := strings.TrimSpace(cert.CommitVoteProposalHash)
		proposalHashMatches := false
		for _, candidate := range executionVoteProposalHashCandidatesForFinalBlock(block) {
			if strings.EqualFold(proposalHash, strings.TrimSpace(candidate)) {
				proposalHashMatches = true
				break
			}
		}
		if proposalHash == "" || !proposalHashMatches {
			return errors.New("finality_certificate_commit_proposal_mismatch")
		}
		// `verified` stores the value produced by this operation.
		verified := make(map[string]struct{}, len(cert.CommitVoteSignatures))
		// `rawSigner` and `rawSig` track the current values while iterating.
		for rawSigner, rawSig := range cert.CommitVoteSignatures {
			// `signer` stores the value produced by this operation.
			signer := normalizeValidatorID(rawSigner)
			// `sig` stores the value produced by this operation.
			sig := strings.TrimSpace(rawSig)
			if signer == "" || sig == "" || !containsNormalizedValidatorID(certSigners, signer) {
				return fmt.Errorf("finality_certificate_commit_signature_signer_mismatch: %s", signer)
			}
			// `msg` stores the value produced by this operation.
			msg := CommitMsg{
				Height:                  block.ID,
				Hash:                    proposalHash,
				ExecHash:                strings.TrimSpace(block.StateRoot),
				TxMerkle:                strings.TrimSpace(block.MempoolRoot),
				ExecutionCommitmentHash: expectedExecutionCommitmentHash,
				From:                    signer,
				Signature:               sig,
			}
			validSignature := verifyCommitVoteSignature(msg)
			if n != nil {
				validSignature = n.verifyCommitVoteSignatureForHeight(msg)
			}
			if !validSignature {
				return fmt.Errorf("finality_certificate_commit_signature_invalid: %s", signer)
			}
			verified[signer] = struct{}{}
		}
		if len(verified) < required {
			return fmt.Errorf("finality_certificate_commit_signature_shortfall: signatures=%d required=%d", len(verified), required)
		}
		// `signer` tracks the current values while iterating.
		for _, signer := range certSigners {
			// `ok` stores whether the related condition is satisfied.
			if _, ok := verified[signer]; !ok {
				return fmt.Errorf("finality_certificate_commit_signature_missing: %s", signer)
			}
		}
	} else {
		// `witness` tracks the current values while iterating.
		for _, witness := range cert.Signatures {
			// `signer` stores the value produced by this operation.
			signer := normalizeValidatorID(witness.Validator)
			// `sig` stores the value produced by this operation.
			sig := strings.TrimSpace(witness.Signature)
			if signer == "" || sig == "" {
				return errors.New("finality_certificate_signature_empty")
			}
			if !containsNormalizedValidatorID(certSigners, signer) {
				return fmt.Errorf("finality_certificate_signature_signer_mismatch: %s", signer)
			}
			// `expected` stores the value produced by this operation.
			if expected := strings.TrimSpace(resultSigs[signer]); expected != "" && !strings.EqualFold(expected, sig) {
				return fmt.Errorf("finality_certificate_signature_mismatch: %s", signer)
			}
		}
	}
	if protocolRequiresStrictConsensusSignatures() {
		// `signatureCount` stores the measured quantity used by this operation.
		signatureCount := finalityCertificateSignatureCount(cert)
		if signatureCount < required {
			return fmt.Errorf("finality_certificate_signature_shortfall: signatures=%d required=%d", signatureCount, required)
		}
	}
	return nil
}

// finalityCertificateSignatureCount implements the finality certificate signature count helper.
func finalityCertificateSignatureCount(cert *FinalizedEpochCertificate) int {
	if cert == nil {
		return 0
	}
	if strings.TrimSpace(cert.Version) == finalityCertificateVersionV2 {
		// `seen` stores the value produced by this operation.
		seen := make(map[string]struct{}, len(cert.CommitVoteSignatures))
		// `rawSigner` and `rawSig` track the current values while iterating.
		for rawSigner, rawSig := range cert.CommitVoteSignatures {
			// `signer` stores the value produced by this operation.
			if signer := normalizeValidatorID(rawSigner); signer != "" && strings.TrimSpace(rawSig) != "" {
				seen[signer] = struct{}{}
			}
		}
		return len(seen)
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(cert.ExecutionResultSignatures)+len(cert.Signatures))
	// `rawSigner` and `rawSig` track the current values while iterating.
	for rawSigner, rawSig := range cert.ExecutionResultSignatures {
		// `signer` stores the value produced by this operation.
		signer := normalizeValidatorID(rawSigner)
		if signer != "" && strings.TrimSpace(rawSig) != "" {
			seen[signer] = struct{}{}
		}
	}
	// `witness` tracks the current values while iterating.
	for _, witness := range cert.Signatures {
		// `signer` stores the value produced by this operation.
		signer := normalizeValidatorID(witness.Validator)
		if signer != "" && strings.TrimSpace(witness.Signature) != "" {
			seen[signer] = struct{}{}
		}
	}
	return len(seen)
}

// finalityAnchorDBKey implements the finality anchor db key helper.
func finalityAnchorDBKey(height uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d", finalityAnchorDBPrefix, height))
}

// finalityCertificateDBKey implements the finality certificate db key helper.
func finalityCertificateDBKey(height uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d", finalityCertificateDBPrefix, height))
}

// finalityCheckpointDBKey implements the finality checkpoint db key helper.
func finalityCheckpointDBKey(height uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d", finalityCheckpointDBPrefix, height))
}

// finalityCheckpointRecordFromBlock implements the finality checkpoint record from block helper.
func finalityCheckpointRecordFromBlock(block Block) finalityCheckpointRecord {
	// `version` and `domain` store the value produced by this operation.
	version, domain := finalityCertificateVersionDomain(block)
	return finalityCheckpointRecord{
		Version:                   version,
		Domain:                    domain,
		Epoch:                     block.FinalizedEpoch,
		Height:                    block.FinalizedHeight,
		BlockHash:                 strings.TrimSpace(block.BlockHash),
		StateRoot:                 strings.TrimSpace(block.FinalizedStateRoot),
		ValidatorSetHash:          strings.TrimSpace(block.ValidatorSetHash),
		ValidatorSetRoot:          strings.TrimSpace(block.ValidatorSetRoot),
		FinalizedValidatorSetHash: strings.TrimSpace(block.FinalizedValidatorSetHash),
		FinalizedValidatorSetRoot: strings.TrimSpace(block.FinalizedValidatorSetRoot),
		EpochAnchorHash:           strings.TrimSpace(block.EpochAnchorHash),
		PreviousEpochAnchorHash:   strings.TrimSpace(block.PreviousEpochAnchorHash),
		FinalityRoot:              strings.TrimSpace(block.FinalityRoot),
	}
}

// epochAnchorRecordFromBlock implements the epoch anchor record from block helper.
func epochAnchorRecordFromBlock(block Block) EpochAnchorRecord {
	// `version` and `domain` store the value produced by this operation.
	version, domain := finalityCertificateVersionDomain(block)
	return EpochAnchorRecord{
		Version:                   version,
		Domain:                    domain,
		Epoch:                     block.FinalizedEpoch,
		Height:                    block.FinalizedHeight,
		AnchorHash:                strings.TrimSpace(block.EpochAnchorHash),
		PreviousAnchorHash:        strings.TrimSpace(block.PreviousEpochAnchorHash),
		FinalizedHash:             strings.TrimSpace(block.BlockHash),
		StateRoot:                 strings.TrimSpace(block.FinalizedStateRoot),
		ValidatorSetHash:          strings.TrimSpace(block.ValidatorSetHash),
		ValidatorSetRoot:          strings.TrimSpace(block.ValidatorSetRoot),
		FinalizedValidatorSetHash: strings.TrimSpace(block.FinalizedValidatorSetHash),
		FinalizedValidatorSetRoot: strings.TrimSpace(block.FinalizedValidatorSetRoot),
		FinalityRoot:              strings.TrimSpace(block.FinalityRoot),
	}
}

// validatorCommitmentRecordFromBlock implements the validator commitment record from block helper.
func validatorCommitmentRecordFromBlock(block Block) ValidatorCommitmentRecord {
	// `version` and `domain` store the value produced by this operation.
	version, domain := finalityCertificateVersionDomain(block)
	return ValidatorCommitmentRecord{
		Version:                   version,
		Domain:                    domain,
		Epoch:                     block.FinalizedEpoch,
		Height:                    block.FinalizedHeight,
		ValidatorSetHash:          strings.TrimSpace(block.ValidatorSetHash),
		ValidatorSetRoot:          strings.TrimSpace(block.ValidatorSetRoot),
		FinalizedValidatorSetHash: strings.TrimSpace(block.FinalizedValidatorSetHash),
		FinalizedValidatorSetRoot: strings.TrimSpace(block.FinalizedValidatorSetRoot),
	}
}

// irreversibleRootFromBlock implements the irreversible root from block helper.
func irreversibleRootFromBlock(block Block) IrreversibleRoot {
	// `version` and `domain` store the value produced by this operation.
	version, domain := finalityCertificateVersionDomain(block)
	return IrreversibleRoot{
		Version:         version,
		Domain:          domain,
		Epoch:           block.FinalizedEpoch,
		Height:          block.FinalizedHeight,
		FinalizedHash:   strings.TrimSpace(block.BlockHash),
		StateRoot:       strings.TrimSpace(block.FinalizedStateRoot),
		FinalityRoot:    strings.TrimSpace(block.FinalityRoot),
		EpochAnchorHash: strings.TrimSpace(block.EpochAnchorHash),
	}
}

// loadPersistedFinalityCheckpoint implements the load persisted finality checkpoint helper.
func (n *Node) loadPersistedFinalityCheckpoint(height uint64) (finalityCheckpointRecord, bool, error) {
	// `out` stores the result produced by this operation.
	var out finalityCheckpointRecord
	if n == nil || n.DB == nil || n.DB.Meta == nil || height == 0 {
		return out, false, nil
	}
	// `found` stores whether the related condition is satisfied.
	found := false
	// `err` stores the error produced by this operation.
	err := n.DB.Meta.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
		item, err := txn.Get(finalityCheckpointDBKey(height))
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil
			}
			return err
		}
		found = true
		return item.Value(func(val []byte) error {
			// `plain` and `err` store the error produced by this operation.
			plain, err := decryptDBValue(val)
			if err != nil {
				return err
			}
			return json.Unmarshal(plain, &out)
		})
	})
	return out, found, err
}

// loadPersistedFinalityCertificate implements the load persisted finality certificate helper.
func (n *Node) loadPersistedFinalityCertificate(height uint64) (FinalizedEpochCertificate, bool, error) {
	// `out` stores the result produced by this operation.
	var out FinalizedEpochCertificate
	if n == nil || n.DB == nil || n.DB.Meta == nil || height == 0 {
		return out, false, nil
	}
	// `found` stores whether the related condition is satisfied.
	found := false
	// `err` stores the error produced by this operation.
	err := n.DB.Meta.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
		item, err := txn.Get(finalityCertificateDBKey(height))
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil
			}
			return err
		}
		found = true
		return item.Value(func(val []byte) error {
			// `plain` and `err` store the error produced by this operation.
			plain, err := decryptDBValue(val)
			if err != nil {
				return err
			}
			return json.Unmarshal(plain, &out)
		})
	})
	return out, found, err
}

// verifyFinalityCheckpointRecordMatchesBlock verifies finality checkpoint record matches block.
func verifyFinalityCheckpointRecordMatchesBlock(record finalityCheckpointRecord, block Block) error {
	// `expected` stores the value produced by this operation.
	expected := finalityCheckpointRecordFromBlock(block)
	if record.Height != expected.Height || record.Epoch != expected.Epoch {
		return errors.New("finality_checkpoint_height_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.BlockHash), expected.BlockHash) {
		return errors.New("finality_checkpoint_block_hash_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.StateRoot), expected.StateRoot) {
		return errors.New("finality_checkpoint_state_root_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.ValidatorSetHash), expected.ValidatorSetHash) ||
		!strings.EqualFold(strings.TrimSpace(record.FinalizedValidatorSetHash), expected.FinalizedValidatorSetHash) {
		return errors.New("finality_checkpoint_validator_set_hash_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.ValidatorSetRoot), expected.ValidatorSetRoot) ||
		!strings.EqualFold(strings.TrimSpace(record.FinalizedValidatorSetRoot), expected.FinalizedValidatorSetRoot) {
		return errors.New("finality_checkpoint_validator_set_root_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.EpochAnchorHash), expected.EpochAnchorHash) {
		return errors.New("finality_checkpoint_anchor_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.PreviousEpochAnchorHash), expected.PreviousEpochAnchorHash) {
		return errors.New("finality_checkpoint_previous_anchor_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.FinalityRoot), expected.FinalityRoot) {
		return errors.New("finality_checkpoint_root_mismatch")
	}
	return nil
}

// finalityArtifactPaths implements the finality artifact paths helper.
func finalityArtifactPaths(dataDir, nodeID string, height uint64) map[string]string {
	return map[string]string{
		finalityCertificatesDir:         finalityArtifactFilePath(dataDir, nodeID, finalityCertificatesDir, height),
		finalityEpochAnchorsDir:         finalityArtifactFilePath(dataDir, nodeID, finalityEpochAnchorsDir, height),
		finalityValidatorCommitmentsDir: finalityArtifactFilePath(dataDir, nodeID, finalityValidatorCommitmentsDir, height),
		finalityIrreversibleRootsDir:    finalityArtifactFilePath(dataDir, nodeID, finalityIrreversibleRootsDir, height),
	}
}

// finalityArtifactFilePath implements the finality artifact file path helper.
func finalityArtifactFilePath(dataDir, nodeID, dir string, height uint64) string {
	return filepath.Join(nodeDataPath(dataDir, nodeID), dir, fmt.Sprintf("epoch_%020d.json", height))
}

// finalityArtifactCheckpointBoundary implements the finality artifact checkpoint boundary helper.
func finalityArtifactCheckpointBoundary(height uint64) bool {
	if height <= 1 {
		return true
	}
	// `interval` stores the value currently being processed.
	interval := syncCheckpointIntervalBlocks()
	if interval == 0 {
		return true
	}
	return height%interval == 0
}

// writeFinalityArtifactJSON implements the write finality artifact json helper.
func writeFinalityArtifactJSON(path string, value any) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("finality_artifact_path_empty")
	}
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeFileAtomic(path, raw, 0o600)
}

// loadFinalityArtifactJSON implements the load finality artifact json helper.
func loadFinalityArtifactJSON(path string, out any) (bool, error) {
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, out); err != nil {
		return true, err
	}
	return true, nil
}

// finalityArtifactFileCount implements the finality artifact file count helper.
func (n *Node) finalityArtifactFileCount(height uint64) int {
	if n == nil || height == 0 {
		return 0
	}
	// `count` stores the measured quantity used by this operation.
	count := 0
	// `path` tracks the current values while iterating.
	for _, path := range finalityArtifactPaths(n.DataDir, n.ID, height) {
		// `err` stores the error produced by this operation.
		if _, err := os.Stat(path); err == nil {
			count++
		}
	}
	return count
}

// shouldPersistFinalityArtifactFiles implements the should persist finality artifact files helper.
func (n *Node) shouldPersistFinalityArtifactFiles(height uint64) bool {
	if n == nil || height == 0 {
		return true
	}
	if finalityArtifactCheckpointBoundary(height) {
		return true
	}
	switch normalizeNodeRole(n.Role) {
	case "full", "light":
		// `syncing` stores the value produced by this operation.
		syncing := false
		if n.Consensus != nil {
			n.Consensus.mu.Lock()
			syncing = n.Consensus.Syncing || n.Consensus.Paused || n.Consensus.syncInFlight
			n.Consensus.mu.Unlock()
		}
		if syncing {
			return n.finalityArtifactFileCount(height) > 0
		}
	}
	return true
}

// verifyEpochAnchorArtifactMatchesBlock verifies epoch anchor artifact matches block.
func verifyEpochAnchorArtifactMatchesBlock(record EpochAnchorRecord, block Block) error {
	// `expected` stores the value produced by this operation.
	expected := epochAnchorRecordFromBlock(block)
	if record.Epoch != expected.Epoch || record.Height != expected.Height {
		return errors.New("epoch_anchor_artifact_height_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.AnchorHash), expected.AnchorHash) ||
		!strings.EqualFold(strings.TrimSpace(record.PreviousAnchorHash), expected.PreviousAnchorHash) ||
		!strings.EqualFold(strings.TrimSpace(record.FinalizedHash), expected.FinalizedHash) ||
		!strings.EqualFold(strings.TrimSpace(record.StateRoot), expected.StateRoot) ||
		!strings.EqualFold(strings.TrimSpace(record.FinalityRoot), expected.FinalityRoot) {
		return errors.New("epoch_anchor_artifact_root_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.ValidatorSetHash), expected.ValidatorSetHash) ||
		!strings.EqualFold(strings.TrimSpace(record.ValidatorSetRoot), expected.ValidatorSetRoot) ||
		!strings.EqualFold(strings.TrimSpace(record.FinalizedValidatorSetHash), expected.FinalizedValidatorSetHash) ||
		!strings.EqualFold(strings.TrimSpace(record.FinalizedValidatorSetRoot), expected.FinalizedValidatorSetRoot) {
		return errors.New("epoch_anchor_artifact_validator_commitment_mismatch")
	}
	return nil
}

// verifyValidatorCommitmentArtifactMatchesBlock verifies validator commitment artifact matches block.
func verifyValidatorCommitmentArtifactMatchesBlock(record ValidatorCommitmentRecord, block Block) error {
	// `expected` stores the value produced by this operation.
	expected := validatorCommitmentRecordFromBlock(block)
	if record.Epoch != expected.Epoch || record.Height != expected.Height {
		return errors.New("validator_commitment_artifact_height_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.ValidatorSetHash), expected.ValidatorSetHash) ||
		!strings.EqualFold(strings.TrimSpace(record.ValidatorSetRoot), expected.ValidatorSetRoot) ||
		!strings.EqualFold(strings.TrimSpace(record.FinalizedValidatorSetHash), expected.FinalizedValidatorSetHash) ||
		!strings.EqualFold(strings.TrimSpace(record.FinalizedValidatorSetRoot), expected.FinalizedValidatorSetRoot) {
		return errors.New("validator_commitment_artifact_mismatch")
	}
	return nil
}

// verifyIrreversibleRootArtifactMatchesBlock verifies irreversible root artifact matches block.
func verifyIrreversibleRootArtifactMatchesBlock(record IrreversibleRoot, block Block) error {
	// `expected` stores the value produced by this operation.
	expected := irreversibleRootFromBlock(block)
	if record.Epoch != expected.Epoch || record.Height != expected.Height {
		return errors.New("irreversible_root_artifact_height_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.FinalizedHash), expected.FinalizedHash) ||
		!strings.EqualFold(strings.TrimSpace(record.StateRoot), expected.StateRoot) ||
		!strings.EqualFold(strings.TrimSpace(record.FinalityRoot), expected.FinalityRoot) ||
		!strings.EqualFold(strings.TrimSpace(record.EpochAnchorHash), expected.EpochAnchorHash) {
		return errors.New("irreversible_root_artifact_mismatch")
	}
	return nil
}

// verifyFinalityCertificateArtifactMatchesBlock verifies finality certificate artifact matches block.
func verifyFinalityCertificateArtifactMatchesBlock(cert FinalizedEpochCertificate, block Block) error {
	if cert.Epoch != block.FinalizedEpoch || cert.Height != block.FinalizedHeight {
		return errors.New("finality_certificate_artifact_height_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(cert.BlockHash), strings.TrimSpace(block.BlockHash)) ||
		!strings.EqualFold(strings.TrimSpace(cert.StateRoot), strings.TrimSpace(block.FinalizedStateRoot)) ||
		!strings.EqualFold(strings.TrimSpace(cert.ValidatorSetHash), strings.TrimSpace(block.ValidatorSetHash)) ||
		!strings.EqualFold(strings.TrimSpace(cert.ValidatorSetRoot), strings.TrimSpace(block.ValidatorSetRoot)) ||
		!strings.EqualFold(strings.TrimSpace(cert.FinalizedValidatorSetHash), strings.TrimSpace(block.FinalizedValidatorSetHash)) ||
		!strings.EqualFold(strings.TrimSpace(cert.FinalizedValidatorSetRoot), strings.TrimSpace(block.FinalizedValidatorSetRoot)) ||
		!strings.EqualFold(strings.TrimSpace(cert.EpochAnchorHash), strings.TrimSpace(block.EpochAnchorHash)) ||
		!strings.EqualFold(strings.TrimSpace(cert.PreviousEpochAnchorHash), strings.TrimSpace(block.PreviousEpochAnchorHash)) ||
		!strings.EqualFold(strings.TrimSpace(cert.FinalityRoot), strings.TrimSpace(block.FinalityRoot)) ||
		!strings.EqualFold(strings.TrimSpace(cert.ExecutionCommitmentHash), executionCommitmentHashForBlock(block)) {
		return errors.New("finality_certificate_artifact_mismatch")
	}
	if !sameStringSlice(canonicalValidatorIDs(cert.Signers), finalitySignersForBlock(block)) {
		return errors.New("finality_certificate_artifact_signer_mismatch")
	}
	return nil
}

// verifyFinalityArtifacts verifies finality artifacts.
func (n *Node) verifyFinalityArtifacts(block Block) error {
	if n == nil || block.ID == 0 || !blockFinalityCommitmentsPresent(block) {
		return nil
	}
	// `paths` stores the value produced by this operation.
	paths := finalityArtifactPaths(n.DataDir, n.ID, block.ID)
	// `found` stores whether the related condition is satisfied.
	found := 0
	// `cert` stores the value used by this operation.
	var cert FinalizedEpochCertificate
	// `ok` and `err` store the error produced by this operation.
	if ok, err := loadFinalityArtifactJSON(paths[finalityCertificatesDir], &cert); err != nil {
		return err
	} else if ok {
		found++
		// `err` stores the error produced by this operation.
		if err := verifyFinalityCertificateArtifactMatchesBlock(cert, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	// `anchor` stores the value used by this operation.
	var anchor EpochAnchorRecord
	// `ok` and `err` store the error produced by this operation.
	if ok, err := loadFinalityArtifactJSON(paths[finalityEpochAnchorsDir], &anchor); err != nil {
		return err
	} else if ok {
		found++
		// `err` stores the error produced by this operation.
		if err := verifyEpochAnchorArtifactMatchesBlock(anchor, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	// `commitment` stores the value used by this operation.
	var commitment ValidatorCommitmentRecord
	// `ok` and `err` store the error produced by this operation.
	if ok, err := loadFinalityArtifactJSON(paths[finalityValidatorCommitmentsDir], &commitment); err != nil {
		return err
	} else if ok {
		found++
		// `err` stores the error produced by this operation.
		if err := verifyValidatorCommitmentArtifactMatchesBlock(commitment, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	// `root` stores the digest used to identify or verify the related data.
	var root IrreversibleRoot
	// `ok` and `err` store the error produced by this operation.
	if ok, err := loadFinalityArtifactJSON(paths[finalityIrreversibleRootsDir], &root); err != nil {
		return err
	} else if ok {
		found++
		// `err` stores the error produced by this operation.
		if err := verifyIrreversibleRootArtifactMatchesBlock(root, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	if found > 0 && found != len(paths) {
		return fmt.Errorf("irreversible_finality_artifact_incomplete: found=%d want=%d height=%d", found, len(paths), block.ID)
	}
	return nil
}

// verifyFinalityArtifactsForRepair verifies finality artifacts for repair.
func (n *Node) verifyFinalityArtifactsForRepair(block Block) error {
	if n == nil || block.ID == 0 || !blockFinalityCommitmentsPresent(block) {
		return nil
	}
	// `paths` stores the value produced by this operation.
	paths := finalityArtifactPaths(n.DataDir, n.ID, block.ID)
	// `cert` stores the value used by this operation.
	var cert FinalizedEpochCertificate
	// `ok` and `err` store the error produced by this operation.
	if ok, err := loadFinalityArtifactJSON(paths[finalityCertificatesDir], &cert); err != nil {
		return err
	} else if ok {
		// `err` stores the error produced by this operation.
		if err := verifyFinalityCertificateArtifactMatchesBlock(cert, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	// `anchor` stores the value used by this operation.
	var anchor EpochAnchorRecord
	// `ok` and `err` store the error produced by this operation.
	if ok, err := loadFinalityArtifactJSON(paths[finalityEpochAnchorsDir], &anchor); err != nil {
		return err
	} else if ok {
		// `err` stores the error produced by this operation.
		if err := verifyEpochAnchorArtifactMatchesBlock(anchor, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	// `commitment` stores the value used by this operation.
	var commitment ValidatorCommitmentRecord
	// `ok` and `err` store the error produced by this operation.
	if ok, err := loadFinalityArtifactJSON(paths[finalityValidatorCommitmentsDir], &commitment); err != nil {
		return err
	} else if ok {
		// `err` stores the error produced by this operation.
		if err := verifyValidatorCommitmentArtifactMatchesBlock(commitment, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	// `root` stores the digest used to identify or verify the related data.
	var root IrreversibleRoot
	// `ok` and `err` store the error produced by this operation.
	if ok, err := loadFinalityArtifactJSON(paths[finalityIrreversibleRootsDir], &root); err != nil {
		return err
	} else if ok {
		// `err` stores the error produced by this operation.
		if err := verifyIrreversibleRootArtifactMatchesBlock(root, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	return nil
}

// persistFinalityArtifacts implements the persist finality artifacts helper.
func (n *Node) persistFinalityArtifacts(block Block) error {
	if n == nil || block.ID == 0 || !blockFinalityCommitmentsPresent(block) {
		return nil
	}
	// `err` stores the error produced by this operation.
	if err := n.verifyFinalityArtifactsForRepair(block); err != nil {
		return err
	}
	if !n.shouldPersistFinalityArtifactFiles(block.ID) {
		return nil
	}
	// `artifacts` stores the value produced by this operation.
	artifacts := []struct {
		// `dir` stores the value associated with this record.
		dir string
		// `value` stores the value currently being processed.
		value any
	}{
		{dir: finalityCertificatesDir, value: block.FinalityCertificate},
		{dir: finalityEpochAnchorsDir, value: epochAnchorRecordFromBlock(block)},
		{dir: finalityValidatorCommitmentsDir, value: validatorCommitmentRecordFromBlock(block)},
		{dir: finalityIrreversibleRootsDir, value: irreversibleRootFromBlock(block)},
	}
	// `artifact` tracks the current values while iterating.
	for _, artifact := range artifacts {
		// `path` stores the value produced by this operation.
		path := finalityArtifactFilePath(n.DataDir, n.ID, artifact.dir, block.ID)
		// `err` stores the error produced by this operation.
		if err := writeFinalityArtifactJSON(path, artifact.value); err != nil {
			return err
		}
	}
	return nil
}

// verifyPersistedFinalityCheckpoint verifies persisted finality checkpoint.
func (n *Node) verifyPersistedFinalityCheckpoint(block Block) error {
	if n == nil || block.ID == 0 {
		return nil
	}
	// `checkpointAheadOfTip` stores the value produced by this operation.
	checkpointAheadOfTip := n.finalityCheckpointAheadOfLocalChain(block.ID)
	// `record`, `ok`, and `err` store the error produced by this operation.
	record, ok, err := n.loadPersistedFinalityCheckpoint(block.ID)
	if err != nil || !ok {
		if err != nil {
			return err
		}
		// `anchor`, `anchorOK`, and `anchorErr` store the error produced by this operation.
		if anchor, anchorOK, anchorErr := n.loadPersistedFinalityAnchorHash(block.ID); anchorErr != nil {
			return anchorErr
		} else if anchorOK && strings.TrimSpace(anchor) != "" {
			if checkpointAheadOfTip {
				return nil
			}
			return fmt.Errorf("irreversible_finality_checkpoint_incomplete: anchor_without_checkpoint height=%d", block.ID)
		}
		// `certOK` and `certErr` store the error produced by this operation.
		if _, certOK, certErr := n.loadPersistedFinalityCertificate(block.ID); certErr != nil {
			return certErr
		} else if certOK {
			if checkpointAheadOfTip {
				return nil
			}
			return fmt.Errorf("irreversible_finality_checkpoint_incomplete: certificate_without_checkpoint height=%d", block.ID)
		}
		if checkpointAheadOfTip {
			return nil
		}
		return n.verifyFinalityArtifacts(block)
	}
	// `err` stores the error produced by this operation.
	if err := verifyFinalityCheckpointRecordMatchesBlock(record, block); err != nil {
		if checkpointAheadOfTip {
			return nil
		}
		return fmt.Errorf("irreversible_finality_checkpoint_conflict: %w", err)
	}
	// `anchor`, `anchorOK`, and `anchorErr` store the error produced by this operation.
	if anchor, anchorOK, anchorErr := n.loadPersistedFinalityAnchorHash(block.ID); anchorErr != nil {
		return anchorErr
	} else if anchorOK && !strings.EqualFold(strings.TrimSpace(anchor), strings.TrimSpace(block.EpochAnchorHash)) {
		if checkpointAheadOfTip {
			return nil
		}
		return fmt.Errorf("irreversible_finality_checkpoint_conflict: finality_anchor_mismatch")
	} else if !anchorOK {
		if checkpointAheadOfTip {
			return nil
		}
		return fmt.Errorf("irreversible_finality_checkpoint_incomplete: checkpoint_without_anchor height=%d", block.ID)
	}
	// `cert`, `certOK`, and `certErr` store the error produced by this operation.
	if cert, certOK, certErr := n.loadPersistedFinalityCertificate(block.ID); certErr != nil {
		return certErr
	} else if certOK {
		// `err` stores the error produced by this operation.
		if err := verifyFinalityCertificateArtifactMatchesBlock(cert, block); err != nil {
			if checkpointAheadOfTip {
				return nil
			}
			return fmt.Errorf("irreversible_finality_checkpoint_conflict: %w", err)
		}
	} else {
		if checkpointAheadOfTip {
			return nil
		}
		return fmt.Errorf("irreversible_finality_checkpoint_incomplete: checkpoint_without_certificate height=%d", block.ID)
	}
	if checkpointAheadOfTip {
		return nil
	}
	return n.verifyFinalityArtifacts(block)
}

// finalityCheckpointAheadOfLocalChain implements the finality checkpoint ahead of local chain helper.
func (n *Node) finalityCheckpointAheadOfLocalChain(height uint64) bool {
	if n == nil || n.Blockchain == nil || height == 0 {
		return false
	}
	return n.Blockchain.Height() < height
}

// localChainCommittedBlockMatches implements the local chain committed block matches helper.
func (n *Node) localChainCommittedBlockMatches(height uint64, hash string) bool {
	if n == nil || n.Blockchain == nil || height == 0 || strings.TrimSpace(hash) == "" {
		return false
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.Blockchain.GetBlock(height)
	if !ok {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(block.BlockHash), strings.TrimSpace(hash))
}

// loadPersistedFinalityAnchorHash implements the load persisted finality anchor hash helper.
func (n *Node) loadPersistedFinalityAnchorHash(height uint64) (string, bool, error) {
	if n == nil || n.DB == nil || n.DB.Meta == nil || height == 0 {
		return "", false, nil
	}
	// `out` stores the result produced by this operation.
	out := ""
	// `found` stores whether the related condition is satisfied.
	found := false
	// `err` stores the error produced by this operation.
	err := n.DB.Meta.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
		item, err := txn.Get(finalityAnchorDBKey(height))
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil
			}
			return err
		}
		found = true
		return item.Value(func(val []byte) error {
			// `plain` and `err` store the error produced by this operation.
			plain, err := decryptDBValue(val)
			if err != nil {
				return err
			}
			out = strings.TrimSpace(string(plain))
			return nil
		})
	})
	return out, found, err
}

// finalityAnchorHashForHeight implements the finality anchor hash for height helper.
func (n *Node) finalityAnchorHashForHeight(height uint64) (string, bool, error) {
	if n == nil || height == 0 {
		return "", false, nil
	}
	if n.Blockchain != nil {
		// `block` and `ok` store whether the related condition is satisfied.
		if block, ok := n.Blockchain.GetBlock(height); ok && strings.TrimSpace(block.EpochAnchorHash) != "" {
			return strings.TrimSpace(block.EpochAnchorHash), true, nil
		}
	}
	// `anchor`, `ok`, and `err` store the error produced by this operation.
	if anchor, ok, err := n.loadPersistedFinalityAnchorHash(height); err != nil || ok {
		return anchor, ok, err
	}
	return "", false, nil
}

// persistFinalityCheckpoint stores verified finality anchors, certificates, and irreversible roots for a block.
func (n *Node) persistFinalityCheckpoint(block Block) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil || block.ID == 0 || !blockFinalityCommitmentsPresent(block) {
		return nil
	}
	// `err` stores the error produced by this operation.
	if err := n.verifyFinalityCommitments(block, n.resolveBlockVerificationValidatorsForFinality(block)); err != nil {
		return err
	}
	// `certRaw` and `err` store the error produced by this operation.
	certRaw, err := json.Marshal(block.FinalityCertificate)
	if err != nil {
		return err
	}
	// `checkpoint` stores the value produced by this operation.
	checkpoint := finalityCheckpointRecordFromBlock(block)
	// `checkpointRaw` and `err` store the error produced by this operation.
	checkpointRaw, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	// `anchor` stores the value produced by this operation.
	anchor := strings.TrimSpace(block.EpochAnchorHash)
	// `err` stores the error produced by this operation.
	if err := n.DB.Meta.Update(func(txn *Txn) error {
		// `anchorKey` stores the key used to access the related value.
		anchorKey := finalityAnchorDBKey(block.ID)
		// `shouldWriteAnchor` stores the value produced by this operation.
		shouldWriteAnchor := true
		// `item` and `err` store the error produced by this operation.
		if item, err := txn.Get(anchorKey); err == nil && item != nil {
			// `existing` stores the value produced by this operation.
			existing := ""
			// `vErr` stores the error produced by this operation.
			if vErr := item.Value(func(val []byte) error {
				// `plain` and `derr` store the error produced by this operation.
				plain, derr := decryptDBValue(val)
				if derr != nil {
					return derr
				}
				existing = strings.TrimSpace(string(plain))
				return nil
			}); vErr != nil {
				return vErr
			}
			if existing != "" && strings.EqualFold(existing, anchor) {
				shouldWriteAnchor = false
			} else if existing != "" {
				// `committedHash` and `committed` store the digest used to identify or verify the related data.
				committedHash, committed := n.getCommittedHash(block.ID)
				if (!committed || !strings.EqualFold(strings.TrimSpace(committedHash), strings.TrimSpace(block.BlockHash))) &&
					!n.localChainCommittedBlockMatches(block.ID, block.BlockHash) {
					return fmt.Errorf("irreversible epoch anchor immutable violation height=%d existing=%s got=%s", block.ID, existing, anchor)
				}
			}
		} else if err != nil && !errors.Is(err, ErrKeyNotFound) {
			return err
		}
		if shouldWriteAnchor {
			// `enc` and `err` store the error produced by this operation.
			enc, err := encryptDBValue([]byte(anchor))
			if err != nil {
				return err
			}
			// `err` stores the error produced by this operation.
			if err := txn.Set(anchorKey, enc); err != nil {
				return err
			}
		}
		// `checkpointKey` stores the key used to access the related value.
		checkpointKey := finalityCheckpointDBKey(block.ID)
		// `item` and `err` store the error produced by this operation.
		if item, err := txn.Get(checkpointKey); err == nil && item != nil {
			// `existing` stores the value used by this operation.
			var existing finalityCheckpointRecord
			// `vErr` stores the error produced by this operation.
			if vErr := item.Value(func(val []byte) error {
				// `plain` and `derr` store the error produced by this operation.
				plain, derr := decryptDBValue(val)
				if derr != nil {
					return derr
				}
				return json.Unmarshal(plain, &existing)
			}); vErr != nil {
				return vErr
			}
			// `err` stores the error produced by this operation.
			if err := verifyFinalityCheckpointRecordMatchesBlock(existing, block); err != nil {
				if !n.localChainCommittedBlockMatches(block.ID, block.BlockHash) {
					return fmt.Errorf("irreversible finality checkpoint violation height=%d: %w", block.ID, err)
				}
			}
		} else if err != nil && !errors.Is(err, ErrKeyNotFound) {
			return err
		}
		// `encCheckpoint` and `err` store the error produced by this operation.
		encCheckpoint, err := encryptDBValue(checkpointRaw)
		if err != nil {
			return err
		}
		// `err` stores the error produced by this operation.
		if err := txn.Set(checkpointKey, encCheckpoint); err != nil {
			return err
		}
		// `certKey` stores the key used to access the related value.
		certKey := finalityCertificateDBKey(block.ID)
		// `encCert` and `err` store the error produced by this operation.
		encCert, err := encryptDBValue(certRaw)
		if err != nil {
			return err
		}
		return txn.Set(certKey, encCert)
	}); err != nil {
		return err
	}
	return n.persistFinalityArtifacts(block)
}

// resolveBlockVerificationValidatorsForFinality implements the resolve block verification validators for finality helper.
func (n *Node) resolveBlockVerificationValidatorsForFinality(block Block) []string {
	if n == nil || block.ID == 0 {
		return nil
	}
	// `validators` stores whether the related condition is satisfied.
	if validators := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID))); len(validators) > 0 {
		return canonicalValidatorIDs(validators)
	}
	// `validators` stores whether the related condition is satisfied.
	if validators := finalitySignersForBlock(block); len(validators) > 0 {
		return validators
	}
	return nil
}
