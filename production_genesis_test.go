package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	productionChainID          = "91938"
	productionGenesisPath      = "genesis.json"
	productionGenesisSHA256    = "d6d7d96ea1a70d2aca31389ce7ef7953794ce77b4c933828295269702768fa3c"
	productionStakeAmount      = 100
	productionStakeLockEpochs  = uint64(19872000)
	productionLedgerTotalCoins = 5977385341
)

var productionValidators = map[string]string{
	"A": "f180a970fa11c67b961d79b9fe4cd362da47e5f2816ab1654d4032af0b23658b",
	"B": "bbd7aac5cf70150dd2565a67342950e79f7eeb7a3fbd2ebc353b1d95302d0a88",
	"C": "d3d2c0a3201f85f83c857103803915200616378263e48da0fe973e7e6ff6fa88",
	"D": "e26e21281f1adf98dfde8c76cd858edd21b0c323e55f6bd80623bb0354eafec4",
}

var productionRewardWallets = map[string]string{
	"A": "MSC017d78d2c1920db5321271a2d594a4995a3c5ba99d",
	"B": "MSC01102bdf87789381354be6ec8af1f49688306ea83c",
	"C": "MSC01dc7b2c81d1211199f209a52a9688a31352f3b800",
	"D": "MSC01d8f4952c11e683aac3cf6652513cd90982e4a938",
}

var productionStakeWalletPubKeys = map[string]string{
	"A": "3adadb92850e85603bf122c0bc757987ec633945d5773f4ccc28853e7a9a5978",
	"B": "e985a04375642887373ffb1b217843ca294d425d53d6ef6c7c86872534618e6f",
	"C": "c5f1f3c40667f8430ed528f2176e68f3cb889292aa0ade25df4ecbdc86b217c8",
	"D": "acaf8386bd82afa3e2867b3f2a10580a076d35514bc5d1f62b0866d4df53eff7",
}

var productionBalances = map[string]int{
	"MSC017d78d2c1920db5321271a2d594a4995a3c5ba99d": 1379173540,
	"MSC01102bdf87789381354be6ec8af1f49688306ea83c": 4597011801,
	"MSC01dc7b2c81d1211199f209a52a9688a31352f3b800": 100000,
	"MSC01d8f4952c11e683aac3cf6652513cd90982e4a938": 100000,
	"USER_REW": 1000000,
}

var forbiddenTemporaryGenesisNeedles = []string{
	"a8f8f4f6",
}

func TestProductionGenesisFreeze(t *testing.T) {
	raw, err := os.ReadFile(productionGenesisPath)
	if err != nil {
		t.Fatalf("read production genesis: %v", err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != productionGenesisSHA256 {
		t.Fatalf("production genesis hash changed: got=%s want=%s", got, productionGenesisSHA256)
	}

	lowerRaw := strings.ToLower(string(raw))
	for _, needle := range forbiddenTemporaryGenesisNeedles {
		if strings.Contains(lowerRaw, needle) {
			t.Fatalf("production genesis contains forbidden temporary key/hash fragment %q", needle)
		}
	}

	var genesis Genesis
	if err := json.Unmarshal(raw, &genesis); err != nil {
		t.Fatalf("decode production genesis: %v", err)
	}

	if genesis.ChainID != productionChainID {
		t.Fatalf("chain id = %q, want %q", genesis.ChainID, productionChainID)
	}
	if genesis.Decimals != CoinDecimals {
		t.Fatalf("genesis decimals = %d, want %d", genesis.Decimals, CoinDecimals)
	}
	if !genesis.GenesisLocked {
		t.Fatalf("production genesis must be locked")
	}
	if !genesis.ValidatorSetFrozen {
		t.Fatalf("production validator set must be frozen")
	}
	assertStringMapExact(t, "validators", genesis.Validators, productionValidators)
	assertStringMapExact(t, "reward wallets", genesis.RewardWallets, productionRewardWallets)
	assertIntMapExact(t, "balances", genesis.Balances, productionBalances)

	if !addressesEqual(genesis.Foundation.Wallet, productionRewardWallets["A"]) {
		t.Fatalf("foundation wallet = %q, want %q", genesis.Foundation.Wallet, productionRewardWallets["A"])
	}
	if genesis.Foundation.Allocation != 1379073540 || !genesis.Foundation.Locked || genesis.Foundation.LockEpochs != productionStakeLockEpochs {
		t.Fatalf("unexpected foundation allocation/lock: %+v", genesis.Foundation)
	}
	if !addressesEqual(genesis.Treasury.Wallet, productionRewardWallets["B"]) {
		t.Fatalf("treasury wallet = %q, want %q", genesis.Treasury.Wallet, productionRewardWallets["B"])
	}
	if genesis.Treasury.Allocation != 4596911801 || !genesis.Treasury.Locked || !genesis.Treasury.GovernanceOnly {
		t.Fatalf("unexpected treasury allocation/lock: %+v", genesis.Treasury)
	}

	total := 0
	for _, amount := range genesis.Balances {
		total += amount
	}
	if total != productionLedgerTotalCoins {
		t.Fatalf("genesis balance total = %d, want %d", total, productionLedgerTotalCoins)
	}

	if got, want := sortedStringKeys(genesis.GenesisStakes), sortedStringKeys(productionValidators); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("genesis stake validators = %v, want %v", got, want)
	}
	for id, stake := range genesis.GenesisStakes {
		if stake.Wallet != productionRewardWallets[id] {
			t.Fatalf("validator %s stake wallet = %q, want %q", id, stake.Wallet, productionRewardWallets[id])
		}
		if stake.WalletPubKey != productionStakeWalletPubKeys[id] {
			t.Fatalf("validator %s wallet pubkey = %q, want %q", id, stake.WalletPubKey, productionStakeWalletPubKeys[id])
		}
		if stake.Amount != productionStakeAmount {
			t.Fatalf("validator %s stake amount = %d, want %d", id, stake.Amount, productionStakeAmount)
		}
		if stake.LockEpochs != productionStakeLockEpochs {
			t.Fatalf("validator %s stake lock = %d, want %d", id, stake.LockEpochs, productionStakeLockEpochs)
		}
	}
}

func TestProductionGenesisConfigLock(t *testing.T) {
	var cfg struct {
		Chain struct {
			ChainID     string `toml:"chain_id"`
			GenesisPath string `toml:"genesis_path"`
			GenesisHash string `toml:"genesis_hash"`
		} `toml:"chain"`
		Validators struct {
			ActiveSetSize                  int `toml:"active_set_size"`
			OnboardingMaxNewSlots          int `toml:"onboarding_max_new_slots"`
			OnboardingBootstrapMaxNewSlots int `toml:"onboarding_bootstrap_max_new_slots"`
		} `toml:"validators"`
	}
	if _, err := toml.DecodeFile("config.toml", &cfg); err != nil {
		t.Fatalf("decode config.toml: %v", err)
	}
	if cfg.Chain.ChainID != productionChainID {
		t.Fatalf("config chain_id = %q, want %q", cfg.Chain.ChainID, productionChainID)
	}
	if cfg.Chain.GenesisPath != productionGenesisPath {
		t.Fatalf("config genesis_path = %q, want %q", cfg.Chain.GenesisPath, productionGenesisPath)
	}
	if strings.ToLower(strings.TrimSpace(cfg.Chain.GenesisHash)) != productionGenesisSHA256 {
		t.Fatalf("config genesis_hash = %q, want %q", cfg.Chain.GenesisHash, productionGenesisSHA256)
	}
	if cfg.Validators.ActiveSetSize < len(productionValidators) {
		t.Fatalf("active_set_size = %d, want at least %d genesis validators", cfg.Validators.ActiveSetSize, len(productionValidators))
	}
	if cfg.Validators.OnboardingMaxNewSlots != 0 || cfg.Validators.OnboardingBootstrapMaxNewSlots != 0 {
		t.Fatalf("production genesis must not admit unstaked validators automatically: onboarding=%d bootstrap=%d",
			cfg.Validators.OnboardingMaxNewSlots,
			cfg.Validators.OnboardingBootstrapMaxNewSlots)
	}
	varSource, err := os.ReadFile("var.go")
	if err != nil {
		t.Fatalf("read var.go: %v", err)
	}
	wantDefault := `var GenesisHashExpected = "` + productionGenesisSHA256 + `"`
	if !strings.Contains(string(varSource), wantDefault) {
		t.Fatalf("compiled genesis hash default is not pinned to %s", productionGenesisSHA256)
	}
}

func TestProductionGenesisLegacyLoaderReadsStructuredValidators(t *testing.T) {
	genesis := LoadGenesisFile(productionGenesisPath)
	if genesis.ChainID != productionChainID {
		t.Fatalf("legacy loader chain id = %q, want %q", genesis.ChainID, productionChainID)
	}
	assertStringMapExact(t, "legacy loader validators", genesis.Validators, productionValidators)
}

func TestFrozenGenesisRuntimePolicyLocksValidatorSet(t *testing.T) {
	oldLocked := GenesisRuntimeLocked
	oldFrozen := GenesisValidatorSetFrozen
	oldFrozenSize := GenesisFrozenValidatorSetSize
	oldActiveSet := ValidatorActiveSetSize
	oldOnboarding := ValidatorOnboardingMaxNewSlots
	oldBootstrap := ValidatorOnboardingBootstrapMaxNewSlots
	defer func() {
		GenesisRuntimeLocked = oldLocked
		GenesisValidatorSetFrozen = oldFrozen
		GenesisFrozenValidatorSetSize = oldFrozenSize
		ValidatorActiveSetSize = oldActiveSet
		ValidatorOnboardingMaxNewSlots = oldOnboarding
		ValidatorOnboardingBootstrapMaxNewSlots = oldBootstrap
	}()

	ValidatorActiveSetSize = 50
	ValidatorOnboardingMaxNewSlots = 2
	ValidatorOnboardingBootstrapMaxNewSlots = 2
	applyGenesisRuntimePolicy(&Genesis{
		GenesisLocked:      true,
		ValidatorSetFrozen: true,
		Validators:         productionValidators,
	})

	if !GenesisRuntimeLocked || !GenesisValidatorSetFrozen {
		t.Fatalf("expected genesis runtime locks to be enabled")
	}
	if GenesisFrozenValidatorSetSize != len(productionValidators) {
		t.Fatalf("frozen validator set size = %d, want %d", GenesisFrozenValidatorSetSize, len(productionValidators))
	}
	if ValidatorActiveSetSize != 50 {
		t.Fatalf("active set target = %d, want configured growth capacity 50", ValidatorActiveSetSize)
	}
	if ValidatorOnboardingMaxNewSlots != 2 || ValidatorOnboardingBootstrapMaxNewSlots != 2 {
		t.Fatalf("onboarding slots were overwritten: regular=%d bootstrap=%d", ValidatorOnboardingMaxNewSlots, ValidatorOnboardingBootstrapMaxNewSlots)
	}
}

func TestFrozenGenesisConsensusIgnoresIncompleteCommitteeHint(t *testing.T) {
	defer withStakeConsensusPubKeyGlobals(t)()

	oldFrozen := GenesisValidatorSetFrozen
	oldFrozenSize := GenesisFrozenValidatorSetSize
	oldMode := ValidatorActiveSetMode
	oldActiveSet := ValidatorActiveSetSize
	defer func() {
		GenesisValidatorSetFrozen = oldFrozen
		GenesisFrozenValidatorSetSize = oldFrozenSize
		ValidatorActiveSetMode = oldMode
		ValidatorActiveSetSize = oldActiveSet
	}()

	GenesisValidatorSetFrozen = true
	GenesisFrozenValidatorSetSize = len(productionValidators)
	ValidatorActiveSetMode = "adaptive_committee"
	ValidatorActiveSetSize = 50
	registry := make(map[string]ValidatorRecord, len(productionValidators)+1)
	for id, pub := range productionValidators {
		registry[id] = ValidatorRecord{ID: id, Stake: ValidatorMinStake, ConsensusPubKey: pub}
	}
	registry["F"] = ValidatorRecord{ID: "F", Stake: ValidatorMinStake, ConsensusPubKey: strings.Repeat("ab", 32)}
	GlobalValidatorRegistry.Load(registry)

	node := &Node{
		ID:                "A",
		DataDir:           t.TempDir(),
		GenesisValidators: []string{"A", "B", "C", "D"},
	}
	got := node.freezeValidatorSetForHeight(100, []string{"A", "B"})
	want := []string{"A", "B", "C", "D"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("frozen genesis committee = %v, want %v", got, want)
	}

	got = node.freezeValidatorSetForHeight(101, []string{"A", "B", "C", "D", "F"})
	want = []string{"A", "B", "C", "D", "F"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("post-genesis staked transition committee = %v, want %v", got, want)
	}

	if !activeSetModeAdaptiveCommittee() {
		t.Fatalf("adaptive committee mode should remain available after genesis bootstrap")
	}
}

func TestFrozenGenesisAllowsStakedNonCorePendingActivation(t *testing.T) {
	defer withStakeConsensusPubKeyGlobals(t)()

	oldFrozen := GenesisValidatorSetFrozen
	oldFrozenSize := GenesisFrozenValidatorSetSize
	oldActiveSet := ValidatorActiveSetSize
	oldDeterministic := DeterministicValidatorSelection
	defer func() {
		GenesisValidatorSetFrozen = oldFrozen
		GenesisFrozenValidatorSetSize = oldFrozenSize
		ValidatorActiveSetSize = oldActiveSet
		DeterministicValidatorSelection = oldDeterministic
	}()

	GenesisValidatorSetFrozen = true
	GenesisFrozenValidatorSetSize = len(productionValidators)
	ValidatorActiveSetSize = 50
	DeterministicValidatorSelection = true
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"F": {
			ID:              "F",
			Stake:           ValidatorMinStake,
			ConsensusPubKey: strings.Repeat("ab", 32),
			JoinHeight:      25,
		},
	})

	chain := NewBlockchain()
	chain.Blocks = append(chain.Blocks, Block{ID: 30, Signatures: []string{"A", "B", "C", "D"}})
	node := &Node{
		ID:                       "A",
		Blockchain:               &chain,
		DataDir:                  t.TempDir(),
		GenesisValidators:        []string{"A", "B", "C", "D"},
		candidates:               map[string]*CandidateStatus{"F": {ID: "F", LastHeartbeatAt: time.Now()}},
		pendingValidators:        map[string]uint64{},
		pendingValidatorRemovals: map[string]uint64{},
	}
	node.queuePendingValidator("F", 25)
	if got := node.onboardingPendingAddHeight("F"); got != 25 {
		t.Fatalf("staked non-core pending activation height = %d, want 25", got)
	}
}

func TestFrozenGenesisRecoveryQueuesExistingStakedNonCore(t *testing.T) {
	defer withStakeConsensusPubKeyGlobals(t)()

	oldFrozen := GenesisValidatorSetFrozen
	oldFrozenSize := GenesisFrozenValidatorSetSize
	oldActiveSet := ValidatorActiveSetSize
	oldDeterministic := DeterministicValidatorSelection
	defer func() {
		GenesisValidatorSetFrozen = oldFrozen
		GenesisFrozenValidatorSetSize = oldFrozenSize
		ValidatorActiveSetSize = oldActiveSet
		DeterministicValidatorSelection = oldDeterministic
	}()

	GenesisValidatorSetFrozen = true
	GenesisFrozenValidatorSetSize = len(productionValidators)
	ValidatorActiveSetSize = 50
	DeterministicValidatorSelection = true

	registry := make(map[string]ValidatorRecord, len(productionValidators)+1)
	for id, pub := range productionValidators {
		registry[id] = ValidatorRecord{
			ID:              id,
			Stake:           ValidatorMinStake,
			ConsensusPubKey: pub,
			JoinHeight:      1,
		}
	}
	registry["F"] = ValidatorRecord{
		ID:              "F",
		Stake:           ValidatorMinStake,
		ConsensusPubKey: strings.Repeat("ab", 32),
		JoinHeight:      25,
	}
	GlobalValidatorRegistry.Load(registry)

	chain := NewBlockchain()
	chain.Blocks = append(chain.Blocks, Block{ID: 30, Signatures: []string{"A", "B", "C", "D"}})
	node := &Node{
		ID:                       "A",
		Blockchain:               &chain,
		DataDir:                  t.TempDir(),
		GenesisValidators:        []string{"A", "B", "C", "D"},
		candidates:               map[string]*CandidateStatus{"F": {ID: "F", LastHeartbeatAt: time.Now()}},
		pendingValidators:        map[string]uint64{},
		pendingValidatorRemovals: map[string]uint64{},
	}
	node.observeDeterministicOnboardingCandidatesOnCommit(Block{ID: 30})
	if got := node.onboardingPendingAddHeight("F"); got == 0 {
		t.Fatalf("expected existing staked non-core validator to be queued")
	}
}

func assertStringMapExact(t *testing.T, name string, got, want map[string]string) {
	t.Helper()
	if strings.Join(sortedStringKeys(got), ",") != strings.Join(sortedStringKeys(want), ",") {
		t.Fatalf("%s keys = %v, want %v", name, sortedStringKeys(got), sortedStringKeys(want))
	}
	for k, wantValue := range want {
		if got[k] != wantValue {
			t.Fatalf("%s[%s] = %q, want %q", name, k, got[k], wantValue)
		}
	}
}

func assertIntMapExact(t *testing.T, name string, got, want map[string]int) {
	t.Helper()
	if strings.Join(sortedIntMapKeys(got), ",") != strings.Join(sortedIntMapKeys(want), ",") {
		t.Fatalf("%s keys = %v, want %v", name, sortedIntMapKeys(got), sortedIntMapKeys(want))
	}
	for k, wantValue := range want {
		if got[k] != wantValue {
			t.Fatalf("%s[%s] = %d, want %d", name, k, got[k], wantValue)
		}
	}
}

func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedIntMapKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
