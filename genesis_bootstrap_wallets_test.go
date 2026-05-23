package main

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGenesisBootstrapTestFile(t *testing.T, g Genesis) string {
	t.Helper()

	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatalf("marshal genesis: %v", err)
	}

	path := filepath.Join(t.TempDir(), "genesis.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write genesis: %v", err)
	}

	return path
}

func testGenesisBootstrapWallet(seed byte, chainID string) (string, string) {
	pub := strictActivationTestPub(seed)
	return hex.EncodeToString(pub), AddressFromPublicKeyForChain(pub, chainID)
}

func TestGenesisBootstrapStakeAndAutoAuthLoad(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	chainID := "genesis-bootstrap-test"
	walletPubHex, walletAddr := testGenesisBootstrapWallet(91, chainID)

	genesis := Genesis{
		ChainID: chainID,
		Validators: map[string]string{
			"A": hex.EncodeToString(strictActivationTestPub(11)),
		},
		RewardWallets: map[string]string{
			"A": walletAddr,
		},
		GenesisStakes: map[string]GenesisStake{
			"A": {
				WalletPubKey: walletPubHex,
				Amount:       int(ValidatorMinStake),
			},
		},
	}

	path := writeGenesisBootstrapTestFile(t, genesis)
	GenesisHashExpected = ""
	loaded, err := loadGenesisFromDisk(path)
	if err != nil {
		t.Fatalf("loadGenesisFromDisk failed: %v", err)
	}

	stakeSpec, ok := loaded.GenesisStakes["A"]
	if !ok {
		t.Fatalf("expected normalized genesis stake for A")
	}
	if !addressesEqual(stakeSpec.Wallet, walletAddr) {
		t.Fatalf("unexpected normalized wallet: got=%s want=%s", stakeSpec.Wallet, walletAddr)
	}
	if stakeSpec.LockEpochs != DefaultStakeLockEpochs {
		t.Fatalf("expected default lock epochs, got=%d want=%d", stakeSpec.LockEpochs, DefaultStakeLockEpochs)
	}

	if got, ok := genesisRewardWallet("A"); !ok || !addressesEqual(got, walletAddr) {
		t.Fatalf("expected genesis reward wallet binding, got=%q ok=%v", got, ok)
	}
	if binding, ok := genesisBootstrapWalletBindingForValidator("A"); !ok || !addressesEqual(binding.WalletAddr, walletAddr) || binding.WalletPubKey != walletPubHex {
		t.Fatalf("unexpected bootstrap wallet binding: %+v ok=%v", binding, ok)
	}

	ConfigAuthRequireWallet = true
	ConfigAuthCoreValidators = nil
	setRuntimeCoreValidatorIDs(nil)

	n := makeStrictActivationNode(0)
	empty := NewBlockchain()
	n.Blockchain = &empty
	n.ID = "A"
	n.Ledger = Ledger{}
	if err := n.initGenesisBalancesFromDisk(path); err != nil {
		t.Fatalf("initGenesisBalancesFromDisk failed: %v", err)
	}
	n.bootstrapGenesisValidators(loaded)

	rec, ok := n.Ledger.Stakes[stakeKey(walletAddr, "A")]
	if !ok {
		t.Fatalf("expected genesis stake record for A")
	}
	if rec.Amount != int(ValidatorMinStake) {
		t.Fatalf("unexpected staked amount: got=%d want=%d", rec.Amount, int(ValidatorMinStake))
	}
	if rec.LockedUntil != DefaultStakeLockEpochs {
		t.Fatalf("unexpected stake lock: got=%d want=%d", rec.LockedUntil, DefaultStakeLockEpochs)
	}
	if !n.hasRequiredValidatorStake() {
		t.Fatalf("expected genesis stake to satisfy validator stake gate")
	}
	if resolvedWallet, ok := validatorRewardWallet(&n.Ledger, "A"); !ok || !addressesEqual(resolvedWallet, walletAddr) {
		t.Fatalf("unexpected reward wallet resolution: got=%q ok=%v", resolvedWallet, ok)
	}
	if !n.hasWalletLoginForValidator() {
		t.Fatalf("expected bootstrap auto-auth to satisfy wallet gate before first finalized block")
	}

	n.Blockchain.Blocks = append(n.Blockchain.Blocks, Block{ID: 1})
	if !n.hasWalletLoginForValidator() {
		t.Fatalf("expected genesis-bound wallet auth exemption to remain active after first finalized block")
	}
	if n.requiresWalletAuthCurrent("A") {
		t.Fatalf("expected genesis-bound validator to remain exempt from wallet auth after first finalized block")
	}
	if n.shouldPromptWalletAuthAtStartup() {
		t.Fatalf("expected startup wallet-auth prompt to remain suppressed for genesis-exempt validator")
	}
}

func TestGenesisBootstrapRewardWalletRemainsPinnedPostBootstrap(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	chainID := "genesis-bootstrap-test"
	walletPubHex, walletAddr := testGenesisBootstrapWallet(91, chainID)
	_, otherWalletAddr := testGenesisBootstrapWallet(92, chainID)

	genesis := Genesis{
		ChainID: chainID,
		Validators: map[string]string{
			"A": hex.EncodeToString(strictActivationTestPub(11)),
		},
		RewardWallets: map[string]string{
			"A": walletAddr,
		},
		GenesisStakes: map[string]GenesisStake{
			"A": {
				WalletPubKey: walletPubHex,
				Amount:       int(ValidatorMinStake),
			},
		},
	}

	path := writeGenesisBootstrapTestFile(t, genesis)
	GenesisHashExpected = ""
	loaded, err := loadGenesisFromDisk(path)
	if err != nil {
		t.Fatalf("loadGenesisFromDisk failed: %v", err)
	}

	ConfigAuthRequireWallet = true
	ConfigAuthCoreValidators = nil
	setRuntimeCoreValidatorIDs(nil)

	n := makeStrictActivationNode(0)
	empty := NewBlockchain()
	n.Blockchain = &empty
	n.ID = "A"
	n.Ledger = Ledger{}
	if err := n.initGenesisBalancesFromDisk(path); err != nil {
		t.Fatalf("initGenesisBalancesFromDisk failed: %v", err)
	}
	n.bootstrapGenesisValidators(loaded)

	n.Blockchain.Blocks = append(n.Blockchain.Blocks, Block{ID: 1})
	n.Ledger.Stakes[stakeKey(otherWalletAddr, "A")] = StakeLock{
		ValidatorID: "A",
		Amount:      int(ValidatorMinStake) + 50,
		LockedUntil: DefaultStakeLockEpochs,
	}

	setValidatorRewardWallet(&n.Ledger, "A", otherWalletAddr)
	refreshValidatorRewardWalletBinding(&n.Ledger, "A")

	gotBound := canonicalAddressKey(n.Ledger.ValidatorRewardWallets["A"])
	if !addressesEqual(gotBound, walletAddr) {
		t.Fatalf("expected pinned genesis reward wallet, got=%q want=%q", gotBound, walletAddr)
	}
	if resolvedWallet, ok := validatorRewardWallet(&n.Ledger, "A"); !ok || !addressesEqual(resolvedWallet, walletAddr) {
		t.Fatalf("expected reward wallet resolution to remain pinned to genesis wallet, got=%q ok=%v", resolvedWallet, ok)
	}
}

func TestGenesisBootstrapStakeRejectsWalletPubKeyMismatch(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	chainID := "genesis-bootstrap-test"
	_, walletAddr := testGenesisBootstrapWallet(91, chainID)
	wrongWalletPubHex, _ := testGenesisBootstrapWallet(92, chainID)

	genesis := Genesis{
		ChainID: chainID,
		Validators: map[string]string{
			"A": hex.EncodeToString(strictActivationTestPub(11)),
		},
		RewardWallets: map[string]string{
			"A": walletAddr,
		},
		GenesisStakes: map[string]GenesisStake{
			"A": {
				WalletPubKey: wrongWalletPubHex,
				Amount:       int(ValidatorMinStake),
			},
		},
	}

	path := writeGenesisBootstrapTestFile(t, genesis)
	GenesisHashExpected = ""
	if _, err := loadGenesisFromDisk(path); err == nil || !strings.Contains(err.Error(), "wallet_pubkey does not match wallet") {
		t.Fatalf("expected wallet/pubkey mismatch error, got=%v", err)
	}
}

func TestGenesisBootstrapStakeRejectsMissingWalletSource(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	chainID := "genesis-bootstrap-test"
	walletPubHex, _ := testGenesisBootstrapWallet(91, chainID)

	genesis := Genesis{
		ChainID: chainID,
		Validators: map[string]string{
			"A": hex.EncodeToString(strictActivationTestPub(11)),
		},
		GenesisStakes: map[string]GenesisStake{
			"A": {
				WalletPubKey: walletPubHex,
				Amount:       int(ValidatorMinStake),
			},
		},
	}

	path := writeGenesisBootstrapTestFile(t, genesis)
	GenesisHashExpected = ""
	if _, err := loadGenesisFromDisk(path); err == nil || !strings.Contains(err.Error(), "missing wallet and reward wallet") {
		t.Fatalf("expected missing wallet source error, got=%v", err)
	}
}

func TestStartupWalletAuthPromptUsesCurrentPolicy(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	ConfigAuthRequireWallet = true
	ConfigAuthCoreValidators = nil
	setRuntimeCoreValidatorIDs(nil)

	n := makeStrictActivationNode(0)
	n.ID = "F"
	n.Role = "validator"
	if !n.requiresWalletAuthCurrent("F") {
		t.Fatalf("expected non-exempt validator to require wallet auth")
	}
	if !n.shouldPromptWalletAuthAtStartup() {
		t.Fatalf("expected startup wallet-auth prompt for non-exempt validator")
	}
}

func TestExistingChainDoesNotSynthesizeGenesisBootstrapStake(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	chainID := "genesis-bootstrap-test"
	walletPubHex, walletAddr := testGenesisBootstrapWallet(91, chainID)

	genesis := Genesis{
		ChainID: chainID,
		Validators: map[string]string{
			"A": hex.EncodeToString(strictActivationTestPub(11)),
		},
		RewardWallets: map[string]string{
			"A": walletAddr,
		},
		GenesisStakes: map[string]GenesisStake{
			"A": {
				WalletPubKey: walletPubHex,
				Amount:       int(ValidatorMinStake),
			},
		},
	}

	path := writeGenesisBootstrapTestFile(t, genesis)
	GenesisHashExpected = ""

	n := makeStrictActivationNode(50)
	n.ID = "A"
	n.Ledger = GenesisLedger()
	if err := n.initGenesisBalancesFromDisk(path); err != nil {
		t.Fatalf("initGenesisBalancesFromDisk failed: %v", err)
	}

	if _, ok := n.Ledger.Stakes[stakeKey(walletAddr, "A")]; ok {
		t.Fatalf("did not expect existing-chain ledger to synthesize genesis bootstrap stake")
	}
}
