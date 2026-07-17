# MSC TRON Bridge Release Gate

`ops/bridge_release_gate` makes the final TRON mainnet activation decision from
one immutable, hash-pinned evidence bundle. A successful report is required
before the official USDT route is unpaused. It is not permission to skip an
external audit, operator ceremony, liquidity review, or human release review.

## Build

```powershell
go test ./bridgegate ./ops/bridge_release_gate -count=1
go build -trimpath -o bin\bridge-release-gate.exe .\ops\bridge_release_gate
```

Start with `deploy/bridge/tron-production-gate.example.json`. The example is
intentionally invalid. Keep the manifest and every referenced file in one
protected directory. References must be relative, must remain inside that
directory after symlink resolution, and must identify distinct regular files.

## Required Bundle

The manifest pins all of these files by SHA-256:

- immutable audited TRON deployment record
- actual three-provider observer configuration
- fresh threshold-signed production observer artifact
- actual auto-pause reconciler configuration
- fresh healthy reconciliation report
- threshold-attested DTL authority snapshot
- local copy and public HTTPS copy of the independent audit
- pause-drill report plus four typed, hash-pinned raw attachments
- 72-hour soak report plus seven typed, hash-pinned raw attachments
- operational incident runbook
- recoverable 4-of-5 TRON committee approval of the exact manifest bytes

Use `Get-FileHash -Algorithm SHA256` to calculate hashes. The manifest must be
created in the last 24 hours and expire no more than 24 hours after creation.
The gate always fetches the public audit and compares its body with the local
hash. The local report must be at least 1 KiB. Redirects, private/localhost
hosts, placeholder domains, URL credentials, query strings, non-default HTTPS
ports, and local publication bypasses fail.

## Committee Separation

Production requires all of the following:

- five source observers with a threshold of four
- five DTL mint authorities with a threshold of four
- five TRON release signers with a threshold of four
- separate governance, guardian, and executor operators
- globally unique operator IDs across every trust role
- no Ed25519 key reuse between source observers and DTL authorities
- DTL public keys that derive the declared MSC addresses for chain `91938`
- observer and reconciler use separate non-placeholder provider host sets

Private keys, seeds, API credentials, and admin tokens never belong in the
bundle. DTL authorities sign the exact canonical snapshot bytes independently.
Generate the bytes for HSM/KMS review without loading a private key:

```powershell
bin\bridge-release-gate.exe dtl-payload `
  --snapshot C:\msc-bridge\release\tron-dtl-authority-snapshot.json
```

The signer must hex-decode `signing_bytes_hex` and sign those bytes directly
with Ed25519. Do not sign the displayed SHA-256 digest instead. Add at least
four `{address, public_key, signature}` attestations to the snapshot. The gate
re-derives each MSC address and verifies every signature.

The pause report must reference exactly `msc_pause`, `tron_pause`,
`paused_rejections`, and `resume_approval`. The soak report must reference
exactly `reconciliation_log`, `end_to_end_transfers`, `reorg_drill`,
`replay_drill`, `rotation_drill`, `daily_limit_drill`, and
`crash_recovery_drill`. Each attachment has a bundle-relative path and SHA-256.
The gate reads every attachment, verifies its hash, rejects symlinks and path
escape, and prevents any report or attachment file from being reused.

## Release Approval Ceremony

Finalize every evidence hash, committee identity, role, timestamp, expiry,
source commit, release tag, and `release_approval_path` in the manifest first.
Then generate the exact signing payload:

```powershell
bin\bridge-release-gate.exe approval-payload `
  --manifest C:\msc-bridge\release\tron-production-gate.json
```

The output contains the exact manifest SHA-256, domain-separated signing bytes,
and their legacy Keccak-256 digest. Four of the five deployed TRON release
members independently sign the 32-byte `keccak256` value with their secp256k1
keys. Use raw digest signing without a TRON or Ethereum personal-message prefix.
Each signature must be recoverable 65-byte `r || s || v` hex with low `s`; `v`
may be `0/1` or `27/28`. Assemble the signatures using
`deploy/bridge/tron-release-approval.example.json` at the path already named by
the manifest. Do not modify or reformat the manifest after signatures are
collected. Any byte change requires a new approval ceremony.

The approval file is separate from the manifest to avoid a hash cycle. Every
signature binds the exact manifest hash, and the immutable gate report records
the approval-file hash and accepted signature count.

## Safe Activation Order

1. Deploy the audited vault. Confirm that it starts paused.
2. Run five production observers against three independent TRON API providers.
   Before the first deposit, a fresh canonical empty-event checkpoint is valid.
   Once events exist, every artifact must carry the native Java-Tron transaction
   Merkle proof for each event.
3. Execute only governance action 1, `setTokenRoute`, while the vault remains
   paused. Independently compare its exact calldata with the deployment record.
4. Run the reconciler. Its report must prove a stable solidified head, exact
   vault/token runtime hashes, exact enabled USDT min/max/daily limits, escrow,
   token balance, and paused state through API quorum.
5. Capture the same fresh MSC height/state root in the DTL snapshot, keep the
   wrapped token and global bridge paused, and collect four DTL attestations.
6. Assemble the remaining audit, typed drill/soak attachments, and incident
   evidence. Finalize the manifest, run `approval-payload`, collect four
   independent release signatures, and write the approval file.
7. Run:

```powershell
bin\bridge-release-gate.exe verify `
  --manifest C:\msc-bridge\release\tron-production-gate.json `
  --output C:\msc-bridge\release\tron-production-gate-report.json
```

The report file is created with exclusive-create semantics and cannot overwrite
an earlier decision. Archive the manifest, all evidence, report, source commit,
and release tag together.

8. A separate human release review compares the green report with the live
   paused chain state. Only then may governance submit the exact recorded
   `unpause()` action. The gate never broadcasts or unpauses anything.

## Enforced Checks

The verifier rejects non-canonical TRON identity, unofficial USDT, altered
governance calldata, weak or overlapping committees, stale checkpoints,
under-quorum signatures, missing or stale manifest approval, unrecoverable or
unauthorized TRON signatures, missing native proofs for event artifacts, API split
views, disabled auto-pause, accounting deficits, mismatched state roots, an
unpaused pre-launch state, DTL signer mismatch, incomplete typed drills,
tampered or reused raw attachments, insufficient soak duration, path escape,
local hash substitution, and public audit mismatch.

An empty production checkpoint proves observer liveness before activation; it
does not prove an end-to-end mainnet transfer. Deposit, burn, unlock, replay,
reorg, rotation, daily-limit, and crash-recovery behavior must be proven in the
72-hour testnet soak. The first guarded mainnet pilot transfer must be manually
reviewed and archived immediately after activation.

## Remaining External Gates

Code cannot manufacture external assurance. Keep the route paused until the
independent audit is published, independent organizations control the required
operators and protected keys, production API diversity is real, gas/liquidity
monitoring is staffed, and reorg/recovery drills have retained evidence.
