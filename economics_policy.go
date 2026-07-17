package main

import (
	"fmt"
	"strings"
)

// `EconomicPolicyVersion` defines the constant value used by this package.
const EconomicPolicyVersion = "mainnet-economic-v1"

type EconomicPolicy struct {
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id"`
	// `Coin` stores the value associated with this record.
	Coin string `json:"coin"`
	// `Decimals` stores the value associated with this record.
	Decimals int `json:"decimals"`
	// `FixedTotalSupply` stores the value associated with this record.
	FixedTotalSupply int64 `json:"fixed_total_supply"`
	// `Staking` stores the value associated with this record.
	Staking EconomicStakingRules `json:"staking"`
	// `Slashing` stores the value associated with this record.
	Slashing EconomicSlashingRules `json:"slashing"`
	// `Inflation` stores the current position in the related collection.
	Inflation EconomicInflationRules `json:"inflation"`
	// `Rewards` stores the value associated with this record.
	Rewards EconomicRewardRules `json:"rewards"`
	// `Treasury` stores the value associated with this record.
	Treasury EconomicTreasuryRules `json:"treasury"`
	// `ValidatorMinimums` stores whether the related condition is satisfied.
	ValidatorMinimums EconomicValidatorMinimums `json:"validator_minimums"`
}

type EconomicStakingRules struct {
	// `ValidatorMinStake` stores whether the related condition is satisfied.
	ValidatorMinStake int64 `json:"validator_min_stake"`
	// `RequireStake` stores the request data being processed.
	RequireStake bool `json:"require_stake"`
	// `CoreStakeExempt` stores the value associated with this record.
	CoreStakeExempt bool `json:"core_stake_exempt"`
	// `DefaultLockEpochs` stores the value associated with this record.
	DefaultLockEpochs uint64 `json:"default_lock_epochs"`
	// `MinLockEpochs` stores the value associated with this record.
	MinLockEpochs uint64 `json:"min_lock_epochs"`
	// `MinUnstakeMonths` stores the value associated with this record.
	MinUnstakeMonths int `json:"min_unstake_months"`
	// `OneWalletOneValidator` stores the value associated with this record.
	OneWalletOneValidator bool `json:"one_wallet_one_validator"`
	// `ConsensusPubKeyRequired` stores the value associated with this record.
	ConsensusPubKeyRequired bool `json:"consensus_pubkey_required"`
	// `StakePersistsUntilUnstake` stores the value associated with this record.
	StakePersistsUntilUnstake bool `json:"stake_persists_until_unstake"`
	// `RejoinRequiresRestake` stores the value associated with this record.
	RejoinRequiresRestake bool `json:"rejoin_requires_restake"`
}

type EconomicSlashingRules struct {
	// `SevereSlashBurnBPS` stores the value associated with this record.
	SevereSlashBurnBPS uint64 `json:"severe_slash_burn_bps"`
	// `SevereSlashExitAfter` stores the value associated with this record.
	SevereSlashExitAfter int `json:"severe_slash_exit_after"`
	// `DoubleSignJailBlocks` stores the value associated with this record.
	DoubleSignJailBlocks uint64 `json:"double_sign_jail_blocks"`
	// `BadExecutionJailBlocks` stores the value associated with this record.
	BadExecutionJailBlocks uint64 `json:"bad_execution_jail_blocks"`
	// `InactivityPenaltyEnabled` stores whether the related condition is satisfied.
	InactivityPenaltyEnabled bool `json:"inactivity_penalty_enabled"`
	// `InactivityBaseBurnBPS` stores the current position in the related collection.
	InactivityBaseBurnBPS uint64 `json:"inactivity_base_burn_bps"`
	// `InactivityTierBurnBPS` stores the current position in the related collection.
	InactivityTierBurnBPS []uint64 `json:"inactivity_tier_burn_bps"`
	// `InactivityTierJailBlocks` stores the current position in the related collection.
	InactivityTierJailBlocks []uint64 `json:"inactivity_tier_jail_blocks"`
	// `InactivityCooldownBlocks` stores the current position in the related collection.
	InactivityCooldownBlocks uint64 `json:"inactivity_cooldown_blocks"`
	// `BlockedRewardsBurned` stores the block data handled by this operation.
	BlockedRewardsBurned bool `json:"blocked_rewards_burned"`
	// `SlashedBalanceForfeited` stores the value associated with this record.
	SlashedBalanceForfeited bool `json:"slashed_balance_forfeited"`
}

type EconomicInflationRules struct {
	// `EmissionEnabled` stores whether the related condition is satisfied.
	EmissionEnabled bool `json:"emission_enabled"`
	// `MinReward` stores the value associated with this record.
	MinReward int64 `json:"min_reward"`
	// `MaxReward` stores the value associated with this record.
	MaxReward int64 `json:"max_reward"`
	// `BaseChanceBPS` stores the value associated with this record.
	BaseChanceBPS int `json:"base_chance_bps"`
	// `JackpotChanceBPS` stores the current position in the related collection.
	JackpotChanceBPS int `json:"jackpot_chance_bps"`
	// `HighChanceAfterBlocks` stores the value associated with this record.
	HighChanceAfterBlocks uint64 `json:"high_chance_after_blocks"`
	// `HighChanceBPS` stores the value associated with this record.
	HighChanceBPS int `json:"high_chance_bps"`
	// `HalvingIntervalBlocks` stores the value associated with this record.
	HalvingIntervalBlocks uint64 `json:"halving_interval_blocks"`
	// `TreasuryBPS` stores the value associated with this record.
	TreasuryBPS int `json:"treasury_bps"`
	// `ValidatorBPS` stores whether the related condition is satisfied.
	ValidatorBPS int `json:"validator_bps"`
	// `BurnBPS` stores the value associated with this record.
	BurnBPS int `json:"burn_bps"`
	// `BurnFloor` stores the value associated with this record.
	BurnFloor int64 `json:"burn_floor"`
	// `IntervalMode` stores the current position in the related collection.
	IntervalMode bool `json:"interval_mode"`
	// `GapMinBlocks` stores the value associated with this record.
	GapMinBlocks uint64 `json:"gap_min_blocks"`
	// `GapMaxBlocks` stores the value associated with this record.
	GapMaxBlocks uint64 `json:"gap_max_blocks"`
	// `ValidatorRewardToProposer` stores whether the related condition is satisfied.
	ValidatorRewardToProposer bool `json:"validator_reward_to_proposer"`
	// `FixedSupplyCapEnforced` stores the value associated with this record.
	FixedSupplyCapEnforced bool `json:"fixed_supply_cap_enforced"`
}

type EconomicRewardRules struct {
	// `WorkBlockRewardEnabled` stores whether the related condition is satisfied.
	WorkBlockRewardEnabled bool `json:"work_block_reward_enabled"`
	// `WorkBlockBaseReward` stores the value associated with this record.
	WorkBlockBaseReward int64 `json:"work_block_base_reward"`
	// `UnifiedTeamRewardEnabled` stores whether the related condition is satisfied.
	UnifiedTeamRewardEnabled bool `json:"unified_team_reward_enabled"`
	// `UnifiedTreasuryBPS` stores the value associated with this record.
	UnifiedTreasuryBPS int `json:"unified_treasury_bps"`
	// `UnifiedProposerBPS` stores the value associated with this record.
	UnifiedProposerBPS int `json:"unified_proposer_bps"`
	// `UnifiedValidatorBPS` stores the value associated with this record.
	UnifiedValidatorBPS int `json:"unified_validator_bps"`
	// `LegacyRewardUserPct` stores the value associated with this record.
	LegacyRewardUserPct int `json:"legacy_reward_user_pct"`
	// `LegacyRewardProposerPct` stores the value associated with this record.
	LegacyRewardProposerPct int `json:"legacy_reward_proposer_pct"`
	// `LegacyRewardValidatorsPct` stores the value associated with this record.
	LegacyRewardValidatorsPct int `json:"legacy_reward_validators_pct"`
	// `LegacyRewardOwnerPct` stores the value associated with this record.
	LegacyRewardOwnerPct int `json:"legacy_reward_owner_pct"`
	// `FeeSplitUserPct` stores the value associated with this record.
	FeeSplitUserPct int `json:"fee_split_user_pct"`
	// `FeeSplitProposerPct` stores the value associated with this record.
	FeeSplitProposerPct int `json:"fee_split_proposer_pct"`
	// `FeeSplitValidatorsPct` stores the value associated with this record.
	FeeSplitValidatorsPct int `json:"fee_split_validators_pct"`
	// `FeeSplitOwnerPct` stores the value associated with this record.
	FeeSplitOwnerPct int `json:"fee_split_owner_pct"`
	// `RandomUserRewardEnabled` stores whether the related condition is satisfied.
	RandomUserRewardEnabled bool `json:"random_user_reward_enabled"`
	// `RandomUserRewardChanceBPS` stores the value associated with this record.
	RandomUserRewardChanceBPS int `json:"random_user_reward_chance_bps"`
}

type EconomicTreasuryRules struct {
	// `TreasuryAddress` stores the address used by this operation.
	TreasuryAddress string `json:"treasury_address"`
	// `OwnerAddress` stores the address used by this operation.
	OwnerAddress string `json:"owner_address"`
	// `FoundationAddress` stores whether the related condition is satisfied.
	FoundationAddress string `json:"foundation_address"`
	// `ValidatorBootstrapPool` stores whether the related condition is satisfied.
	ValidatorBootstrapPool string `json:"validator_bootstrap_pool"`
	// `CommunityPool` stores the value associated with this record.
	CommunityPool string `json:"community_pool"`
	// `UserRewardPool` stores the value associated with this record.
	UserRewardPool string `json:"user_reward_pool"`
	// `AllowTreasuryOps` stores the value associated with this record.
	AllowTreasuryOps bool `json:"allow_treasury_ops"`
	// `TransactionFeesToTreasury` stores the transaction data handled by this operation.
	TransactionFeesToTreasury bool `json:"transaction_fees_to_treasury"`
	// `TreasuryOpsRequireAdmin` stores the value associated with this record.
	TreasuryOpsRequireAdmin bool `json:"treasury_ops_require_admin"`
}

type EconomicValidatorMinimums struct {
	// `MinActiveValidators` stores the value associated with this record.
	MinActiveValidators int `json:"min_active_validators"`
	// `ActiveSetSize` stores the measured quantity used by this operation.
	ActiveSetSize int `json:"active_set_size"`
	// `MaxActiveCommittee` stores the value associated with this record.
	MaxActiveCommittee int `json:"max_active_committee"`
	// `StakeCapPct` stores the value associated with this record.
	StakeCapPct float64 `json:"stake_cap_pct"`
	// `InactiveRemoveBlocks` stores the current position in the related collection.
	InactiveRemoveBlocks uint64 `json:"inactive_remove_blocks"`
	// `ActivationDelayBlocks` stores the value associated with this record.
	ActivationDelayBlocks uint64 `json:"activation_delay_blocks"`
	// `RejoinRequiredHeartbeats` stores the value associated with this record.
	RejoinRequiredHeartbeats uint16 `json:"rejoin_required_heartbeats"`
	// `RejoinRequiredSignedBlocks` stores the value associated with this record.
	RejoinRequiredSignedBlocks uint64 `json:"rejoin_required_signed_blocks"`
}

// CurrentEconomicPolicy exposes the active deterministic economics and staking rules.
func CurrentEconomicPolicy() EconomicPolicy {
	// `emissionMin` and `emissionMax` store the value produced by this operation.
	emissionMin, emissionMax := protocolEmissionRewardBounds()
	// `emissionHighAfter` and `emissionHighChance` store the value produced by this operation.
	emissionHighAfter, emissionHighChance := protocolEmissionHighChanceThreshold()
	// `emissionTreasury`, `emissionValidator`, and `emissionBurn` store the value produced by this operation.
	emissionTreasury, emissionValidator, emissionBurn := protocolEmissionSplitBPS()
	// `emissionGapMin` and `emissionGapMax` store the value produced by this operation.
	emissionGapMin, emissionGapMax := protocolEmissionGapBounds()
	// `unifiedTreasury`, `unifiedProposer`, and `unifiedValidator` store the value produced by this operation.
	unifiedTreasury, unifiedProposer, unifiedValidator := protocolUnifiedTeamRewardSplitBPS()

	return EconomicPolicy{
		Version:          EconomicPolicyVersion,
		ChainID:          protocolChainID(),
		Coin:             CoinSymbol,
		Decimals:         CoinDecimals,
		FixedTotalSupply: FixedTotalSupply,
		Staking: EconomicStakingRules{
			ValidatorMinStake:         ConsensusValidatorMinStake,
			RequireStake:              ValidatorRequireStake,
			CoreStakeExempt:           ValidatorCoreStakeExempt,
			DefaultLockEpochs:         DefaultStakeLockEpochs,
			MinLockEpochs:             minUnstakeEpochs(),
			MinUnstakeMonths:          MinUnstakeMonths,
			OneWalletOneValidator:     true,
			ConsensusPubKeyRequired:   true,
			StakePersistsUntilUnstake: true,
			RejoinRequiresRestake:     false,
		},
		Slashing: EconomicSlashingRules{
			SevereSlashBurnBPS:       SlashStakeBurnBPS,
			SevereSlashExitAfter:     SevereSlashExitAfter,
			DoubleSignJailBlocks:     protocolJailDoubleSignBlocksValue(),
			BadExecutionJailBlocks:   protocolJailBadExecutionBlocksValue(),
			InactivityPenaltyEnabled: protocolValidatorInactivityPenaltyEnabledFlag(),
			InactivityBaseBurnBPS:    protocolValidatorInactivityPenaltyBurnBPSValue(),
			InactivityTierBurnBPS: []uint64{
				inactivityPenaltyBurnBPSForCount(1),
				inactivityPenaltyBurnBPSForCount(2),
				inactivityPenaltyBurnBPSForCount(3),
			},
			InactivityTierJailBlocks: []uint64{
				inactivityPenaltyJailBlocksForCount(1),
				inactivityPenaltyJailBlocksForCount(2),
				inactivityPenaltyJailBlocksForCount(3),
			},
			InactivityCooldownBlocks: inactivityPenaltyCooldownBlocks(),
			BlockedRewardsBurned:     true,
			SlashedBalanceForfeited:  true,
		},
		Inflation: EconomicInflationRules{
			EmissionEnabled:           protocolEmissionRewardEnabledFlag(),
			MinReward:                 emissionMin,
			MaxReward:                 emissionMax,
			BaseChanceBPS:             emissionChanceBPSForHeight(1),
			JackpotChanceBPS:          protocolEmissionJackpotChanceBPSValue(),
			HighChanceAfterBlocks:     emissionHighAfter,
			HighChanceBPS:             emissionHighChance,
			HalvingIntervalBlocks:     protocolEmissionHalvingIntervalBlocksValue(),
			TreasuryBPS:               emissionTreasury,
			ValidatorBPS:              emissionValidator,
			BurnBPS:                   emissionBurn,
			BurnFloor:                 protocolBurnStopSupplyValue(),
			IntervalMode:              protocolEmissionIntervalModeEnabled(),
			GapMinBlocks:              emissionGapMin,
			GapMaxBlocks:              emissionGapMax,
			ValidatorRewardToProposer: protocolEmissionValidatorRewardToProposer(),
			FixedSupplyCapEnforced:    true,
		},
		Rewards: EconomicRewardRules{
			WorkBlockRewardEnabled:    protocolWorkBlockRewardEnabledFlag(),
			WorkBlockBaseReward:       protocolWorkBlockBaseRewardValue(),
			UnifiedTeamRewardEnabled:  protocolUnifiedTeamRewardEnabledFlag(),
			UnifiedTreasuryBPS:        unifiedTreasury,
			UnifiedProposerBPS:        unifiedProposer,
			UnifiedValidatorBPS:       unifiedValidator,
			LegacyRewardUserPct:       RewardUser,
			LegacyRewardProposerPct:   RewardProposer,
			LegacyRewardValidatorsPct: RewardValidators,
			LegacyRewardOwnerPct:      RewardOwner,
			FeeSplitUserPct:           FeeSplitUser,
			FeeSplitProposerPct:       FeeSplitProposer,
			FeeSplitValidatorsPct:     FeeSplitValidators,
			FeeSplitOwnerPct:          FeeSplitOwner,
			RandomUserRewardEnabled:   protocolRandomUserRewardEnabledFlag(),
			RandomUserRewardChanceBPS: protocolRandomUserRewardChanceBPSValue(),
		},
		Treasury: EconomicTreasuryRules{
			TreasuryAddress:           TREASURY_ADDRESS,
			OwnerAddress:              OWNER_ADDRESS,
			FoundationAddress:         FOUNDATION_ADDRESS,
			ValidatorBootstrapPool:    VALIDATOR_BOOTSTRAP_POOL,
			CommunityPool:             COMMUNITY_POOL,
			UserRewardPool:            USER_REWARD_POOL,
			AllowTreasuryOps:          AllowTreasuryOps,
			TransactionFeesToTreasury: true,
			TreasuryOpsRequireAdmin:   true,
		},
		ValidatorMinimums: EconomicValidatorMinimums{
			MinActiveValidators:        minActiveValidatorsFloor(),
			ActiveSetSize:              protocolValidatorActiveSetSizeValue(),
			MaxActiveCommittee:         protocolValidatorMaxActiveCommitteeValue(),
			StakeCapPct:                protocolValidatorStakeCapPctValue(),
			InactiveRemoveBlocks:       protocolValidatorInactiveBlocksValue(),
			ActivationDelayBlocks:      validatorSetActivationDelayBlocks(),
			RejoinRequiredHeartbeats:   ValidatorRejoinRequiredHeartbeats,
			RejoinRequiredSignedBlocks: ValidatorRejoinRequiredSignedBlocks,
		},
	}
}

// ValidateEconomicPolicy checks that an economic policy snapshot has complete and safe bounds.
func ValidateEconomicPolicy(policy EconomicPolicy) error {
	if policy.Version == "" {
		return fmt.Errorf("economic_policy_version_missing")
	}
	if policy.FixedTotalSupply <= 0 {
		return fmt.Errorf("fixed_total_supply_missing")
	}
	if policy.Staking.ValidatorMinStake <= 0 {
		return fmt.Errorf("validator_min_stake_missing")
	}
	if policy.Staking.MinLockEpochs == 0 {
		return fmt.Errorf("stake_min_lock_missing")
	}
	if policy.Slashing.SevereSlashBurnBPS > 10000 {
		return fmt.Errorf("severe_slash_burn_bps_invalid")
	}
	if policy.Slashing.SevereSlashExitAfter <= 0 {
		return fmt.Errorf("severe_slash_exit_after_missing")
	}
	if policy.Inflation.MinReward < 0 || policy.Inflation.MaxReward < policy.Inflation.MinReward {
		return fmt.Errorf("emission_reward_range_invalid")
	}
	if policy.Inflation.TreasuryBPS < 0 || policy.Inflation.ValidatorBPS < 0 || policy.Inflation.BurnBPS < 0 {
		return fmt.Errorf("emission_bps_negative")
	}
	if policy.Inflation.TreasuryBPS+policy.Inflation.ValidatorBPS+policy.Inflation.BurnBPS > 10000 {
		return fmt.Errorf("emission_bps_exceeds_10000")
	}
	if policy.Rewards.UnifiedTreasuryBPS+policy.Rewards.UnifiedProposerBPS+policy.Rewards.UnifiedValidatorBPS > 10000 {
		return fmt.Errorf("unified_reward_bps_exceeds_10000")
	}
	if policy.ValidatorMinimums.MinActiveValidators <= 0 {
		return fmt.Errorf("min_active_validators_missing")
	}
	if policy.Treasury.TreasuryAddress == "" || policy.Treasury.UserRewardPool == "" {
		return fmt.Errorf("treasury_accounts_missing")
	}
	return nil
}

// validatorStakeTotalForMinimum implements the validator stake total for minimum helper.
func validatorStakeTotalForMinimum(ledger *Ledger, validatorID string) int64 {
	if ledger == nil {
		return 0
	}
	// `targetID` stores the value produced by this operation.
	targetID := normalizeValidatorID(validatorID)
	if targetID == "" {
		return 0
	}
	ensureStakeMap(ledger)
	// `total` stores the measured quantity used by this operation.
	total := int64(0)
	// `key` and `rec` track the key used to access the related value.
	for key, rec := range ledger.Stakes {
		if rec.Amount <= 0 {
			continue
		}
		// `recID` stores the value produced by this operation.
		recID := normalizeValidatorID(rec.ValidatorID)
		if recID == "" {
			// `parts` stores the value produced by this operation.
			parts := strings.SplitN(key, "|", 2)
			if len(parts) == 2 {
				recID = normalizeValidatorID(parts[1])
			}
		}
		if recID == targetID {
			total += int64(rec.Amount)
		}
	}
	return total
}

// validateValidatorMinimumStakeAfterTx validates validator minimum stake after tx.
func validateValidatorMinimumStakeAfterTx(ledger *Ledger, tx Transaction) error {
	if tx.Type != TxStake {
		return nil
	}
	// `validatorID` stores whether the related condition is satisfied.
	validatorID := normalizeValidatorID(tx.To)
	if validatorID == "" {
		return nil
	}
	// `current` stores the value produced by this operation.
	current := validatorStakeTotalForMinimum(ledger, validatorID)
	if current >= ConsensusValidatorMinStake {
		return nil
	}
	// `after` stores the value produced by this operation.
	after := current + int64(tx.Amount)
	if after < ConsensusValidatorMinStake {
		return fmt.Errorf("validator stake below minimum: got %d want %d", after, ConsensusValidatorMinStake)
	}
	return nil
}
