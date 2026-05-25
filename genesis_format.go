package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type genesisValidatorJSON struct {
	ConsensusPubKey string `json:"consensus_pubkey"`
	RewardWallet    string `json:"reward_wallet,omitempty"`
}

func (g *Genesis) UnmarshalJSON(data []byte) error {
	var raw struct {
		ChainID            string                     `json:"chain_id"`
		Decimals           int                        `json:"decimals,omitempty"`
		GenesisLocked      bool                       `json:"genesis_locked,omitempty"`
		ValidatorSetFrozen bool                       `json:"validator_set_frozen,omitempty"`
		Validators         map[string]json.RawMessage `json:"validators"`
		Balances           map[string]int             `json:"balances,omitempty"`
		RewardWallets      map[string]string          `json:"reward_wallets,omitempty"`
		Foundation         GenesisAllocation          `json:"foundation,omitempty"`
		Treasury           GenesisAllocation          `json:"treasury,omitempty"`
		GenesisStakes      map[string]GenesisStake    `json:"genesis_stakes,omitempty"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	validators := make(map[string]string, len(raw.Validators))
	rewardWallets := make(map[string]string, len(raw.RewardWallets)+len(raw.Validators))
	for id, wallet := range raw.RewardWallets {
		if strings.TrimSpace(id) != "" && strings.TrimSpace(wallet) != "" {
			rewardWallets[id] = wallet
		}
	}

	for id, payload := range raw.Validators {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}

		var pubkey string
		if err := json.Unmarshal(payload, &pubkey); err == nil {
			validators[id] = strings.TrimSpace(pubkey)
			continue
		}

		var spec genesisValidatorJSON
		if err := json.Unmarshal(payload, &spec); err != nil {
			return fmt.Errorf("genesis validator %s invalid format: %w", id, err)
		}
		pubkey = strings.TrimSpace(spec.ConsensusPubKey)
		if pubkey == "" {
			return fmt.Errorf("genesis validator %s missing consensus_pubkey", id)
		}
		validators[id] = pubkey

		wallet := strings.TrimSpace(spec.RewardWallet)
		if wallet == "" {
			continue
		}
		if existing := strings.TrimSpace(rewardWallets[id]); existing != "" && !addressesEqual(existing, wallet) {
			return fmt.Errorf("genesis validator %s reward_wallet mismatch", id)
		}
		rewardWallets[id] = wallet
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
