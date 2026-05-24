package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

const (
	productionChainID          = "91938"
	productionGenesisPath      = "genesis.json"
	productionGenesisSHA256    = "758c62b26cb50ce80450684ae86bdf5681e37776e1c73309169e27dd3d14e71b"
	productionStakeAmount      = 100
	productionStakeLockEpochs  = uint64(19872000)
	productionLedgerTotalCoins = 5977385341
)

var productionValidators = map[string]string{
	"A": "ee8d74edce9d8b17f814be3d76eb8b1c47ea4aec85db9d0b69eb1c6d3123e897",
	"B": "fa810f44ad831ed6be3ab7e1ccece48972eb2572d521369f9f4055a9972d3932",
	"C": "0f71ba143c9a7b2f614733888774c6113aea766402ad5e2c2848af205446fd3a",
	"D": "d6766aec7323b5d425bdb861ee3b8b34794fd07bed9a6b92606c64ad18e28ce8",
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
	"USER_REW":   1000000,
	"TREASURY":   4596911801,
	"FOUNDATION": 1379073540,
	"A":          100000,
	"B":          100000,
	"C":          100000,
	"D":          100000,
}

var forbiddenTemporaryGenesisNeedles = []string{
	"f180a970",
	"bbd7aac5",
	"d3d2c0",
	"e26e212",
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
	assertStringMapExact(t, "validators", genesis.Validators, productionValidators)
	assertStringMapExact(t, "reward wallets", genesis.RewardWallets, productionRewardWallets)
	assertIntMapExact(t, "balances", genesis.Balances, productionBalances)

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
	varSource, err := os.ReadFile("var.go")
	if err != nil {
		t.Fatalf("read var.go: %v", err)
	}
	wantDefault := `var GenesisHashExpected = "` + productionGenesisSHA256 + `"`
	if !strings.Contains(string(varSource), wantDefault) {
		t.Fatalf("compiled genesis hash default is not pinned to %s", productionGenesisSHA256)
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
