package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const validatorUpdateCertDomain = "MSC_VALIDATOR_UPDATE_V1"

type validatorUpdateExecutionContext struct {
	height               uint64
	expectedRegistryHash string
	registrySnapshot     map[string]ValidatorRecord
	activeValidators     []string
	pendingAdds          map[string]uint64
	pendingRemovals      map[string]uint64
}

func normalizeValidatorUpdateCert(cert *ValidatorUpdateCertificate) {
	if cert == nil {
		return
	}
	cert.ParentRegistryHash = strings.ToLower(strings.TrimSpace(cert.ParentRegistryHash))
	normalized := make([]ValidatorUpdateCertSignature, 0, len(cert.Signatures))
	for _, sig := range cert.Signatures {
		signerID := normalizeValidatorID(sig.SignerID)
		sigHex := strings.ToLower(strings.TrimSpace(sig.SigHex))
		if signerID == "" || sigHex == "" {
			continue
		}
		normalized = append(normalized, ValidatorUpdateCertSignature{
			SignerID: signerID,
			SigHex:   sigHex,
		})
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].SignerID != normalized[j].SignerID {
			return normalized[i].SignerID < normalized[j].SignerID
		}
		return normalized[i].SigHex < normalized[j].SigHex
	})
	cert.Signatures = normalized
}

func validatorUpdateCertShapeError(cert *ValidatorUpdateCertificate) error {
	if cert == nil {
		return fmt.Errorf("missing validator_update_cert")
	}
	if hash := strings.TrimSpace(cert.ParentRegistryHash); hash == "" {
		return fmt.Errorf("missing validator_update_parent_registry_hash")
	} else if len(hash) != 64 {
		return fmt.Errorf("invalid validator_update_parent_registry_hash")
	}
	if cert.ProposalNonce == 0 {
		return fmt.Errorf("missing validator_update_proposal_nonce")
	}
	if cert.ExpiryHeight == 0 {
		return fmt.Errorf("missing validator_update_expiry_height")
	}
	if len(cert.Signatures) == 0 {
		return fmt.Errorf("missing validator_update_signatures")
	}
	if len(cert.Signatures) > 64 {
		return fmt.Errorf("validator_update_signatures_too_many")
	}
	for _, sig := range cert.Signatures {
		if normalizeValidatorID(sig.SignerID) == "" {
			return fmt.Errorf("invalid validator_update_signer_id")
		}
		if raw := strings.TrimSpace(sig.SigHex); len(raw) != ed25519.SignatureSize*2 {
			return fmt.Errorf("invalid validator_update_signature")
		}
	}
	return nil
}

func validatorUpdateCertSigningPayload(chainID, action, validatorID, parentRegistryHash string, proposalNonce, expiryHeight uint64) []byte {
	return []byte(strings.Join([]string{
		validatorUpdateCertDomain,
		strings.TrimSpace(chainID),
		strings.TrimSpace(action),
		normalizeValidatorID(validatorID),
		strings.ToLower(strings.TrimSpace(parentRegistryHash)),
		strconv.FormatUint(proposalNonce, 10),
		strconv.FormatUint(expiryHeight, 10),
	}, "|"))
}

func validatorUpdateCertMessageHash(chainID, action, validatorID, parentRegistryHash string, proposalNonce, expiryHeight uint64) string {
	sum := sha256.Sum256(validatorUpdateCertSigningPayload(chainID, action, validatorID, parentRegistryHash, proposalNonce, expiryHeight))
	return hex.EncodeToString(sum[:])
}

func validatorUpdateMessageHash(tx Transaction) string {
	if tx.ValidatorUpdateCert == nil {
		return ""
	}
	action, validatorID, ok := parseValidatorUpdateTarget(tx.To)
	if !ok {
		return ""
	}
	cert := tx.ValidatorUpdateCert
	return validatorUpdateCertMessageHash(
		tx.ChainID,
		action,
		validatorID,
		cert.ParentRegistryHash,
		cert.ProposalNonce,
		cert.ExpiryHeight,
	)
}

func ensureValidatorUpdateCertLedgerState(ledger *Ledger) {
	if ledger == nil {
		return
	}
	if ledger.UsedValidatorUpdateCerts == nil {
		ledger.UsedValidatorUpdateCerts = make(map[string]uint64)
	}
}

func validatorUpdateCertUsed(ledger Ledger, messageHash string) bool {
	if messageHash == "" || len(ledger.UsedValidatorUpdateCerts) == 0 {
		return false
	}
	_, ok := ledger.UsedValidatorUpdateCerts[messageHash]
	return ok
}

func markValidatorUpdateCertUsed(ledger *Ledger, messageHash string, height uint64) {
	if ledger == nil || messageHash == "" {
		return
	}
	ensureValidatorUpdateCertLedgerState(ledger)
	ledger.UsedValidatorUpdateCerts[messageHash] = height
}

func validatorUpdateCommitmentHeight(inclusionHeight uint64) uint64 {
	if inclusionHeight == 0 {
		return 0
	}
	if validatorSetCommitmentV2EnabledAt(inclusionHeight) {
		return inclusionHeight
	}
	delay := validatorSetActivationDelayBlocks()
	if delay == 0 {
		delay = 1
	}
	if delay == 1 {
		return inclusionHeight
	}
	maxU64 := ^uint64(0)
	offset := delay - 1
	if inclusionHeight > maxU64-offset {
		return maxU64
	}
	return inclusionHeight + offset
}

func validatorUpdateEnvelopeBasicError(tx Transaction, ledger *Ledger, height uint64) error {
	if tx.Amount != 0 {
		return fmt.Errorf("validator_update_amount_must_be_zero")
	}
	action, validatorID, ok := parseValidatorUpdateTarget(tx.To)
	if !ok || validatorID == "" || (action != "add" && action != "remove") {
		return fmt.Errorf("invalid_validator_update_target")
	}
	cert := tx.ValidatorUpdateCert
	if err := validatorUpdateCertShapeError(cert); err != nil {
		return err
	}
	if cert.ExpiryHeight < height {
		return fmt.Errorf("validator_update_cert_expired")
	}
	messageHash := validatorUpdateMessageHash(tx)
	if messageHash == "" {
		return fmt.Errorf("invalid_validator_update_message_hash")
	}
	if ledger != nil && validatorUpdateCertUsed(*ledger, messageHash) {
		return fmt.Errorf("validator_update_cert_replayed")
	}
	return nil
}

func validatorUpdateAuthorityFromSnapshot(snapshot map[string]ValidatorRecord) ([]string, map[string]ed25519.PublicKey) {
	if len(snapshot) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(snapshot))
	pubs := make(map[string]ed25519.PublicKey, len(snapshot))
	for key, rec := range snapshot {
		id := normalizeValidatorID(rec.ID)
		if id == "" {
			id = normalizeValidatorID(key)
		}
		if id == "" || !rec.GovernanceSigner {
			continue
		}
		pubHex := strings.TrimSpace(rec.ConsensusPubKey)
		if pubHex == "" {
			continue
		}
		pubRaw, err := hex.DecodeString(pubHex)
		if err != nil || len(pubRaw) != ed25519.PublicKeySize {
			continue
		}
		ids = append(ids, id)
		pubs[id] = ed25519.PublicKey(append([]byte(nil), pubRaw...))
	}
	ids = canonicalValidatorIDs(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	return ids, pubs
}

func copyUint64Map(src map[string]uint64) map[string]uint64 {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]uint64, len(src))
	for key, value := range src {
		norm := normalizeValidatorID(key)
		if norm == "" {
			continue
		}
		if existing, ok := out[norm]; !ok || value < existing {
			out[norm] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (n *Node) validatorUpdateSnapshotPendingState(height uint64) (map[string]uint64, map[string]uint64) {
	if n == nil || height == 0 {
		return nil, nil
	}
	parentHeight := height - 1
	if parentHeight > 0 && n.DB != nil && n.DB.State != nil {
		if snap, err := n.GetSnapshot(parentHeight); err == nil && snap != nil {
			return copyUint64Map(snap.PendingValidators), copyUint64Map(snap.PendingValidatorRemovals)
		}
	}
	n.validatorSetMu.RLock()
	defer n.validatorSetMu.RUnlock()
	return copyUint64Map(n.pendingValidators), copyUint64Map(n.pendingValidatorRemovals)
}

func (n *Node) validatorUpdateActiveSetForHeight(height uint64) []string {
	if n == nil || height == 0 {
		return nil
	}
	if current := n.plannedValidatorSetForHeightFromChain(height); len(current) > 0 {
		return current
	}
	if current := n.consensusValidatorsForHeight(height); len(current) > 0 {
		return current
	}
	if height == 1 && len(n.GenesisValidators) > 0 {
		return canonicalValidatorIDs(append([]string{}, n.GenesisValidators...))
	}
	return nil
}

func (n *Node) newValidatorUpdateExecutionContext(height uint64) *validatorUpdateExecutionContext {
	if n == nil || height == 0 || !validatorSetCommitmentV2EnabledAt(height) {
		return nil
	}
	registryHash, source := n.expectedValidatorRegistryHashWithSource(height)
	registrySnapshot := n.validatorRegistrySnapshotForHeight(height)
	chainHeight := uint64(0)
	if n.Blockchain != nil {
		chainHeight = n.Blockchain.Height()
	}
	// Bootstrap/early-height validator-update plans must validate against the
	// full runtime registry snapshot. Filtering the registry down to the active
	// committee breaks the governance cert parent hash and causes the proposer
	// to silently drop validator-update transactions from freshly built blocks.
	if chainHeight <= 1 {
		if runtimeRegistry := copyValidatorRegistrySnapshot(GlobalValidatorRegistry.Snapshot()); len(runtimeRegistry) > 0 {
			registrySnapshot = runtimeRegistry
			runtimeHash := strings.TrimSpace(ValidatorRegistrySnapshotHash(runtimeRegistry))
			if runtimeHash != "" && (strings.TrimSpace(registryHash) == "" || source == "none" || source == "bootstrap_registry") {
				registryHash = runtimeHash
				source = "bootstrap_registry"
			}
		}
	}
	if strings.TrimSpace(registryHash) == "" &&
		source == "none" &&
		chainHeight <= 1 &&
		len(registrySnapshot) > 0 {
		registryHash = ValidatorRegistrySnapshotHash(registrySnapshot)
		source = "bootstrap_registry"
	}
	if registryHash == "" || (source != "snapshot_parent" && source != "registry_snapshot" && source != "chain_block" && source != "chain_parent_commitment" && source != "bootstrap_registry") {
		return nil
	}
	if len(registrySnapshot) == 0 {
		return nil
	}
	active := n.validatorUpdateActiveSetForHeight(height)
	if len(active) == 0 {
		return nil
	}
	pendingAdds, pendingRemovals := n.validatorUpdateSnapshotPendingState(height)
	return &validatorUpdateExecutionContext{
		height:               height,
		expectedRegistryHash: strings.ToLower(strings.TrimSpace(registryHash)),
		registrySnapshot:     registrySnapshot,
		activeValidators:     canonicalValidatorIDs(active),
		pendingAdds:          pendingAdds,
		pendingRemovals:      pendingRemovals,
	}
}

func (ctx *validatorUpdateExecutionContext) activeSetContains(id string) bool {
	id = normalizeValidatorID(id)
	if id == "" {
		return false
	}
	for _, existing := range ctx.activeValidators {
		if normalizeValidatorID(existing) == id {
			return true
		}
	}
	return false
}

func (ctx *validatorUpdateExecutionContext) queueAdd(id string, updateHeight uint64) {
	id = normalizeValidatorID(id)
	if id == "" {
		return
	}
	if ctx.pendingAdds == nil {
		ctx.pendingAdds = make(map[string]uint64)
	}
	if existing, ok := ctx.pendingAdds[id]; !ok || updateHeight < existing {
		ctx.pendingAdds[id] = updateHeight
	}
	if ctx.pendingRemovals != nil {
		delete(ctx.pendingRemovals, id)
	}
}

func (ctx *validatorUpdateExecutionContext) queueRemoval(id string, updateHeight uint64) {
	id = normalizeValidatorID(id)
	if id == "" {
		return
	}
	if ctx.pendingRemovals == nil {
		ctx.pendingRemovals = make(map[string]uint64)
	}
	if existing, ok := ctx.pendingRemovals[id]; !ok || updateHeight < existing {
		ctx.pendingRemovals[id] = updateHeight
	}
	if ctx.pendingAdds != nil {
		delete(ctx.pendingAdds, id)
	}
}

func (ctx *validatorUpdateExecutionContext) cancelPendingAdd(id string) {
	id = normalizeValidatorID(id)
	if id == "" || ctx.pendingAdds == nil {
		return
	}
	delete(ctx.pendingAdds, id)
}

func (ctx *validatorUpdateExecutionContext) cancelPendingRemoval(id string) {
	id = normalizeValidatorID(id)
	if id == "" || ctx.pendingRemovals == nil {
		return
	}
	delete(ctx.pendingRemovals, id)
}

func (ctx *validatorUpdateExecutionContext) plannedValidatorsForHeight(height uint64) []string {
	if ctx == nil || height == 0 {
		return nil
	}
	current := canonicalValidatorIDs(append([]string{}, ctx.activeValidators...))
	if len(current) == 0 {
		return nil
	}
	targetFinalized := height - 1
	set := make(map[string]struct{}, len(current))
	for _, id := range current {
		set[id] = struct{}{}
	}
	for id, act := range ctx.pendingAdds {
		if act == 0 || act > targetFinalized {
			continue
		}
		set[normalizeValidatorID(id)] = struct{}{}
	}
	for id, act := range ctx.pendingRemovals {
		if act == 0 || act > targetFinalized {
			continue
		}
		delete(set, normalizeValidatorID(id))
	}
	next := make([]string, 0, len(set))
	for id := range set {
		if id == "" {
			continue
		}
		next = append(next, id)
	}
	return canonicalValidatorIDs(next)
}

func (ctx *validatorUpdateExecutionContext) plannedNextCommitment(height uint64) (string, string, string) {
	if ctx == nil || height == 0 {
		return "", "", "none"
	}
	nextHeight := height + 1
	validators := ctx.plannedValidatorsForHeight(nextHeight)
	if len(validators) == 0 {
		return "", "", "none"
	}
	hash := strings.TrimSpace(validatorSetHashFromSnapshotForHeight(nextHeight, validators, ctx.registrySnapshot))
	root := strings.TrimSpace(ValidatorSetMerkleRoot(nextHeight, validators, ctx.registrySnapshot))
	if hash == "" {
		return "", "", "none"
	}
	return hash, root, "block_tx_plan"
}

func (n *Node) blockValidatorUpdatePlanContext(block Block) *validatorUpdateExecutionContext {
	if n == nil || block.ID == 0 || !validatorSetCommitmentV2EnabledAt(block.ID) {
		return nil
	}
	ctx := n.newValidatorUpdateExecutionContext(block.ID)
	if ctx == nil {
		return nil
	}
	for _, tx := range block.Transactions {
		ctx.applyPlanOnly(tx)
	}
	return ctx
}

func (n *Node) expectedNextValidatorSetCommitmentForBlock(block Block) (string, string, string) {
	if n == nil || block.ID == 0 {
		return "", "", "none"
	}
	if ctx := n.blockValidatorUpdatePlanContext(block); ctx != nil {
		if hash, root, source := ctx.plannedNextCommitment(block.ID); hash != "" {
			return hash, root, source
		}
	}
	nextHash, source := n.deterministicNextValidatorSetHashWithSource(block.ID, block.ValidatorSetHash)
	nextRoot, _ := n.expectedNextValidatorSetRootWithSource(block.ID, block.ValidatorSetHash, block.ValidatorSetRoot)
	return strings.TrimSpace(nextHash), strings.TrimSpace(nextRoot), source
}

func (n *Node) authoritativeNextValidatorSetCandidateForBlock(block Block, targetHash string) ([]string, string, string, bool) {
	if n == nil || block.ID == 0 {
		return nil, "", "none", false
	}
	targetHash = strings.TrimSpace(targetHash)
	if targetHash == "" {
		return nil, "", "none", false
	}
	nextHeight := block.ID + 1
	registrySnapshot := map[string]ValidatorRecord(nil)
	candidates := make([][]string, 0, 6)
	tryCandidate := func(values []string, source string) ([]string, string, string, bool) {
		if len(values) == 0 {
			return nil, "", "none", false
		}
		matched, ok := n.validatorSetCandidateMatchesTarget(nextHeight, targetHash, values, registrySnapshot)
		if !ok {
			return nil, "", "none", false
		}
		root := ""
		if len(registrySnapshot) > 0 {
			root = strings.TrimSpace(ValidatorSetMerkleRoot(nextHeight, matched, registrySnapshot))
		}
		return matched, root, source, true
	}
	if ctx := n.blockValidatorUpdatePlanContext(block); ctx != nil {
		if len(ctx.registrySnapshot) > 0 {
			registrySnapshot = copyValidatorRegistrySnapshot(ctx.registrySnapshot)
		}
		if planned := ctx.plannedValidatorsForHeight(nextHeight); len(planned) > 0 {
			candidates = append(candidates, planned)
			if matched, root, source, ok := tryCandidate(planned, "block_execution_plan"); ok {
				return matched, root, source, true
			}
		}
	}
	if len(registrySnapshot) == 0 {
		registrySnapshot = n.validatorRegistrySnapshotForHeight(nextHeight)
	}
	if planned := n.plannedValidatorSetForHeightFromChain(nextHeight); len(planned) > 0 {
		candidates = append(candidates, planned)
		if matched, root, source, ok := tryCandidate(planned, "chain_planned_transition"); ok {
			return matched, root, source, true
		}
	}
	if committed, ok := n.blockValidatorSetFromSignatures(block); ok && len(committed) > 0 {
		candidates = append(candidates, committed)
		if matched, root, source, ok := tryCandidate(committed, "current_block_signatures"); ok {
			return matched, root, source, true
		}
	}
	if frozen := n.frozenValidatorsForHeight(nextHeight); len(frozen) > 0 {
		candidates = append(candidates, frozen)
		if matched, root, source, ok := tryCandidate(frozen, "frozen_next_height"); ok {
			return matched, root, source, true
		}
	}
	if frozen := n.frozenValidatorsForHeight(block.ID); len(frozen) > 0 {
		candidates = append(candidates, frozen)
		if matched, root, source, ok := tryCandidate(frozen, "frozen_current_height"); ok {
			return matched, root, source, true
		}
	}
	if len(registrySnapshot) == 0 {
		if snap, _, ok := n.committedParentProjectionSnapshot(nextHeight); ok && snap != nil {
			registrySnapshot = validatorRegistrySnapshotFromStateSnapshot(snap)
		}
	}
	if reconstructed, ok := n.reconstructValidatorSetCandidateForTarget(nextHeight, targetHash, registrySnapshot, candidates...); ok {
		root := ""
		source := "next_commitment_subset_reconstructed"
		if len(registrySnapshot) > 0 {
			root = strings.TrimSpace(ValidatorSetMerkleRoot(nextHeight, reconstructed, registrySnapshot))
			source = "registry_verified_next_commitment"
		}
		return reconstructed, root, source, true
	}
	return nil, "", "none", false
}

func (n *Node) blockNextValidatorSetHashMatchesAuthoritativeCandidates(block Block, targetHash string) (string, bool) {
	if n == nil || block.ID == 0 {
		return "", false
	}
	targetHash = strings.TrimSpace(targetHash)
	if targetHash == "" {
		return "", false
	}
	_, _, source, ok := n.authoritativeNextValidatorSetCandidateForBlock(block, targetHash)
	return source, ok
}

func (n *Node) queuedChildMatchesParentNextValidatorSetCommitment(block Block, targetHash string) (string, bool) {
	if n == nil || block.ID == 0 {
		return "", false
	}
	targetHash = strings.TrimSpace(targetHash)
	if targetHash == "" || strings.TrimSpace(block.BlockHash) == "" {
		return "", false
	}
	n.forkMu.RLock()
	children := append([]Block(nil), n.ForkBlocks[block.ID+1]...)
	n.forkMu.RUnlock()
	for _, child := range children {
		if !strings.EqualFold(strings.TrimSpace(child.PrevHash), strings.TrimSpace(block.BlockHash)) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(child.ValidatorSetHash), targetHash) {
			return "queued_child_parent_commitment", true
		}
	}
	return "", false
}

func (n *Node) queuedChildExtendsBlockDuringSync(block Block) (string, bool) {
	if n == nil || block.ID == 0 || strings.TrimSpace(block.BlockHash) == "" {
		return "", false
	}
	stage, _ := n.syncDiagnosticContext()
	if strings.TrimSpace(stage) == "" {
		return "", false
	}
	n.forkMu.RLock()
	children := append([]Block(nil), n.ForkBlocks[block.ID+1]...)
	n.forkMu.RUnlock()
	for _, child := range children {
		if strings.EqualFold(strings.TrimSpace(child.PrevHash), strings.TrimSpace(block.BlockHash)) {
			return "queued_child_chain_continuity", true
		}
	}
	return "", false
}

func (n *Node) syncContinuityValidatorFallback(block Block) []string {
	if _, ok := n.queuedChildExtendsBlockDuringSync(block); !ok {
		if n == nil || n.Blockchain == nil || block.ID != n.Blockchain.Height()+1 {
			return nil
		}
		stage, _ := n.syncDiagnosticContext()
		if strings.TrimSpace(stage) == "" {
			return nil
		}
		last := n.Blockchain.LastBlock()
		if strings.TrimSpace(last.BlockHash) == "" || !strings.EqualFold(strings.TrimSpace(block.PrevHash), strings.TrimSpace(last.BlockHash)) {
			return nil
		}
		if len(block.Signatures) == 0 && len(block.ExecutionResults) == 0 {
			return nil
		}
	}
	out := make([]string, 0, len(block.Signatures)+len(block.ExecutionResults)+1)
	if proposer := normalizeValidatorID(block.Proposer); proposer != "" {
		out = append(out, proposer)
	}
	for _, signer := range block.Signatures {
		if id := normalizeValidatorID(signer); id != "" {
			out = append(out, id)
		}
	}
	for _, result := range block.ExecutionResults {
		if id := normalizeValidatorID(result.Signer); id != "" {
			out = append(out, id)
		}
	}
	return canonicalValidatorIDs(out)
}

func (n *Node) syncExecutionResultQuorumFallback(block Block, validators []string) bool {
	if n == nil || n.Blockchain == nil || block.ID == 0 || len(block.ExecutionResults) == 0 {
		return false
	}
	stage, _ := n.syncDiagnosticContext()
	if strings.TrimSpace(stage) == "" {
		return false
	}
	if block.ID != n.Blockchain.Height()+1 {
		return false
	}
	last := n.Blockchain.LastBlock()
	if strings.TrimSpace(last.BlockHash) == "" || !strings.EqualFold(strings.TrimSpace(block.PrevHash), strings.TrimSpace(last.BlockHash)) {
		return false
	}

	validatorSet := make(map[string]struct{}, len(validators))
	for _, id := range canonicalValidatorIDs(validators) {
		validatorSet[id] = struct{}{}
	}
	signers := make(map[string]struct{}, len(block.ExecutionResults))
	for _, result := range block.ExecutionResults {
		if strings.TrimSpace(result.BlockHash) != "" && !strings.EqualFold(strings.TrimSpace(result.BlockHash), strings.TrimSpace(block.BlockHash)) {
			continue
		}
		id := normalizeValidatorID(result.Signer)
		if id == "" {
			continue
		}
		if len(validatorSet) > 0 {
			if _, ok := validatorSet[id]; !ok {
				continue
			}
		}
		signers[id] = struct{}{}
	}
	if len(signers) > 0 {
		total := len(validatorSet)
		if total == 0 {
			total = len(signers)
		}
		required := execQuorumRequired(total)
		if required <= 0 {
			required = 1
		}
		if len(signers) >= required {
			return true
		}
	}

	metadataRequired := block.RequiredQuorum
	if metadataRequired <= 0 {
		return false
	}
	metadataActiveReady := block.ActiveReadyCount
	if metadataActiveReady <= 0 {
		metadataActiveReady = metadataRequired
	}
	if metadataRequired > metadataActiveReady {
		return false
	}
	if block.StrictQuorum > 0 && metadataRequired < block.StrictQuorum {
		return false
	}
	mode := strings.ToUpper(strings.TrimSpace(block.ConsensusMode))
	if mode == "" {
		mode = "NORMAL"
	}
	switch mode {
	case "NORMAL":
		if metadataRequired < strictExecSupermajority(metadataActiveReady) {
			return false
		}
	case "DEGRADED", "RECOVERY":
		if metadataActiveReady > 1 && metadataRequired < 2 {
			return false
		}
	default:
		return false
	}
	metadataSigners := syncBlockQuorumEvidenceSigners(block)
	return len(metadataSigners) >= metadataRequired
}

func (n *Node) validateCommittedBlockQuorumEvidence(block Block) error {
	if block.ID == 0 {
		return nil
	}
	// Proposal blocks can carry validator-set metadata before execution votes
	// exist. Enforce this only for finalized result-gossip blocks.
	if len(block.ExecutionResults) == 0 {
		return nil
	}
	required := block.RequiredQuorum
	if required <= 0 {
		return nil
	}
	activeReady := block.ActiveReadyCount
	strict := block.StrictQuorum
	if strict <= 0 && activeReady > 0 {
		strict = strictExecSupermajority(activeReady)
	}
	mode := strings.ToUpper(strings.TrimSpace(block.ConsensusMode))
	if mode == "" {
		mode = "NORMAL"
	}
	switch mode {
	case "NORMAL":
		if strict > 0 && required < strict {
			return fmt.Errorf("quorum_metadata_weak_normal: required=%d strict=%d", required, strict)
		}
	case "DEGRADED", "RECOVERY":
		if strict > 0 && required < strict {
			return fmt.Errorf("quorum_metadata_below_strict: required=%d strict=%d mode=%s", required, strict, mode)
		}
		if activeReady > 1 && required < 2 {
			return fmt.Errorf("quorum_metadata_weak_degraded: required=%d active_ready=%d", required, activeReady)
		}
		if !IsTestnet && strict >= 3 && required < 3 {
			return fmt.Errorf("quorum_metadata_below_mainnet_floor: required=%d strict=%d", required, strict)
		}
	default:
		return fmt.Errorf("quorum_metadata_unknown_mode: %s", mode)
	}
	signers := syncBlockQuorumEvidenceSigners(block)
	if len(signers) < required {
		return fmt.Errorf("quorum_evidence_shortfall: signers=%d required=%d mode=%s", len(signers), required, mode)
	}
	return nil
}

func syncBlockQuorumEvidenceSigners(block Block) map[string]struct{} {
	signers := make(map[string]struct{}, len(block.Signatures)+len(block.ExecutionResults))
	for _, signer := range block.Signatures {
		if id := normalizeValidatorID(signer); id != "" {
			signers[id] = struct{}{}
		}
	}
	for _, result := range block.ExecutionResults {
		if strings.TrimSpace(result.BlockHash) != "" && !strings.EqualFold(strings.TrimSpace(result.BlockHash), strings.TrimSpace(block.BlockHash)) {
			continue
		}
		if id := normalizeValidatorID(result.Signer); id != "" {
			signers[id] = struct{}{}
		}
	}
	return signers
}

func (n *Node) queuedChildMatchesParentNextValidatorSetRootCommitment(block Block, targetRoot string) (string, bool) {
	if n == nil || block.ID == 0 {
		return "", false
	}
	targetRoot = strings.TrimSpace(targetRoot)
	if targetRoot == "" || strings.TrimSpace(block.BlockHash) == "" {
		return "", false
	}
	n.forkMu.RLock()
	children := append([]Block(nil), n.ForkBlocks[block.ID+1]...)
	n.forkMu.RUnlock()
	for _, child := range children {
		if !strings.EqualFold(strings.TrimSpace(child.PrevHash), strings.TrimSpace(block.BlockHash)) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(child.ValidatorSetRoot), targetRoot) {
			return "queued_child_parent_commitment", true
		}
	}
	return "", false
}

func (n *Node) blockNextValidatorSetRootMatchesAuthoritativeCandidates(block Block) (string, bool) {
	if n == nil || block.ID == 0 {
		return "", false
	}
	got := strings.TrimSpace(block.NextValidatorSetRoot)
	if got == "" {
		return "", false
	}
	_, expectedRoot, source, ok := n.authoritativeNextValidatorSetCandidateForBlock(block, block.NextValidatorSetHash)
	if !ok || strings.TrimSpace(expectedRoot) == "" {
		return "", false
	}
	return source, strings.EqualFold(strings.TrimSpace(expectedRoot), got)
}

func (ctx *validatorUpdateExecutionContext) applyPlanOnly(tx Transaction) {
	if ctx == nil || tx.Type != TxValidatorUpdate {
		return
	}
	action, validatorID, ok := parseValidatorUpdateTarget(tx.To)
	if !ok {
		return
	}
	updateHeight := validatorUpdateCommitmentHeight(ctx.height)
	if updateHeight == 0 {
		return
	}
	activeNow := ctx.activeSetContains(validatorID)
	switch action {
	case "add":
		if activeNow {
			if _, ok := ctx.pendingRemovals[normalizeValidatorID(validatorID)]; ok {
				ctx.cancelPendingRemoval(validatorID)
			}
			return
		}
		ctx.queueAdd(validatorID, updateHeight)
	case "remove":
		if !activeNow {
			if _, ok := ctx.pendingAdds[normalizeValidatorID(validatorID)]; ok {
				ctx.cancelPendingAdd(validatorID)
			}
			return
		}
		ctx.queueRemoval(validatorID, updateHeight)
	}
}

func (ctx *validatorUpdateExecutionContext) validateAndApply(tx Transaction, ledger *Ledger) error {
	if ctx == nil {
		return fmt.Errorf("validator updates disabled")
	}
	if err := validatorUpdateEnvelopeBasicError(tx, ledger, ctx.height); err != nil {
		return err
	}
	action, validatorID, ok := parseValidatorUpdateTarget(tx.To)
	if !ok {
		return fmt.Errorf("invalid_validator_update_target")
	}
	cert := tx.ValidatorUpdateCert
	parentHash := strings.ToLower(strings.TrimSpace(cert.ParentRegistryHash))
	if ctx.expectedRegistryHash == "" || parentHash != ctx.expectedRegistryHash {
		return fmt.Errorf("validator_update_parent_registry_hash_mismatch")
	}
	authorityIDs, authorityPubs := validatorUpdateAuthorityFromSnapshot(ctx.registrySnapshot)
	if len(authorityIDs) == 0 || len(authorityPubs) == 0 {
		return fmt.Errorf("validator_update_governance_unavailable")
	}
	required := coreRegistryRequiredSignatures(len(authorityIDs), 0)
	if required == 0 {
		return fmt.Errorf("validator_update_governance_unavailable")
	}
	messagePayload := validatorUpdateCertSigningPayload(
		tx.ChainID,
		action,
		validatorID,
		cert.ParentRegistryHash,
		cert.ProposalNonce,
		cert.ExpiryHeight,
	)
	seenSigners := make(map[string]struct{}, len(cert.Signatures))
	validSigners := make(map[string]struct{}, len(cert.Signatures))
	for _, sigEntry := range cert.Signatures {
		signerID := normalizeValidatorID(sigEntry.SignerID)
		if signerID == "" {
			return fmt.Errorf("invalid validator_update signer")
		}
		if _, ok := seenSigners[signerID]; ok {
			return fmt.Errorf("duplicate validator_update signer")
		}
		seenSigners[signerID] = struct{}{}
		pub, ok := authorityPubs[signerID]
		if !ok || len(pub) != ed25519.PublicKeySize {
			return fmt.Errorf("unauthorized validator_update signer")
		}
		sigRaw, err := hex.DecodeString(sigEntry.SigHex)
		if err != nil || len(sigRaw) != ed25519.SignatureSize {
			return fmt.Errorf("invalid validator_update signature")
		}
		if !ed25519.Verify(pub, messagePayload, sigRaw) {
			return fmt.Errorf("invalid validator_update signature")
		}
		validSigners[signerID] = struct{}{}
	}
	if len(validSigners) < required {
		return fmt.Errorf("insufficient validator_update signatures")
	}
	record, ok := validatorRegistryRecordFromSnapshot(ctx.registrySnapshot, validatorID)
	activeNow := ctx.activeSetContains(validatorID)
	updateHeight := validatorUpdateCommitmentHeight(ctx.height)
	switch action {
	case "add":
		if !ok {
			return fmt.Errorf("validator_update_missing_registry")
		}
		recordCopy := record
		ValidatorStateMachine{}.Update(&recordCopy, ctx.height)
		if isValidatorBanned(validatorID) {
			return fmt.Errorf("validator_update_banned")
		}
		if recordCopy.Status == ValidatorExited {
			return fmt.Errorf("validator_update_exited")
		}
		if recordCopy.JailUntilHeight > 0 && ctx.height < recordCopy.JailUntilHeight {
			return fmt.Errorf("validator_update_jailed")
		}
		if !validatorPassesStakeGate(validatorID, recordCopy.Stake) {
			return fmt.Errorf("validator_update_no_stake")
		}
		if activeNow {
			if _, exists := ctx.pendingRemovals[normalizeValidatorID(validatorID)]; exists {
				ctx.cancelPendingRemoval(validatorID)
				break
			}
			return fmt.Errorf("validator_update_already_active")
		}
		if _, exists := ctx.pendingAdds[normalizeValidatorID(validatorID)]; exists {
			return fmt.Errorf("validator_update_already_pending")
		}
		ctx.queueAdd(validatorID, updateHeight)
	case "remove":
		if _, exists := ctx.pendingAdds[normalizeValidatorID(validatorID)]; exists && !activeNow {
			ctx.cancelPendingAdd(validatorID)
			break
		}
		if !activeNow {
			return fmt.Errorf("validator_update_not_active")
		}
		ctx.queueRemoval(validatorID, updateHeight)
	default:
		return fmt.Errorf("invalid_validator_update_target")
	}
	if ledger != nil {
		markValidatorUpdateCertUsed(ledger, validatorUpdateMessageHash(tx), ctx.height)
	}
	return nil
}

func validatorUpdateCertPayloadForTx(cert *ValidatorUpdateCertificate) string {
	if cert == nil {
		return ""
	}
	clone := *cert
	normalizeValidatorUpdateCert(&clone)
	parts := make([]string, 0, len(clone.Signatures))
	for _, sig := range clone.Signatures {
		parts = append(parts, sig.SignerID+":"+sig.SigHex)
	}
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(clone.ParentRegistryHash)),
		strconv.FormatUint(clone.ProposalNonce, 10),
		strconv.FormatUint(clone.ExpiryHeight, 10),
		strings.Join(parts, ","),
	}, "|")
}

func ExecuteTransactionWithNodeContext(
	n *Node,
	ctx *validatorUpdateExecutionContext,
	ledger *Ledger,
	tx Transaction,
	height int,
) (Ledger, error) {
	nextLedger, err := ExecuteTransaction(ledger, tx, height)
	if err != nil {
		return Ledger{}, err
	}
	if tx.Type != TxValidatorUpdate {
		return nextLedger, nil
	}
	if ctx == nil {
		return Ledger{}, fmt.Errorf("validator updates disabled")
	}
	if err := ctx.validateAndApply(tx, &nextLedger); err != nil {
		return Ledger{}, err
	}
	return nextLedger, nil
}

func executeTransactionForBlock(
	n *Node,
	ctx **validatorUpdateExecutionContext,
	ledger *Ledger,
	tx Transaction,
	height uint64,
) (Ledger, error) {
	if tx.Type != TxValidatorUpdate {
		return ExecuteTransaction(ledger, tx, int(height))
	}
	if n == nil {
		return Ledger{}, fmt.Errorf("validator updates disabled")
	}
	if ctx == nil {
		return Ledger{}, fmt.Errorf("validator updates disabled")
	}
	if *ctx == nil {
		*ctx = n.newValidatorUpdateExecutionContext(height)
	}
	if *ctx == nil {
		return Ledger{}, fmt.Errorf("validator updates disabled")
	}
	return ExecuteTransactionWithNodeContext(n, *ctx, ledger, tx, int(height))
}

func ApplyBlockStateWithNode(n *Node, ledger Ledger, block Block) (Ledger, error) {
	if n == nil {
		return ApplyBlockState(ledger, block)
	}
	nextLedger := ledger.Clone()
	var updateCtx *validatorUpdateExecutionContext
	for _, tx := range block.Transactions {
		applied, err := executeTransactionForBlock(n, &updateCtx, &nextLedger, tx, block.ID)
		if err != nil {
			return ledger, err
		}
		nextLedger = applied
	}
	return nextLedger, nil
}

func VerifyWorkBlockExecutionWithNode(n *Node, block Block, parentLedger Ledger) bool {
	if len(block.Transactions) != len(block.Receipts) {
		return false
	}
	ledger := parentLedger.Clone()
	var updateCtx *validatorUpdateExecutionContext
	for i, tx := range block.Transactions {
		receipt := block.Receipts[i]
		if HashLedger(ledger) != receipt.PreStateHash {
			return false
		}
		newLedger, err := executeTransactionForBlock(n, &updateCtx, &ledger, tx, block.ID)
		if err != nil {
			return false
		}
		ledger = newLedger
		if HashLedger(ledger) != receipt.PostStateHash {
			return false
		}
	}
	return true
}

func (n *Node) applyValidatorUpdateTransactionsFromBlock(block Block) {
	if n == nil || block.ID == 0 || !validatorSetCommitmentV2EnabledAt(block.ID) {
		return
	}
	ctx := n.newValidatorUpdateExecutionContext(block.ID)
	if ctx == nil {
		return
	}
	for _, tx := range block.Transactions {
		if tx.Type != TxValidatorUpdate {
			continue
		}
		if err := ctx.validateAndApply(tx, nil); err != nil {
			if DebugConsensus {
				fmt.Printf("[VALIDATOR-UPDATE] ignored height=%d tx=%s err=%v\n", block.ID, ShortHash(tx.ID), err)
			}
			continue
		}
	}

	n.validatorSetMu.Lock()
	if len(ctx.pendingAdds) == 0 {
		n.pendingValidators = make(map[string]uint64)
	} else {
		n.pendingValidators = copyUint64Map(ctx.pendingAdds)
	}
	if len(ctx.pendingRemovals) == 0 {
		n.pendingValidatorRemovals = make(map[string]uint64)
	} else {
		n.pendingValidatorRemovals = copyUint64Map(ctx.pendingRemovals)
	}
	n.validatorSetMu.Unlock()
}
