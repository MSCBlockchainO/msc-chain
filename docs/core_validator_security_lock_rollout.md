# Core Validator Security Lock Rollout

This rollout keeps validator key handling strict while allowing future core-node onboarding via signed core registry.

Beginner entrypoint:

1. `docs/beginner_core_validator_runbook.md`
2. `scripts/core_beginner.ps1`

Default config selection:

1. Runtime default uses `config.toml` only.
2. Single-config mode rejects non-`config.toml` config overrides.

## Required strict flags

Keep these in `config.toml`:

1. `validators.fail_on_key_unavailable = true`
2. `validators.allow_identity_rotation_on_existing_chain = false`
3. `validators.key_backup_required = true`
4. `validators.key_restore_allowed_on_missing = true`
5. `validators.allow_env_password_in_production = false`
6. `validators.core_env_password_allowed = false`
7. `validators.password_mode = "prompt_only"` (for core node configs)
8. `validators.validator_set_autoheal_mode = "strict_core_quorum"`
9. `validators.validator_set_autoheal_trusted_only_on_mismatch = true`
10. `validators.onboarding_bootstrap_lane_enabled = true`
11. `validators.onboarding_bootstrap_max_new_slots = 1`

Shared-config caveat:

1. `validators.required_key_fingerprint` is a single value in one config file.
2. If running multiple core validators simultaneously with strict unique fingerprint locks, use another mechanism (for example signed core registry mapping).

## Core registry controls

Add and maintain:

1. `core.registry_path = "core_validators.json"`
2. `core.registry_enforcement = "warn"` (move to `enforce` after signer ceremony is ready)
3. `core.registry_min_signatures = 0` (auto 2/3 quorum)
4. `core.registry_reload_seconds = 10`

## Beginner-safe startup script usage

Use one command workflow:

```powershell
# First bootstrap only (validator.sec missing)
.\scripts\core_beginner.ps1 `
  -NodeId A `
  -Action init `
  -Port 7001 `
  -Rpc "127.0.0.1:26657" `
  -DataDir data/A

# Normal restart (no create override)
.\scripts\core_beginner.ps1 `
  -NodeId A `
  -Action start `
  -Port 7001 `
  -Rpc "127.0.0.1:26657" `
  -DataDir data/A
```

Legacy ENV scripts are compatibility-only and not recommended for beginners.

## Explicit ENV override contract (temporary/dev)

Use only when intentionally overriding prompt-only for one process:

```powershell
$env:MSC_VALIDATOR_PASSWORD_MODE="env_only"
$env:MSC_VALIDATOR_PASSWORD="<secret>"
go run . --mode=full --id=A --port=7001 --datadir=data/A --rpcaddr 127.0.0.1:26657
```

Notes:

1. `env_only` requires non-empty `MSC_VALIDATOR_PASSWORD`.
2. It bypasses file/prompt for that runtime only.
3. Clear env vars immediately after use.

## Status checks

After startup verify:

1. `validator_key_loaded = true`
2. `validator_key_backup_present = true`
3. `core_registry_loaded = true|false` (expected false before first signed registry deployment)
4. `core_registry_verified = true` once signed registry is deployed
5. `core_activation_status.status = "active"` for active core nodes
6. `core_required_fingerprint_match = true`
7. `core_env_password_blocked = false`
8. `validator_autoheal_state` and `validator_autoheal_last_reason` show mismatch-repair lifecycle.
9. `validator_bootstrap_lane_candidates` and `validator_bootstrap_lane_slots_used` show persistent non-core onboarding lane usage.

## Next runbook

1. Use `docs/core_validator_onboarding_signed_registry.md` for detailed signed-registry signer ceremony.
2. Use `scripts/core_beginner.ps1 -Action add-core-pending|add-core-active` to generate onboarding snippets.
