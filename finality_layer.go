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
	finalityCertificateVersionV1 = "finality_epoch_v1"
	finalityCertificateDomainV1  = "MSC_FINALIZED_EPOCH_V1"
	finalityAnchorDBPrefix       = "finality_anchor:"
	finalityCertificateDBPrefix  = "finality_cert:"
	finalityCheckpointDBPrefix   = "finality_checkpoint:"

	finalityCertificatesDir         = "finalized_epoch_certificates"
	finalityEpochAnchorsDir         = "epoch_anchor_hashes"
	finalityValidatorCommitmentsDir = "validator_commitments"
	finalityIrreversibleRootsDir    = "irreversible_roots"
)

type finalityCheckpointRecord struct {
	Version                   string `json:"version"`
	Domain                    string `json:"domain"`
	Epoch                     uint64 `json:"epoch"`
	Height                    uint64 `json:"height"`
	BlockHash                 string `json:"block_hash"`
	StateRoot                 string `json:"state_root"`
	ValidatorSetHash          string `json:"validator_set_hash"`
	ValidatorSetRoot          string `json:"validator_set_root,omitempty"`
	FinalizedValidatorSetHash string `json:"finalized_validator_set_hash"`
	FinalizedValidatorSetRoot string `json:"finalized_validator_set_root,omitempty"`
	EpochAnchorHash           string `json:"epoch_anchor_hash"`
	PreviousEpochAnchorHash   string `json:"previous_epoch_anchor_hash,omitempty"`
	FinalityRoot              string `json:"finality_root"`
}

type EpochAnchorRecord struct {
	Version                   string `json:"version"`
	Domain                    string `json:"domain"`
	Epoch                     uint64 `json:"epoch"`
	Height                    uint64 `json:"height"`
	AnchorHash                string `json:"anchor_hash"`
	PreviousAnchorHash        string `json:"previous_anchor_hash,omitempty"`
	FinalizedHash             string `json:"finalized_hash"`
	StateRoot                 string `json:"state_root"`
	ValidatorSetHash          string `json:"validator_set_hash"`
	ValidatorSetRoot          string `json:"validator_set_root,omitempty"`
	FinalizedValidatorSetHash string `json:"finalized_validator_set_hash"`
	FinalizedValidatorSetRoot string `json:"finalized_validator_set_root,omitempty"`
	FinalityRoot              string `json:"finality_root"`
}

type ValidatorCommitmentRecord struct {
	Version                   string `json:"version"`
	Domain                    string `json:"domain"`
	Epoch                     uint64 `json:"epoch"`
	Height                    uint64 `json:"height"`
	ValidatorSetHash          string `json:"validator_set_hash"`
	ValidatorSetRoot          string `json:"validator_set_root,omitempty"`
	FinalizedValidatorSetHash string `json:"finalized_validator_set_hash"`
	FinalizedValidatorSetRoot string `json:"finalized_validator_set_root,omitempty"`
}

type IrreversibleRoot struct {
	Version         string `json:"version"`
	Domain          string `json:"domain"`
	Epoch           uint64 `json:"epoch"`
	Height          uint64 `json:"height"`
	FinalizedHash   string `json:"finalized_hash"`
	StateRoot       string `json:"state_root"`
	FinalityRoot    string `json:"finality_root"`
	EpochAnchorHash string `json:"epoch_anchor_hash"`
}

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

func finalitySignersForBlock(block Block) []string {
	if len(block.Signatures) > 0 {
		return canonicalValidatorIDs(block.Signatures)
	}
	if block.FinalityCertificate != nil && len(block.FinalityCertificate.Signers) > 0 {
		return canonicalValidatorIDs(block.FinalityCertificate.Signers)
	}
	derived := make([]string, 0, len(block.ExecutionResults))
	for _, result := range block.ExecutionResults {
		derived = append(derived, result.Signer)
	}
	return canonicalValidatorIDs(derived)
}

func computeFinalityRoot(block Block, signers []string) string {
	// The exact quorum witnesses are certificate evidence, not canonical chain
	// identity. Binding local signer subsets into the block hash lets two honest
	// nodes finalize the same state with different hashes if they observe
	// different valid quorum subsets.
	return HashStrings([]string{
		finalityCertificateDomainV1,
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

func computeEpochAnchorHash(block Block, previousAnchor string, signers []string) string {
	// Keep the anchor deterministic across equivalent quorum subsets. The
	// certificate still carries and verifies the actual signers.
	return HashStrings([]string{
		finalityCertificateDomainV1,
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

func (n *Node) attachFinalityCommitments(block *Block) {
	if block == nil || block.ID == 0 || block.BlockTime.Tick != TickFinalize {
		return
	}
	signers := finalitySignersForBlock(*block)
	if len(signers) == 0 {
		return
	}
	previousAnchor := ""
	if n != nil && block.ID > 1 {
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

func finalityValidatorSignatures(signers []string, signatureMap map[string]string) []ValidatorSignature {
	if len(signers) == 0 || len(signatureMap) == 0 {
		return nil
	}
	out := make([]ValidatorSignature, 0, len(signers))
	for _, signer := range canonicalValidatorIDs(signers) {
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

func (n *Node) attachFinalityCertificate(block *Block) {
	if block == nil || block.ID == 0 || strings.TrimSpace(block.BlockHash) == "" {
		return
	}
	if !blockFinalityCommitmentsPresent(*block) {
		return
	}
	signers := finalitySignersForBlock(*block)
	if len(signers) == 0 {
		return
	}
	signatureMap := make(map[string]string)
	for _, result := range block.ExecutionResults {
		id := normalizeValidatorID(result.Signer)
		sig := strings.TrimSpace(result.Signature)
		if id == "" || sig == "" {
			continue
		}
		signatureMap[id] = sig
	}
	if len(signatureMap) == 0 {
		signatureMap = nil
	}
	block.FinalityCertificate = &FinalizedEpochCertificate{
		Version:                   finalityCertificateVersionV1,
		Domain:                    finalityCertificateDomainV1,
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
		Signatures:                finalityValidatorSignatures(signers, signatureMap),
		ExecutionResultSignatures: signatureMap,
	}
}

func finalityRequiredQuorum(block Block, validators []string) int {
	strict := strictExecSupermajority(len(canonicalValidatorIDs(validators)))
	if block.RequiredQuorum > strict {
		return block.RequiredQuorum
	}
	if strict > 0 {
		return strict
	}
	if len(validators) > 0 {
		return len(validators)
	}
	return 1
}

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
	signers := finalitySignersForBlock(block)
	if len(signers) == 0 {
		return errors.New("finality_signers_missing")
	}
	validatorSet := make(map[string]struct{}, len(validators))
	for _, id := range canonicalValidatorIDs(validators) {
		validatorSet[id] = struct{}{}
	}
	for _, signer := range signers {
		if _, ok := validatorSet[signer]; !ok {
			return fmt.Errorf("finality_signer_not_validator: %s", signer)
		}
	}
	required := finalityRequiredQuorum(block, validators)
	if len(signers) < required {
		return fmt.Errorf("finality_quorum_shortfall: signers=%d required=%d", len(signers), required)
	}
	if n != nil {
		if err := n.verifyPersistedFinalityCheckpoint(block); err != nil {
			return err
		}
	}
	expectedRoot := computeFinalityRoot(block, signers)
	if !strings.EqualFold(strings.TrimSpace(block.FinalityRoot), expectedRoot) {
		return errors.New("finality_root_mismatch")
	}
	previousAnchor := strings.TrimSpace(block.PreviousEpochAnchorHash)
	if n != nil && block.ID > 1 {
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
	expectedAnchor := computeEpochAnchorHash(block, previousAnchor, signers)
	if !strings.EqualFold(strings.TrimSpace(block.EpochAnchorHash), expectedAnchor) {
		return errors.New("epoch_anchor_mismatch")
	}
	if err := verifyFinalityCertificate(block, signers, required); err != nil {
		return err
	}
	return nil
}

func verifyFinalityCertificate(block Block, signers []string, required int) error {
	cert := block.FinalityCertificate
	if cert == nil {
		return errors.New("finality_certificate_missing")
	}
	if strings.TrimSpace(cert.Version) != finalityCertificateVersionV1 {
		return errors.New("finality_certificate_version_mismatch")
	}
	if strings.TrimSpace(cert.Domain) != finalityCertificateDomainV1 {
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
	if cert.RequiredQuorum != block.RequiredQuorum ||
		cert.StrictQuorum != block.StrictQuorum ||
		cert.ActiveReadyCount != block.ActiveReadyCount ||
		!strings.EqualFold(strings.TrimSpace(cert.ConsensusMode), strings.TrimSpace(block.ConsensusMode)) ||
		strings.TrimSpace(cert.QuorumPolicyVersion) != strings.TrimSpace(block.QuorumPolicyVersion) {
		return errors.New("finality_certificate_quorum_metadata_mismatch")
	}
	certSigners := canonicalValidatorIDs(cert.Signers)
	if !sameStringSlice(certSigners, signers) {
		return errors.New("finality_certificate_signer_mismatch")
	}
	if len(certSigners) < required {
		return fmt.Errorf("finality_certificate_quorum_shortfall: signers=%d required=%d", len(certSigners), required)
	}
	resultSigs := make(map[string]string, len(block.ExecutionResults))
	for _, result := range block.ExecutionResults {
		id := normalizeValidatorID(result.Signer)
		sig := strings.TrimSpace(result.Signature)
		if id != "" && sig != "" {
			resultSigs[id] = sig
		}
	}
	for rawSigner, rawSig := range cert.ExecutionResultSignatures {
		signer := normalizeValidatorID(rawSigner)
		sig := strings.TrimSpace(rawSig)
		if signer == "" || sig == "" {
			return errors.New("finality_certificate_signature_empty")
		}
		if !containsNormalizedValidatorID(certSigners, signer) {
			return fmt.Errorf("finality_certificate_signature_signer_mismatch: %s", signer)
		}
		if expected := strings.TrimSpace(resultSigs[signer]); expected != "" && !strings.EqualFold(expected, sig) {
			return fmt.Errorf("finality_certificate_signature_mismatch: %s", signer)
		}
	}
	for _, witness := range cert.Signatures {
		signer := normalizeValidatorID(witness.Validator)
		sig := strings.TrimSpace(witness.Signature)
		if signer == "" || sig == "" {
			return errors.New("finality_certificate_signature_empty")
		}
		if !containsNormalizedValidatorID(certSigners, signer) {
			return fmt.Errorf("finality_certificate_signature_signer_mismatch: %s", signer)
		}
		if expected := strings.TrimSpace(resultSigs[signer]); expected != "" && !strings.EqualFold(expected, sig) {
			return fmt.Errorf("finality_certificate_signature_mismatch: %s", signer)
		}
	}
	if !IsTestnet {
		signatureCount := finalityCertificateSignatureCount(cert)
		if signatureCount < required {
			return fmt.Errorf("finality_certificate_signature_shortfall: signatures=%d required=%d", signatureCount, required)
		}
	}
	return nil
}

func finalityCertificateSignatureCount(cert *FinalizedEpochCertificate) int {
	if cert == nil {
		return 0
	}
	seen := make(map[string]struct{}, len(cert.ExecutionResultSignatures)+len(cert.Signatures))
	for rawSigner, rawSig := range cert.ExecutionResultSignatures {
		signer := normalizeValidatorID(rawSigner)
		if signer != "" && strings.TrimSpace(rawSig) != "" {
			seen[signer] = struct{}{}
		}
	}
	for _, witness := range cert.Signatures {
		signer := normalizeValidatorID(witness.Validator)
		if signer != "" && strings.TrimSpace(witness.Signature) != "" {
			seen[signer] = struct{}{}
		}
	}
	return len(seen)
}

func finalityAnchorDBKey(height uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d", finalityAnchorDBPrefix, height))
}

func finalityCertificateDBKey(height uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d", finalityCertificateDBPrefix, height))
}

func finalityCheckpointDBKey(height uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d", finalityCheckpointDBPrefix, height))
}

func finalityCheckpointRecordFromBlock(block Block) finalityCheckpointRecord {
	return finalityCheckpointRecord{
		Version:                   finalityCertificateVersionV1,
		Domain:                    finalityCertificateDomainV1,
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

func epochAnchorRecordFromBlock(block Block) EpochAnchorRecord {
	return EpochAnchorRecord{
		Version:                   finalityCertificateVersionV1,
		Domain:                    finalityCertificateDomainV1,
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

func validatorCommitmentRecordFromBlock(block Block) ValidatorCommitmentRecord {
	return ValidatorCommitmentRecord{
		Version:                   finalityCertificateVersionV1,
		Domain:                    finalityCertificateDomainV1,
		Epoch:                     block.FinalizedEpoch,
		Height:                    block.FinalizedHeight,
		ValidatorSetHash:          strings.TrimSpace(block.ValidatorSetHash),
		ValidatorSetRoot:          strings.TrimSpace(block.ValidatorSetRoot),
		FinalizedValidatorSetHash: strings.TrimSpace(block.FinalizedValidatorSetHash),
		FinalizedValidatorSetRoot: strings.TrimSpace(block.FinalizedValidatorSetRoot),
	}
}

func irreversibleRootFromBlock(block Block) IrreversibleRoot {
	return IrreversibleRoot{
		Version:         finalityCertificateVersionV1,
		Domain:          finalityCertificateDomainV1,
		Epoch:           block.FinalizedEpoch,
		Height:          block.FinalizedHeight,
		FinalizedHash:   strings.TrimSpace(block.BlockHash),
		StateRoot:       strings.TrimSpace(block.FinalizedStateRoot),
		FinalityRoot:    strings.TrimSpace(block.FinalityRoot),
		EpochAnchorHash: strings.TrimSpace(block.EpochAnchorHash),
	}
}

func (n *Node) loadPersistedFinalityCheckpoint(height uint64) (finalityCheckpointRecord, bool, error) {
	var out finalityCheckpointRecord
	if n == nil || n.DB == nil || n.DB.Meta == nil || height == 0 {
		return out, false, nil
	}
	found := false
	err := n.DB.Meta.View(func(txn *Txn) error {
		item, err := txn.Get(finalityCheckpointDBKey(height))
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil
			}
			return err
		}
		found = true
		return item.Value(func(val []byte) error {
			plain, err := decryptDBValue(val)
			if err != nil {
				return err
			}
			return json.Unmarshal(plain, &out)
		})
	})
	return out, found, err
}

func (n *Node) loadPersistedFinalityCertificate(height uint64) (FinalizedEpochCertificate, bool, error) {
	var out FinalizedEpochCertificate
	if n == nil || n.DB == nil || n.DB.Meta == nil || height == 0 {
		return out, false, nil
	}
	found := false
	err := n.DB.Meta.View(func(txn *Txn) error {
		item, err := txn.Get(finalityCertificateDBKey(height))
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil
			}
			return err
		}
		found = true
		return item.Value(func(val []byte) error {
			plain, err := decryptDBValue(val)
			if err != nil {
				return err
			}
			return json.Unmarshal(plain, &out)
		})
	})
	return out, found, err
}

func verifyFinalityCheckpointRecordMatchesBlock(record finalityCheckpointRecord, block Block) error {
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

func finalityArtifactPaths(dataDir, nodeID string, height uint64) map[string]string {
	return map[string]string{
		finalityCertificatesDir:         finalityArtifactFilePath(dataDir, nodeID, finalityCertificatesDir, height),
		finalityEpochAnchorsDir:         finalityArtifactFilePath(dataDir, nodeID, finalityEpochAnchorsDir, height),
		finalityValidatorCommitmentsDir: finalityArtifactFilePath(dataDir, nodeID, finalityValidatorCommitmentsDir, height),
		finalityIrreversibleRootsDir:    finalityArtifactFilePath(dataDir, nodeID, finalityIrreversibleRootsDir, height),
	}
}

func finalityArtifactFilePath(dataDir, nodeID, dir string, height uint64) string {
	return filepath.Join(nodeDataPath(dataDir, nodeID), dir, fmt.Sprintf("epoch_%020d.json", height))
}

func finalityArtifactCheckpointBoundary(height uint64) bool {
	if height <= 1 {
		return true
	}
	interval := syncCheckpointIntervalBlocks()
	if interval == 0 {
		return true
	}
	return height%interval == 0
}

func writeFinalityArtifactJSON(path string, value any) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("finality_artifact_path_empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeFileAtomic(path, raw, 0o600)
}

func loadFinalityArtifactJSON(path string, out any) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return true, err
	}
	return true, nil
}

func (n *Node) finalityArtifactFileCount(height uint64) int {
	if n == nil || height == 0 {
		return 0
	}
	count := 0
	for _, path := range finalityArtifactPaths(n.DataDir, n.ID, height) {
		if _, err := os.Stat(path); err == nil {
			count++
		}
	}
	return count
}

func (n *Node) shouldPersistFinalityArtifactFiles(height uint64) bool {
	if n == nil || height == 0 {
		return true
	}
	if finalityArtifactCheckpointBoundary(height) {
		return true
	}
	switch normalizeNodeRole(n.Role) {
	case "full", "light":
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

func verifyEpochAnchorArtifactMatchesBlock(record EpochAnchorRecord, block Block) error {
	expected := epochAnchorRecordFromBlock(block)
	if record.Epoch != expected.Epoch || record.Height != expected.Height {
		return errors.New("epoch_anchor_artifact_height_mismatch")
	}
	if strings.TrimSpace(record.Version) != expected.Version || strings.TrimSpace(record.Domain) != expected.Domain {
		return errors.New("epoch_anchor_artifact_domain_mismatch")
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

func verifyValidatorCommitmentArtifactMatchesBlock(record ValidatorCommitmentRecord, block Block) error {
	expected := validatorCommitmentRecordFromBlock(block)
	if record.Epoch != expected.Epoch || record.Height != expected.Height {
		return errors.New("validator_commitment_artifact_height_mismatch")
	}
	if strings.TrimSpace(record.Version) != expected.Version || strings.TrimSpace(record.Domain) != expected.Domain {
		return errors.New("validator_commitment_artifact_domain_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.ValidatorSetHash), expected.ValidatorSetHash) ||
		!strings.EqualFold(strings.TrimSpace(record.ValidatorSetRoot), expected.ValidatorSetRoot) ||
		!strings.EqualFold(strings.TrimSpace(record.FinalizedValidatorSetHash), expected.FinalizedValidatorSetHash) ||
		!strings.EqualFold(strings.TrimSpace(record.FinalizedValidatorSetRoot), expected.FinalizedValidatorSetRoot) {
		return errors.New("validator_commitment_artifact_mismatch")
	}
	return nil
}

func verifyIrreversibleRootArtifactMatchesBlock(record IrreversibleRoot, block Block) error {
	expected := irreversibleRootFromBlock(block)
	if record.Epoch != expected.Epoch || record.Height != expected.Height {
		return errors.New("irreversible_root_artifact_height_mismatch")
	}
	if strings.TrimSpace(record.Version) != expected.Version || strings.TrimSpace(record.Domain) != expected.Domain {
		return errors.New("irreversible_root_artifact_domain_mismatch")
	}
	if !strings.EqualFold(strings.TrimSpace(record.FinalizedHash), expected.FinalizedHash) ||
		!strings.EqualFold(strings.TrimSpace(record.StateRoot), expected.StateRoot) ||
		!strings.EqualFold(strings.TrimSpace(record.FinalityRoot), expected.FinalityRoot) ||
		!strings.EqualFold(strings.TrimSpace(record.EpochAnchorHash), expected.EpochAnchorHash) {
		return errors.New("irreversible_root_artifact_mismatch")
	}
	return nil
}

func verifyFinalityCertificateArtifactMatchesBlock(cert FinalizedEpochCertificate, block Block) error {
	if strings.TrimSpace(cert.Version) != finalityCertificateVersionV1 || strings.TrimSpace(cert.Domain) != finalityCertificateDomainV1 {
		return errors.New("finality_certificate_artifact_domain_mismatch")
	}
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
		!strings.EqualFold(strings.TrimSpace(cert.FinalityRoot), strings.TrimSpace(block.FinalityRoot)) {
		return errors.New("finality_certificate_artifact_mismatch")
	}
	if !sameStringSlice(canonicalValidatorIDs(cert.Signers), finalitySignersForBlock(block)) {
		return errors.New("finality_certificate_artifact_signer_mismatch")
	}
	return nil
}

func (n *Node) verifyFinalityArtifacts(block Block) error {
	if n == nil || block.ID == 0 || !blockFinalityCommitmentsPresent(block) {
		return nil
	}
	paths := finalityArtifactPaths(n.DataDir, n.ID, block.ID)
	found := 0
	var cert FinalizedEpochCertificate
	if ok, err := loadFinalityArtifactJSON(paths[finalityCertificatesDir], &cert); err != nil {
		return err
	} else if ok {
		found++
		if err := verifyFinalityCertificateArtifactMatchesBlock(cert, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	var anchor EpochAnchorRecord
	if ok, err := loadFinalityArtifactJSON(paths[finalityEpochAnchorsDir], &anchor); err != nil {
		return err
	} else if ok {
		found++
		if err := verifyEpochAnchorArtifactMatchesBlock(anchor, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	var commitment ValidatorCommitmentRecord
	if ok, err := loadFinalityArtifactJSON(paths[finalityValidatorCommitmentsDir], &commitment); err != nil {
		return err
	} else if ok {
		found++
		if err := verifyValidatorCommitmentArtifactMatchesBlock(commitment, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	var root IrreversibleRoot
	if ok, err := loadFinalityArtifactJSON(paths[finalityIrreversibleRootsDir], &root); err != nil {
		return err
	} else if ok {
		found++
		if err := verifyIrreversibleRootArtifactMatchesBlock(root, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	if found > 0 && found != len(paths) {
		return fmt.Errorf("irreversible_finality_artifact_incomplete: found=%d want=%d height=%d", found, len(paths), block.ID)
	}
	return nil
}

func (n *Node) verifyFinalityArtifactsForRepair(block Block) error {
	if n == nil || block.ID == 0 || !blockFinalityCommitmentsPresent(block) {
		return nil
	}
	paths := finalityArtifactPaths(n.DataDir, n.ID, block.ID)
	var cert FinalizedEpochCertificate
	if ok, err := loadFinalityArtifactJSON(paths[finalityCertificatesDir], &cert); err != nil {
		return err
	} else if ok {
		if err := verifyFinalityCertificateArtifactMatchesBlock(cert, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	var anchor EpochAnchorRecord
	if ok, err := loadFinalityArtifactJSON(paths[finalityEpochAnchorsDir], &anchor); err != nil {
		return err
	} else if ok {
		if err := verifyEpochAnchorArtifactMatchesBlock(anchor, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	var commitment ValidatorCommitmentRecord
	if ok, err := loadFinalityArtifactJSON(paths[finalityValidatorCommitmentsDir], &commitment); err != nil {
		return err
	} else if ok {
		if err := verifyValidatorCommitmentArtifactMatchesBlock(commitment, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	var root IrreversibleRoot
	if ok, err := loadFinalityArtifactJSON(paths[finalityIrreversibleRootsDir], &root); err != nil {
		return err
	} else if ok {
		if err := verifyIrreversibleRootArtifactMatchesBlock(root, block); err != nil {
			return fmt.Errorf("irreversible_finality_artifact_conflict: %w", err)
		}
	}
	return nil
}

func (n *Node) persistFinalityArtifacts(block Block) error {
	if n == nil || block.ID == 0 || !blockFinalityCommitmentsPresent(block) {
		return nil
	}
	if err := n.verifyFinalityArtifactsForRepair(block); err != nil {
		return err
	}
	if !n.shouldPersistFinalityArtifactFiles(block.ID) {
		return nil
	}
	artifacts := []struct {
		dir   string
		value any
	}{
		{dir: finalityCertificatesDir, value: block.FinalityCertificate},
		{dir: finalityEpochAnchorsDir, value: epochAnchorRecordFromBlock(block)},
		{dir: finalityValidatorCommitmentsDir, value: validatorCommitmentRecordFromBlock(block)},
		{dir: finalityIrreversibleRootsDir, value: irreversibleRootFromBlock(block)},
	}
	for _, artifact := range artifacts {
		path := finalityArtifactFilePath(n.DataDir, n.ID, artifact.dir, block.ID)
		if err := writeFinalityArtifactJSON(path, artifact.value); err != nil {
			return err
		}
	}
	return nil
}

func (n *Node) verifyPersistedFinalityCheckpoint(block Block) error {
	if n == nil || block.ID == 0 {
		return nil
	}
	checkpointAheadOfTip := n.finalityCheckpointAheadOfLocalChain(block.ID)
	record, ok, err := n.loadPersistedFinalityCheckpoint(block.ID)
	if err != nil || !ok {
		if err != nil {
			return err
		}
		if anchor, anchorOK, anchorErr := n.loadPersistedFinalityAnchorHash(block.ID); anchorErr != nil {
			return anchorErr
		} else if anchorOK && strings.TrimSpace(anchor) != "" {
			if checkpointAheadOfTip {
				return nil
			}
			return fmt.Errorf("irreversible_finality_checkpoint_incomplete: anchor_without_checkpoint height=%d", block.ID)
		}
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
	if err := verifyFinalityCheckpointRecordMatchesBlock(record, block); err != nil {
		if checkpointAheadOfTip {
			return nil
		}
		return fmt.Errorf("irreversible_finality_checkpoint_conflict: %w", err)
	}
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
	if cert, certOK, certErr := n.loadPersistedFinalityCertificate(block.ID); certErr != nil {
		return certErr
	} else if certOK {
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

func (n *Node) finalityCheckpointAheadOfLocalChain(height uint64) bool {
	if n == nil || n.Blockchain == nil || height == 0 {
		return false
	}
	return n.Blockchain.Height() < height
}

func (n *Node) localChainCommittedBlockMatches(height uint64, hash string) bool {
	if n == nil || n.Blockchain == nil || height == 0 || strings.TrimSpace(hash) == "" {
		return false
	}
	block, ok := n.Blockchain.GetBlock(height)
	if !ok {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(block.BlockHash), strings.TrimSpace(hash))
}

func (n *Node) loadPersistedFinalityAnchorHash(height uint64) (string, bool, error) {
	if n == nil || n.DB == nil || n.DB.Meta == nil || height == 0 {
		return "", false, nil
	}
	out := ""
	found := false
	err := n.DB.Meta.View(func(txn *Txn) error {
		item, err := txn.Get(finalityAnchorDBKey(height))
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil
			}
			return err
		}
		found = true
		return item.Value(func(val []byte) error {
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

func (n *Node) finalityAnchorHashForHeight(height uint64) (string, bool, error) {
	if n == nil || height == 0 {
		return "", false, nil
	}
	if n.Blockchain != nil {
		if block, ok := n.Blockchain.GetBlock(height); ok && strings.TrimSpace(block.EpochAnchorHash) != "" {
			return strings.TrimSpace(block.EpochAnchorHash), true, nil
		}
	}
	if anchor, ok, err := n.loadPersistedFinalityAnchorHash(height); err != nil || ok {
		return anchor, ok, err
	}
	return "", false, nil
}

func (n *Node) persistFinalityCheckpoint(block Block) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil || block.ID == 0 || !blockFinalityCommitmentsPresent(block) {
		return nil
	}
	if err := n.verifyFinalityCommitments(block, n.resolveBlockVerificationValidatorsForFinality(block)); err != nil {
		return err
	}
	certRaw, err := json.Marshal(block.FinalityCertificate)
	if err != nil {
		return err
	}
	checkpoint := finalityCheckpointRecordFromBlock(block)
	checkpointRaw, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	anchor := strings.TrimSpace(block.EpochAnchorHash)
	if err := n.DB.Meta.Update(func(txn *Txn) error {
		anchorKey := finalityAnchorDBKey(block.ID)
		shouldWriteAnchor := true
		if item, err := txn.Get(anchorKey); err == nil && item != nil {
			existing := ""
			if vErr := item.Value(func(val []byte) error {
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
			enc, err := encryptDBValue([]byte(anchor))
			if err != nil {
				return err
			}
			if err := txn.Set(anchorKey, enc); err != nil {
				return err
			}
		}
		checkpointKey := finalityCheckpointDBKey(block.ID)
		if item, err := txn.Get(checkpointKey); err == nil && item != nil {
			var existing finalityCheckpointRecord
			if vErr := item.Value(func(val []byte) error {
				plain, derr := decryptDBValue(val)
				if derr != nil {
					return derr
				}
				return json.Unmarshal(plain, &existing)
			}); vErr != nil {
				return vErr
			}
			if err := verifyFinalityCheckpointRecordMatchesBlock(existing, block); err != nil {
				if !n.localChainCommittedBlockMatches(block.ID, block.BlockHash) {
					return fmt.Errorf("irreversible finality checkpoint violation height=%d: %w", block.ID, err)
				}
			}
		} else if err != nil && !errors.Is(err, ErrKeyNotFound) {
			return err
		}
		encCheckpoint, err := encryptDBValue(checkpointRaw)
		if err != nil {
			return err
		}
		if err := txn.Set(checkpointKey, encCheckpoint); err != nil {
			return err
		}
		certKey := finalityCertificateDBKey(block.ID)
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

func (n *Node) resolveBlockVerificationValidatorsForFinality(block Block) []string {
	if n == nil || block.ID == 0 {
		return nil
	}
	if validators := n.freezeValidatorSetForHeight(block.ID, n.GetConsensusValidators(int(block.ID))); len(validators) > 0 {
		return canonicalValidatorIDs(validators)
	}
	if validators := finalitySignersForBlock(block); len(validators) > 0 {
		return validators
	}
	return nil
}
