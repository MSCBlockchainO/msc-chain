# Beginner Core Validator Runbook (A/B/C/D + Future Core Add)

This is the canonical beginner runbook for secure core-validator operations.

## Goal

Run core validators with secure prompt-default mode, optional explicit ENV override for dev/emergency, without accidental key regeneration, and without chain reset/delete.

Config default:

1. Runtime default always uses `config.toml`.
2. Single-config mode hard-locks config path to `config.toml` only.

## Security Defaults (must stay enabled)

1. `validators.fail_on_key_unavailable = true`
2. `validators.allow_identity_rotation_on_existing_chain = false`
3. `validators.key_backup_required = true`
4. `validators.key_restore_allowed_on_missing = true`
5. `validators.allow_env_password_in_production = false`
6. `validators.core_env_password_allowed = false`
7. `validators.password_mode = "prompt_only"` in the active config file (`config.toml` by default).
8. `validators.core_stake_exempt = true` keeps core validators stake-exempt while preserving non-core stake onboarding.
9. `validators.onboarding_grace_blocks = 64` and `validators.onboarding_max_new_slots = 1` allow controlled non-core onboarding without weakening committee safety.
10. `validators.validator_set_autoheal_mode = "strict_core_quorum"` keeps mismatch recovery deterministic from core quorum snapshots only.
11. `validators.validator_set_autoheal_trusted_only_on_mismatch = true` prevents local divergent snapshot bounce during mismatch repair.
12. `validators.onboarding_bootstrap_lane_enabled = true` allows staked non-core onboarding even after grace expiry with slot caps.

## Script Entry Point

Use only:

```powershell
.\scripts\core_beginner.ps1
```

Supported actions:

1. `init`
2. `start`
3. `status`
4. `backup`
5. `add-core-pending`
6. `add-core-active`

## Preflight (once per terminal)

If you ever see env-policy errors, clear legacy env vars:

```powershell
Remove-Item Env:MSC_VALIDATOR_PASSWORD -ErrorAction SilentlyContinue
Remove-Item Env:MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE -ErrorAction SilentlyContinue
Remove-Item Env:MSC_ALLOW_VALIDATOR_KEY_CREATE -ErrorAction SilentlyContinue
```

## 1) First-time Bootstrap (safe key init)

Example for `A`:

```powershell
.\scripts\core_beginner.ps1 -NodeId A -Action init -Port 7001 -Rpc 127.0.0.1:26657 -DataDir data/A
```

What this does:

1. Blocks legacy env-secret usage.
2. Sets `validators.password_mode="prompt_only"` for core-safe default.
3. Runs offline keygen (`--mode=keygen`).
4. Verifies artifacts:
   - `data/A/node_A/validator.sec`
   - `data/A/node_A/validator.pub`
   - `data/A/node_A/fingerprint.lock`
   - `data/A/node_A/validator.meta.json`
   - `data/A/node_A/validator.backup.manifest.json`
5. Pins `required_key_fingerprint` in the active config file (`config.toml` by default).

Optional legacy-compatible init (file mode):

```powershell
.\scripts\core_beginner.ps1 -NodeId A -Action init -PasswordMode file_or_prompt -Port 7001 -Rpc 127.0.0.1:26657 -DataDir data/A
```

## 2) Daily Start (no ENV secrets)

```powershell
.\scripts\core_beginner.ps1 -NodeId A -Action start -Port 7001 -Rpc 127.0.0.1:26657 -DataDir data/A
```

Notes:

1. You do not need `MSC_VALIDATOR_PASSWORD`.
2. You do not need `MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE`.
3. Runtime always uses `config.toml`; non-`config.toml` config override is rejected.

Shared-config warning:

1. A single `config.toml` can only pin one `validators.required_key_fingerprint` value at a time.
2. Strict unique per-node fingerprint locks across simultaneous A/B/C/D require another mechanism (for example signed core registry mapping).
3. Core IDs in `[auth].core_validators` are stake-exempt by default; non-core validators (like `F`) still require stake and activation delay.
4. First-time non-core stake onboarding must include `validator_pubkey` on the `TxStake` request so the validator consensus pubkey is anchored in committed registry state before committee admission.
5. `MSC_ALLOW_VALIDATOR_KEY_CREATE` and `MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE` remain bootstrap-only overrides; do not use them on an existing chain unless you are explicitly performing a controlled recovery/bootstrap.

If non-core onboarding appears stuck (`COMMITTEE size=4` forever):

1. Stop all nodes cleanly.
2. Reconcile only the divergent node data (example `F`) using:

```powershell
.\scripts\prod_resync.ps1 -Root . -Nodes F
```

3. Restart A/B/C/D/F together.
4. Verify stake record exists on F node ledger, the onboarding stake transaction carried `validator_pubkey`, and `/v1/peers` shows `hash_match=YES` for stable peers.

Automatic self-heal expectations:

1. `/status` should expose `validator_autoheal_state`, `validator_autoheal_last_reason`, and last mismatch hashes.
2. With strict core quorum mode, a divergent node waits for trusted core votes instead of replaying local snapshots.
3. Persistent onboarding lane fields (`validator_bootstrap_lane_candidates`, `validator_bootstrap_lane_slots_used`) show non-core admission queue.

## 2B) Explicit ENV Override (dev/emergency only)

If you want your exact ENV flow for one process:

```powershell
$env:MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE="1"
$env:MSC_VALIDATOR_PASSWORD="mfd@123"
$env:MSC_VALIDATOR_PASSWORD_MODE="env_only"
go run . --mode=full --id=A --port=7001 --datadir=data/A --rpcaddr 127.0.0.1:26657
```

Behavior:

1. `MSC_VALIDATOR_PASSWORD_MODE=env_only` overrides `prompt_only` for that process.
2. `MSC_VALIDATOR_PASSWORD` is required and used as the only source.
3. Remove env vars after run:

```powershell
Remove-Item Env:MSC_VALIDATOR_PASSWORD_MODE -ErrorAction SilentlyContinue
Remove-Item Env:MSC_VALIDATOR_PASSWORD -ErrorAction SilentlyContinue
Remove-Item Env:MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE -ErrorAction SilentlyContinue
```

## 3) Health Check

```powershell
.\scripts\core_beginner.ps1 -NodeId A -Action status -Rpc 127.0.0.1:26657
```

Required `true` checks:

1. `validator_key_loaded`
2. `validator_key_fingerprint_match`
3. `validator_key_integrity_ok`
4. `validator_key_backup_present`
5. `core_required_fingerprint_match`
6. `ready`

## 4) Backup Discipline

```powershell
.\scripts\core_beginner.ps1 -NodeId A -Action backup -DataDir data/A
```

Optional external copy:

```powershell
.\scripts\core_beginner.ps1 -NodeId A -Action backup -DataDir data/A -ExternalBackupDir C:\offline-backups\A
```

Monthly drill:

1. Simulate missing `validator.sec` on staging copy.
2. Confirm restore path works via backup manifest.
3. Confirm no chain reset/delete needed.

## 5) Parallel Core Startup (A/B/C/D)

Run in separate terminals:

```powershell
.\scripts\core_beginner.ps1 -NodeId A -Action start -Port 7001 -Rpc 127.0.0.1:26657 -DataDir data/A
.\scripts\core_beginner.ps1 -NodeId B -Action start -Port 7002 -Rpc 127.0.0.1:26658 -DataDir data/B
.\scripts\core_beginner.ps1 -NodeId C -Action start -Port 7003 -Rpc 127.0.0.1:26659 -DataDir data/C
.\scripts\core_beginner.ps1 -NodeId D -Action start -Port 7004 -Rpc 127.0.0.1:26660 -DataDir data/D
```

Notes:
1. Use unique `-Rpc` per node; if `config.toml` sets `rpc.addr`, it overrides CLI.
2. Remove `MSC_ALLOW_VALIDATOR_KEY_CREATE` after first bootstrap; rotating keys changes peer IDs. If keys rotate, delete `data/<node>/node_<id>/peers.json` before restart.

Troubleshooting (snapshot/DB):
Pebble is the current storage backend. If you see snapshot/DB warnings or if you upgraded from an older Badger-based build, wipe the affected `data/<node>` directory and re-sync or re-bootstrap that node.
If you see `Invalid proposer signature`, it usually means a key mismatch during startup. Ensure `MSC_ALLOW_VALIDATOR_KEY_CREATE` is unset after bootstrap, keys are stable, and if the node rotated keys, wipe the affected `data/<node>` or re-bootstrap.

## 6) Future Core Add (E/F)

Generate snippets for signed-registry workflow:

```powershell
.\scripts\core_beginner.ps1 -NodeId E -Action add-core-pending -DataDir data/E -Rpc 127.0.0.1:26657
.\scripts\core_beginner.ps1 -NodeId E -Action add-core-active -DataDir data/E -Rpc 127.0.0.1:26657
```

Then follow full registry ceremony:

1. Merge snippet into `core_validators.json`.
2. Bump epoch/effective height.
3. Collect quorum signatures from active core set.
4. Distribute signed registry to all nodes.
5. Verify `/status` on every node.

## Advanced Appendix (Operators)

1. `scripts/start_core_validator.ps1` and `scripts/validator_key_bootstrap.ps1` are legacy compatibility helpers.
2. They are ENV-based and not the beginner-recommended path.
3. Use them only for controlled debug or transitional migration.
