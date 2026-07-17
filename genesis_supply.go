package main

import (
	"fmt"
	"sort"
	"strings"
)

type GenesisSupplyBreakdown struct {
	Foundation    int64 `json:"foundation"`
	Treasury      int64 `json:"treasury"`
	ValidatorPool int64 `json:"validator_stake"`
	Community     int64 `json:"community"`
	Ecosystem     int64 `json:"ecosystem"`
	Reserved      int64 `json:"reserved"`
	Total         int64 `json:"total"`
}

// applyGenesisBalanceAllocations applies configured genesis balances as
// transfers from the fixed-supply protocol pools. It must never mint supply.
func applyGenesisBalanceAllocations(ledger *Ledger, genesis *Genesis) error {
	if ledger == nil {
		return fmt.Errorf("nil genesis ledger")
	}
	if genesis == nil {
		return nil
	}

	remappedWallets := make(map[string]struct{}, 2)
	if err := remapGenesisPolicyBucket(ledger, FOUNDATION_ADDRESS, genesis.Foundation.Wallet); err != nil {
		return err
	}
	if wallet := strings.TrimSpace(genesis.Foundation.Wallet); wallet != "" {
		remappedWallets[strings.ToUpper(canonicalAddressKey(wallet))] = struct{}{}
	}
	if err := remapGenesisPolicyBucket(ledger, OWNER_ADDRESS, genesis.Treasury.Wallet); err != nil {
		return err
	}
	if wallet := strings.TrimSpace(genesis.Treasury.Wallet); wallet != "" {
		remappedWallets[strings.ToUpper(canonicalAddressKey(wallet))] = struct{}{}
	}

	validatorWallets := genesisValidatorWalletSet(genesis)
	addresses := make([]string, 0, len(genesis.Balances))
	for addr := range genesis.Balances {
		addresses = append(addresses, addr)
	}
	sort.Slice(addresses, func(i, j int) bool {
		return strings.ToUpper(canonicalAddressKey(addresses[i])) <
			strings.ToUpper(canonicalAddressKey(addresses[j]))
	})

	for _, addr := range addresses {
		target := strings.TrimSpace(addr)
		desired := genesis.Balances[addr]
		if target == "" {
			return fmt.Errorf("genesis balance has empty address")
		}
		if desired < 0 {
			return fmt.Errorf("genesis balance for %s is negative", target)
		}
		if _, remapped := remappedWallets[strings.ToUpper(canonicalAddressKey(target))]; remapped {
			// Foundation and treasury allocations follow the current fixed-supply
			// policy, not stale absolute values embedded in an older genesis file.
			continue
		}

		current := getBalance(*ledger, CoinSymbol, target)
		if current >= desired {
			continue
		}

		sources := genesisGeneralBalanceSources()
		if _, validatorWallet := validatorWallets[strings.ToUpper(canonicalAddressKey(target))]; validatorWallet {
			sources = genesisValidatorBalanceSources()
		}
		if err := transferGenesisBalance(ledger, target, desired-current, sources); err != nil {
			return err
		}
	}
	return nil
}

func remapGenesisPolicyBucket(ledger *Ledger, source, target string) error {
	target = strings.TrimSpace(target)
	if target == "" || addressesEqual(source, target) {
		return nil
	}
	amount := getBalance(*ledger, CoinSymbol, source)
	if amount <= 0 {
		return nil
	}
	return transferGenesisBalance(ledger, target, amount, []string{source})
}

func genesisValidatorWalletSet(genesis *Genesis) map[string]struct{} {
	wallets := make(map[string]struct{})
	if genesis == nil {
		return wallets
	}
	for _, wallet := range genesis.RewardWallets {
		if key := strings.ToUpper(canonicalAddressKey(wallet)); key != "" {
			wallets[key] = struct{}{}
		}
	}
	for _, stake := range genesis.GenesisStakes {
		if key := strings.ToUpper(canonicalAddressKey(stake.Wallet)); key != "" {
			wallets[key] = struct{}{}
		}
	}
	return wallets
}

func genesisValidatorBalanceSources() []string {
	return []string{
		VALIDATOR_BOOTSTRAP_POOL,
		COMMUNITY_POOL,
		USER_REWARD_POOL,
		FOUNDATION_ADDRESS,
		OWNER_ADDRESS,
	}
}

func genesisGeneralBalanceSources() []string {
	return []string{
		COMMUNITY_POOL,
		USER_REWARD_POOL,
		VALIDATOR_BOOTSTRAP_POOL,
		FOUNDATION_ADDRESS,
		OWNER_ADDRESS,
	}
}

func transferGenesisBalance(ledger *Ledger, target string, amount int, sources []string) error {
	if amount <= 0 {
		return nil
	}
	remaining := amount
	targetKey := strings.ToUpper(canonicalAddressKey(target))
	seen := make(map[string]struct{}, len(sources))

	for _, source := range sources {
		source = strings.TrimSpace(source)
		sourceKey := strings.ToUpper(canonicalAddressKey(source))
		if sourceKey == "" || sourceKey == targetKey {
			continue
		}
		if _, duplicate := seen[sourceKey]; duplicate {
			continue
		}
		seen[sourceKey] = struct{}{}

		available := getBalance(*ledger, CoinSymbol, source)
		if available <= 0 {
			continue
		}
		used := available
		if used > remaining {
			used = remaining
		}
		addBalance(ledger, CoinSymbol, source, -used)
		addBalance(ledger, CoinSymbol, target, used)
		remaining -= used
		if remaining == 0 {
			return nil
		}
	}

	return fmt.Errorf(
		"genesis balance for %s exceeds fixed supply reserves: requested=%d missing=%d",
		target,
		amount,
		remaining,
	)
}

func genesisSupplyBreakdown(ledger *Ledger, genesis *Genesis) (GenesisSupplyBreakdown, error) {
	var out GenesisSupplyBreakdown
	if err := validateSupplyState(ledger); err != nil {
		return out, err
	}

	categoryByAddress := make(map[string]*int64)
	register := func(target *int64, addresses ...string) {
		for _, address := range addresses {
			key := strings.ToUpper(canonicalAddressKey(address))
			if key != "" {
				categoryByAddress[key] = target
			}
		}
	}
	register(&out.Foundation, FOUNDATION_ADDRESS)
	register(&out.Treasury, OWNER_ADDRESS, TREASURY_ADDRESS, OwnerAddress)
	register(&out.ValidatorPool, VALIDATOR_BOOTSTRAP_POOL)
	register(&out.Community, COMMUNITY_POOL)
	register(&out.Ecosystem, USER_REWARD_POOL)
	if genesis != nil {
		register(&out.Foundation, genesis.Foundation.Wallet)
		register(&out.Treasury, genesis.Treasury.Wallet)
		for _, wallet := range genesis.RewardWallets {
			register(&out.ValidatorPool, wallet)
		}
		for _, stake := range genesis.GenesisStakes {
			register(&out.ValidatorPool, stake.Wallet)
		}
	}

	prefix := normalizeCoin(CoinSymbol) + "|"
	for key, amount := range ledger.Balances {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		address := strings.TrimPrefix(key, prefix)
		target := categoryByAddress[strings.ToUpper(canonicalAddressKey(address))]
		if target == nil {
			out.Reserved += int64(amount)
			continue
		}
		*target += int64(amount)
	}
	for _, stake := range ledger.Stakes {
		out.ValidatorPool += int64(stake.Amount)
	}
	out.Total = out.Foundation +
		out.Treasury +
		out.ValidatorPool +
		out.Community +
		out.Ecosystem +
		out.Reserved
	return out, nil
}

func validateGenesisFixedSupply(ledger *Ledger, genesis *Genesis) error {
	breakdown, err := genesisSupplyBreakdown(ledger, genesis)
	if err != nil {
		return err
	}
	actual := currentCoinSupply(ledger, CoinSymbol)
	if breakdown.Total > FixedTotalSupply || actual > FixedTotalSupply {
		return fmt.Errorf(
			"FATAL: Genesis allocation exceeds configured max supply. allocated=%d max=%d",
			breakdown.Total,
			FixedTotalSupply,
		)
	}
	if breakdown.Total != actual {
		return fmt.Errorf(
			"FATAL: Genesis allocation sum mismatch. categories=%d ledger=%d",
			breakdown.Total,
			actual,
		)
	}
	if actual != FixedTotalSupply {
		return fmt.Errorf(
			"genesis supply invariant failed: got=%d want=%d",
			actual,
			FixedTotalSupply,
		)
	}
	return nil
}
