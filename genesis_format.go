package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type genesisValidatorJSON struct {
	// `ConsensusPubKey` stores the key used to access the related value.
	ConsensusPubKey string `json:"consensus_pubkey"`
	// `RewardWallet` stores the value associated with this record.
	RewardWallet    string `json:"reward_wallet,omitempty"`
}

// parseGenesisValidatorMap parses genesis validator map.
func parseGenesisValidatorMap(rawValidators map[string]json.RawMessage, rawRewardWallets map[string]string) (map[string]string, map[string]string, error) {
	// `validators` stores whether the related condition is satisfied.
	validators := make(map[string]string, len(rawValidators))
	// `rewardWallets` stores the value produced by this operation.
	rewardWallets := make(map[string]string, len(rawRewardWallets)+len(rawValidators))
	// `id` and `wallet` track the current position in the related collection.
	for id, wallet := range rawRewardWallets {
		if strings.TrimSpace(id) != "" && strings.TrimSpace(wallet) != "" {
			rewardWallets[id] = wallet
		}
	}

	// `id` and `payload` track the current position in the related collection.
	for id, payload := range rawValidators {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}

		// `pubkey` stores the key used to access the related value.
		var pubkey string
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(payload, &pubkey); err == nil {
			validators[id] = strings.TrimSpace(pubkey)
			continue
		}

		// `spec` stores the value used by this operation.
		var spec genesisValidatorJSON
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(payload, &spec); err != nil {
			return nil, nil, fmt.Errorf("genesis validator %s invalid format: %w", id, err)
		}
		pubkey = strings.TrimSpace(spec.ConsensusPubKey)
		if pubkey == "" {
			return nil, nil, fmt.Errorf("genesis validator %s missing consensus_pubkey", id)
		}
		validators[id] = pubkey

		// `wallet` stores the value produced by this operation.
		wallet := strings.TrimSpace(spec.RewardWallet)
		if wallet == "" {
			continue
		}
		// `existing` stores the value produced by this operation.
		if existing := strings.TrimSpace(rewardWallets[id]); existing != "" && !addressesEqual(existing, wallet) {
			return nil, nil, fmt.Errorf("genesis validator %s reward_wallet mismatch", id)
		}
		rewardWallets[id] = wallet
	}

	return validators, rewardWallets, nil
}

// UnmarshalJSON implements the unmarshal json helper.
func (g *GenesisFile) UnmarshalJSON(data []byte) error {
	// `raw` stores the value used by this operation.
	var raw struct {
		// `ChainID` stores the value associated with this record.
		ChainID    string                     `json:"chain_id"`
		// `Validators` stores whether the related condition is satisfied.
		Validators map[string]json.RawMessage `json:"validators"`
	}
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	// `validators` and `err` store the error produced by this operation.
	validators, _, err := parseGenesisValidatorMap(raw.Validators, nil)
	if err != nil {
		return err
	}
	g.ChainID = raw.ChainID
	g.Validators = validators
	return nil
}

// UnmarshalJSON implements the unmarshal json helper.
func (g *Genesis) UnmarshalJSON(data []byte) error {
	// `raw` stores the value used by this operation.
	var raw struct {
		// `ChainID` stores the value associated with this record.
		ChainID            string                     `json:"chain_id"`
		// `Decimals` stores the value associated with this record.
		Decimals           int                        `json:"decimals,omitempty"`
		// `GenesisLocked` stores the value associated with this record.
		GenesisLocked      bool                       `json:"genesis_locked,omitempty"`
		// `ValidatorSetFrozen` stores whether the related condition is satisfied.
		ValidatorSetFrozen bool                       `json:"validator_set_frozen,omitempty"`
		// `Validators` stores whether the related condition is satisfied.
		Validators         map[string]json.RawMessage `json:"validators"`
		// `Balances` stores the value associated with this record.
		Balances           map[string]int             `json:"balances,omitempty"`
		// `RewardWallets` stores the value associated with this record.
		RewardWallets      map[string]string          `json:"reward_wallets,omitempty"`
		// `Foundation` stores whether the related condition is satisfied.
		Foundation         GenesisAllocation          `json:"foundation,omitempty"`
		// `Treasury` stores the value associated with this record.
		Treasury           GenesisAllocation          `json:"treasury,omitempty"`
		// `GenesisStakes` stores the value associated with this record.
		GenesisStakes      map[string]GenesisStake    `json:"genesis_stakes,omitempty"`
	}

	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// `validators`, `rewardWallets`, and `err` store the error produced by this operation.
	validators, rewardWallets, err := parseGenesisValidatorMap(raw.Validators, raw.RewardWallets)
	if err != nil {
		return err
	}

	g.ChainID = raw.ChainID
	g.Decimals = raw.Decimals
	g.GenesisLocked = raw.GenesisLocked
	g.ValidatorSetFrozen = raw.ValidatorSetFrozen
	g.Validators = validators
	g.Balances = raw.Balances
	g.RewardWallets = rewardWallets
	g.Foundation = raw.Foundation
	g.Treasury = raw.Treasury
	g.GenesisStakes = raw.GenesisStakes
	return nil
}
