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

// `validatorUpdateCertDomain` defines whether the related condition is satisfied.
const validatorUpdateCertDomain = "MSC_VALIDATOR_UPDATE_V1"

type validatorUpdateExecutionContext struct {
	// `height` stores the value associated with this record.
	height uint64
	// `expectedRegistryHash` stores the digest used to identify or verify the related data.
	expectedRegistryHash string
	// `registrySnapshot` stores the value associated with this record.
	registrySnapshot map[string]ValidatorRecord
	// `activeValidators` stores the value associated with this record.
	activeValidators []string
	// governance authority is frozen from the parent committed registry for the
	// whole block. Earlier txs in the same block cannot rewrite their own quorum.
	governanceIDs  []string
	governancePubs map[string]ed25519.PublicKey
	// `pendingAdds` stores the value associated with this record.
	pendingAdds map[string]uint64
	// `pendingRemovals` stores the value associated with this record.
	pendingRemovals map[string]uint64
}

// projectedRegistryHash implements the projected registry hash helper.
func (ctx *validatorUpdateExecutionContext) projectedRegistryHash() string {
	if ctx == nil || len(ctx.registrySnapshot) == 0 {
		return ""
	}
	return strings.TrimSpace(ValidatorRegistrySnapshotHash(ctx.registrySnapshot))
}

// normalizeValidatorUpdateCert normalizes validator update cert.
func normalizeValidatorUpdateCert(cert *ValidatorUpdateCertificate) {
	if cert == nil {
		return
	}
	cert.ParentRegistryHash = strings.ToLower(strings.TrimSpace(cert.ParentRegistryHash))
	// `normalized` stores the value produced by this operation.
	normalized := make([]ValidatorUpdateCertSignature, 0, len(cert.Signatures))
	// `sig` tracks the current values while iterating.
	for _, sig := range cert.Signatures {
		// `signerID` stores the value produced by this operation.
		signerID := normalizeValidatorID(sig.SignerID)
		// `sigHex` stores the value produced by this operation.
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

// validatorUpdateCertShapeError implements the validator update cert shape error helper.
func validatorUpdateCertShapeError(cert *ValidatorUpdateCertificate) error {
	if cert == nil {
		return fmt.Errorf("missing validator_update_cert")
	}
	// `hash` stores the digest used to identify or verify the related data.
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
	// `sig` tracks the current values while iterating.
	for _, sig := range cert.Signatures {
		if normalizeValidatorID(sig.SignerID) == "" {
			return fmt.Errorf("invalid validator_update_signer_id")
		}
		// `raw` stores the value produced by this operation.
		if raw := strings.TrimSpace(sig.SigHex); len(raw) != ed25519.SignatureSize*2 {
			return fmt.Errorf("invalid validator_update_signature")
		}
	}
	return nil
}

// validatorUpdateCertSigningPayload implements the validator update cert signing payload helper.
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

// validatorUpdateCertMessageHash implements the validator update cert message hash helper.
func validatorUpdateCertMessageHash(chainID, action, validatorID, parentRegistryHash string, proposalNonce, expiryHeight uint64) string {
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(validatorUpdateCertSigningPayload(chainID, action, validatorID, parentRegistryHash, proposalNonce, expiryHeight))
	return hex.EncodeToString(sum[:])
}

// validatorUpdateMessageHash implements the validator update message hash helper.
func validatorUpdateMessageHash(tx Transaction) string {
	if tx.ValidatorUpdateCert == nil {
		return ""
	}
	// `action`, `validatorID`, and `ok` store whether the related condition is satisfied.
	action, validatorID, ok := parseValidatorUpdateTarget(tx.To)
	if !ok {
		return ""
	}
	// `cert` stores the value produced by this operation.
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

// ensureValidatorUpdateCertLedgerState implements the ensure validator update cert ledger state helper.
func ensureValidatorUpdateCertLedgerState(ledger *Ledger) {
	if ledger == nil {
		return
	}
	if ledger.UsedValidatorUpdateCerts == nil {
		ledger.UsedValidatorUpdateCerts = make(map[string]uint64)
	}
}

// validatorUpdateCertUsed implements the validator update cert used helper.
func validatorUpdateCertUsed(ledger Ledger, messageHash string) bool {
	if messageHash == "" || len(ledger.UsedValidatorUpdateCerts) == 0 {
		return false
	}
	// `ok` stores whether the related condition is satisfied.
	_, ok := ledger.UsedValidatorUpdateCerts[messageHash]
	return ok
}

// markValidatorUpdateCertUsed implements the mark validator update cert used helper.
func markValidatorUpdateCertUsed(ledger *Ledger, messageHash string, height uint64) {
	if ledger == nil || messageHash == "" {
		return
	}
	ensureValidatorUpdateCertLedgerState(ledger)
	ledger.UsedValidatorUpdateCerts[messageHash] = height
}

// validatorUpdateCommitmentHeight implements the validator update commitment height helper.
func validatorUpdateCommitmentHeight(inclusionHeight uint64) uint64 {
	if inclusionHeight == 0 {
		return 0
	}
	if validatorSetCommitmentV2EnabledAt(inclusionHeight) {
		return inclusionHeight
	}
	// `delay` stores the value produced by this operation.
	delay := validatorSetActivationDelayBlocks()
	if delay == 0 {
		delay = 1
	}
	if delay == 1 {
		return inclusionHeight
	}
	// `maxU64` stores the value produced by this operation.
	maxU64 := ^uint64(0)
	// `offset` stores the value produced by this operation.
	offset := delay - 1
	if inclusionHeight > maxU64-offset {
		return maxU64
	}
	return inclusionHeight + offset
}

// validatorUpdateEnvelopeBasicError implements the validator update envelope basic error helper.
func validatorUpdateEnvelopeBasicError(tx Transaction, ledger *Ledger, height uint64) error {
	if tx.Amount != 0 {
		return fmt.Errorf("validator_update_amount_must_be_zero")
	}
	// `action`, `validatorID`, and `ok` store whether the related condition is satisfied.
	action, validatorID, ok := parseValidatorUpdateTarget(tx.To)
	if !ok || validatorID == "" ||
		(action != "add" && action != "activate" && action != "suspend" && action != "remove") {
		return fmt.Errorf("invalid_validator_update_target")
	}
	// `cert` stores the value produced by this operation.
	cert := tx.ValidatorUpdateCert
	// `err` stores the error produced by this operation.
	if err := validatorUpdateCertShapeError(cert); err != nil {
		return err
	}
	if cert.ExpiryHeight < height {
		return fmt.Errorf("validator_update_cert_expired")
	}
	// `messageHash` stores the digest used to identify or verify the related data.
	messageHash := validatorUpdateMessageHash(tx)
	if messageHash == "" {
		return fmt.Errorf("invalid_validator_update_message_hash")
	}
	if ledger != nil && validatorUpdateCertUsed(*ledger, messageHash) {
		return fmt.Errorf("validator_update_cert_replayed")
	}
	return nil
}

// validatorUpdateAuthorityFromSnapshot implements the validator update authority from snapshot helper.
func validatorUpdateAuthorityFromSnapshot(snapshot map[string]ValidatorRecord, activeValidators []string) ([]string, map[string]ed25519.PublicKey) {
	if len(snapshot) == 0 {
		return nil, nil
	}
	activeSet := make(map[string]struct{}, len(activeValidators))
	for _, id := range canonicalValidatorIDs(activeValidators) {
		activeSet[id] = struct{}{}
	}
	explicitIDs := make([]string, 0, len(snapshot))
	fallbackIDs := make([]string, 0, len(snapshot))
	explicitPubs := make(map[string]ed25519.PublicKey, len(snapshot))
	fallbackPubs := make(map[string]ed25519.PublicKey, len(snapshot))
	// `key` and `rec` track the key used to access the related value.
	for key, rec := range snapshot {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(rec.ID)
		if id == "" {
			id = normalizeValidatorID(key)
		}
		if id == "" || rec.Status != ValidatorActive {
			continue
		}
		if len(activeSet) > 0 {
			if _, ok := activeSet[id]; !ok {
				continue
			}
		}
		if validatorStateIsRemoved(rec.Status) {
			continue
		}
		// `pubHex` stores the value produced by this operation.
		pubHex := strings.TrimSpace(rec.ConsensusPubKey)
		if pubHex == "" {
			continue
		}
		// `pubRaw` and `err` store the error produced by this operation.
		pubRaw, err := hex.DecodeString(pubHex)
		if err != nil || len(pubRaw) != ed25519.PublicKeySize {
			continue
		}
		pub := ed25519.PublicKey(append([]byte(nil), pubRaw...))
		fallbackIDs = append(fallbackIDs, id)
		fallbackPubs[id] = pub
		if rec.GovernanceSigner {
			explicitIDs = append(explicitIDs, id)
			explicitPubs[id] = pub
		}
	}
	explicitIDs = canonicalValidatorIDs(explicitIDs)
	if len(explicitIDs) > 0 {
		return explicitIDs, explicitPubs
	}
	// Legacy/fresh registries may not carry explicit governance flags. In that
	// case the committed ACTIVE committee is the deterministic approval body.
	// Candidates outside the committed active set never gain authority.
	fallbackIDs = canonicalValidatorIDs(fallbackIDs)
	if len(fallbackIDs) == 0 {
		return nil, nil
	}
	return fallbackIDs, fallbackPubs
}

// copyUint64Map copies uint64 map.
func copyUint64Map(src map[string]uint64) map[string]uint64 {
	if len(src) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[string]uint64, len(src))
	// `key` and `value` track the key used to access the related value.
	for key, value := range src {
		// `norm` stores the value produced by this operation.
		norm := normalizeValidatorID(key)
		if norm == "" {
			continue
		}
		// `existing` and `ok` store whether the related condition is satisfied.
		if existing, ok := out[norm]; !ok || value < existing {
			out[norm] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// validatorUpdateSnapshotPendingState implements the validator update snapshot pending state helper.
func (n *Node) validatorUpdateSnapshotPendingState(height uint64) (map[string]uint64, map[string]uint64) {
	if n == nil || height == 0 {
		return nil, nil
	}
	// `parentHeight` stores the value produced by this operation.
	parentHeight := height - 1
	if parentHeight > 0 && n.DB != nil && n.DB.State != nil {
		// `snap` and `err` store the error produced by this operation.
		if snap, err := n.GetSnapshot(parentHeight); err == nil && snap != nil {
			return copyUint64Map(snap.PendingValidators), copyUint64Map(snap.PendingValidatorRemovals)
		}
	}
	n.validatorSetMu.RLock()
	defer n.validatorSetMu.RUnlock()
	return copyUint64Map(n.pendingValidators), copyUint64Map(n.pendingValidatorRemovals)
}

// validatorUpdateActiveSetForHeight implements the validator update active set for height helper.
func (n *Node) validatorUpdateActiveSetForHeight(height uint64) []string {
	if n == nil || height == 0 {
		return nil
	}
	// Validator-update add/remove validation must use the chain-authoritative
	// consensus set. A registry projection can include already-active records
	// that are not yet in the committed/frozen committee, and treating those as
	// active makes reconciliation adds impossible.
	if current := n.consensusValidatorsForHeight(height); len(current) > 0 {
		return current
	}
	// `current` stores the value produced by this operation.
	if current := n.plannedValidatorSetForHeightFromChain(height); len(current) > 0 {
		return current
	}
	if height == 1 && len(n.GenesisValidators) > 0 {
		return canonicalValidatorIDs(append([]string{}, n.GenesisValidators...))
	}
	return nil
}

// newValidatorUpdateExecutionContext implements the new validator update execution context helper.
func (n *Node) newValidatorUpdateExecutionContext(height uint64) *validatorUpdateExecutionContext {
	if n == nil || height == 0 || !validatorSetCommitmentV2EnabledAt(height) {
		return nil
	}
	// `registryHash` and `source` store the digest used to identify or verify the related data.
	registryHash, source := n.expectedValidatorRegistryHashWithSource(height)
	// `registrySnapshot` stores the value produced by this operation.
	registrySnapshot := n.validatorRegistrySnapshotForHeight(height)
	// `chainHeight` stores the value produced by this operation.
	chainHeight := uint64(0)
	if n.Blockchain != nil {
		chainHeight = n.Blockchain.Height()
	}
	// Bootstrap/early-height validator-update plans must validate against the
	// full runtime registry snapshot. Filtering the registry down to the active
	// committee breaks the governance cert parent hash and causes the proposer
	// to silently drop validator-update transactions from freshly built blocks.
	if chainHeight <= 1 {
		// `runtimeRegistry` stores the value produced by this operation.
		if runtimeRegistry := copyValidatorRegistrySnapshot(GlobalValidatorRegistry.Snapshot()); len(runtimeRegistry) > 0 {
			registrySnapshot = runtimeRegistry
			// `runtimeHash` stores the digest used to identify or verify the related data.
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
	registrySnapshot = n.validatorUpdateRegistrySnapshotWithLedgerCandidates(height, registrySnapshot)
	if len(registrySnapshot) == 0 {
		return nil
	}
	// `active` stores the value produced by this operation.
	active := n.validatorUpdateActiveSetForHeight(height)
	if len(active) == 0 {
		return nil
	}
	// `pendingAdds` and `pendingRemovals` store the value produced by this operation.
	pendingAdds, pendingRemovals := n.validatorUpdateSnapshotPendingState(height)
	active = canonicalValidatorIDs(active)
	governanceIDs, governancePubs := validatorUpdateAuthorityFromSnapshot(registrySnapshot, active)
	return &validatorUpdateExecutionContext{
		height:               height,
		expectedRegistryHash: strings.ToLower(strings.TrimSpace(registryHash)),
		registrySnapshot:     registrySnapshot,
		activeValidators:     active,
		governanceIDs:        governanceIDs,
		governancePubs:       governancePubs,
		pendingAdds:          pendingAdds,
		pendingRemovals:      pendingRemovals,
	}
}

// validatorUpdateRegistrySnapshotWithLedgerCandidates implements the validator update registry snapshot with ledger candidates helper.
func (n *Node) validatorUpdateRegistrySnapshotWithLedgerCandidates(height uint64, snapshot map[string]ValidatorRecord) map[string]ValidatorRecord {
	if n == nil || height == 0 || len(n.Ledger.Stakes) == 0 {
		return snapshot
	}
	// `out` stores the result produced by this operation.
	out := copyValidatorRegistrySnapshot(snapshot)
	// `stakeTotals` stores the value produced by this operation.
	stakeTotals := make(map[string]int64)
	// `consensusPubKeys` stores the value produced by this operation.
	consensusPubKeys := make(map[string]string)
	// `key` and `lock` track the synchronization state protecting shared data.
	for key, lock := range n.Ledger.Stakes {
		if lock.Amount <= 0 {
			continue
		}
		// `parts` stores the value produced by this operation.
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(lock.ValidatorID)
		if id == "" {
			id = normalizeValidatorID(parts[1])
		}
		if id == "" || id != normalizeValidatorID(parts[1]) {
			continue
		}
		stakeTotals[id] += int64(lock.Amount)
		// `pubKey` stores the key used to access the related value.
		if pubKey := normalizeConsensusPubKeyHex(lock.ConsensusPubKey); pubKey != "" {
			consensusPubKeys[id] = pubKey
		}
	}
	if len(stakeTotals) == 0 {
		return snapshot
	}
	if out == nil {
		out = make(map[string]ValidatorRecord, len(stakeTotals))
	}
	// `id` and `stake` track the current position in the related collection.
	for id, stake := range stakeTotals {
		if !validatorPassesStakeGate(id, stake) {
			continue
		}
		// `rec` and `exists` store whether the related condition is satisfied.
		rec, exists := validatorRegistryRecordFromSnapshot(out, id)
		if !exists {
			rec = ValidatorRecord{
				ID:              id,
				ConsensusPubKey: consensusPubKeys[id],
				Stake:           stake,
				Reputation:      protocolValidatorReputationInitialValue(),
				Status:          ValidatorPending,
				JoinHeight:      height,
			}
		} else {
			rec.ID = id
			if rec.Stake < stake {
				rec.Stake = stake
			}
			if normalizeConsensusPubKeyHex(rec.ConsensusPubKey) == "" {
				rec.ConsensusPubKey = consensusPubKeys[id]
			}
			if strings.TrimSpace(string(rec.Status)) == "" {
				rec.Status = ValidatorPending
			}
			if rec.JoinHeight == 0 {
				rec.JoinHeight = height
			}
		}
		// Ledger-discovered candidates must not expand governance authority for
		// the certificate that is authorizing their own admission.
		if !exists {
			rec.GovernanceSigner = false
		}
		out[id] = rec
	}
	return out
}

// activeSetContains implements the active set contains helper.
func (ctx *validatorUpdateExecutionContext) activeSetContains(id string) bool {
	id = normalizeValidatorID(id)
	if id == "" {
		return false
	}
	// `existing` tracks the current values while iterating.
	for _, existing := range ctx.activeValidators {
		if normalizeValidatorID(existing) == id {
			return true
		}
	}
	return false
}

// queueAdd implements the queue add helper.
func (ctx *validatorUpdateExecutionContext) queueAdd(id string, updateHeight uint64) {
	id = normalizeValidatorID(id)
	if id == "" {
		return
	}
	if ctx.pendingAdds == nil {
		ctx.pendingAdds = make(map[string]uint64)
	}
	// `existing` and `ok` store whether the related condition is satisfied.
	if existing, ok := ctx.pendingAdds[id]; !ok || updateHeight < existing {
		ctx.pendingAdds[id] = updateHeight
	}
	if ctx.pendingRemovals != nil {
		delete(ctx.pendingRemovals, id)
	}
	ctx.activateRegistryRecord(id, updateHeight)
}

func (ctx *validatorUpdateExecutionContext) queueSuspension(id string, updateHeight uint64) {
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
	ctx.suspendRegistryRecord(id, updateHeight)
}

// queueRemoval implements the queue removal helper.
func (ctx *validatorUpdateExecutionContext) queueRemoval(id string, updateHeight uint64) {
	id = normalizeValidatorID(id)
	if id == "" {
		return
	}
	if ctx.pendingRemovals == nil {
		ctx.pendingRemovals = make(map[string]uint64)
	}
	// `existing` and `ok` store whether the related condition is satisfied.
	if existing, ok := ctx.pendingRemovals[id]; !ok || updateHeight < existing {
		ctx.pendingRemovals[id] = updateHeight
	}
	if ctx.pendingAdds != nil {
		delete(ctx.pendingAdds, id)
	}
	ctx.removeRegistryRecord(id, updateHeight)
}

// cancelPendingAdd implements the cancel pending add helper.
func (ctx *validatorUpdateExecutionContext) cancelPendingAdd(id string) {
	id = normalizeValidatorID(id)
	if id == "" || ctx.pendingAdds == nil {
		return
	}
	delete(ctx.pendingAdds, id)
}

// cancelPendingRemoval implements the cancel pending removal helper.
func (ctx *validatorUpdateExecutionContext) cancelPendingRemoval(id string) {
	id = normalizeValidatorID(id)
	if id == "" || ctx.pendingRemovals == nil {
		return
	}
	delete(ctx.pendingRemovals, id)
}

// activateRegistryRecord implements the activate registry record helper.
func (ctx *validatorUpdateExecutionContext) activateRegistryRecord(id string, height uint64) {
	if ctx == nil {
		return
	}
	id = normalizeValidatorID(id)
	if id == "" {
		return
	}
	if ctx.registrySnapshot == nil {
		ctx.registrySnapshot = make(map[string]ValidatorRecord)
	}
	// `rec` and `ok` store whether the related condition is satisfied.
	rec, ok := validatorRegistryRecordFromSnapshot(ctx.registrySnapshot, id)
	if !ok {
		rec = ValidatorRecord{ID: id, Reputation: protocolValidatorReputationInitialValue()}
	}
	rec.ID = id
	if rec.Reputation == 0 {
		rec.Reputation = protocolValidatorReputationInitialValue()
	}
	if rec.JoinHeight == 0 || (height > 0 && rec.JoinHeight > height) {
		rec.JoinHeight = height
	}
	rec.Status = ValidatorActive
	rec.VotingPower = 1
	rec.SuspensionRecommended = false
	rec.SuspensionRecommendedHeight = 0
	ctx.registrySnapshot[id] = rec
}

func (ctx *validatorUpdateExecutionContext) suspendRegistryRecord(id string, height uint64) {
	if ctx == nil || len(ctx.registrySnapshot) == 0 {
		return
	}
	id = normalizeValidatorID(id)
	if id == "" {
		return
	}
	rec, ok := validatorRegistryRecordFromSnapshot(ctx.registrySnapshot, id)
	if !ok {
		return
	}
	rec.ID = id
	rec.Status = ValidatorInactive
	rec.VotingPower = 0
	rec.SuspensionRecommended = false
	rec.SuspensionRecommendedHeight = height
	ctx.registrySnapshot[id] = rec
}

// removeRegistryRecord implements the terminal governance removal transition.
func (ctx *validatorUpdateExecutionContext) removeRegistryRecord(id string, height uint64) {
	if ctx == nil || len(ctx.registrySnapshot) == 0 {
		return
	}
	id = normalizeValidatorID(id)
	if id == "" {
		return
	}
	// `rec` and `ok` store whether the related condition is satisfied.
	rec, ok := validatorRegistryRecordFromSnapshot(ctx.registrySnapshot, id)
	if !ok {
		return
	}
	rec.ID = id
	rec.Status = ValidatorRemoved
	rec.VotingPower = 0
	rec.JailUntilHeight = 0
	rec.SuspensionRecommended = false
	rec.SuspensionRecommendedHeight = height
	ctx.registrySnapshot[id] = rec
}

func (ctx *validatorUpdateExecutionContext) activeLimitReachedForAdd() bool {
	if ctx == nil {
		return true
	}
	if validatorEpochSetV1EnabledAt(validatorEpochTransitionHeight(ctx.height)) {
		// Registration remains open when the active set is full. The candidate is
		// kept in the registry as standby and can enter only at an epoch boundary.
		return false
	}
	maxActive := protocolValidatorMaxActiveCommitteeValue()
	if maxActive <= 0 {
		return false
	}
	return len(ctx.plannedValidatorsForHeight(ctx.height+1)) >= maxActive
}

// plannedValidatorsForHeight implements the planned validators for height helper.
func (ctx *validatorUpdateExecutionContext) plannedValidatorsForHeight(height uint64) []string {
	if ctx == nil || height == 0 {
		return nil
	}
	// `current` stores the value produced by this operation.
	current := canonicalValidatorIDs(append([]string{}, ctx.activeValidators...))
	if len(current) == 0 {
		return nil
	}
	if validatorEpochSetV1EnabledAt(height) {
		if !isValidatorEpochBoundary(height) {
			return current
		}
		return selectEpochActiveValidatorSet(height, current, ctx.registrySnapshot)
	}
	// `targetFinalized` stores the value produced by this operation.
	targetFinalized := height - 1
	// `set` stores the value produced by this operation.
	set := make(map[string]struct{}, len(current))
	// `id` tracks the current position in the related collection.
	for _, id := range current {
		set[id] = struct{}{}
	}
	// `id` and `act` track the current position in the related collection.
	for id, act := range ctx.pendingAdds {
		if act == 0 || act > targetFinalized {
			continue
		}
		set[normalizeValidatorID(id)] = struct{}{}
	}
	// `id` and `act` track the current position in the related collection.
	for id, act := range ctx.pendingRemovals {
		if act == 0 || act > targetFinalized {
			continue
		}
		delete(set, normalizeValidatorID(id))
	}
	// `next` stores the value produced by this operation.
	next := make([]string, 0, len(set))
	// `id` tracks the current position in the related collection.
	for id := range set {
		if id == "" {
			continue
		}
		next = append(next, id)
	}
	return canonicalValidatorIDs(next)
}

// hasVisibleTransitionForHeight implements the has visible transition for height helper.
func (ctx *validatorUpdateExecutionContext) hasVisibleTransitionForHeight(height uint64) bool {
	if ctx == nil || height == 0 {
		return false
	}
	if validatorEpochSetV1EnabledAt(height) && !isValidatorEpochBoundary(height) {
		return false
	}
	// `targetFinalized` stores the value produced by this operation.
	targetFinalized := height - 1
	if targetFinalized == 0 {
		return false
	}
	// `act` tracks the current values while iterating.
	for _, act := range ctx.pendingAdds {
		if validatorSetTransitionVisibleInChildSetAt(act, height) {
			return true
		}
	}
	// `act` tracks the current values while iterating.
	for _, act := range ctx.pendingRemovals {
		if validatorSetTransitionVisibleInChildSetAt(act, height) {
			return true
		}
	}
	return false
}

// plannedNextCommitment implements the planned next commitment helper.
func (ctx *validatorUpdateExecutionContext) plannedNextCommitment(height uint64) (string, string, string) {
	if ctx == nil || height == 0 {
		return "", "", "none"
	}
	// `nextHeight` stores the value produced by this operation.
	nextHeight := height + 1
	// `validators` stores whether the related condition is satisfied.
	validators := ctx.plannedValidatorsForHeight(nextHeight)
	if len(validators) == 0 {
		return "", "", "none"
	}
	// `hash` stores the digest used to identify or verify the related data.
	hash := strings.TrimSpace(validatorSetHashFromSnapshotForHeight(nextHeight, validators, ctx.registrySnapshot))
	// `root` stores the digest used to identify or verify the related data.
	root := strings.TrimSpace(ValidatorSetMerkleRoot(nextHeight, validators, ctx.registrySnapshot))
	if hash == "" {
		return "", "", "none"
	}
	return hash, root, "block_tx_plan"
}

// blockValidatorUpdatePlanContext implements the block validator update plan context helper.
func (n *Node) blockValidatorUpdatePlanContext(block Block) *validatorUpdateExecutionContext {
	if n == nil || block.ID == 0 || !validatorSetCommitmentV2EnabledAt(block.ID) {
		return nil
	}
	// `ctx` stores the context controlling this operation.
	ctx := n.newValidatorUpdateExecutionContext(block.ID)
	if ctx == nil {
		return nil
	}
	// `tx` tracks the transaction data handled by this operation.
	for _, tx := range block.Transactions {
		ctx.applyPlanOnly(tx)
	}
	return ctx
}

// blockHasValidatorUpdateTx implements the block has validator update tx helper.
func blockHasValidatorUpdateTx(block Block) bool {
	// `tx` tracks the transaction data handled by this operation.
	for _, tx := range block.Transactions {
		if tx.Type == TxValidatorUpdate {
			return true
		}
	}
	return false
}

// carryForwardNextValidatorSetCommitmentForBlock implements the carry forward next validator set commitment for block helper.
func (n *Node) carryForwardNextValidatorSetCommitmentForBlock(block Block) (string, string, string) {
	if n == nil || block.ID == 0 {
		return "", "", "none"
	}
	// `activeHash` stores the digest used to identify or verify the related data.
	activeHash := strings.TrimSpace(block.ValidatorSetHash)
	if activeHash == "" {
		return "", "", "none"
	}
	// Runtime liveness queues are local observations and can differ between
	// honest nodes. Without an explicit on-chain validator update, the current
	// committed set/root must carry forward unchanged.
	if !blockHasValidatorUpdateTx(block) {
		return activeHash, strings.TrimSpace(block.ValidatorSetRoot), "carry_forward"
	}
	// `active` stores the value produced by this operation.
	active := n.freezeValidatorSetForHeight(block.ID, n.consensusValidatorsForHeight(block.ID))
	if len(active) == 0 {
		// `committed` and `ok` store whether the related condition is satisfied.
		if committed, ok := n.blockValidatorSetFromSignatures(block); ok {
			active = committed
		}
	}
	if len(active) == 0 {
		return activeHash, strings.TrimSpace(block.ValidatorSetRoot), "carry_forward"
	}
	// `matched` and `ok` store whether the related condition is satisfied.
	if matched, ok := n.validatorSetCandidateMatchesTarget(block.ID, activeHash, active, nil); ok {
		active = matched
	} else {
		return activeHash, strings.TrimSpace(block.ValidatorSetRoot), "carry_forward"
	}
	// `nextHeight` stores the value produced by this operation.
	nextHeight := block.ID + 1
	// `registry` stores the value produced by this operation.
	registry := n.validatorRegistrySnapshotForHeight(nextHeight)
	if len(registry) == 0 {
		registry = n.validatorRegistrySnapshotForHeight(block.ID)
	}
	// `root` stores the digest used to identify or verify the related data.
	root := ""
	if len(registry) > 0 {
		root = strings.TrimSpace(ValidatorSetMerkleRoot(nextHeight, active, registry))
	}
	if root == "" && strings.EqualFold(strings.TrimSpace(block.NextValidatorSetHash), activeHash) {
		root = strings.TrimSpace(block.NextValidatorSetRoot)
	}
	if root == "" {
		root = strings.TrimSpace(block.ValidatorSetRoot)
	}
	return activeHash, root, "carry_forward"
}

// projectedValidatorUpdateRegistrySnapshotForBlock implements the projected validator update registry snapshot for block helper.
func (n *Node) projectedValidatorUpdateRegistrySnapshotForBlock(block Block) (map[string]ValidatorRecord, string, bool) {
	if n == nil || block.ID == 0 || !validatorSetCommitmentV2EnabledAt(block.ID) {
		return nil, "", false
	}
	// `ctx` stores the context controlling this operation.
	ctx := n.newValidatorUpdateExecutionContext(block.ID)
	if ctx == nil {
		return nil, "", false
	}
	// `hasUpdate` stores the value produced by this operation.
	hasUpdate := false
	// `tx` tracks the transaction data handled by this operation.
	for _, tx := range block.Transactions {
		if tx.Type != TxValidatorUpdate {
			continue
		}
		hasUpdate = true
		// `err` stores the error produced by this operation.
		if err := ctx.validateAndApply(tx, nil); err != nil {
			return nil, "", false
		}
	}
	if !hasUpdate {
		return nil, "", false
	}
	// `hash` stores the digest used to identify or verify the related data.
	hash := ctx.projectedRegistryHash()
	if hash == "" {
		return nil, "", false
	}
	return copyValidatorRegistrySnapshot(ctx.registrySnapshot), hash, true
}

// expectedNextValidatorSetCommitmentForBlock implements the expected next validator set commitment for block helper.
func (n *Node) expectedNextValidatorSetCommitmentForBlock(block Block) (string, string, string) {
	if n == nil || block.ID == 0 {
		return "", "", "none"
	}
	// `hasUpdate` stores the value produced by this operation.
	hasUpdate := blockHasValidatorUpdateTx(block)
	if hasUpdate {
		// `ctx` stores the context controlling this operation.
		if ctx := n.blockValidatorUpdatePlanContext(block); ctx != nil {
			// `hash`, `root`, and `source` store the digest used to identify or verify the related data.
			if hash, root, source := ctx.plannedNextCommitment(block.ID); hash != "" {
				return hash, root, source
			}
		}
	}
	if !hasUpdate {
		if validators, registrySnapshot, ok := n.epochBoundaryValidatorSetForBlock(block); ok {
			hash := strings.TrimSpace(validatorSetHashFromSnapshotForHeight(block.ID+1, validators, registrySnapshot))
			root := strings.TrimSpace(ValidatorSetMerkleRoot(block.ID+1, validators, registrySnapshot))
			if hash == "" {
				hash = strings.TrimSpace(ValidatorSetHash(validators))
			}
			if hash != "" {
				return hash, root, "epoch_boundary"
			}
		}
	}
	if !hasUpdate {
		// Committed pending validator transitions are deterministic block-level
		// authority. Prefer them before the no-TX carry-forward fallback, or a
		// node can ignore an already-finalized add/remove plan and propose a
		// stale next-set commitment.
		nextHash, source := n.deterministicNextValidatorSetHashWithSource(block.ID, block.ValidatorSetHash)
		if strings.TrimSpace(nextHash) != "" && !strings.EqualFold(strings.TrimSpace(source), "carry_forward") {
			nextRoot, _ := n.expectedNextValidatorSetRootWithSource(block.ID, block.ValidatorSetHash, block.ValidatorSetRoot)
			if strings.EqualFold(strings.TrimSpace(source), "chain_planned_transition") {
				source = "block_tx_plan"
			}
			return strings.TrimSpace(nextHash), strings.TrimSpace(nextRoot), source
		}
	}
	// `hash`, `root`, and `source` store the digest used to identify or verify the related data.
	if hash, root, source := n.carryForwardNextValidatorSetCommitmentForBlock(block); hash != "" {
		return hash, root, source
	}
	// `nextHash` and `source` store the digest used to identify or verify the related data.
	nextHash, source := n.deterministicNextValidatorSetHashWithSource(block.ID, block.ValidatorSetHash)
	// `nextRoot` stores the digest used to identify or verify the related data.
	nextRoot, _ := n.expectedNextValidatorSetRootWithSource(block.ID, block.ValidatorSetHash, block.ValidatorSetRoot)
	return strings.TrimSpace(nextHash), strings.TrimSpace(nextRoot), source
}

// authoritativeNextValidatorSetCandidateForBlock implements the authoritative next validator set candidate for block helper.
func (n *Node) authoritativeNextValidatorSetCandidateForBlock(block Block, targetHash string) ([]string, string, string, bool) {
	if n == nil || block.ID == 0 {
		return nil, "", "none", false
	}
	targetHash = strings.TrimSpace(targetHash)
	if targetHash == "" {
		return nil, "", "none", false
	}
	// `nextHeight` stores the value produced by this operation.
	nextHeight := block.ID + 1
	// `registrySnapshot` stores the value produced by this operation.
	registrySnapshot := map[string]ValidatorRecord(nil)
	// `candidates` stores the value produced by this operation.
	candidates := make([][]string, 0, 6)
	// `tryCandidate` stores the value produced by this operation.
	tryCandidate := func(values []string, source string) ([]string, string, string, bool) {
		if len(values) == 0 {
			return nil, "", "none", false
		}
		// `matched` and `ok` store whether the related condition is satisfied.
		matched, ok := n.validatorSetCandidateMatchesTarget(nextHeight, targetHash, values, registrySnapshot)
		if !ok {
			return nil, "", "none", false
		}
		// `root` stores the digest used to identify or verify the related data.
		root := ""
		if len(registrySnapshot) > 0 {
			root = strings.TrimSpace(ValidatorSetMerkleRoot(nextHeight, matched, registrySnapshot))
		}
		return matched, root, source, true
	}
	// `ctx` stores the context controlling this operation.
	if ctx := n.blockValidatorUpdatePlanContext(block); ctx != nil {
		if len(ctx.registrySnapshot) > 0 {
			registrySnapshot = copyValidatorRegistrySnapshot(ctx.registrySnapshot)
		}
		// `planned` stores the value produced by this operation.
		if planned := ctx.plannedValidatorsForHeight(nextHeight); len(planned) > 0 {
			candidates = append(candidates, planned)
			// `matched`, `root`, `source`, and `ok` store whether the related condition is satisfied.
			if matched, root, source, ok := tryCandidate(planned, "block_execution_plan"); ok {
				return matched, root, source, true
			}
		}
	}
	if validators, snapshot, ok := n.epochBoundaryValidatorSetForBlock(block); ok {
		if len(snapshot) > 0 {
			registrySnapshot = copyValidatorRegistrySnapshot(snapshot)
		}
		candidates = append(candidates, validators)
		if matched, root, source, ok := tryCandidate(validators, "epoch_boundary"); ok {
			return matched, root, source, true
		}
	}
	if len(registrySnapshot) == 0 {
		registrySnapshot = n.validatorRegistrySnapshotForHeight(nextHeight)
	}
	// `planned` stores the value produced by this operation.
	if planned := n.plannedValidatorSetForHeightFromChain(nextHeight); len(planned) > 0 {
		candidates = append(candidates, planned)
		// `matched`, `root`, `source`, and `ok` store whether the related condition is satisfied.
		if matched, root, source, ok := tryCandidate(planned, "chain_planned_transition"); ok {
			return matched, root, source, true
		}
	}
	// `committed` and `ok` store whether the related condition is satisfied.
	if committed, ok := n.blockValidatorSetFromSignatures(block); ok && len(committed) > 0 {
		candidates = append(candidates, committed)
		// `matched`, `root`, `source`, and `ok` store whether the related condition is satisfied.
		if matched, root, source, ok := tryCandidate(committed, "current_block_signatures"); ok {
			return matched, root, source, true
		}
	}
	// `frozen` stores the value produced by this operation.
	if frozen := n.frozenValidatorsForHeight(nextHeight); len(frozen) > 0 {
		candidates = append(candidates, frozen)
		// `matched`, `root`, `source`, and `ok` store whether the related condition is satisfied.
		if matched, root, source, ok := tryCandidate(frozen, "frozen_next_height"); ok {
			return matched, root, source, true
		}
	}
	// `frozen` stores the value produced by this operation.
	if frozen := n.frozenValidatorsForHeight(block.ID); len(frozen) > 0 {
		candidates = append(candidates, frozen)
		// `matched`, `root`, `source`, and `ok` store whether the related condition is satisfied.
		if matched, root, source, ok := tryCandidate(frozen, "frozen_current_height"); ok {
			return matched, root, source, true
		}
	}
	if len(registrySnapshot) == 0 {
		// `snap` and `ok` store whether the related condition is satisfied.
		if snap, _, ok := n.committedParentProjectionSnapshot(nextHeight); ok && snap != nil {
			registrySnapshot = validatorRegistrySnapshotFromStateSnapshot(snap)
		}
	}
	// `reconstructed` and `ok` store whether the related condition is satisfied.
	if reconstructed, ok := n.reconstructValidatorSetCandidateForTarget(nextHeight, targetHash, registrySnapshot, candidates...); ok {
		// `root` stores the digest used to identify or verify the related data.
		root := ""
		// `source` stores the value produced by this operation.
		source := "next_commitment_subset_reconstructed"
		if len(registrySnapshot) > 0 {
			root = strings.TrimSpace(ValidatorSetMerkleRoot(nextHeight, reconstructed, registrySnapshot))
			source = "registry_verified_next_commitment"
		}
		return reconstructed, root, source, true
	}
	return nil, "", "none", false
}

// blockNextValidatorSetHashMatchesAuthoritativeCandidates implements the block next validator set hash matches authoritative candidates helper.
func (n *Node) blockNextValidatorSetHashMatchesAuthoritativeCandidates(block Block, targetHash string) (string, bool) {
	if n == nil || block.ID == 0 {
		return "", false
	}
	targetHash = strings.TrimSpace(targetHash)
	if targetHash == "" {
		return "", false
	}
	// `source` and `ok` store whether the related condition is satisfied.
	_, _, source, ok := n.authoritativeNextValidatorSetCandidateForBlock(block, targetHash)
	return source, ok
}

// queuedChildMatchesParentNextValidatorSetCommitment implements the queued child matches parent next validator set commitment helper.
func (n *Node) queuedChildMatchesParentNextValidatorSetCommitment(block Block, targetHash string) (string, bool) {
	if n == nil || block.ID == 0 {
		return "", false
	}
	targetHash = strings.TrimSpace(targetHash)
	if targetHash == "" || strings.TrimSpace(block.BlockHash) == "" {
		return "", false
	}
	n.forkMu.RLock()
	// `children` stores the value produced by this operation.
	children := append([]Block(nil), n.ForkBlocks[block.ID+1]...)
	n.forkMu.RUnlock()
	// `child` tracks the current values while iterating.
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

// queuedChildExtendsBlockDuringSync implements the queued child extends block during sync helper.
func (n *Node) queuedChildExtendsBlockDuringSync(block Block) (string, bool) {
	if n == nil || block.ID == 0 || strings.TrimSpace(block.BlockHash) == "" {
		return "", false
	}
	// `stage` stores the value produced by this operation.
	stage, _ := n.syncDiagnosticContext()
	if strings.TrimSpace(stage) == "" {
		return "", false
	}
	n.forkMu.RLock()
	// `children` stores the value produced by this operation.
	children := append([]Block(nil), n.ForkBlocks[block.ID+1]...)
	n.forkMu.RUnlock()
	// `child` tracks the current values while iterating.
	for _, child := range children {
		if strings.EqualFold(strings.TrimSpace(child.PrevHash), strings.TrimSpace(block.BlockHash)) {
			return "queued_child_chain_continuity", true
		}
	}
	return "", false
}

// syncContinuityValidatorFallback implements the sync continuity validator fallback helper.
func (n *Node) syncContinuityValidatorFallback(block Block) []string {
	if n == nil || n.Blockchain == nil || block.ID == 0 {
		return nil
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := n.queuedChildExtendsBlockDuringSync(block); !ok {
		if block.ID != n.Blockchain.Height()+1 {
			return nil
		}
		// `stage` stores the value produced by this operation.
		stage, _ := n.syncDiagnosticContext()
		if strings.TrimSpace(stage) == "" {
			return nil
		}
		// `last` stores the value produced by this operation.
		last := n.Blockchain.LastBlock()
		if strings.TrimSpace(last.BlockHash) == "" || !strings.EqualFold(strings.TrimSpace(block.PrevHash), strings.TrimSpace(last.BlockHash)) {
			return nil
		}
	}

	// Continuity fallback may relax local timing/leader checks during sync, but
	// it must not synthesize validator authority from local witness subsets.
	// Resolve only from committed snapshots, frozen sets, or the registry view
	// that matches the block's committed validator-set hash.
	targetHash := strings.TrimSpace(block.ValidatorSetHash)
	if resolved, _, _, ok := n.validatorSetCandidateFromStoredSnapshot(block.ID, targetHash); ok && len(resolved) > 0 {
		return canonicalValidatorIDs(resolved)
	}

	// `preferredHeights` stores the value produced by this operation.
	preferredHeights := []uint64{block.ID}
	if block.ID > 1 {
		preferredHeights = append(preferredHeights, block.ID-1)
	}
	if targetHash != "" {
		if frozen, ok := n.committedFrozenValidatorSetCandidate(targetHash, preferredHeights...); ok && len(frozen) > 0 {
			return canonicalValidatorIDs(frozen)
		}
	}

	// `registrySnapshot` stores the value produced by this operation.
	registrySnapshot := n.validatorRegistrySnapshotForHeight(block.ID)
	// `candidates` stores the value produced by this operation.
	candidates := [][]string{
		selectAllStakedValidatorsFromSnapshot(block.ID, registrySnapshot),
		protocolRewardValidatorsFromRegistrySnapshot(block.ID, registrySnapshot),
	}
	if targetHash == "" {
		// Legacy direct-tip sync without set-hash can still use a deterministic
		// committed registry view, but never local witness material.
		// `candidate` tracks the current values while iterating.
		for _, candidate := range candidates {
			if canonical := canonicalValidatorIDs(candidate); len(canonical) > 0 {
				return canonical
			}
		}
		return nil
	}
	// `candidate` tracks the current values while iterating.
	for _, candidate := range candidates {
		// `matched` and `ok` store whether the related condition is satisfied.
		if matched, ok := n.validatorSetCandidateMatchesTarget(block.ID, targetHash, candidate, registrySnapshot); ok {
			return canonicalValidatorIDs(matched)
		}
	}
	// `reconstructed` and `ok` store whether the related condition is satisfied.
	if reconstructed, ok := n.reconstructValidatorSetCandidateForTarget(block.ID, targetHash, registrySnapshot, candidates...); ok {
		return canonicalValidatorIDs(reconstructed)
	}
	if frozen := n.frozenValidatorsForCommittedHash(targetHash, preferredHeights...); len(frozen) > 0 {
		return canonicalValidatorIDs(frozen)
	}
	return nil
}

// syncExecutionResultQuorumFallback implements the sync execution result quorum fallback helper.
func (n *Node) syncExecutionResultQuorumFallback(block Block, validators []string) bool {
	if n == nil || n.Blockchain == nil || block.ID == 0 || len(block.ExecutionResults) == 0 {
		return false
	}
	// `stage` stores the value produced by this operation.
	stage, _ := n.syncDiagnosticContext()
	if strings.TrimSpace(stage) == "" {
		return false
	}
	if block.ID != n.Blockchain.Height()+1 {
		return false
	}
	// `last` stores the value produced by this operation.
	last := n.Blockchain.LastBlock()
	if strings.TrimSpace(last.BlockHash) == "" || !strings.EqualFold(strings.TrimSpace(block.PrevHash), strings.TrimSpace(last.BlockHash)) {
		return false
	}

	// `validatorSet` stores whether the related condition is satisfied.
	validatorSet := make(map[string]struct{}, len(validators))
	// `id` tracks the current position in the related collection.
	for _, id := range canonicalValidatorIDs(validators) {
		validatorSet[id] = struct{}{}
	}
	// `signers` stores the value produced by this operation.
	signers := make(map[string]struct{}, len(block.ExecutionResults))
	// `result` tracks the result produced by this operation.
	for _, result := range block.ExecutionResults {
		if strings.TrimSpace(result.BlockHash) != "" && !strings.EqualFold(strings.TrimSpace(result.BlockHash), strings.TrimSpace(block.BlockHash)) {
			continue
		}
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(result.Signer)
		if id == "" {
			continue
		}
		if len(validatorSet) > 0 {
			// `ok` stores whether the related condition is satisfied.
			if _, ok := validatorSet[id]; !ok {
				continue
			}
		}
		signers[id] = struct{}{}
	}
	if len(signers) > 0 {
		// `total` stores the measured quantity used by this operation.
		total := len(validatorSet)
		if total == 0 {
			total = len(signers)
		}
		// `required` stores the request data being processed.
		required := execQuorumRequired(total)
		if required <= 0 {
			required = 1
		}
		if len(signers) >= required {
			return true
		}
	}

	// `metadataRequired` stores the value produced by this operation.
	metadataRequired := block.RequiredQuorum
	if metadataRequired <= 0 {
		return false
	}
	// `metadataActiveReady` stores the value produced by this operation.
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
	// `mode` stores the value produced by this operation.
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
	// `metadataSigners` stores the value produced by this operation.
	metadataSigners := syncBlockQuorumEvidenceSigners(block)
	return len(metadataSigners) >= metadataRequired
}

// validateCommittedBlockQuorumEvidence validates committed block quorum evidence.
func (n *Node) validateCommittedBlockQuorumEvidence(block Block) error {
	if block.ID == 0 {
		return nil
	}
	// Proposal blocks can carry validator-set metadata before execution votes
	// exist. Enforce this only for finalized result-gossip blocks.
	if len(block.ExecutionResults) == 0 {
		return nil
	}
	// `required` stores the request data being processed.
	required := block.RequiredQuorum
	if required <= 0 {
		return nil
	}
	// `activeReady` stores the value produced by this operation.
	activeReady := block.ActiveReadyCount
	// `strict` stores the value produced by this operation.
	strict := block.StrictQuorum
	if strict <= 0 && activeReady > 0 {
		strict = strictExecSupermajority(activeReady)
	}
	// `mode` stores the value produced by this operation.
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
		if protocolRequiresStrictConsensusSignatures() && strict >= 3 && required < 3 {
			return fmt.Errorf("quorum_metadata_below_mainnet_floor: required=%d strict=%d", required, strict)
		}
	default:
		return fmt.Errorf("quorum_metadata_unknown_mode: %s", mode)
	}
	// `signers` stores the value produced by this operation.
	signers := syncBlockQuorumEvidenceSigners(block)
	if len(signers) < required {
		return fmt.Errorf("quorum_evidence_shortfall: signers=%d required=%d mode=%s", len(signers), required, mode)
	}
	return nil
}

// syncBlockQuorumEvidenceSigners implements the sync block quorum evidence signers helper.
func syncBlockQuorumEvidenceSigners(block Block) map[string]struct{} {
	// `signers` stores the value produced by this operation.
	signers := make(map[string]struct{}, len(block.Signatures)+len(block.ExecutionResults))
	// `signer` tracks the current values while iterating.
	for _, signer := range block.Signatures {
		// `id` stores the current position in the related collection.
		if id := normalizeValidatorID(signer); id != "" {
			signers[id] = struct{}{}
		}
	}
	// `result` tracks the result produced by this operation.
	for _, result := range block.ExecutionResults {
		if strings.TrimSpace(result.BlockHash) != "" && !strings.EqualFold(strings.TrimSpace(result.BlockHash), strings.TrimSpace(block.BlockHash)) {
			continue
		}
		// `id` stores the current position in the related collection.
		if id := normalizeValidatorID(result.Signer); id != "" {
			signers[id] = struct{}{}
		}
	}
	return signers
}

// queuedChildMatchesParentNextValidatorSetRootCommitment implements the queued child matches parent next validator set root commitment helper.
func (n *Node) queuedChildMatchesParentNextValidatorSetRootCommitment(block Block, targetRoot string) (string, bool) {
	if n == nil || block.ID == 0 {
		return "", false
	}
	targetRoot = strings.TrimSpace(targetRoot)
	if targetRoot == "" || strings.TrimSpace(block.BlockHash) == "" {
		return "", false
	}
	n.forkMu.RLock()
	// `children` stores the value produced by this operation.
	children := append([]Block(nil), n.ForkBlocks[block.ID+1]...)
	n.forkMu.RUnlock()
	// `child` tracks the current values while iterating.
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

// blockNextValidatorSetRootMatchesAuthoritativeCandidates implements the block next validator set root matches authoritative candidates helper.
func (n *Node) blockNextValidatorSetRootMatchesAuthoritativeCandidates(block Block) (string, bool) {
	if n == nil || block.ID == 0 {
		return "", false
	}
	// `got` stores the value produced by this operation.
	got := strings.TrimSpace(block.NextValidatorSetRoot)
	if got == "" {
		return "", false
	}
	// `expectedRoot`, `source`, and `ok` store whether the related condition is satisfied.
	_, expectedRoot, source, ok := n.authoritativeNextValidatorSetCandidateForBlock(block, block.NextValidatorSetHash)
	if !ok || strings.TrimSpace(expectedRoot) == "" {
		return "", false
	}
	return source, strings.EqualFold(strings.TrimSpace(expectedRoot), got)
}

// applyPlanOnly applies plan only.
func (ctx *validatorUpdateExecutionContext) applyPlanOnly(tx Transaction) {
	if ctx == nil || tx.Type != TxValidatorUpdate {
		return
	}
	// `action`, `validatorID`, and `ok` store whether the related condition is satisfied.
	action, validatorID, ok := parseValidatorUpdateTarget(tx.To)
	if !ok {
		return
	}
	// `updateHeight` stores the value produced by this operation.
	updateHeight := validatorUpdateCommitmentHeight(ctx.height)
	if updateHeight == 0 {
		return
	}
	// `activeNow` stores the value produced by this operation.
	activeNow := ctx.activeSetContains(validatorID)
	switch action {
	case "add", "activate":
		if activeNow {
			// `ok` stores whether the related condition is satisfied.
			if _, ok := ctx.pendingRemovals[normalizeValidatorID(validatorID)]; ok {
				ctx.cancelPendingRemoval(validatorID)
				ctx.activateRegistryRecord(validatorID, updateHeight)
			}
			return
		}
		if rec, exists := validatorRegistryRecordFromSnapshot(ctx.registrySnapshot, validatorID); exists && validatorStateIsRemoved(rec.Status) {
			return
		}
		if ctx.activeLimitReachedForAdd() {
			return
		}
		ctx.queueAdd(validatorID, updateHeight)
	case "suspend":
		if !activeNow {
			return
		}
		ctx.queueSuspension(validatorID, updateHeight)
	case "remove":
		if !activeNow {
			// `ok` stores whether the related condition is satisfied.
			if _, ok := ctx.pendingAdds[normalizeValidatorID(validatorID)]; ok {
				ctx.cancelPendingAdd(validatorID)
			}
			ctx.removeRegistryRecord(validatorID, updateHeight)
			return
		}
		ctx.queueRemoval(validatorID, updateHeight)
	}
}

// validateAndApply validates and apply.
func (ctx *validatorUpdateExecutionContext) validateAndApply(tx Transaction, ledger *Ledger) error {
	if ctx == nil {
		return fmt.Errorf("validator updates disabled")
	}
	// `err` stores the error produced by this operation.
	if err := validatorUpdateEnvelopeBasicError(tx, ledger, ctx.height); err != nil {
		return err
	}
	// `action`, `validatorID`, and `ok` store whether the related condition is satisfied.
	action, validatorID, ok := parseValidatorUpdateTarget(tx.To)
	if !ok {
		return fmt.Errorf("invalid_validator_update_target")
	}
	// `cert` stores the value produced by this operation.
	cert := tx.ValidatorUpdateCert
	// `parentHash` stores the digest used to identify or verify the related data.
	parentHash := strings.ToLower(strings.TrimSpace(cert.ParentRegistryHash))
	if ctx.expectedRegistryHash == "" || parentHash != ctx.expectedRegistryHash {
		return fmt.Errorf("validator_update_parent_registry_hash_mismatch")
	}
	// `authorityIDs` and `authorityPubs` store the value produced by this operation.
	authorityIDs, authorityPubs := ctx.governanceIDs, ctx.governancePubs
	if len(authorityIDs) == 0 || len(authorityPubs) == 0 {
		return fmt.Errorf("validator_update_governance_unavailable")
	}
	// `required` stores the request data being processed.
	required := coreRegistryRequiredSignatures(len(authorityIDs), 0)
	if required == 0 {
		return fmt.Errorf("validator_update_governance_unavailable")
	}
	// `messagePayload` stores the value produced by this operation.
	messagePayload := validatorUpdateCertSigningPayload(
		tx.ChainID,
		action,
		validatorID,
		cert.ParentRegistryHash,
		cert.ProposalNonce,
		cert.ExpiryHeight,
	)
	// `seenSigners` stores the value produced by this operation.
	seenSigners := make(map[string]struct{}, len(cert.Signatures))
	// `validSigners` stores whether the related condition is satisfied.
	validSigners := make(map[string]struct{}, len(cert.Signatures))
	// `sigEntry` tracks the current values while iterating.
	for _, sigEntry := range cert.Signatures {
		// `signerID` stores the value produced by this operation.
		signerID := normalizeValidatorID(sigEntry.SignerID)
		if signerID == "" {
			return fmt.Errorf("invalid validator_update signer")
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seenSigners[signerID]; ok {
			return fmt.Errorf("duplicate validator_update signer")
		}
		seenSigners[signerID] = struct{}{}
		// `pub` and `ok` store whether the related condition is satisfied.
		pub, ok := authorityPubs[signerID]
		if !ok || len(pub) != ed25519.PublicKeySize {
			return fmt.Errorf("unauthorized validator_update signer")
		}
		// `sigRaw` and `err` store the error produced by this operation.
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
	// `record` and `ok` store whether the related condition is satisfied.
	record, ok := validatorRegistryRecordFromSnapshot(ctx.registrySnapshot, validatorID)
	// `activeNow` stores the value produced by this operation.
	activeNow := ctx.activeSetContains(validatorID)
	// `updateHeight` stores the value produced by this operation.
	updateHeight := validatorUpdateCommitmentHeight(ctx.height)
	switch action {
	case "add", "activate":
		if !ok {
			return fmt.Errorf("validator_update_missing_registry")
		}
		if activeNow {
			// `exists` stores whether the related condition is satisfied.
			if _, exists := ctx.pendingRemovals[normalizeValidatorID(validatorID)]; exists {
				ctx.cancelPendingRemoval(validatorID)
				ctx.activateRegistryRecord(validatorID, updateHeight)
				break
			}
			return fmt.Errorf("validator_update_already_active")
		}
		// `recordCopy` stores the value produced by this operation.
		recordCopy := record
		ValidatorStateMachine{}.Update(&recordCopy, ctx.height)
		if isProtocolValidatorBanned(validatorID) {
			return fmt.Errorf("validator_update_banned")
		}
		if validatorStateIsRemoved(recordCopy.Status) {
			return fmt.Errorf("validator_update_removed")
		}
		if recordCopy.JailUntilHeight > 0 && ctx.height < recordCopy.JailUntilHeight {
			return fmt.Errorf("validator_update_jailed")
		}
		if !validatorPassesStakeGate(validatorID, recordCopy.Stake) {
			return fmt.Errorf("validator_update_no_stake")
		}
		// `exists` stores whether the related condition is satisfied.
		if _, exists := ctx.pendingAdds[normalizeValidatorID(validatorID)]; exists {
			return fmt.Errorf("validator_update_already_pending")
		}
		if ctx.activeLimitReachedForAdd() {
			return fmt.Errorf("validator_update_active_limit_reached: max=%d", protocolValidatorMaxActiveCommitteeValue())
		}
		ctx.queueAdd(validatorID, updateHeight)
	case "suspend":
		if !ok {
			return fmt.Errorf("validator_update_missing_registry")
		}
		if !activeNow {
			return fmt.Errorf("validator_update_not_active")
		}
		if _, exists := ctx.pendingRemovals[normalizeValidatorID(validatorID)]; exists {
			return fmt.Errorf("validator_update_already_pending")
		}
		ctx.queueSuspension(validatorID, updateHeight)
	case "remove":
		// `exists` stores whether the related condition is satisfied.
		if _, exists := ctx.pendingAdds[normalizeValidatorID(validatorID)]; exists && !activeNow {
			ctx.cancelPendingAdd(validatorID)
			ctx.removeRegistryRecord(validatorID, updateHeight)
			break
		}
		if !ok {
			return fmt.Errorf("validator_update_missing_registry")
		}
		if validatorStateIsRemoved(record.Status) {
			return fmt.Errorf("validator_update_already_removed")
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

// validatorUpdateCertPayloadForTx implements the validator update cert payload for tx helper.
func validatorUpdateCertPayloadForTx(cert *ValidatorUpdateCertificate) string {
	if cert == nil {
		return ""
	}
	// `clone` stores the value produced by this operation.
	clone := *cert
	normalizeValidatorUpdateCert(&clone)
	// `parts` stores the value produced by this operation.
	parts := make([]string, 0, len(clone.Signatures))
	// `sig` tracks the current values while iterating.
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

// ExecuteTransactionWithNodeContext executes a transaction with validator-update state and committed registry context.
func ExecuteTransactionWithNodeContext(
	n *Node,
	ctx *validatorUpdateExecutionContext,
	ledger *Ledger,
	tx Transaction,
	height int,
) (Ledger, error) {
	authority := BlockExecutionAuthority{
		ValidatorUpdates: ctx,
	}
	if ctx != nil {
		authority.ValidatorRegistry = ctx.registrySnapshot
	} else if tx.Type == TxStake && n != nil && height > 0 {
		authority.ValidatorRegistry = n.validatorRegistrySnapshotForHeight(uint64(height))
	}
	return executeTransactionWithAuthority(&authority, ledger, tx, Block{
		ID:              uint64(height),
		ProtocolVersion: blockProtocolVersionV1,
	})
}

// ApplyBlockStateWithNode replays a block against a node-aware committed validator registry snapshot.
func ApplyBlockStateWithNode(n *Node, ledger Ledger, block Block) (Ledger, error) {
	if n == nil {
		return ApplyBlockState(ledger, block)
	}
	result, err := (DeterministicBlockExecutionEngine{}).ExecuteBlock(BlockExecutionInput{
		Context:   newExecutionStateContext(ledger),
		Block:     block,
		Authority: n.prepareBlockExecutionAuthority(block),
	})
	if err != nil {
		return ledger, err
	}
	return result.NextLedger, nil
}

// VerifyWorkBlockExecutionWithNode re-executes receipts with node-aware validator registry context.
func VerifyWorkBlockExecutionWithNode(n *Node, block Block, parentLedger Ledger) bool {
	authority := BlockExecutionAuthority{}
	if n != nil {
		authority = n.prepareBlockExecutionAuthority(block)
	}
	result, err := (DeterministicBlockExecutionEngine{}).ExecuteBlock(BlockExecutionInput{
		Context:   newExecutionStateContext(parentLedger),
		Block:     block,
		Authority: authority,
	})
	if err != nil {
		return false
	}
	return executionReceiptsEqual(result.Receipts, block.Receipts, block.ProtocolVersion)
}

// transactionHasDTLEnvelope implements the transaction has dtl envelope helper.
func transactionHasDTLEnvelope(tx Transaction) bool {
	return tx.Type == TxDTL ||
		strings.TrimSpace(tx.DTLTxType) != "" ||
		strings.TrimSpace(tx.DTLTokenID) != "" ||
		strings.TrimSpace(tx.DTLPayload) != "" ||
		strings.TrimSpace(tx.DTLGovernanceCert) != ""
}

// verifyDTLReceiptMetadataForExecutedTx verifies dtl receipt metadata for executed tx.
func verifyDTLReceiptMetadataForExecutedTx(tx Transaction, receipt StateReceipt, postLedger Ledger, preDTLLogCount int, txIndex int, blockHeight uint64) bool {
	if !transactionHasDTLEnvelope(tx) {
		return true
	}
	if strings.TrimSpace(receipt.TxHash) != strings.TrimSpace(tx.ID) {
		return false
	}
	// `meta` and `ok` store whether the related condition is satisfied.
	meta, ok := deriveDTLReceiptMetadata(tx, &postLedger)
	if !ok {
		return false
	}
	// `expectedLogs` stores the value produced by this operation.
	expectedLogs := buildReceiptDTLLogs(postLedger, preDTLLogCount, tx.ID, txIndex, blockHeight)
	return strings.TrimSpace(receipt.DTLTxType) == strings.TrimSpace(meta.DTLTxType) &&
		strings.TrimSpace(receipt.ContractID) == strings.TrimSpace(meta.ContractID) &&
		strings.TrimSpace(receipt.RuntimeMode) == strings.TrimSpace(meta.RuntimeMode) &&
		strings.TrimSpace(receipt.ContractStandard) == strings.TrimSpace(meta.ContractStandard) &&
		dtlContractInterfacesEqual(receipt.ContractInterfaces, meta.ContractInterfaces) &&
		strings.TrimSpace(receipt.ABIHash) == strings.TrimSpace(meta.ABIHash) &&
		receipt.Upgradeable == meta.Upgradeable &&
		strings.TrimSpace(receipt.ProxyTarget) == strings.TrimSpace(meta.ProxyTarget) &&
		strings.TrimSpace(receipt.OracleFeedID) == strings.TrimSpace(meta.OracleFeedID) &&
		receipt.HealthFactor == meta.HealthFactor &&
		receipt.RouteHops == meta.RouteHops &&
		strings.TrimSpace(receipt.RouteTokenIn) == strings.TrimSpace(meta.RouteTokenIn) &&
		strings.TrimSpace(receipt.RouteTokenOut) == strings.TrimSpace(meta.RouteTokenOut) &&
		strings.TrimSpace(receipt.BytecodeFormat) == strings.TrimSpace(meta.BytecodeFormat) &&
		strings.TrimSpace(receipt.BytecodeHash) == strings.TrimSpace(meta.BytecodeHash) &&
		receipt.BytecodeSize == meta.BytecodeSize &&
		strings.TrimSpace(receipt.Compiler) == strings.TrimSpace(meta.Compiler) &&
		strings.TrimSpace(receipt.SourceHash) == strings.TrimSpace(meta.SourceHash) &&
		dtlEventLogsEqual(receipt.Logs, expectedLogs)
}

// dtlStringSlicesEqual implements the dtl string slices equal helper.
func dtlStringSlicesEqual(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	// `i` tracks the current position in the related collection.
	for i := range left {
		if strings.TrimSpace(left[i]) != strings.TrimSpace(right[i]) {
			return false
		}
	}
	return true
}

// dtlContractInterfacesEqual compares DTL contract interface metadata as a
// canonical set while preserving order-sensitive comparison for other slices.
func dtlContractInterfacesEqual(left []string, right []string) bool {
	// `canonicalLeft` and `canonicalRight` store the value produced by this operation.
	canonicalLeft := canonicalDTLInterfaces(left)
	canonicalRight := canonicalDTLInterfaces(right)
	return dtlStringSlicesEqual(canonicalLeft, canonicalRight)
}

// dtlEventLogsEqual implements the dtl event logs equal helper.
func dtlEventLogsEqual(left []DTLEventLog, right []DTLEventLog) bool {
	if len(left) != len(right) {
		return false
	}
	// `i` tracks the current position in the related collection.
	for i := range left {
		// `l` stores the value produced by this operation.
		l := left[i]
		// `r` stores the value produced by this operation.
		r := right[i]
		if normalizeDTLContractID(l.ContractID) != normalizeDTLContractID(r.ContractID) ||
			canonicalDTLRawJSONStringForHash(l.Data) != canonicalDTLRawJSONStringForHash(r.Data) ||
			l.BlockHeight != r.BlockHeight ||
			!strings.EqualFold(strings.TrimSpace(l.TxID), strings.TrimSpace(r.TxID)) ||
			l.TxIndex != r.TxIndex ||
			l.LogIndex != r.LogIndex ||
			!dtlStringSlicesEqual(l.Topics, r.Topics) {
			return false
		}
	}
	return true
}

// applyValidatorUpdateTransactionsFromBlock applies validator update transactions from block.
func (n *Node) applyValidatorUpdateTransactionsFromBlock(block Block) {
	if n == nil || block.ID == 0 || !validatorSetCommitmentV2EnabledAt(block.ID) {
		return
	}
	// `ctx` stores the context controlling this operation.
	ctx := n.newValidatorUpdateExecutionContext(block.ID)
	if ctx == nil {
		return
	}
	// `tx` tracks the transaction data handled by this operation.
	for _, tx := range block.Transactions {
		if tx.Type != TxValidatorUpdate {
			continue
		}
		// `err` stores the error produced by this operation.
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
