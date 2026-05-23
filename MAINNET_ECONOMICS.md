# Mainnet Economics Policy

Policy version: `mainnet-economic-v1`

This file documents the production economics currently exposed by `CurrentEconomicPolicy()` and `/tokenomics`.

## Staking Rules

- Validators must meet `validators.min_stake`.
- A stake transaction cannot create a below-minimum validator position.
- Stake lock defaults to `DefaultStakeLockEpochs`.
- Minimum lock is `MinUnstakeMonths` converted to epochs by block time.
- One wallet can be bound to one validator at a time.
- First non-core validator stake must include a consensus public key.
- Stake persists across restart and rejoin until the wallet submits an unstake transaction.
- Rejoining does not require staking again if the stake was never unstaked.

## Slashing Rules

- Severe faults burn `SlashStakeBurnBPS` from delegated stake.
- Severe faults include double proposal/double sign, invalid block/proposer, bad execution, execution equivocation, and systematic censorship.
- After `SevereSlashExitAfter` severe slashes, the validator exits permanently.
- Offline inactivity uses tiered burn and jail:
  - tier 1: half of `validators.inactivity_penalty_burn_bps`
  - tier 2: full `validators.inactivity_penalty_burn_bps`
  - tier 3+: double, capped at 10000 bps
- Inactivity penalties are cooldown-limited by `validators.inactivity_penalty_cooldown_blocks`.
- Rewards for jailed/exited validators are routed to treasury and burned.

## Inflation

- Supply is capped by `FixedTotalSupply`.
- Scheduled emission is controlled by `[tokenomics] emission_*`.
- Emission amount is deterministic per block and bounded by `emission_min_reward` and `emission_max_reward`.
- Emission halves every `emission_halving_interval_blocks`.
- Emission split:
  - treasury: `emission_treasury_bps`
  - validators/proposer: `emission_validator_bps`
  - burn: `emission_burn_bps`
- Burn stops at `supply_burn_floor` when configured.

## Rewards

- Transaction fees route to `MSC_TREASURY`.
- Work blocks can include `work_block_base_reward`.
- Unified team reward mode splits rewards by:
  - treasury: `unified_team_reward_treasury_bps`
  - proposer: `unified_team_reward_proposer_bps`
  - validators: `unified_team_reward_validator_bps`
- Validator reward wallets are deterministic and based on staked wallet binding.

## Treasury

- Treasury account: `MSC_TREASURY`.
- Owner account: `MSC_OWNER_ACCOUNT`.
- Foundation pool: `MSC_FOUNDATION`.
- Validator bootstrap pool: `MSC_VALIDATOR_BOOTSTRAP`.
- Community pool: `MSC_COMMUNITY_POOL`.
- User reward pool: `USER_REWARD_POOL`.
- Treasury operations are disabled on mainnet unless `allow_treasury_ops=true` and admin auth passes.

## Validator Minimums

- Minimum active validators: `validators.min_active_validators`.
- Active set target: `validators.active_set_size`.
- Maximum committee: `validators.max_active_committee`.
- New validator activation delay: `validators.validator_set_activation_delay`.
- Inactive validator removal waits `validators.inactive_remove_blocks`.
