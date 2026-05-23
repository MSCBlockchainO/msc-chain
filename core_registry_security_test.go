package main

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type coreSecuritySnapshot struct {
	coreIDs                    []string
	coreEnvAllowed             bool
	allowEnvInProd             bool
	corePasswordFile           string
	passwordMode               string
	isTestnet                  bool
	corePendingExcluded        bool
	coreActivationHeightBuffer uint64
}

func snapshotCoreSecurityConfig() coreSecuritySnapshot {
	return coreSecuritySnapshot{
		coreIDs:                    append([]string{}, ConfigAuthCoreValidators...),
		coreEnvAllowed:             ValidatorCoreEnvPasswordAllowed,
		allowEnvInProd:             ValidatorAllowEnvPasswordInProduction,
		corePasswordFile:           ValidatorCorePasswordFile,
		passwordMode:               ValidatorPasswordMode,
		isTestnet:                  IsTestnet,
		corePendingExcluded:        ConsensusCorePendingExcludedFromProposer,
		coreActivationHeightBuffer: ConsensusCoreActivationEffectiveHeightBuffer,
	}
}

func (s coreSecuritySnapshot) restore() {
	ConfigAuthCoreValidators = append([]string{}, s.coreIDs...)
	ValidatorCoreEnvPasswordAllowed = s.coreEnvAllowed
	ValidatorAllowEnvPasswordInProduction = s.allowEnvInProd
	ValidatorCorePasswordFile = s.corePasswordFile
	ValidatorPasswordMode = s.passwordMode
	IsTestnet = s.isTestnet
	ConsensusCorePendingExcludedFromProposer = s.corePendingExcluded
	ConsensusCoreActivationEffectiveHeightBuffer = s.coreActivationHeightBuffer
	setRuntimeCoreValidatorIDs(nil)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func signRegistryHashHex(t *testing.T, hashHex string, priv ed25519.PrivateKey) string {
	t.Helper()
	payloadHashBytes, err := hex.DecodeString(hashHex)
	if err != nil {
		t.Fatalf("decode payload hash: %v", err)
	}
	return hex.EncodeToString(ed25519.Sign(priv, payloadHashBytes))
}

func TestVerifyCoreRegistryDocumentThreshold(t *testing.T) {
	pubA, privA, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate key A: %v", err)
	}
	pubB, privB, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate key B: %v", err)
	}
	pubC, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate key C: %v", err)
	}

	reg := CoreRegistry{
		ChainID:         "91938",
		Version:         1,
		Epoch:           2,
		EffectiveHeight: 100,
		Validators: []CoreRegistryEntry{
			{ID: "A", RequiredKeyFingerprint: "aa11", ConsensusPubKey: hex.EncodeToString(pubA), Status: "active"},
			{ID: "B", RequiredKeyFingerprint: "bb22", ConsensusPubKey: hex.EncodeToString(pubB), Status: "active"},
			{ID: "C", RequiredKeyFingerprint: "cc33", ConsensusPubKey: hex.EncodeToString(pubC), Status: "pending"},
		},
	}
	reg.PayloadHash = coreRegistryPayloadHash(reg)
	reg.Signatures = []CoreRegistrySignature{
		{
			SignerID:     "A",
			SignerPubKey: hex.EncodeToString(pubA),
			SigHex:       signRegistryHashHex(t, reg.PayloadHash, privA),
		},
		{
			SignerID:     "B",
			SignerPubKey: hex.EncodeToString(pubB),
			SigHex:       signRegistryHashHex(t, reg.PayloadHash, privB),
		},
	}

	authority := map[string]ed25519.PublicKey{
		"A": pubA,
		"B": pubB,
		"C": pubC,
	}
	hash, signers, err := verifyCoreRegistryDocument(reg, authority, []string{"A", "B", "C"}, 0)
	if err != nil {
		t.Fatalf("verify registry: %v", err)
	}
	if hash != reg.PayloadHash {
		t.Fatalf("payload hash mismatch: got=%s want=%s", hash, reg.PayloadHash)
	}
	if len(signers) != 2 {
		t.Fatalf("valid signer count mismatch: got=%d want=2", len(signers))
	}

	reg.Signatures = reg.Signatures[:1]
	if _, _, err := verifyCoreRegistryDocument(reg, authority, []string{"A", "B", "C"}, 0); err == nil {
		t.Fatalf("expected threshold failure with one signature")
	}

	reg.Signatures = []CoreRegistrySignature{
		{
			SignerID:     "A",
			SignerPubKey: hex.EncodeToString(pubA),
			SigHex:       signRegistryHashHex(t, reg.PayloadHash, privA),
		},
		{
			SignerID:     "B",
			SignerPubKey: hex.EncodeToString(pubB),
			SigHex:       signRegistryHashHex(t, reg.PayloadHash, privB),
		},
	}
	reg.Validators[0].Status = "retired"
	if _, _, err := verifyCoreRegistryDocument(reg, authority, []string{"A", "B", "C"}, 0); err == nil {
		t.Fatalf("expected payload mismatch after tamper")
	}
}

func TestSignedCoreRegistrySeedsPeerAddresses(t *testing.T) {
	oldAddrBook := map[string]string{}
	ValidatorAddrBook.mu.Lock()
	for id, addr := range ValidatorAddrBook.m {
		oldAddrBook[id] = addr
	}
	ValidatorAddrBook.m = make(map[string]string)
	ValidatorAddrBook.mu.Unlock()
	defer func() {
		ValidatorAddrBook.mu.Lock()
		ValidatorAddrBook.m = oldAddrBook
		ValidatorAddrBook.mu.Unlock()
	}()

	n := &Node{
		ID:      "F",
		DataDir: t.TempDir(),
		Config:  &NodeConfig{},
	}
	if err := os.MkdirAll(nodeDataPath(n.DataDir, n.ID), 0o700); err != nil {
		t.Fatalf("create node data dir: %v", err)
	}
	entries := map[string]CoreRegistryEntry{
		"A": {
			ID:      "A",
			P2PSeed: "/ip4/127.0.0.1/tcp/7001/p2p/12D3KooWSjgBtznLkWFcuKkib3o4GAxxREFUPrRtcYqwTAUruXJo",
		},
		"B": {
			ID:      "B",
			P2PSeed: "/ip4/127.0.0.1/tcp/7002",
		},
		"C": {
			ID:      "C",
			P2PSeed: "/ip4/127.0.0.1/tcp/7003/p2p/not_a_peer_id",
		},
	}
	seeded := n.ingestCoreRegistryPeerSeeds(entries)
	if seeded != 1 {
		t.Fatalf("expected exactly one valid registry seed to be ingested, got=%d", seeded)
	}

	ValidatorAddrBook.mu.RLock()
	gotA := strings.TrimSpace(ValidatorAddrBook.m["A"])
	_, hasB := ValidatorAddrBook.m["B"]
	_, hasC := ValidatorAddrBook.m["C"]
	ValidatorAddrBook.mu.RUnlock()
	if gotA == "" {
		t.Fatalf("expected validator A peer seed to be loaded into validator address book")
	}
	if hasB || hasC {
		t.Fatalf("expected invalid registry seeds to be rejected, got B=%t C=%t", hasB, hasC)
	}
}

func TestCoreConsensusFilterPendingExcluded(t *testing.T) {
	snap := snapshotCoreSecurityConfig()
	defer snap.restore()

	ConsensusCorePendingExcludedFromProposer = true
	n := &Node{
		ID:         "E",
		Blockchain: &Blockchain{Blocks: []Block{{ID: 1}}},
		coreRegistryEntries: map[string]CoreRegistryEntry{
			"A": {ID: "A", Status: "active"},
			"E": {ID: "E", Status: "pending"},
		},
		coreRegistryState: CoreRegistryState{
			Verified:        true,
			EffectiveHeight: 10,
		},
	}

	filtered := n.applyCoreConsensusFilter(100, []string{"A", "E", "X"})
	if !containsString(filtered, "A") || !containsString(filtered, "E") || !containsString(filtered, "X") {
		t.Fatalf("expected post-bootstrap filter to keep validator set unchanged, got=%v", filtered)
	}

	ConsensusCorePendingExcludedFromProposer = false
	filtered = n.applyCoreConsensusFilter(100, []string{"A", "E", "X"})
	if !containsString(filtered, "A") || !containsString(filtered, "E") || !containsString(filtered, "X") {
		t.Fatalf("expected post-bootstrap filter to remain unchanged, got=%v", filtered)
	}
}

func TestCoreEligibleForConsensusHonorsEffectiveHeightBuffer(t *testing.T) {
	snap := snapshotCoreSecurityConfig()
	defer snap.restore()

	ConsensusCorePendingExcludedFromProposer = true
	ConsensusCoreActivationEffectiveHeightBuffer = 64
	n := &Node{
		ID:         "E",
		Blockchain: &Blockchain{Blocks: []Block{{ID: 1}}},
		coreRegistryEntries: map[string]CoreRegistryEntry{
			"E": {ID: "E", Status: "active"},
		},
		coreRegistryState: CoreRegistryState{
			Verified:        true,
			EffectiveHeight: 10,
		},
	}

	if !n.coreEligibleForConsensus(10) {
		t.Fatalf("expected post-bootstrap node to be eligible without core pending filter")
	}
	if !n.coreEligibleForConsensus(74) {
		t.Fatalf("expected post-bootstrap node to remain eligible")
	}
}

func TestCoreEnvPasswordBlockedByPolicy(t *testing.T) {
	snap := snapshotCoreSecurityConfig()
	defer snap.restore()

	setRuntimeCoreValidatorIDs(nil)
	ConfigAuthCoreValidators = []string{"A"}
	ValidatorCoreEnvPasswordAllowed = false
	ValidatorAllowEnvPasswordInProduction = false
	IsTestnet = true
	t.Setenv(validatorPasswordEnv, "StrongPass123!")

	pass, fromEnv, err := getValidatorPasswordWithSource("A", t.TempDir())
	if err == nil {
		t.Fatalf("expected env password to be blocked for core validator")
	}
	if pass != "" {
		t.Fatalf("expected empty password on blocked env source")
	}
	if !fromEnv {
		t.Fatalf("expected blocked source to be env")
	}
	if !coreEnvPasswordBlocked("A") {
		t.Fatalf("expected core env-password blocked status to be true")
	}

	pass, fromEnv, err = getValidatorPasswordWithSource("Z", t.TempDir())
	if err != nil {
		t.Fatalf("non-core env password should pass in test mode: %v", err)
	}
	if pass != "StrongPass123!" || !fromEnv {
		t.Fatalf("unexpected non-core password source result pass=%q fromEnv=%t", pass, fromEnv)
	}
}

func TestCorePasswordFileRequiresPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not reliable on Windows ACL")
	}

	snap := snapshotCoreSecurityConfig()
	defer snap.restore()

	setRuntimeCoreValidatorIDs(nil)
	ConfigAuthCoreValidators = []string{"A"}
	ValidatorCoreEnvPasswordAllowed = false
	ValidatorAllowEnvPasswordInProduction = false
	IsTestnet = true

	originalEnv, hadEnv := os.LookupEnv(validatorPasswordEnv)
	if hadEnv {
		defer os.Setenv(validatorPasswordEnv, originalEnv)
	} else {
		defer os.Unsetenv(validatorPasswordEnv)
	}
	_ = os.Unsetenv(validatorPasswordEnv)

	passwordFile := t.TempDir() + string(os.PathSeparator) + "core.pass"
	if err := os.WriteFile(passwordFile, []byte("StrongPass123!"), 0o644); err != nil {
		t.Fatalf("write password file: %v", err)
	}
	ValidatorCorePasswordFile = passwordFile

	_, _, err := getValidatorPasswordWithSource("A", t.TempDir())
	if err == nil {
		t.Fatalf("expected permission check failure for core password file")
	}
	if !strings.Contains(err.Error(), "must be private") {
		t.Fatalf("unexpected permission check error: %v", err)
	}
}

func TestCorePasswordAutoDiscoveryUsesNodePaths(t *testing.T) {
	snap := snapshotCoreSecurityConfig()
	defer snap.restore()

	setRuntimeCoreValidatorIDs(nil)
	ConfigAuthCoreValidators = []string{"A"}
	ValidatorCoreEnvPasswordAllowed = false
	ValidatorAllowEnvPasswordInProduction = false
	ValidatorCorePasswordFile = ""
	IsTestnet = true

	originalEnv, hadEnv := os.LookupEnv(validatorPasswordEnv)
	if hadEnv {
		defer os.Setenv(validatorPasswordEnv, originalEnv)
	} else {
		defer os.Unsetenv(validatorPasswordEnv)
	}
	_ = os.Unsetenv(validatorPasswordEnv)

	root := t.TempDir()
	nodePath := filepath.Join(root, "node_A")
	if err := os.MkdirAll(nodePath, 0o700); err != nil {
		t.Fatalf("mkdir node path: %v", err)
	}
	autoPass := filepath.Join(root, "validator.pass")
	if err := os.WriteFile(autoPass, []byte("StrongPass123!"), 0o600); err != nil {
		t.Fatalf("write auto password file: %v", err)
	}

	pass, fromEnv, err := getValidatorPasswordWithSource("A", nodePath)
	if err != nil {
		t.Fatalf("expected auto password file lookup to succeed: %v", err)
	}
	if fromEnv {
		t.Fatalf("expected file source, got env")
	}
	if pass != "StrongPass123!" {
		t.Fatalf("unexpected password: %q", pass)
	}
}

func TestCorePasswordAutoDiscoveryUsesCentralSecretsPath(t *testing.T) {
	snap := snapshotCoreSecurityConfig()
	defer snap.restore()

	setRuntimeCoreValidatorIDs(nil)
	ConfigAuthCoreValidators = []string{"A"}
	ValidatorCoreEnvPasswordAllowed = false
	ValidatorAllowEnvPasswordInProduction = false
	ValidatorCorePasswordFile = ""
	IsTestnet = true

	originalEnv, hadEnv := os.LookupEnv(validatorPasswordEnv)
	if hadEnv {
		defer os.Setenv(validatorPasswordEnv, originalEnv)
	} else {
		defer os.Unsetenv(validatorPasswordEnv)
	}
	_ = os.Unsetenv(validatorPasswordEnv)

	tempHome := t.TempDir()
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("HOME", tempHome)
	secretDir := filepath.Join(tempHome, ".msc-secrets")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatalf("mkdir secrets dir: %v", err)
	}
	centralPass := filepath.Join(secretDir, "A.pass")
	if err := os.WriteFile(centralPass, []byte("StrongPass123!"), 0o600); err != nil {
		t.Fatalf("write central pass file: %v", err)
	}

	nodePath := filepath.Join(t.TempDir(), "node_A")
	if err := os.MkdirAll(nodePath, 0o700); err != nil {
		t.Fatalf("mkdir node path: %v", err)
	}

	pass, fromEnv, err := getValidatorPasswordWithSource("A", nodePath)
	if err != nil {
		t.Fatalf("expected central auto password file lookup to succeed: %v", err)
	}
	if fromEnv {
		t.Fatalf("expected file source, got env")
	}
	if pass != "StrongPass123!" {
		t.Fatalf("unexpected password: %q", pass)
	}
}

func TestCorePromptOnlyBlocksEnvAndRequiresPrompt(t *testing.T) {
	snap := snapshotCoreSecurityConfig()
	defer snap.restore()

	setRuntimeCoreValidatorIDs(nil)
	ConfigAuthCoreValidators = []string{"A"}
	ValidatorPasswordMode = validatorPasswordModePromptOnly
	ValidatorCoreEnvPasswordAllowed = false
	ValidatorAllowEnvPasswordInProduction = false
	ValidatorCorePasswordFile = filepath.Join(t.TempDir(), "ignored.pass")
	IsTestnet = true

	t.Setenv(validatorPasswordEnv, "StrongPass123!")
	pass, fromEnv, err := getValidatorPasswordWithSource("A", t.TempDir())
	if err == nil {
		t.Fatalf("expected env to be blocked in prompt_only mode")
	}
	if !fromEnv {
		t.Fatalf("expected blocked source to be env")
	}
	if pass != "" {
		t.Fatalf("expected empty password on blocked env")
	}
	if !strings.Contains(err.Error(), "env_blocked_core_prompt_only") {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = os.Unsetenv(validatorPasswordEnv)
	_, fromEnv, err = getValidatorPasswordWithSource("A", t.TempDir())
	if err == nil {
		t.Fatalf("expected prompt requirement failure in non-interactive test")
	}
	if fromEnv {
		t.Fatalf("expected non-env failure when env is empty")
	}
	if !strings.Contains(err.Error(), "prompt_required_non_interactive") {
		t.Fatalf("unexpected error for prompt_only non-interactive path: %v", err)
	}
}

func TestCoreEnvOnlyOverrideUsesEnv(t *testing.T) {
	snap := snapshotCoreSecurityConfig()
	defer snap.restore()

	setRuntimeCoreValidatorIDs(nil)
	ConfigAuthCoreValidators = []string{"A"}
	ValidatorPasswordMode = validatorPasswordModePromptOnly
	ValidatorCoreEnvPasswordAllowed = false
	ValidatorAllowEnvPasswordInProduction = false
	IsTestnet = true

	t.Setenv(validatorPasswordModeEnv, validatorPasswordModeEnvOnly)
	t.Setenv(validatorPasswordEnv, "StrongPass123!")
	pass, fromEnv, err := getValidatorPasswordWithSource("A", t.TempDir())
	if err != nil {
		t.Fatalf("expected env_only override to succeed: %v", err)
	}
	if !fromEnv {
		t.Fatalf("expected env source in env_only mode")
	}
	if pass != "StrongPass123!" {
		t.Fatalf("unexpected password: %q", pass)
	}
}

func TestCoreEnvOnlyOverrideMissingPasswordFails(t *testing.T) {
	snap := snapshotCoreSecurityConfig()
	defer snap.restore()

	setRuntimeCoreValidatorIDs(nil)
	ConfigAuthCoreValidators = []string{"A"}
	ValidatorPasswordMode = validatorPasswordModePromptOnly
	ValidatorCoreEnvPasswordAllowed = false
	ValidatorAllowEnvPasswordInProduction = false
	IsTestnet = true

	t.Setenv(validatorPasswordModeEnv, validatorPasswordModeEnvOnly)
	t.Setenv(validatorPasswordEnv, "")
	_, fromEnv, err := getValidatorPasswordWithSource("A", t.TempDir())
	if err == nil {
		t.Fatalf("expected missing env password failure in env_only mode")
	}
	if !fromEnv {
		t.Fatalf("expected env source failure")
	}
	if !strings.Contains(err.Error(), "env_only_password_missing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCoreEnvPasswordModeInvalidFails(t *testing.T) {
	snap := snapshotCoreSecurityConfig()
	defer snap.restore()

	setRuntimeCoreValidatorIDs(nil)
	ConfigAuthCoreValidators = []string{"A"}
	ValidatorPasswordMode = validatorPasswordModePromptOnly
	IsTestnet = true

	t.Setenv(validatorPasswordModeEnv, "bogus")
	t.Setenv(validatorPasswordEnv, "StrongPass123!")
	_, _, err := getValidatorPasswordWithSource("A", t.TempDir())
	if err == nil {
		t.Fatalf("expected invalid env password mode to fail")
	}
	if !strings.Contains(err.Error(), "env_password_mode_invalid") {
		t.Fatalf("unexpected error: %v", err)
	}
}
