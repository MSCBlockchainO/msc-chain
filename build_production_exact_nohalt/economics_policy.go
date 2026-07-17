package main

import (
	"fmt"
	"strings"
)

const EconomicPolicyVersion = "mainnet-economic-v1"

type EconomicPolicy struct {
	Version           string                    `json:"version"`
	ChainID           string                    `json:"chain_id"`
	Coin              string                    `json:"coin"`
	Decimals          int                       `json:"decimals"`
	FixedTotalSupply  int64                     `json:"fixed_total_supply"`
	Staking           EconomicStakingRules      `json:"staking"`
	Slashing          EconomicSlashingRules     `json:"slashing"`
	Inflation         EconomicInflationRules    `json:"inflation"`
	Rewards           EconomicRewardRules       `json:"rewards"`
	Treasury          EconomicTreasuryRules     `json:"treasury"`
	ValidatorMinimums EconomicValidatorMinimums `json:"validator_minimums"`
}

type EconomicStakingRules struct {
	ValidatorMinStake         int64  `json:"validator_min_stake"`
	RequireStake              bool   `json:"require_stake"`
	CoreStakeExempt           bool   `json:"core_stake_exempt"`
	DefaultLockEpochs         uint64 `json:"default_lock_epochs"`
	MinLockEpochs             uint64 `json:"min_lock_epochs"`
	MinUnstakeMonths          int    `json:"min_unstake_months"`
	OneWalletOneValidator     bool   `json:"one_wallet_one_validator"`
	ConsensusPubKeyRequired   bool   `json:"consensus_pubkey_required"`
	StakePersistsUntilUnstake bool   `json:"stake_persists_until_unstake"`
	RejoinRequiresRestake     bool   `json:"rejoin_requires_restake"`
}

type EconomicSlashingRules struct {
	SevereSlashBurnBPS       uint64   `json:"severe_slash_burn_bps"`
	SevereSlashExitAfter     int      `json:"severe_slash_exit_after"`
	DoubleSignJailBlocks     uint64   `json:"double_sign_jail_blocks"`
	BadExecutionJailBlocks   uint64   `json:"bad_execution_jail_blocks"`
	InactivityPenaltyEnabled bool     `json:"inactivity_penalty_enabled"`
	InactivityBaseBurnBPS    uint64   `json:"inactivity_base_burn_bps"`
	InactivityTierBurnBPS    []uint64 `json:"inactivity_tier_burn_bps"`
	InactivityTierJailBlocks []uint64 `json:"inactivity_tier_jail_blocks"`
	InactivityCooldownBlocks uint64   `json:"inactivity_cooldown_blocks"`
	BlockedRewardsBurned     bool     `json:"blocked_rewards_burned"`
	SlashedBalanceForfeited  bool     `json:"slashed_balance_forfeited"`
}

type EconomicInflationRules struct {
	EmissionEnabled           bool   `json:"emission_enabled"`
	MinReward                 int64  `json:"min_reward"`
	MaxReward                 int64  `json:"max_reward"`
	BaseChanceBPS             int    `json:"base_chance_bps"`
	JackpotChanceBPS          int    `json:"jackpot_chance_bps"`
	HighChanceAfterBlocks     uint64 `json:"high_chance_after_blocks"`
	HighChanceBPS             int    `json:"high_chance_bps"`
	HalvingIntervalBlocks     uint64 `json:"halving_interval_blocks"`
	TreasuryBPS               int    `json:"treasury_bps"`
	ValidatorBPS              int    `json:"validator_bps"`
	BurnBPS                   int    `json:"burn_bps"`
	BurnFloor                 int64  `json:"burn_floor"`
	IntervalMode              bool   `json:"interval_mode"`
	GapMinBlocks              uint64 `json:"gap_min_blocks"`
	GapMaxBlocks              uint64 `json:"gap_max_blocks"`
	ValidatorRewardToProposer bool   `json:"validator_reward_to_proposer"`
	FixedSupplyCapEnforced    bool   `json:"fixed_supply_cap_enforced"`
}

type EconomicRewardRules struct {
	WorkBlockRewardEnabled    bool  `json:"work_block_reward_enabled"`
	WorkBlockBaseReward       int64 `json:"work_block_base_reward"`
	UnifiedTeamRewardEnabled  bool  `json:"unified_team_reward_enabled"`
	UnifiedTreasuryBPS        int   `json:"unified_treasury_bps"`
	UnifiedProposerBPS        int   `json:"unified_proposer_bps"`
	UnifiedValidatorBPS       int   `json:"unified_validator_bps"`
	LegacyRewardUserPct       int   `json:"legacy_reward_user_pct"`
	LegacyRewardProposerPct   int   `json:"legacy_reward_proposer_pct"`
	LegacyRewardValidatorsPct int   `json:"legacy_reward_validators_pct"`
	LegacyRewardOwnerPct      int   `json:"legacy_reward_owner_pct"`
	FeeSplitUserPct           int   `json:"fee_split_user_pct"`
	FeeSplitProposerPct       int   `json:"fee_split_proposer_pct"`
	FeeSplitValidatorsPct     int   `json:"fee_split_validators_pct"`
	FeeSplitOwnerPct          int   `json:"fee_split_owner_pct"`
	RandomUserRewardEnabled   bool  `json:"random_user_reward_enabled"`
	RandomUserRewardChanceBPS int   `json:"random_user_reward_chance_bps"`
}

type EconomicTreasuryRules struct {
	TreasuryAddress           string `json:"treasury_address"`
	OwnerAddress              string `json:"owner_address"`
	FoundationAddress         string `json:"foundation_address"`
	ValidatorBootstrapPool    string `json:"validator_bootstrap_pool"`
	CommunityPool             string `json:"community_pool"`
	UserRewardPool            string `json:"user_reward_pool"`
	AllowTreasuryOps          bool   `json:"allow_treasury_ops"`
	TransactionFeesToTreasury bool   `json:"transaction_fees_to_treasury"`
	TreasuryOpsRequireAdmin   bool   `json:"treasury_ops_require_admin"`
}

type EconomicValidatorMinimums struct {
	MinActiveValidators        int     `json:"min_active_validators"`
	ActiveSetSize              int     `json:"active_set_size"`
	MaxActiveCommittee         int     `json:"max_active_committee"`
	StakeCapPct                float64 `json:"stake_cap_pct"`
	InactiveRemoveBlocks       uint64  `json:"inactive_remove_blocks"`
	ActivationDelayBlocks      uint64  `json:"activation_delay_blocks"`
	RejoinRequiredHeartbeats   uint16  `json:"rejoin_required_heartbeats"`
	RejoinRequiredSignedBlocks uint64  `json:"rejoin_required_signed_blocks"`
}

func CurrentEconomicPolicy() EconomicPolicy {
	return EconomicPolicy{
		Version:          EconomicPolicyVersion,
		ChainID:          ChainID,
		Coin:             CoinSymbol,
		Decimals:         CoinDecimals,
		FixedTotalSupply: FixedTotalSupply,
		Staking: EconomicStakingRules{
			ValidatorMinStake:         ValidatorMinStake,
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
			DoubleSignJailBlocks:     JailDoubleSignBlocks,
			BadExecutionJailBlocks:   JailBadExecutionBlocks,
			InactivityPenaltyEnabled: ValidatorInactivityPenaltyEnabled,
			InactivityBaseBurnBPS:    sanitizeBPS(ValidatorInactivityPenaltyBurnBPS),
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
			EmissionEnabled:           EmissionRewardEnabled,
			MinReward:                 EmissionMinReward,
			MaxReward:                 EmissionMaxReward,
			BaseChanceBPS:             emissionChanceBPSForHeight(1),
			JackpotChanceBPS:          EmissionJackpotChanceBPS,
			HighChanceAfterBlocks:     EmissionHighChanceAfterBlocks,
			HighChanceBPS:             EmissionHighChanceBPS,
			HalvingIntervalBlocks:     EmissionHalvingIntervalBlocks,
			TreasuryBPS:               EmissionTreasuryBPS,
			ValidatorBPS:              EmissionValidatorBPS,
			BurnBPS:                   EmissionBurnBPS,
			BurnFloor:                 BurnStopSupply,
			IntervalMode:              EmissionIntervalMode,
			GapMinBlocks:              EmissionGapMinBlocks,
			GapMaxBlocks:              EmissionGapMaxBlocks,
			ValidatorRewardToProposer: EmissionValidatorToProposer,
			FixedSupplyCapEnforced:    true,
		},
		Rewards: EconomicRewardRules{
			WorkBlockRewardEnabled:    WorkBlockRewardEnabled,
			WorkBlockBaseReward:       WorkBlockBaseReward,
			UnifiedTeamRewardEnabled:  UnifiedTeamRewardEnabled,
			UnifiedTreasuryBPS:        UnifiedTeamRewardTreasuryBPS,
			UnifiedProposerBPS:        UnifiedTeamRewardProposerBPS,
			UnifiedValidatorBPS:       UnifiedTeamRewardValidatorBPS,
			LegacyRewardUserPct:       RewardUser,
			LegacyRewardProposerPct:   RewardProposer,
			LegacyRewardValidatorsPct: RewardValidators,
			LegacyRewardOwnerPct:      RewardOwner,
			FeeSplitUserPct:           FeeSplitUser,
			FeeSplitProposerPct:       FeeSplitProposer,
			FeeSplitValidatorsPct:     FeeSplitValidators,
			FeeSplitOwnerPct:          FeeSplitOwner,
			RandomUserRewardEnabled:   RandomUserRewardEnabled,
			RandomUserRewardChanceBPS: RandomUserRewardChanceBPS,
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
			ActiveSetSize:              ValidatorActiveSetSize,
			MaxActiveCommittee:         ValidatorMaxActiveCommittee,
			StakeCapPct:                ValidatorStakeCapPct,
			InactiveRemoveBlocks:       ValidatorInactiveBlocks,
			ActivationDelayBlocks:      validatorSetActivationDelayBlocks(),
			RejoinRequiredHeartbeats:   ValidatorRejoinRequiredHeartbeats,
			RejoinRequiredSignedBlocks: ValidatorRejoinRequiredSignedBlocks,
		},
	}
}

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

func validatorStakeTotalForMinimum(ledger *Ledger, validatorID string) int64 {
	if ledger == nil {
		return 0
	}
	targetID := normalizeValidatorID(validatorID)
	if targetID == "" {
		return 0
	}
	ensureStakeMap(ledger)
	total := int64(0)
	for key, rec := range ledger.Stakes {
		if rec.Amount <= 0 {
			continue
		}
		recID := normalizeValidatorID(rec.ValidatorID)
		if recID == "" {
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

func validateValidatorMinimumStakeAfterTx(ledger *Ledger, tx Transaction) error {
	if tx.Type != TxStake || ValidatorMinStake <= 0 {
		return nil
	}
	validatorID := normalizeValidatorID(tx.To)
	if validatorID == "" {
		return nil
	}
	current := validatorStakeTotalForMinimum(ledger, validatorID)
	if current >= ValidatorMinStake {
		return nil
	}
	after := current + int64(tx.Amount)
	if after < ValidatorMinStake {
		return fmt.Errorf("validator stake below minimum: got %d want %d", after, ValidatorMinStake)
	}
	return nil
}
