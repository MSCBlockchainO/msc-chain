# Validator Key Operations (Reference)

Canonical beginner guide has moved to:

1. `docs/beginner_core_validator_runbook.md`

This file remains as an operator reference for lifecycle policy.

## Core Policy

1. `validator.sec` is identity root.
2. Fingerprint lock mismatch must fail startup.
3. Backup restore is preferred over chain reset/delete.
4. Core default secret mode is `validators.password_mode="prompt_only"`.
5. Optional explicit override: `MSC_VALIDATOR_PASSWORD_MODE=env_only` + `MSC_VALIDATOR_PASSWORD`.

## Password Mode Contract

1. Config modes:
   - `file_or_prompt` (compat default)
   - `prompt_only` (core default)
2. Core effective precedence:
   - If `MSC_VALIDATOR_PASSWORD_MODE=env_only` => env-only source, require `MSC_VALIDATOR_PASSWORD`.
   - Else apply config mode.
3. `prompt_only`:
   - env source blocked
   - file source ignored
   - hidden prompt required.
4. Invalid `MSC_VALIDATOR_PASSWORD_MODE` => startup fail (`env_password_mode_invalid`).

## Backup Policy (3 copies minimum)

1. Local encrypted vault copy.
2. Offline removable media copy.
3. Secondary secure host copy.

Use:

1. `scripts/core_beginner.ps1 -Action backup`
2. Optional external target via `-ExternalBackupDir`.

## Restore Behavior

When key is missing on existing chain:

1. Node attempts deterministic restore from backup path/manifest.
2. Startup proceeds only if integrity checks pass.
3. On failure, startup hard-fails with explicit reason.

## Rotation (Emergency Only)

1. Keep `allow_identity_rotation_on_existing_chain = false` by default.
2. Temporary enable only for approved emergency.
3. Rotate through governed process.
4. Re-lock flag to `false`.
5. Refresh backup + fingerprint pin.

## Legacy Scripts

These scripts are retained for compatibility and are not beginner-recommended:

1. `scripts/start_core_validator.ps1`
2. `scripts/validator_key_bootstrap.ps1`
