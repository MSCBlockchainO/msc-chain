# Governance, Upgrade, And Validator Lifecycle Runbook

MSC mainnet governance has four guarded flows:

- Proposal create
- Validator voting
- Activation height
- Apply or schedule

All voting uses strict validator/governance-signer quorum. Governance must never lower finality quorum, bypass validator identity checks, or apply a protocol rollback unless the proposal explicitly allows rollback.

## Proposal Types

| Type | Purpose |
| --- | --- |
| `validator` | Records approved validator-set lifecycle intent. Execution remains through validator/stake transactions and activation delay. |
| `treasury` | Releases treasury funds after quorum approval. |
| `protocol_upgrade` | Schedules versioned protocol gate changes at an activation height. |
| `emergency` | Schedules urgent protocol gate changes with shortened activation delay, still requiring strict quorum. |
| `emergency_pause` | Records a bounded emergency pause window for operator coordination and alerting. |

## Upgrade Flow

1. Submit proposal with `upgrade_name`, `upgrade_version`, `activation_height`, and `protocol_changes`.
2. Validators vote `yes`, `no`, or `abstain`.
3. Finalize after strict quorum.
4. Apply before activation height to schedule, or at/after activation height to activate due gates.
5. Publish the proposal ID, activation height, expected binary tag, genesis hash, and release checksums.

Rollback protection:

- Protocol gates cannot move backward by default.
- A rollback must set `rollback_allowed=true`.
- Rollback proposals still require strict quorum.

## Emergency Pause Flow

Emergency pause is for operator coordination during a critical incident. It is bounded by block height and visible over RPC and Prometheus.

Example proposal body:

```json
{
  "kind": "emergency_pause",
  "title": "pause unsafe upgrade lane",
  "proposer": "A",
  "created_height": 100,
  "voting_start_height": 100,
  "voting_end_height": 110,
  "activation_height": 105,
  "pause_until_height": 140,
  "pause_reason": "operator-drill"
}
```

Rules:

- Strict quorum is still required.
- `pause_until_height` must be greater than `activation_height`.
- Emergency pause window is capped by policy.
- Operators must publish the runbook action: pause, patch, redeploy from GitHub, verify, then resume normal operations.

Observability:

- `/governance/status` includes `emergency_pause`.
- Prometheus exposes `msc_governance_emergency_pause_active`.
- Prometheus exposes `msc_governance_emergency_pause_until_height`.

## Slashing And Validator Lifecycle

Validator states:

- `PENDING`
- `ACTIVE`
- `JAILED`
- `EXITED`

Slashing behavior:

- Double-sign and double-proposal jail the validator.
- Invalid block, invalid proposer, fake execution, execution mismatch, and equivocation jail the validator.
- Repeated severe slashes permanently exit the validator.
- Offline/inactivity penalties are recoverable through jail/cooldown and reputation recovery.

Lifecycle:

1. Stake with consensus public key.
2. Enter pending activation.
3. Become active after activation delay and readiness gates.
4. If jailed, wait until `jail_until_height` and reputation recovery threshold.
5. If exited, validator cannot rejoin without a new approved lifecycle path.

Mainnet gate:

```powershell
go test . -run "TestGovernance|TestProtocolUpgrade|TestEmergency|TestSevereSlash|TestValidator.*Lifecycle|TestSlashing" -count=1
```
