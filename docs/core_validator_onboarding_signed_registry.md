# Core Validator Onboarding (Signed Registry, Two-Phase)

This runbook adds a new core validator without chain reset/delete.

Beginner helper commands:

1. `.\scripts\core_beginner.ps1 -NodeId E -Action add-core-pending -DataDir data/E -Rpc 127.0.0.1:26657`
2. `.\scripts\core_beginner.ps1 -NodeId E -Action add-core-active -DataDir data/E -Rpc 127.0.0.1:26657`

## Preconditions

1. Existing core validators are healthy and in quorum.
2. New node key is generated and backed up (`validator.sec`, `validator.meta.json`, `validator.backup.manifest.json`).
3. Node uses per-node config and strict key lifecycle flags.

## Registry File

Path: `core_validators.json`

Required fields:

1. `chain_id`
2. `version`
3. `epoch`
4. `effective_height`
5. `previous_registry_hash`
6. `validators[]`:
   - `id`
   - `required_key_fingerprint`
   - `consensus_pubkey`
   - `p2p_seed`
   - `status` (`pending|active|retired`)
7. `signatures[]`:
   - `signer_id`
   - `signer_pubkey`
   - `sig_hex`
8. `payload_hash`

## Phase 1: Add Pending Core Node

1. Add new node entry with `status: "pending"`.
2. Keep current active core entries as `active`.
3. Set `effective_height` for the update.
4. Compute canonical `payload_hash` from unsigned payload.
5. Collect quorum signatures from active core validators.
6. Distribute updated `core_validators.json` to all nodes.

Expected behavior:

1. Node loads as core-pending.
2. It can sync and heartbeat.
3. It is excluded from proposer/committee selection while pending.

## Phase 2: Promote Pending -> Active

1. Submit next signed registry update with that node `status: "active"`.
2. Keep `effective_height` high enough for rollout buffer.
3. Collect quorum signatures again and distribute file.
4. Wait until `effective_height + consensus.core_activation_effective_height_buffer`.

Expected behavior:

1. Node transitions to active automatically.
2. It joins proposer/committee selection.
3. No identity drift or manual reset required.

## Verification Checklist

On each node `/status`:

1. `core_registry_loaded = true`
2. `core_registry_verified = true`
3. `core_registry_hash` matches expected payload hash
4. `core_activation_status.status` is `pending` or `active` as expected
5. `core_required_fingerprint_match = true`
6. `validator_key_loaded = true`

## Rollout Modes

1. Stage-1: `core.registry_enforcement = "warn"`
2. Stage-2+: `core.registry_enforcement = "enforce"`

In `enforce`, invalid/missing registry blocks core-node startup.
