package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

const (
	// `coreRegistryStatusPending` defines the constant value used by this package.
	coreRegistryStatusPending = "pending"
	// `coreRegistryStatusActive` defines the constant value used by this package.
	coreRegistryStatusActive = "active"
	// `coreRegistryStatusRetired` defines the constant value used by this package.
	coreRegistryStatusRetired = "retired"
	// `coreRegistryLastValidFile` defines the constant value used by this package.
	coreRegistryLastValidFile = "core_registry.last_valid.json"
)

// `runtimeCoreValidatorSet` stores the value used by this operation.
var runtimeCoreValidatorSet = struct {
	// `mu` stores the synchronization state protecting shared data.
	mu sync.RWMutex
	// `ids` stores the current position in the related collection.
	ids map[string]struct{}
}{
	ids: make(map[string]struct{}),
}

// `coreEnvPasswordPolicy` stores the value used by this operation.
var coreEnvPasswordPolicy = struct {
	// `mu` stores the synchronization state protecting shared data.
	mu sync.RWMutex
	// `blocked` stores the block data handled by this operation.
	blocked map[string]bool
}{
	blocked: make(map[string]bool),
}

// `validatorSecretRuntimePolicy` stores whether the related condition is satisfied.
var validatorSecretRuntimePolicy = struct {
	// `mu` stores the synchronization state protecting shared data.
	mu sync.RWMutex
	// `mode` stores the value associated with this record.
	mode map[string]string
	// `source` stores the value associated with this record.
	source map[string]string
}{
	mode:   make(map[string]string),
	source: make(map[string]string),
}

type coreRegistryPayload struct {
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id"`
	// `Version` stores the value associated with this record.
	Version uint64 `json:"version"`
	// `Epoch` stores the value associated with this record.
	Epoch uint64 `json:"epoch"`
	// `EffectiveHeight` stores the value associated with this record.
	EffectiveHeight uint64 `json:"effective_height"`
	// `PreviousRegistryHash` stores the digest used to identify or verify the related data.
	PreviousRegistryHash string `json:"previous_registry_hash"`
	// `Validators` stores whether the related condition is satisfied.
	Validators []CoreRegistryEntry `json:"validators"`
}

// normalizeCoreRegistryEnforcementMode normalizes core registry enforcement mode.
func normalizeCoreRegistryEnforcementMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "warn":
		return "warn"
	case "enforce":
		return "enforce"
	default:
		return "warn"
	}
}

// normalizeCoreRegistryStatus normalizes core registry status.
func normalizeCoreRegistryStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case coreRegistryStatusPending:
		return coreRegistryStatusPending
	case coreRegistryStatusActive:
		return coreRegistryStatusActive
	case coreRegistryStatusRetired:
		return coreRegistryStatusRetired
	default:
		return coreRegistryStatusPending
	}
}

// coreRegistryReloadInterval implements the core registry reload interval helper.
func coreRegistryReloadInterval() time.Duration {
	// `seconds` stores the value produced by this operation.
	seconds := CoreRegistryReloadSeconds
	if seconds == 0 {
		seconds = 10
	}
	if seconds > 3600 {
		seconds = 3600
	}
	return time.Duration(seconds) * time.Second
}

// coreRegistryConfigPath implements the core registry config path helper.
func coreRegistryConfigPath() string {
	// `path` stores the value produced by this operation.
	path := strings.TrimSpace(CoreRegistryPath)
	if path == "" {
		return "core_validators.json"
	}
	return path
}

// coreRegistryLastValidPath implements the core registry last valid path helper.
func coreRegistryLastValidPath(nodePath string) string {
	// `base` stores the value produced by this operation.
	base := strings.TrimSpace(nodePath)
	if base == "" {
		return coreRegistryLastValidFile
	}
	return filepath.Join(base, coreRegistryLastValidFile)
}

// coreRegistryActivationEligibleHeight implements the core registry activation eligible height helper.
func coreRegistryActivationEligibleHeight(effectiveHeight uint64) uint64 {
	if effectiveHeight == 0 {
		return 0
	}
	// `buffer` stores the value produced by this operation.
	buffer := ConsensusCoreActivationEffectiveHeightBuffer
	if buffer == 0 {
		return effectiveHeight
	}
	// `maxU64` stores the value produced by this operation.
	maxU64 := ^uint64(0)
	if effectiveHeight > maxU64-buffer {
		return maxU64
	}
	return effectiveHeight + buffer
}

// setRuntimeCoreValidatorIDs implements the set runtime core validator i ds helper.
func setRuntimeCoreValidatorIDs(ids []string) {
	// `norm` stores the value produced by this operation.
	norm := canonicalValidatorIDs(ids)
	runtimeCoreValidatorSet.mu.Lock()
	defer runtimeCoreValidatorSet.mu.Unlock()
	runtimeCoreValidatorSet.ids = make(map[string]struct{}, len(norm))
	// `id` tracks the current position in the related collection.
	for _, id := range norm {
		runtimeCoreValidatorSet.ids[id] = struct{}{}
	}
}

// runtimeCoreValidatorContains implements the runtime core validator contains helper.
func runtimeCoreValidatorContains(id string) bool {
	id = normalizeValidatorID(id)
	if id == "" {
		return false
	}
	runtimeCoreValidatorSet.mu.RLock()
	// `ok` stores whether the related condition is satisfied.
	_, ok := runtimeCoreValidatorSet.ids[id]
	runtimeCoreValidatorSet.mu.RUnlock()
	return ok
}

// runtimeCoreValidatorIDs implements the runtime core validator i ds helper.
func runtimeCoreValidatorIDs() []string {
	runtimeCoreValidatorSet.mu.RLock()
	defer runtimeCoreValidatorSet.mu.RUnlock()
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(runtimeCoreValidatorSet.ids))
	// `id` tracks the current position in the related collection.
	for id := range runtimeCoreValidatorSet.ids {
		out = append(out, id)
	}
	return canonicalValidatorIDs(out)
}

// coreAuthorityHasChainHistory implements the core authority has chain history helper.
func (n *Node) coreAuthorityHasChainHistory() bool {
	if n == nil {
		return false
	}
	// `finalized` stores the value produced by this operation.
	if finalized := n.getFinalizedHeight(); finalized > 0 {
		return true
	}
	if n.Blockchain == nil {
		return false
	}
	if n.Blockchain.FinalizedHeight() > 0 {
		return true
	}
	return n.Blockchain.Height() > 0
}

// coreBootstrapAuthorityAllowed implements the core bootstrap authority allowed helper.
func (n *Node) coreBootstrapAuthorityAllowed() bool {
	return !n.coreAuthorityHasChainHistory()
}

// coreAuthoritySource implements the core authority source helper.
func (n *Node) coreAuthoritySource() string {
	if n == nil {
		if len(canonicalValidatorIDs(ConfigAuthCoreValidators)) > 0 {
			return "bootstrap_core"
		}
		return "none"
	}
	if n.coreBootstrapAuthorityAllowed() {
		if len(canonicalValidatorIDs(ConfigAuthCoreValidators)) > 0 || len(runtimeCoreValidatorIDs()) > 0 {
			return "bootstrap_core"
		}
		return "none"
	}
	return "on_chain_validator_state"
}

// isCoreValidatorCurrent implements the is core validator current helper.
func (n *Node) isCoreValidatorCurrent(nodeID string) bool {
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return false
	}
	if n != nil {
		if runtimeCoreValidatorContains(id) && n.coreBootstrapAuthorityAllowed() {
			return true
		}
		if !n.coreBootstrapAuthorityAllowed() {
			return false
		}
		// `height` stores the value produced by this operation.
		height := uint64(1)
		if n.Blockchain != nil {
			height = n.Blockchain.Height() + 1
		}
		// `active` and `pending` store the value produced by this operation.
		active, pending := n.coreSetsForHeight(height)
		if containsValidatorID(active, id) || containsValidatorID(pending, id) {
			return true
		}
		// Legacy/warn mode may keep bootstrap authority entries active even when
		// chain history already exists.
		if n.coreRegistryHasEntryID(id) {
			return true
		}
		if n.coreBootstrapAuthorityAllowed() && containsValidatorID(ConfigAuthCoreValidators, id) {
			return true
		}
		return false
	}
	return containsValidatorID(ConfigAuthCoreValidators, id)
}

// coreMembershipStatus implements the core membership status helper.
func (n *Node) coreMembershipStatus(nodeID string) string {
	if n.isCoreValidatorCurrent(nodeID) {
		return "core"
	}
	return "non_core"
}

// requiresWalletAuthCurrent implements the requires wallet auth current helper.
func (n *Node) requiresWalletAuthCurrent(nodeID string) bool {
	if !ConfigAuthRequireWallet {
		return false
	}
	if genesisWalletAuthExemptValidator(nodeID) {
		return false
	}
	return !n.isCoreValidatorCurrent(nodeID)
}

// coreRegistryVerificationAuthorityIDs implements the core registry verification authority i ds helper.
func (n *Node) coreRegistryVerificationAuthorityIDs() []string {
	if n == nil {
		return canonicalValidatorIDs(ConfigAuthCoreValidators)
	}
	n.coreRegistryMu.RLock()
	// `active` stores the value produced by this operation.
	active := append([]string{}, n.coreRegistryState.ActiveCoreSet...)
	// `verified` stores the value produced by this operation.
	verified := n.coreRegistryState.Verified
	n.coreRegistryMu.RUnlock()
	if verified && len(active) > 0 {
		return canonicalValidatorIDs(active)
	}
	return canonicalValidatorIDs(ConfigAuthCoreValidators)
}

// setCoreEnvPasswordBlocked implements the set core env password blocked helper.
func setCoreEnvPasswordBlocked(nodeID string, blocked bool) {
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return
	}
	coreEnvPasswordPolicy.mu.Lock()
	coreEnvPasswordPolicy.blocked[id] = blocked
	coreEnvPasswordPolicy.mu.Unlock()
}

// coreEnvPasswordBlocked implements the core env password blocked helper.
func coreEnvPasswordBlocked(nodeID string) bool {
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return false
	}
	coreEnvPasswordPolicy.mu.RLock()
	// `blocked` stores the block data handled by this operation.
	blocked := coreEnvPasswordPolicy.blocked[id]
	coreEnvPasswordPolicy.mu.RUnlock()
	return blocked
}

// normalizeValidatorSecretSource normalizes validator secret source.
func normalizeValidatorSecretSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "env":
		return "env"
	case "file":
		return "file"
	case "prompt":
		return "prompt"
	case "blocked":
		return "blocked"
	default:
		return ""
	}
}

// setValidatorSecretRuntime implements the set validator secret runtime helper.
func setValidatorSecretRuntime(nodeID, mode, source string) {
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return
	}
	// `normMode` stores the value produced by this operation.
	normMode := normalizeValidatorPasswordMode(mode)
	if normMode == "" {
		normMode = configuredValidatorPasswordMode()
	}
	// `normSource` stores the value produced by this operation.
	normSource := normalizeValidatorSecretSource(source)
	validatorSecretRuntimePolicy.mu.Lock()
	validatorSecretRuntimePolicy.mode[id] = normMode
	if normSource == "" {
		delete(validatorSecretRuntimePolicy.source, id)
	} else {
		validatorSecretRuntimePolicy.source[id] = normSource
	}
	validatorSecretRuntimePolicy.mu.Unlock()
}

// validatorSecretRuntime implements the validator secret runtime helper.
func validatorSecretRuntime(nodeID string) (string, string) {
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return configuredValidatorPasswordMode(), ""
	}
	validatorSecretRuntimePolicy.mu.RLock()
	// `mode` stores the value produced by this operation.
	mode := validatorSecretRuntimePolicy.mode[id]
	// `source` stores the value produced by this operation.
	source := validatorSecretRuntimePolicy.source[id]
	validatorSecretRuntimePolicy.mu.RUnlock()
	mode = normalizeValidatorPasswordMode(mode)
	if mode == "" {
		mode = configuredValidatorPasswordMode()
	}
	source = normalizeValidatorSecretSource(source)
	return mode, source
}

// normalizeCoreRegistryDocument normalizes core registry document.
func normalizeCoreRegistryDocument(reg *CoreRegistry) {
	if reg == nil {
		return
	}
	reg.ChainID = strings.TrimSpace(reg.ChainID)
	reg.PreviousRegistryHash = strings.ToLower(strings.TrimSpace(reg.PreviousRegistryHash))
	reg.PayloadHash = strings.ToLower(strings.TrimSpace(reg.PayloadHash))
	// `i` tracks the current position in the related collection.
	for i := range reg.Validators {
		reg.Validators[i].ID = normalizeValidatorID(reg.Validators[i].ID)
		reg.Validators[i].RequiredKeyFingerprint = strings.ToLower(strings.TrimSpace(reg.Validators[i].RequiredKeyFingerprint))
		reg.Validators[i].ConsensusPubKey = strings.ToLower(strings.TrimSpace(reg.Validators[i].ConsensusPubKey))
		reg.Validators[i].P2PSeed = strings.TrimSpace(reg.Validators[i].P2PSeed)
		reg.Validators[i].Status = normalizeCoreRegistryStatus(reg.Validators[i].Status)
	}
	sort.Slice(reg.Validators, func(i, j int) bool {
		return reg.Validators[i].ID < reg.Validators[j].ID
	})
	// `i` tracks the current position in the related collection.
	for i := range reg.Signatures {
		reg.Signatures[i].SignerID = normalizeValidatorID(reg.Signatures[i].SignerID)
		reg.Signatures[i].SignerPubKey = strings.ToLower(strings.TrimSpace(reg.Signatures[i].SignerPubKey))
		reg.Signatures[i].SigHex = strings.ToLower(strings.TrimSpace(reg.Signatures[i].SigHex))
	}
	sort.Slice(reg.Signatures, func(i, j int) bool {
		return reg.Signatures[i].SignerID < reg.Signatures[j].SignerID
	})
}

// coreRegistryCanonicalPayload implements the core registry canonical payload helper.
func coreRegistryCanonicalPayload(reg CoreRegistry) coreRegistryPayload {
	// `normalized` stores the value produced by this operation.
	normalized := reg
	normalizeCoreRegistryDocument(&normalized)
	return coreRegistryPayload{
		ChainID:              normalized.ChainID,
		Version:              normalized.Version,
		Epoch:                normalized.Epoch,
		EffectiveHeight:      normalized.EffectiveHeight,
		PreviousRegistryHash: normalized.PreviousRegistryHash,
		Validators:           normalized.Validators,
	}
}

// coreRegistryPayloadHash implements the core registry payload hash helper.
func coreRegistryPayloadHash(reg CoreRegistry) string {
	// `payload` stores the value produced by this operation.
	payload := coreRegistryCanonicalPayload(reg)
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// coreRegistryRequiredSignatures implements the core registry required signatures helper.
func coreRegistryRequiredSignatures(activeCoreCount int, thresholdOverride int) int {
	if thresholdOverride > 0 {
		// `required` stores the request data being processed.
		required := thresholdOverride
		if activeCoreCount > 0 && required > activeCoreCount {
			required = activeCoreCount
		}
		return required
	}
	if activeCoreCount <= 0 {
		return 0
	}
	// `required` stores the request data being processed.
	required := int(math.Ceil((2.0 / 3.0) * float64(activeCoreCount)))
	if required < 2 {
		required = 2
	}
	if required > activeCoreCount {
		required = activeCoreCount
	}
	return required
}

// verifyCoreRegistryDocument verifies core registry document.
func verifyCoreRegistryDocument(reg CoreRegistry, authority map[string]ed25519.PublicKey, activeAuthority []string, thresholdOverride int) (string, []string, error) {
	// `normalized` stores the value produced by this operation.
	normalized := reg
	normalizeCoreRegistryDocument(&normalized)
	// `payloadHash` stores the digest used to identify or verify the related data.
	payloadHash := coreRegistryPayloadHash(normalized)
	if payloadHash == "" {
		return "", nil, errors.New("core registry payload hash computation failed")
	}
	if normalized.PayloadHash == "" || !strings.EqualFold(normalized.PayloadHash, payloadHash) {
		return "", nil, fmt.Errorf("core registry payload hash mismatch: expected=%s got=%s", payloadHash, normalized.PayloadHash)
	}

	// `authoritySet` stores the value produced by this operation.
	authoritySet := make(map[string]struct{})
	// `id` tracks the current position in the related collection.
	for _, id := range canonicalValidatorIDs(activeAuthority) {
		authoritySet[id] = struct{}{}
	}
	if len(authoritySet) == 0 {
		// `id` tracks the current position in the related collection.
		for id := range authority {
			// `norm` stores the value produced by this operation.
			norm := normalizeValidatorID(id)
			if norm == "" {
				continue
			}
			authoritySet[norm] = struct{}{}
		}
	}

	// `required` stores the request data being processed.
	required := coreRegistryRequiredSignatures(len(authoritySet), thresholdOverride)
	if required == 0 {
		return payloadHash, nil, nil
	}

	// `payloadHashBytes` and `err` store the error produced by this operation.
	payloadHashBytes, err := hex.DecodeString(payloadHash)
	if err != nil {
		return "", nil, fmt.Errorf("invalid payload hash encoding: %w", err)
	}
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(normalized.Signatures))
	// `validSigners` stores whether the related condition is satisfied.
	validSigners := make([]string, 0, len(normalized.Signatures))
	// `sig` tracks the current values while iterating.
	for _, sig := range normalized.Signatures {
		// `signerID` stores the value produced by this operation.
		signerID := normalizeValidatorID(sig.SignerID)
		if signerID == "" {
			continue
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := authoritySet[signerID]; !ok {
			continue
		}
		// `dup` stores the value produced by this operation.
		if _, dup := seen[signerID]; dup {
			continue
		}
		// `pubBytes` and `err` store the error produced by this operation.
		pubBytes, err := hex.DecodeString(strings.TrimSpace(sig.SignerPubKey))
		if err != nil || len(pubBytes) != ed25519.PublicKeySize {
			continue
		}
		// `pub` stores the value produced by this operation.
		pub := ed25519.PublicKey(pubBytes)
		// `knownPub` and `ok` store whether the related condition is satisfied.
		if knownPub, ok := authority[signerID]; ok && len(knownPub) == ed25519.PublicKeySize && !bytes.Equal(knownPub, pub) {
			continue
		}
		// `sigBytes` and `err` store the error produced by this operation.
		sigBytes, err := hex.DecodeString(strings.TrimSpace(sig.SigHex))
		if err != nil || len(sigBytes) != ed25519.SignatureSize {
			continue
		}
		if !ed25519.Verify(pub, payloadHashBytes, sigBytes) && !ed25519.Verify(pub, []byte(payloadHash), sigBytes) {
			continue
		}
		seen[signerID] = struct{}{}
		validSigners = append(validSigners, signerID)
	}
	sort.Strings(validSigners)
	if len(validSigners) < required {
		return "", validSigners, fmt.Errorf("core registry signatures below threshold: got=%d required=%d", len(validSigners), required)
	}
	return payloadHash, validSigners, nil
}

// loadCoreRegistryFromPath implements the load core registry from path helper.
func loadCoreRegistryFromPath(path string) (CoreRegistry, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return CoreRegistry{}, os.ErrNotExist
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(path)
	if err != nil {
		return CoreRegistry{}, err
	}
	// `reg` stores the value used by this operation.
	var reg CoreRegistry
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &reg); err != nil {
		return CoreRegistry{}, fmt.Errorf("decode core registry %s: %w", path, err)
	}
	normalizeCoreRegistryDocument(&reg)
	return reg, nil
}

// persistCoreRegistryToPath implements the persist core registry to path helper.
func persistCoreRegistryToPath(path string, reg CoreRegistry) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("empty core registry path")
	}
	// `normalized` stores the value produced by this operation.
	normalized := reg
	normalizeCoreRegistryDocument(&normalized)
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writePrivateFile(path, raw)
}

// activeCoreAuthorityIDs implements the active core authority i ds helper.
func (n *Node) activeCoreAuthorityIDs() []string {
	if n == nil {
		return nil
	}
	if n.coreBootstrapAuthorityAllowed() {
		// `runtime` stores the value produced by this operation.
		if runtime := runtimeCoreValidatorIDs(); len(runtime) > 0 {
			return runtime
		}
		return canonicalValidatorIDs(ConfigAuthCoreValidators)
	}
	return nil
}

// validatorPubKeyForID implements the validator pub key for id helper.
func validatorPubKeyForID(id string) (ed25519.PublicKey, bool) {
	id = normalizeValidatorID(id)
	if id == "" {
		return nil, false
	}
	validatorPubKeysMu.RLock()
	// `pub` and `ok` store whether the related condition is satisfied.
	pub, ok := ValidatorPubKeys[id]
	validatorPubKeysMu.RUnlock()
	if !ok || len(pub) != ed25519.PublicKeySize {
		return nil, false
	}
	return append(ed25519.PublicKey(nil), pub...), true
}

// coreAuthorityPubKeys implements the core authority pub keys helper.
func (n *Node) coreAuthorityPubKeys(authorityIDs []string) map[string]ed25519.PublicKey {
	// `out` stores the result produced by this operation.
	out := make(map[string]ed25519.PublicKey, len(authorityIDs))
	// `entryPubs` stores the value produced by this operation.
	entryPubs := make(map[string]string)
	if n != nil {
		n.coreRegistryMu.RLock()
		// `id` and `entry` track the current position in the related collection.
		for id, entry := range n.coreRegistryEntries {
			entryPubs[id] = entry.ConsensusPubKey
		}
		n.coreRegistryMu.RUnlock()
	}
	// `id` tracks the current position in the related collection.
	for _, id := range canonicalValidatorIDs(authorityIDs) {
		// `pub` and `ok` store whether the related condition is satisfied.
		if pub, ok := validatorPubKeyForID(id); ok {
			out[id] = pub
			continue
		}
		// `hexKey` stores the key used to access the related value.
		hexKey := strings.TrimSpace(entryPubs[id])
		if hexKey == "" {
			continue
		}
		// `pubRaw` and `err` store the error produced by this operation.
		pubRaw, err := hex.DecodeString(hexKey)
		if err != nil || len(pubRaw) != ed25519.PublicKeySize {
			continue
		}
		out[id] = ed25519.PublicKey(pubRaw)
	}
	return out
}

// deriveCoreSetsForHeight implements the derive core sets for height helper.
func deriveCoreSetsForHeight(entries map[string]CoreRegistryEntry, effectiveHeight uint64, height uint64) ([]string, []string) {
	// `active` stores the value produced by this operation.
	active := make([]string, 0, len(entries))
	// `pending` stores the value produced by this operation.
	pending := make([]string, 0, len(entries))
	// `eligibleHeight` stores the value produced by this operation.
	eligibleHeight := coreRegistryActivationEligibleHeight(effectiveHeight)
	// `entry` tracks the current values while iterating.
	for _, entry := range entries {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(entry.ID)
		if id == "" {
			continue
		}
		switch normalizeCoreRegistryStatus(entry.Status) {
		case coreRegistryStatusRetired:
			continue
		case coreRegistryStatusPending:
			pending = append(pending, id)
		case coreRegistryStatusActive:
			if height >= eligibleHeight {
				active = append(active, id)
			} else {
				pending = append(pending, id)
			}
		default:
			pending = append(pending, id)
		}
	}
	return canonicalValidatorIDs(active), canonicalValidatorIDs(pending)
}

// computeCoreActivationStatusLocked computes core activation status locked.
func (n *Node) computeCoreActivationStatusLocked(nodeID string, height uint64, keyFingerprint string) CoreActivationStatus {
	// `status` stores the value produced by this operation.
	status := CoreActivationStatus{
		NodeID: normalizeValidatorID(nodeID),
		Status: "none",
		Reason: "not_core",
	}
	if status.NodeID == "" {
		return status
	}

	// `entry` and `hasEntry` store the value produced by this operation.
	entry, hasEntry := n.coreRegistryEntries[status.NodeID]
	// `effectiveHeight` stores the value produced by this operation.
	effectiveHeight := n.coreRegistryState.EffectiveHeight
	// `eligibleHeight` stores the value produced by this operation.
	eligibleHeight := coreRegistryActivationEligibleHeight(effectiveHeight)
	status.EligibleHeight = eligibleHeight
	if !hasEntry {
		if runtimeCoreValidatorContains(status.NodeID) {
			status.Status = "active"
			status.Reason = "verified_core_registry"
			return status
		}
		if !n.coreAuthorityHasChainHistory() && containsValidatorID(ConfigAuthCoreValidators, status.NodeID) {
			status.Status = "active"
			status.Reason = "genesis_bootstrap"
		}
		return status
	}

	switch normalizeCoreRegistryStatus(entry.Status) {
	case coreRegistryStatusRetired:
		status.Status = "retired"
		status.Reason = "retired"
	case coreRegistryStatusPending:
		status.Status = "pending"
		status.Reason = "pending_registry"
	case coreRegistryStatusActive:
		if height >= eligibleHeight {
			status.Status = "active"
			status.Reason = "active_registry"
		} else {
			status.Status = "pending"
			status.Reason = "await_effective_height"
		}
	default:
		status.Status = "pending"
		status.Reason = "pending_registry"
	}

	// `expectedFP` stores the value produced by this operation.
	expectedFP := strings.TrimSpace(entry.RequiredKeyFingerprint)
	if expectedFP != "" {
		if strings.TrimSpace(keyFingerprint) == "" {
			status.Status = "blocked"
			status.Reason = "fingerprint_required_key_unavailable"
		} else if !strings.EqualFold(expectedFP, keyFingerprint) {
			status.Status = "blocked"
			status.Reason = "fingerprint_mismatch"
		}
	}
	return status
}

// applyUnverifiedCoreRegistryLocked applies unverified core registry locked.
func (n *Node) applyUnverifiedCoreRegistryLocked(now time.Time, enforcement string) {
	n.coreRegistryEntries = make(map[string]CoreRegistryEntry)
	n.coreRegistryState = CoreRegistryState{
		Hash:            "",
		Epoch:           0,
		EffectiveHeight: 0,
		Verified:        false,
		ActiveCoreSet:   nil,
		PendingCoreSet:  nil,
		LastReloadAt:    now,
		EnforcementMode: enforcement,
	}
}

// containsValidatorID implements the contains validator id helper.
func containsValidatorID(values []string, id string) bool {
	id = normalizeValidatorID(id)
	if id == "" {
		return false
	}
	// `value` tracks the value currently being processed.
	for _, value := range values {
		if normalizeValidatorID(value) == id {
			return true
		}
	}
	return false
}

// applyLegacyCoreRegistryLocked applies legacy core registry locked.
func (n *Node) applyLegacyCoreRegistryLocked(now time.Time, enforcement string) {
	// `active` stores the value produced by this operation.
	active := canonicalValidatorIDs(ConfigAuthCoreValidators)
	n.coreRegistryEntries = make(map[string]CoreRegistryEntry, len(active))
	// `id` tracks the current position in the related collection.
	for _, id := range active {
		n.coreRegistryEntries[id] = CoreRegistryEntry{
			ID:     id,
			Status: coreRegistryStatusActive,
		}
	}
	n.coreRegistryState = CoreRegistryState{
		Hash:            "",
		Epoch:           0,
		EffectiveHeight: 0,
		Verified:        false,
		ActiveCoreSet:   active,
		PendingCoreSet:  nil,
		LastReloadAt:    now,
		EnforcementMode: enforcement,
	}
}

// applyVerifiedCoreRegistryLocked applies verified core registry locked.
func (n *Node) applyVerifiedCoreRegistryLocked(reg CoreRegistry, payloadHash string, now time.Time) {
	// `entries` stores the value produced by this operation.
	entries := make(map[string]CoreRegistryEntry, len(reg.Validators))
	// `entry` tracks the current values while iterating.
	for _, entry := range reg.Validators {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(entry.ID)
		if id == "" {
			continue
		}
		entry.ID = id
		entry.RequiredKeyFingerprint = strings.ToLower(strings.TrimSpace(entry.RequiredKeyFingerprint))
		entry.ConsensusPubKey = strings.ToLower(strings.TrimSpace(entry.ConsensusPubKey))
		entry.Status = normalizeCoreRegistryStatus(entry.Status)
		entries[id] = entry
	}

	// `height` stores the value produced by this operation.
	height := uint64(1)
	if n.Blockchain != nil {
		height = n.Blockchain.Height() + 1
	}
	// `active` and `pending` store the value produced by this operation.
	active, pending := deriveCoreSetsForHeight(entries, reg.EffectiveHeight, height)
	n.coreRegistryEntries = entries
	n.coreRegistryState = CoreRegistryState{
		Hash:            payloadHash,
		Epoch:           reg.Epoch,
		EffectiveHeight: reg.EffectiveHeight,
		Verified:        true,
		ActiveCoreSet:   active,
		PendingCoreSet:  pending,
		LastReloadAt:    now,
		EnforcementMode: normalizeCoreRegistryEnforcementMode(CoreRegistryEnforcementMode),
	}
	n.coreActivationStatus = n.computeCoreActivationStatusLocked(n.ID, height, n.validatorKeyFingerprint)
}

// normalizeCoreRegistryPeerSeed normalizes core registry peer seed.
func normalizeCoreRegistryPeerSeed(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	// `base`, `pid`, and `hasPID` store the value produced by this operation.
	base, pid, hasPID := splitPeerAddress(raw)
	if !hasPID || strings.TrimSpace(base) == "" || strings.TrimSpace(pid) == "" {
		return "", false
	}
	// `err` stores the error produced by this operation.
	if _, err := ma.NewMultiaddr(base); err != nil {
		return "", false
	}
	// `err` stores the error produced by this operation.
	if _, err := peer.Decode(pid); err != nil {
		return "", false
	}
	return fmt.Sprintf("%s/p2p/%s", base, pid), true
}

// ingestCoreRegistryPeerSeeds implements the ingest core registry peer seeds helper.
func (n *Node) ingestCoreRegistryPeerSeeds(entries map[string]CoreRegistryEntry) int {
	if n == nil || len(entries) == 0 {
		return 0
	}
	// `updated` stores the value produced by this operation.
	updated := 0
	// `id` and `entry` track the current position in the related collection.
	for id, entry := range entries {
		// `vid` stores the value produced by this operation.
		vid := normalizeValidatorID(id)
		if vid == "" {
			continue
		}
		// `seedAddr` and `ok` store whether the related condition is satisfied.
		seedAddr, ok := normalizeCoreRegistryPeerSeed(entry.P2PSeed)
		if !ok {
			continue
		}
		// `changed` stores the value produced by this operation.
		changed := false
		ValidatorAddrBook.mu.Lock()
		// `old` stores the value produced by this operation.
		if old := strings.TrimSpace(ValidatorAddrBook.m[vid]); !strings.EqualFold(old, seedAddr) {
			ValidatorAddrBook.m[vid] = seedAddr
			changed = true
		}
		ValidatorAddrBook.mu.Unlock()
		if changed {
			updated++
		}
		n.upsertDiscoveredPeerAddress(seedAddr, true)
	}
	return updated
}

// isCoreRegistryTrustReadyForValidatorParticipation implements the is core registry trust ready for validator participation helper.
func (n *Node) isCoreRegistryTrustReadyForValidatorParticipation() bool {
	if n == nil {
		return false
	}
	// Post-bootstrap validator participation is driven by committed on-chain
	// validator state only. Core registry trust remains a bootstrap/security
	// concern and no longer gates existing-chain consensus participation.
	return true
}

// initializeCoreRegistry implements the initialize core registry helper.
func (n *Node) initializeCoreRegistry() error {
	if n == nil {
		return nil
	}
	// `err` stores the error produced by this operation.
	if err := n.reloadCoreRegistry(true); err != nil {
		return err
	}
	// `interval` stores the value currently being processed.
	interval := coreRegistryReloadInterval()
	if interval > 0 {
		n.SafeGo("core_registry_reload", func() {
			// `ticker` stores the value produced by this operation.
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-n.RootContext().Done():
					return
				case <-ticker.C:
					_ = n.reloadCoreRegistry(false)
				}
			}
		})
	}
	return nil
}

// reloadCoreRegistry implements the reload core registry helper.
func (n *Node) reloadCoreRegistry(startup bool) error {
	if n == nil {
		return nil
	}
	// `enforcement` stores the value produced by this operation.
	enforcement := normalizeCoreRegistryEnforcementMode(CoreRegistryEnforcementMode)
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `primaryPath` stores the value produced by this operation.
	primaryPath := coreRegistryConfigPath()
	// `fallbackPath` stores the value produced by this operation.
	fallbackPath := coreRegistryLastValidPath(n.DataDir)
	// `paths` stores the value produced by this operation.
	paths := []string{primaryPath}
	if fallbackPath != "" && fallbackPath != primaryPath {
		paths = append(paths, fallbackPath)
	}

	// `lastErr` stores the error produced by this operation.
	var lastErr error
	// `idx` and `path` track the current position in the related collection.
	for idx, path := range paths {
		// `reg` and `err` store the error produced by this operation.
		reg, err := loadCoreRegistryFromPath(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				lastErr = err
			}
			continue
		}
		if reg.ChainID != "" && !isProtocolChainID(reg.ChainID) {
			lastErr = fmt.Errorf("core registry chain mismatch: got=%s want=%s", reg.ChainID, protocolChainID())
			continue
		}
		n.coreRegistryMu.RLock()
		// `prevHash` stores the digest used to identify or verify the related data.
		prevHash := strings.TrimSpace(n.coreRegistryState.Hash)
		n.coreRegistryMu.RUnlock()
		if strings.TrimSpace(reg.PreviousRegistryHash) != "" && prevHash != "" && !strings.EqualFold(reg.PreviousRegistryHash, prevHash) {
			lastErr = fmt.Errorf("core registry previous hash mismatch: got=%s want=%s", reg.PreviousRegistryHash, prevHash)
			continue
		}
		// `authorityIDs` stores the value produced by this operation.
		authorityIDs := n.coreRegistryVerificationAuthorityIDs()
		// `authorityPubs` stores the value produced by this operation.
		authorityPubs := n.coreAuthorityPubKeys(authorityIDs)
		// `payloadHash`, `signers`, and `err` store the error produced by this operation.
		payloadHash, signers, err := verifyCoreRegistryDocument(reg, authorityPubs, authorityIDs, CoreRegistryMinSignatures)
		if err != nil {
			lastErr = err
			continue
		}
		// `seedSnapshot` stores the value used by this operation.
		var seedSnapshot map[string]CoreRegistryEntry
		n.coreRegistryMu.Lock()
		n.applyVerifiedCoreRegistryLocked(reg, payloadHash, now)
		seedSnapshot = make(map[string]CoreRegistryEntry, len(n.coreRegistryEntries))
		// `id` and `entry` track the current position in the related collection.
		for id, entry := range n.coreRegistryEntries {
			seedSnapshot[id] = entry
		}
		n.coreRegistryMu.Unlock()
		// `allRuntimeCore` stores the value produced by this operation.
		allRuntimeCore := append([]string{}, n.coreRegistryState.ActiveCoreSet...)
		allRuntimeCore = append(allRuntimeCore, n.coreRegistryState.PendingCoreSet...)
		setRuntimeCoreValidatorIDs(allRuntimeCore)
		// `seeded` stores the value produced by this operation.
		seeded := n.ingestCoreRegistryPeerSeeds(seedSnapshot)
		if idx == 0 {
			_ = persistCoreRegistryToPath(fallbackPath, reg)
		}
		if DebugConsensus {
			fmt.Printf("[CORE-REGISTRY] verified=true hash=%s epoch=%d active=%d pending=%d signers=%d seeded_peers=%d\n",
				ShortHash(payloadHash),
				reg.Epoch,
				len(n.coreRegistryState.ActiveCoreSet),
				len(n.coreRegistryState.PendingCoreSet),
				len(signers),
				seeded,
			)
		}
		return nil
	}

	n.coreRegistryMu.Lock()
	if !n.coreRegistryState.Verified {
		if n.coreBootstrapAuthorityAllowed() {
			n.applyLegacyCoreRegistryLocked(now, enforcement)
		} else {
			n.applyUnverifiedCoreRegistryLocked(now, enforcement)
		}
	}
	n.coreRegistryState.EnforcementMode = enforcement
	n.coreRegistryState.LastReloadAt = now
	// `height` stores the value produced by this operation.
	height := uint64(1)
	if n.Blockchain != nil {
		height = n.Blockchain.Height() + 1
	}
	n.coreActivationStatus = n.computeCoreActivationStatusLocked(n.ID, height, n.validatorKeyFingerprint)
	// `active` stores the value produced by this operation.
	active := append([]string{}, n.coreRegistryState.ActiveCoreSet...)
	// `pending` stores the value produced by this operation.
	pending := append([]string{}, n.coreRegistryState.PendingCoreSet...)
	n.coreRegistryMu.Unlock()
	setRuntimeCoreValidatorIDs(append(active, pending...))

	if lastErr != nil {
		if startup || DebugConsensus {
			fmt.Printf("[CORE-REGISTRY] verified=false mode=%s error=%v\n", enforcement, lastErr)
		}
	}

	// `isCoreNode` stores the current position in the related collection.
	isCoreNode := isCoreValidatorForSecurityPolicy(n.ID, n.DataDir)
	if startup && enforcement == "enforce" && isCoreNode {
		if lastErr == nil {
			lastErr = errors.New("core registry missing or invalid")
		}
		// Fail-closed without process abort: network/sync can run, validator
		// participation remains blocked by runtime trust gate until verified.
		fmt.Printf("[CORE-REGISTRY] fail_closed=true node=%s mode=%s reason=%v\n", n.ID, enforcement, lastErr)
		return nil
	}
	return nil
}

// coreRegistryStatus implements the core registry status helper.
func (n *Node) coreRegistryStatus() (CoreRegistryState, CoreActivationStatus) {
	if n == nil {
		return CoreRegistryState{}, CoreActivationStatus{}
	}
	// `height` stores the value produced by this operation.
	height := uint64(1)
	if n.Blockchain != nil {
		height = n.Blockchain.Height() + 1
	}
	n.coreRegistryMu.Lock()
	// `active` and `pending` store the value produced by this operation.
	active, pending := deriveCoreSetsForHeight(n.coreRegistryEntries, n.coreRegistryState.EffectiveHeight, height)
	n.coreRegistryState.ActiveCoreSet = active
	n.coreRegistryState.PendingCoreSet = pending
	n.coreActivationStatus = n.computeCoreActivationStatusLocked(n.ID, height, n.validatorKeyFingerprint)
	// `state` stores the value produced by this operation.
	state := n.coreRegistryState
	// `activation` stores the value produced by this operation.
	activation := n.coreActivationStatus
	n.coreRegistryMu.Unlock()
	setRuntimeCoreValidatorIDs(append(append([]string{}, state.ActiveCoreSet...), state.PendingCoreSet...))
	return state, activation
}

// coreSetsForHeight implements the core sets for height helper.
func (n *Node) coreSetsForHeight(height uint64) ([]string, []string) {
	if n == nil {
		return runtimeCoreValidatorIDs(), nil
	}
	if height == 0 {
		height = 1
		if n.Blockchain != nil {
			height = n.Blockchain.Height() + 1
		}
	}
	n.coreRegistryMu.RLock()
	// `active` and `pending` store the value produced by this operation.
	active, pending := deriveCoreSetsForHeight(n.coreRegistryEntries, n.coreRegistryState.EffectiveHeight, height)
	// `verified` stores the value produced by this operation.
	verified := n.coreRegistryState.Verified
	n.coreRegistryMu.RUnlock()
	if verified && (len(active) > 0 || len(pending) > 0) {
		return active, pending
	}
	// `runtime` stores the value produced by this operation.
	if runtime := runtimeCoreValidatorIDs(); len(runtime) > 0 {
		return runtime, nil
	}
	if n.coreBootstrapAuthorityAllowed() {
		return canonicalValidatorIDs(ConfigAuthCoreValidators), nil
	}
	return nil, nil
}

// coreRegistryHasEntryID implements the core registry has entry id helper.
func (n *Node) coreRegistryHasEntryID(id string) bool {
	if n == nil {
		return false
	}
	id = normalizeValidatorID(id)
	if id == "" {
		return false
	}
	n.coreRegistryMu.RLock()
	// `ok` stores whether the related condition is satisfied.
	_, ok := n.coreRegistryEntries[id]
	n.coreRegistryMu.RUnlock()
	return ok
}

// coreExpectedFingerprintForID implements the core expected fingerprint for id helper.
func (n *Node) coreExpectedFingerprintForID(nodeID string) (string, bool) {
	if n == nil {
		return "", false
	}
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return "", false
	}
	n.coreRegistryMu.RLock()
	// `entry` and `ok` store whether the related condition is satisfied.
	entry, ok := n.coreRegistryEntries[id]
	n.coreRegistryMu.RUnlock()
	if !ok {
		return "", false
	}
	// `expected` stores the value produced by this operation.
	expected := strings.ToLower(strings.TrimSpace(entry.RequiredKeyFingerprint))
	if expected == "" {
		return "", false
	}
	return expected, true
}

// coreRequiredFingerprintMatch implements the core required fingerprint match helper.
func (n *Node) coreRequiredFingerprintMatch(nodeID string, gotFingerprint string) bool {
	// `expected` and `ok` store whether the related condition is satisfied.
	expected, ok := n.coreExpectedFingerprintForID(nodeID)
	if !ok {
		return true
	}
	return strings.EqualFold(expected, strings.TrimSpace(gotFingerprint))
}

// coreEligibleForConsensus implements the core eligible for consensus helper.
func (n *Node) coreEligibleForConsensus(height uint64) bool {
	if n == nil {
		return false
	}
	if !n.coreBootstrapAuthorityAllowed() {
		return true
	}
	localID := n.localConsensusValidatorIDForHeight(height)
	if !n.isCoreValidatorCurrent(localID) && !n.coreRegistryHasEntryID(localID) {
		return true
	}
	if !ConsensusCorePendingExcludedFromProposer {
		return true
	}
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(localID)
	// `active` and `pending` store the value produced by this operation.
	active, pending := n.coreSetsForHeight(height)
	if containsValidatorID(active, id) {
		return true
	}
	if containsValidatorID(pending, id) {
		return false
	}
	if n.coreRegistryHasEntryID(id) {
		return false
	}
	return true
}

// isCorePendingAtHeight implements the is core pending at height helper.
func (n *Node) isCorePendingAtHeight(id string, height uint64) bool {
	id = normalizeValidatorID(id)
	if n == nil || id == "" {
		return false
	}
	// `pending` stores the value produced by this operation.
	_, pending := n.coreSetsForHeight(height)
	return containsValidatorID(pending, id)
}

// canAdvertiseValidatorPresence implements the can advertise validator presence helper.
func (n *Node) canAdvertiseValidatorPresence() bool {
	if n == nil || n.Role != "validator" || !isValidatorKeyUsable(n.ValidatorKey) {
		return false
	}
	if n.canParticipateAsValidator() {
		return true
	}
	if !n.coreBootstrapAuthorityAllowed() {
		return false
	}
	// `height` stores the value produced by this operation.
	height := n.currentEpoch()
	return n.isCorePendingAtHeight(n.ID, height)
}

// applyCoreConsensusFilter applies core consensus filter.
func (n *Node) applyCoreConsensusFilter(height uint64, validators []string) []string {
	// `out` stores the result produced by this operation.
	out := canonicalValidatorIDs(validators)
	if n == nil || len(out) == 0 {
		return out
	}
	if !n.coreBootstrapAuthorityAllowed() {
		return out
	}
	// `active` and `pending` store the value produced by this operation.
	active, pending := n.coreSetsForHeight(height)
	if len(active) == 0 && len(pending) == 0 {
		return out
	}
	// `activeSet` stores the value produced by this operation.
	activeSet := make(map[string]struct{}, len(active))
	// `id` tracks the current position in the related collection.
	for _, id := range active {
		activeSet[id] = struct{}{}
	}
	// `pendingSet` stores the value produced by this operation.
	pendingSet := make(map[string]struct{}, len(pending))
	// `id` tracks the current position in the related collection.
	for _, id := range pending {
		pendingSet[id] = struct{}{}
	}

	// `filtered` stores the value produced by this operation.
	filtered := make([]string, 0, len(out))
	// `id` tracks the current position in the related collection.
	for _, id := range out {
		// `isPending` stores the current position in the related collection.
		if _, isPending := pendingSet[id]; isPending {
			if ConsensusCorePendingExcludedFromProposer {
				continue
			}
			filtered = append(filtered, id)
			continue
		}
		if len(activeSet) > 0 {
			// `isActive` stores the current position in the related collection.
			if _, isActive := activeSet[id]; isActive {
				filtered = append(filtered, id)
				continue
			}
			if n.coreRegistryHasEntryID(id) {
				// Entry exists but not active for this height.
				continue
			}
		}
		filtered = append(filtered, id)
	}
	if len(filtered) == 0 {
		return out
	}
	return canonicalValidatorIDs(filtered)
}

// isCoreValidatorForSecurityPolicy implements the is core validator for security policy helper.
func isCoreValidatorForSecurityPolicy(nodeID string, nodePath string) bool {
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return false
	}
	if runtimeCoreValidatorContains(id) {
		return true
	}
	if containsValidatorID(ConfigAuthCoreValidators, id) {
		return true
	}

	// `containsInRegistry` stores the value produced by this operation.
	containsInRegistry := func(path string) bool {
		// `reg` and `err` store the error produced by this operation.
		reg, err := loadCoreRegistryFromPath(path)
		if err != nil {
			return false
		}
		// `entry` tracks the current values while iterating.
		for _, entry := range reg.Validators {
			if normalizeValidatorID(entry.ID) != id {
				continue
			}
			if normalizeCoreRegistryStatus(entry.Status) == coreRegistryStatusRetired {
				return false
			}
			return true
		}
		return false
	}

	if containsInRegistry(coreRegistryConfigPath()) {
		return true
	}
	if containsInRegistry(coreRegistryLastValidPath(nodePath)) {
		return true
	}
	return false
}
