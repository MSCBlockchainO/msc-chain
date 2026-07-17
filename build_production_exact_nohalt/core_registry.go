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
	coreRegistryStatusPending = "pending"
	coreRegistryStatusActive  = "active"
	coreRegistryStatusRetired = "retired"
	coreRegistryLastValidFile = "core_registry.last_valid.json"
)

var runtimeCoreValidatorSet = struct {
	mu  sync.RWMutex
	ids map[string]struct{}
}{
	ids: make(map[string]struct{}),
}

var coreEnvPasswordPolicy = struct {
	mu      sync.RWMutex
	blocked map[string]bool
}{
	blocked: make(map[string]bool),
}

var validatorSecretRuntimePolicy = struct {
	mu     sync.RWMutex
	mode   map[string]string
	source map[string]string
}{
	mode:   make(map[string]string),
	source: make(map[string]string),
}

type coreRegistryPayload struct {
	ChainID              string              `json:"chain_id"`
	Version              uint64              `json:"version"`
	Epoch                uint64              `json:"epoch"`
	EffectiveHeight      uint64              `json:"effective_height"`
	PreviousRegistryHash string              `json:"previous_registry_hash"`
	Validators           []CoreRegistryEntry `json:"validators"`
}

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

func coreRegistryReloadInterval() time.Duration {
	seconds := CoreRegistryReloadSeconds
	if seconds == 0 {
		seconds = 10
	}
	if seconds > 3600 {
		seconds = 3600
	}
	return time.Duration(seconds) * time.Second
}

func coreRegistryConfigPath() string {
	path := strings.TrimSpace(CoreRegistryPath)
	if path == "" {
		return "core_validators.json"
	}
	return path
}

func coreRegistryLastValidPath(nodePath string) string {
	base := strings.TrimSpace(nodePath)
	if base == "" {
		return coreRegistryLastValidFile
	}
	return filepath.Join(base, coreRegistryLastValidFile)
}

func coreRegistryActivationEligibleHeight(effectiveHeight uint64) uint64 {
	if effectiveHeight == 0 {
		return 0
	}
	buffer := ConsensusCoreActivationEffectiveHeightBuffer
	if buffer == 0 {
		return effectiveHeight
	}
	maxU64 := ^uint64(0)
	if effectiveHeight > maxU64-buffer {
		return maxU64
	}
	return effectiveHeight + buffer
}

func setRuntimeCoreValidatorIDs(ids []string) {
	norm := canonicalValidatorIDs(ids)
	runtimeCoreValidatorSet.mu.Lock()
	defer runtimeCoreValidatorSet.mu.Unlock()
	runtimeCoreValidatorSet.ids = make(map[string]struct{}, len(norm))
	for _, id := range norm {
		runtimeCoreValidatorSet.ids[id] = struct{}{}
	}
}

func runtimeCoreValidatorContains(id string) bool {
	id = normalizeValidatorID(id)
	if id == "" {
		return false
	}
	runtimeCoreValidatorSet.mu.RLock()
	_, ok := runtimeCoreValidatorSet.ids[id]
	runtimeCoreValidatorSet.mu.RUnlock()
	return ok
}

func runtimeCoreValidatorIDs() []string {
	runtimeCoreValidatorSet.mu.RLock()
	defer runtimeCoreValidatorSet.mu.RUnlock()
	out := make([]string, 0, len(runtimeCoreValidatorSet.ids))
	for id := range runtimeCoreValidatorSet.ids {
		out = append(out, id)
	}
	return canonicalValidatorIDs(out)
}

func (n *Node) coreAuthorityHasChainHistory() bool {
	if n == nil {
		return false
	}
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

func (n *Node) coreBootstrapAuthorityAllowed() bool {
	return !n.coreAuthorityHasChainHistory()
}

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

func (n *Node) isCoreValidatorCurrent(nodeID string) bool {
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
		height := uint64(1)
		if n.Blockchain != nil {
			height = n.Blockchain.Height() + 1
		}
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

func (n *Node) coreMembershipStatus(nodeID string) string {
	if n.isCoreValidatorCurrent(nodeID) {
		return "core"
	}
	return "non_core"
}

func (n *Node) requiresWalletAuthCurrent(nodeID string) bool {
	if !ConfigAuthRequireWallet {
		return false
	}
	if genesisWalletAuthExemptValidator(nodeID) {
		return false
	}
	return !n.isCoreValidatorCurrent(nodeID)
}

func (n *Node) coreRegistryVerificationAuthorityIDs() []string {
	if n == nil {
		return canonicalValidatorIDs(ConfigAuthCoreValidators)
	}
	n.coreRegistryMu.RLock()
	active := append([]string{}, n.coreRegistryState.ActiveCoreSet...)
	verified := n.coreRegistryState.Verified
	n.coreRegistryMu.RUnlock()
	if verified && len(active) > 0 {
		return canonicalValidatorIDs(active)
	}
	return canonicalValidatorIDs(ConfigAuthCoreValidators)
}

func setCoreEnvPasswordBlocked(nodeID string, blocked bool) {
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return
	}
	coreEnvPasswordPolicy.mu.Lock()
	coreEnvPasswordPolicy.blocked[id] = blocked
	coreEnvPasswordPolicy.mu.Unlock()
}

func coreEnvPasswordBlocked(nodeID string) bool {
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return false
	}
	coreEnvPasswordPolicy.mu.RLock()
	blocked := coreEnvPasswordPolicy.blocked[id]
	coreEnvPasswordPolicy.mu.RUnlock()
	return blocked
}

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

func setValidatorSecretRuntime(nodeID, mode, source string) {
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return
	}
	normMode := normalizeValidatorPasswordMode(mode)
	if normMode == "" {
		normMode = configuredValidatorPasswordMode()
	}
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

func validatorSecretRuntime(nodeID string) (string, string) {
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return configuredValidatorPasswordMode(), ""
	}
	validatorSecretRuntimePolicy.mu.RLock()
	mode := validatorSecretRuntimePolicy.mode[id]
	source := validatorSecretRuntimePolicy.source[id]
	validatorSecretRuntimePolicy.mu.RUnlock()
	mode = normalizeValidatorPasswordMode(mode)
	if mode == "" {
		mode = configuredValidatorPasswordMode()
	}
	source = normalizeValidatorSecretSource(source)
	return mode, source
}

func normalizeCoreRegistryDocument(reg *CoreRegistry) {
	if reg == nil {
		return
	}
	reg.ChainID = strings.TrimSpace(reg.ChainID)
	reg.PreviousRegistryHash = strings.ToLower(strings.TrimSpace(reg.PreviousRegistryHash))
	reg.PayloadHash = strings.ToLower(strings.TrimSpace(reg.PayloadHash))
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
	for i := range reg.Signatures {
		reg.Signatures[i].SignerID = normalizeValidatorID(reg.Signatures[i].SignerID)
		reg.Signatures[i].SignerPubKey = strings.ToLower(strings.TrimSpace(reg.Signatures[i].SignerPubKey))
		reg.Signatures[i].SigHex = strings.ToLower(strings.TrimSpace(reg.Signatures[i].SigHex))
	}
	sort.Slice(reg.Signatures, func(i, j int) bool {
		return reg.Signatures[i].SignerID < reg.Signatures[j].SignerID
	})
}

func coreRegistryCanonicalPayload(reg CoreRegistry) coreRegistryPayload {
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

func coreRegistryPayloadHash(reg CoreRegistry) string {
	payload := coreRegistryCanonicalPayload(reg)
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func coreRegistryRequiredSignatures(activeCoreCount int, thresholdOverride int) int {
	if thresholdOverride > 0 {
		required := thresholdOverride
		if activeCoreCount > 0 && required > activeCoreCount {
			required = activeCoreCount
		}
		return required
	}
	if activeCoreCount <= 0 {
		return 0
	}
	required := int(math.Ceil((2.0 / 3.0) * float64(activeCoreCount)))
	if required < 2 {
		required = 2
	}
	if required > activeCoreCount {
		required = activeCoreCount
	}
	return required
}

func verifyCoreRegistryDocument(reg CoreRegistry, authority map[string]ed25519.PublicKey, activeAuthority []string, thresholdOverride int) (string, []string, error) {
	normalized := reg
	normalizeCoreRegistryDocument(&normalized)
	payloadHash := coreRegistryPayloadHash(normalized)
	if payloadHash == "" {
		return "", nil, errors.New("core registry payload hash computation failed")
	}
	if normalized.PayloadHash == "" || !strings.EqualFold(normalized.PayloadHash, payloadHash) {
		return "", nil, fmt.Errorf("core registry payload hash mismatch: expected=%s got=%s", payloadHash, normalized.PayloadHash)
	}

	authoritySet := make(map[string]struct{})
	for _, id := range canonicalValidatorIDs(activeAuthority) {
		authoritySet[id] = struct{}{}
	}
	if len(authoritySet) == 0 {
		for id := range authority {
			norm := normalizeValidatorID(id)
			if norm == "" {
				continue
			}
			authoritySet[norm] = struct{}{}
		}
	}

	required := coreRegistryRequiredSignatures(len(authoritySet), thresholdOverride)
	if required == 0 {
		return payloadHash, nil, nil
	}

	payloadHashBytes, err := hex.DecodeString(payloadHash)
	if err != nil {
		return "", nil, fmt.Errorf("invalid payload hash encoding: %w", err)
	}
	seen := make(map[string]struct{}, len(normalized.Signatures))
	validSigners := make([]string, 0, len(normalized.Signatures))
	for _, sig := range normalized.Signatures {
		signerID := normalizeValidatorID(sig.SignerID)
		if signerID == "" {
			continue
		}
		if _, ok := authoritySet[signerID]; !ok {
			continue
		}
		if _, dup := seen[signerID]; dup {
			continue
		}
		pubBytes, err := hex.DecodeString(strings.TrimSpace(sig.SignerPubKey))
		if err != nil || len(pubBytes) != ed25519.PublicKeySize {
			continue
		}
		pub := ed25519.PublicKey(pubBytes)
		if knownPub, ok := authority[signerID]; ok && len(knownPub) == ed25519.PublicKeySize && !bytes.Equal(knownPub, pub) {
			continue
		}
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

func loadCoreRegistryFromPath(path string) (CoreRegistry, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return CoreRegistry{}, os.ErrNotExist
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return CoreRegistry{}, err
	}
	var reg CoreRegistry
	if err := json.Unmarshal(raw, &reg); err != nil {
		return CoreRegistry{}, fmt.Errorf("decode core registry %s: %w", path, err)
	}
	normalizeCoreRegistryDocument(&reg)
	return reg, nil
}

func persistCoreRegistryToPath(path string, reg CoreRegistry) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("empty core registry path")
	}
	normalized := reg
	normalizeCoreRegistryDocument(&normalized)
	raw, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writePrivateFile(path, raw)
}

func (n *Node) activeCoreAuthorityIDs() []string {
	if n == nil {
		return nil
	}
	if n.coreBootstrapAuthorityAllowed() {
		if runtime := runtimeCoreValidatorIDs(); len(runtime) > 0 {
			return runtime
		}
		return canonicalValidatorIDs(ConfigAuthCoreValidators)
	}
	return nil
}

func validatorPubKeyForID(id string) (ed25519.PublicKey, bool) {
	id = normalizeValidatorID(id)
	if id == "" {
		return nil, false
	}
	validatorPubKeysMu.RLock()
	pub, ok := ValidatorPubKeys[id]
	validatorPubKeysMu.RUnlock()
	if !ok || len(pub) != ed25519.PublicKeySize {
		return nil, false
	}
	return append(ed25519.PublicKey(nil), pub...), true
}

func (n *Node) coreAuthorityPubKeys(authorityIDs []string) map[string]ed25519.PublicKey {
	out := make(map[string]ed25519.PublicKey, len(authorityIDs))
	entryPubs := make(map[string]string)
	if n != nil {
		n.coreRegistryMu.RLock()
		for id, entry := range n.coreRegistryEntries {
			entryPubs[id] = entry.ConsensusPubKey
		}
		n.coreRegistryMu.RUnlock()
	}
	for _, id := range canonicalValidatorIDs(authorityIDs) {
		if pub, ok := validatorPubKeyForID(id); ok {
			out[id] = pub
			continue
		}
		hexKey := strings.TrimSpace(entryPubs[id])
		if hexKey == "" {
			continue
		}
		pubRaw, err := hex.DecodeString(hexKey)
		if err != nil || len(pubRaw) != ed25519.PublicKeySize {
			continue
		}
		out[id] = ed25519.PublicKey(pubRaw)
	}
	return out
}

func deriveCoreSetsForHeight(entries map[string]CoreRegistryEntry, effectiveHeight uint64, height uint64) ([]string, []string) {
	active := make([]string, 0, len(entries))
	pending := make([]string, 0, len(entries))
	eligibleHeight := coreRegistryActivationEligibleHeight(effectiveHeight)
	for _, entry := range entries {
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

func (n *Node) computeCoreActivationStatusLocked(nodeID string, height uint64, keyFingerprint string) CoreActivationStatus {
	status := CoreActivationStatus{
		NodeID: normalizeValidatorID(nodeID),
		Status: "none",
		Reason: "not_core",
	}
	if status.NodeID == "" {
		return status
	}

	entry, hasEntry := n.coreRegistryEntries[status.NodeID]
	effectiveHeight := n.coreRegistryState.EffectiveHeight
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

func containsValidatorID(values []string, id string) bool {
	id = normalizeValidatorID(id)
	if id == "" {
		return false
	}
	for _, value := range values {
		if normalizeValidatorID(value) == id {
			return true
		}
	}
	return false
}

func (n *Node) applyLegacyCoreRegistryLocked(now time.Time, enforcement string) {
	active := canonicalValidatorIDs(ConfigAuthCoreValidators)
	n.coreRegistryEntries = make(map[string]CoreRegistryEntry, len(active))
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

func (n *Node) applyVerifiedCoreRegistryLocked(reg CoreRegistry, payloadHash string, now time.Time) {
	entries := make(map[string]CoreRegistryEntry, len(reg.Validators))
	for _, entry := range reg.Validators {
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

	height := uint64(1)
	if n.Blockchain != nil {
		height = n.Blockchain.Height() + 1
	}
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

func normalizeCoreRegistryPeerSeed(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	base, pid, hasPID := splitPeerAddress(raw)
	if !hasPID || strings.TrimSpace(base) == "" || strings.TrimSpace(pid) == "" {
		return "", false
	}
	if _, err := ma.NewMultiaddr(base); err != nil {
		return "", false
	}
	if _, err := peer.Decode(pid); err != nil {
		return "", false
	}
	return fmt.Sprintf("%s/p2p/%s", base, pid), true
}

func (n *Node) ingestCoreRegistryPeerSeeds(entries map[string]CoreRegistryEntry) int {
	if n == nil || len(entries) == 0 {
		return 0
	}
	updated := 0
	for id, entry := range entries {
		vid := normalizeValidatorID(id)
		if vid == "" {
			continue
		}
		seedAddr, ok := normalizeCoreRegistryPeerSeed(entry.P2PSeed)
		if !ok {
			continue
		}
		changed := false
		ValidatorAddrBook.mu.Lock()
		if old := strings.TrimSpace(ValidatorAddrBook.m[vid]); !strings.EqualFold(old, seedAddr) {
			ValidatorAddrBook.m[vid] = seedAddr
			changed = true
		}
		ValidatorAddrBook.mu.Unlock()
		if changed {
			updated++
		}
		n.upsertDiscoveredPeerAddress(seedAddr)
	}
	return updated
}

func (n *Node) isCoreRegistryTrustReadyForValidatorParticipation() bool {
	if n == nil {
		return false
	}
	// Post-bootstrap validator participation is driven by committed on-chain
	// validator state only. Core registry trust remains a bootstrap/security
	// concern and no longer gates existing-chain consensus participation.
	return true
}

func (n *Node) initializeCoreRegistry() error {
	if n == nil {
		return nil
	}
	if err := n.reloadCoreRegistry(true); err != nil {
		return err
	}
	interval := coreRegistryReloadInterval()
	if interval > 0 {
		n.SafeGo("core_registry_reload", func() {
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

func (n *Node) reloadCoreRegistry(startup bool) error {
	if n == nil {
		return nil
	}
	enforcement := normalizeCoreRegistryEnforcementMode(CoreRegistryEnforcementMode)
	now := time.Now()
	primaryPath := coreRegistryConfigPath()
	fallbackPath := coreRegistryLastValidPath(n.DataDir)
	paths := []string{primaryPath}
	if fallbackPath != "" && fallbackPath != primaryPath {
		paths = append(paths, fallbackPath)
	}

	var lastErr error
	for idx, path := range paths {
		reg, err := loadCoreRegistryFromPath(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				lastErr = err
			}
			continue
		}
		if reg.ChainID != "" && ChainID != "" && !strings.EqualFold(reg.ChainID, ChainID) {
			lastErr = fmt.Errorf("core registry chain mismatch: got=%s want=%s", reg.ChainID, ChainID)
			continue
		}
		n.coreRegistryMu.RLock()
		prevHash := strings.TrimSpace(n.coreRegistryState.Hash)
		n.coreRegistryMu.RUnlock()
		if strings.TrimSpace(reg.PreviousRegistryHash) != "" && prevHash != "" && !strings.EqualFold(reg.PreviousRegistryHash, prevHash) {
			lastErr = fmt.Errorf("core registry previous hash mismatch: got=%s want=%s", reg.PreviousRegistryHash, prevHash)
			continue
		}
		authorityIDs := n.coreRegistryVerificationAuthorityIDs()
		authorityPubs := n.coreAuthorityPubKeys(authorityIDs)
		payloadHash, signers, err := verifyCoreRegistryDocument(reg, authorityPubs, authorityIDs, CoreRegistryMinSignatures)
		if err != nil {
			lastErr = err
			continue
		}
		var seedSnapshot map[string]CoreRegistryEntry
		n.coreRegistryMu.Lock()
		n.applyVerifiedCoreRegistryLocked(reg, payloadHash, now)
		seedSnapshot = make(map[string]CoreRegistryEntry, len(n.coreRegistryEntries))
		for id, entry := range n.coreRegistryEntries {
			seedSnapshot[id] = entry
		}
		n.coreRegistryMu.Unlock()
		allRuntimeCore := append([]string{}, n.coreRegistryState.ActiveCoreSet...)
		allRuntimeCore = append(allRuntimeCore, n.coreRegistryState.PendingCoreSet...)
		setRuntimeCoreValidatorIDs(allRuntimeCore)
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
	height := uint64(1)
	if n.Blockchain != nil {
		height = n.Blockchain.Height() + 1
	}
	n.coreActivationStatus = n.computeCoreActivationStatusLocked(n.ID, height, n.validatorKeyFingerprint)
	active := append([]string{}, n.coreRegistryState.ActiveCoreSet...)
	pending := append([]string{}, n.coreRegistryState.PendingCoreSet...)
	n.coreRegistryMu.Unlock()
	setRuntimeCoreValidatorIDs(append(active, pending...))

	if lastErr != nil {
		if startup || DebugConsensus {
			fmt.Printf("[CORE-REGISTRY] verified=false mode=%s error=%v\n", enforcement, lastErr)
		}
	}

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

func (n *Node) coreRegistryStatus() (CoreRegistryState, CoreActivationStatus) {
	if n == nil {
		return CoreRegistryState{}, CoreActivationStatus{}
	}
	height := uint64(1)
	if n.Blockchain != nil {
		height = n.Blockchain.Height() + 1
	}
	n.coreRegistryMu.Lock()
	active, pending := deriveCoreSetsForHeight(n.coreRegistryEntries, n.coreRegistryState.EffectiveHeight, height)
	n.coreRegistryState.ActiveCoreSet = active
	n.coreRegistryState.PendingCoreSet = pending
	n.coreActivationStatus = n.computeCoreActivationStatusLocked(n.ID, height, n.validatorKeyFingerprint)
	state := n.coreRegistryState
	activation := n.coreActivationStatus
	n.coreRegistryMu.Unlock()
	setRuntimeCoreValidatorIDs(append(append([]string{}, state.ActiveCoreSet...), state.PendingCoreSet...))
	return state, activation
}

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
	active, pending := deriveCoreSetsForHeight(n.coreRegistryEntries, n.coreRegistryState.EffectiveHeight, height)
	verified := n.coreRegistryState.Verified
	n.coreRegistryMu.RUnlock()
	if verified && (len(active) > 0 || len(pending) > 0) {
		return active, pending
	}
	if runtime := runtimeCoreValidatorIDs(); len(runtime) > 0 {
		return runtime, nil
	}
	if n.coreBootstrapAuthorityAllowed() {
		return canonicalValidatorIDs(ConfigAuthCoreValidators), nil
	}
	return nil, nil
}

func (n *Node) coreRegistryHasEntryID(id string) bool {
	if n == nil {
		return false
	}
	id = normalizeValidatorID(id)
	if id == "" {
		return false
	}
	n.coreRegistryMu.RLock()
	_, ok := n.coreRegistryEntries[id]
	n.coreRegistryMu.RUnlock()
	return ok
}

func (n *Node) coreExpectedFingerprintForID(nodeID string) (string, bool) {
	if n == nil {
		return "", false
	}
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return "", false
	}
	n.coreRegistryMu.RLock()
	entry, ok := n.coreRegistryEntries[id]
	n.coreRegistryMu.RUnlock()
	if !ok {
		return "", false
	}
	expected := strings.ToLower(strings.TrimSpace(entry.RequiredKeyFingerprint))
	if expected == "" {
		return "", false
	}
	return expected, true
}

func (n *Node) coreRequiredFingerprintMatch(nodeID string, gotFingerprint string) bool {
	expected, ok := n.coreExpectedFingerprintForID(nodeID)
	if !ok {
		return true
	}
	return strings.EqualFold(expected, strings.TrimSpace(gotFingerprint))
}

func (n *Node) coreEligibleForConsensus(height uint64) bool {
	if n == nil {
		return false
	}
	if !n.coreBootstrapAuthorityAllowed() {
		return true
	}
	if !n.isCoreValidatorCurrent(n.ID) && !n.coreRegistryHasEntryID(n.ID) {
		return true
	}
	if !ConsensusCorePendingExcludedFromProposer {
		return true
	}
	id := normalizeValidatorID(n.ID)
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

func (n *Node) isCorePendingAtHeight(id string, height uint64) bool {
	id = normalizeValidatorID(id)
	if n == nil || id == "" {
		return false
	}
	_, pending := n.coreSetsForHeight(height)
	return containsValidatorID(pending, id)
}

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
	height := n.currentEpoch()
	return n.isCorePendingAtHeight(n.ID, height)
}

func (n *Node) applyCoreConsensusFilter(height uint64, validators []string) []string {
	out := canonicalValidatorIDs(validators)
	if n == nil || len(out) == 0 {
		return out
	}
	if !n.coreBootstrapAuthorityAllowed() {
		return out
	}
	active, pending := n.coreSetsForHeight(height)
	if len(active) == 0 && len(pending) == 0 {
		return out
	}
	activeSet := make(map[string]struct{}, len(active))
	for _, id := range active {
		activeSet[id] = struct{}{}
	}
	pendingSet := make(map[string]struct{}, len(pending))
	for _, id := range pending {
		pendingSet[id] = struct{}{}
	}

	filtered := make([]string, 0, len(out))
	for _, id := range out {
		if _, isPending := pendingSet[id]; isPending {
			if ConsensusCorePendingExcludedFromProposer {
				continue
			}
			filtered = append(filtered, id)
			continue
		}
		if len(activeSet) > 0 {
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

func isCoreValidatorForSecurityPolicy(nodeID string, nodePath string) bool {
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

	containsInRegistry := func(path string) bool {
		reg, err := loadCoreRegistryFromPath(path)
		if err != nil {
			return false
		}
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
