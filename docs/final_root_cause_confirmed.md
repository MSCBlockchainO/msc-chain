# FINAL ROOT CAUSE (CONFIRMED)

Har jagah same pattern repeat ho raha tha:

```text
parent_ledger = f892a7de
runtime_ledger = 6e78f653
expected_root = XXXXX
block_root    = XXXXX
reason=state_root_mismatch
```

## Iska matlab

- Node block ko same parent hash ke saath different parent state par execute kar raha tha
- Runtime ledger aur sealed execution parent ledger alag drift me chale gaye the
- Isliye quorum stable nahi ho raha tha

## Actual loop

Leader propose block

-> validators execute using inconsistent parent execution state

-> different state root

-> `EXEC-PREFLIGHT` fail

-> broadcast blocked

-> `ROUND-FAILOVER`

-> repeat forever

## Evidence from logs

Case 1:

```text
expected_root=3b933689
block_root=f24c92d2
```

Case 2:

```text
expected_root=9675ae94
block_root=9103cb0f
```

Case 3:

```text
leader=B -> root=3cda2d1a
others   -> different
```

Har round me different root aa raha tha, yani execution non-deterministic dikh raha tha at consensus layer.

## Confirmed core bug

System ka confirmed bug yeh tha:

- execution root calculation committed parent execution state se aani chahiye thi
- lekin code me parent execution ledger ke do competing sources the
- live consensus path `snapshotExecutionLedgerByHeight` use kar raha tha
- startup snapshot rebuild bhi usi in-memory cache me write kar raha tha
- live commit path kabhi kabhi execution cache ko `n.Ledger` lineage se seed kar raha tha, jo runtime/post-effects drift carry kar sakta tha

Yani effective bug yeh tha:

```go
// Wrong behavior at system level:
// parent execution source was not uniquely authoritative
ExecuteBlock(inconsistentParentLedger, block)

// Required behavior:
ExecuteBlock(committedParentExecutionLedger, block)
```

Important clarification:

- problem sirf itna nahi tha ke `runtimeLedger` mutable hai
- real problem yeh tha ke parent execution source ownership broken thi
- proposal build, preflight, commit caching, aur startup rebuild ek hi height ke liye different parent execution lineage de sakte the

## Why it breaks after restart

Restart ke baad:

snapshot load

-> trusted snapshot rebuild begin

-> rebuild live execution cache ko touch kar deta tha

-> parent execution lineage replace / conflict ho jati thi

-> `parent != runtime` symptom aur strong ho jata tha

-> chain stuck ho jati thi

## Code-level confirmed fix

Implemented fix ka core:

1. Execution ledger derivation unify ki gayi
2. Live commit ab execution cache ko sealed execution ledger se fill karta hai, `n.Ledger` se nahi
3. Startup rebuild ab disk-only trusted snapshot repair hai
4. Startup readiness ab valid live execution cache ko accept karti hai, unnecessary rebuild force nahi karti

Relevant paths:

- `c.go`: `executionLedgerForBlock`
- `recovery_fix.go`: committed execution ledger cache write
- `h.go`: snapshot creation with explicit execution ledger
- `learining3.go`: startup rebuild and readiness guard

## Correct final statement

Final confirmed RCA:

> Root cause was non-authoritative execution parent state. Live consensus, commit caching, and startup rebuild were all able to influence the parent execution ledger used for state-root computation. This caused proposals and preflight to execute the same block against different parent ledgers, producing `state_root_mismatch`, blocked broadcasts, and infinite round failover after restart or runtime drift.

## Core problem (business language)

Tumhari chain me:

- state execution layer deterministic nahi reh rahi thi
- runtime aur committed state diverge ho rahe the
- result = consensus deadlock around height 16

Business impact:

- block finalization ruk jati hai
- leader rotation hoti rehti hai but chain advance nahi karti
- restart ke baad recovery stable nahi rehti

## Strategic idea (high-level)

System ko 3-layer architecture me convert karna chahiye:

1. `CommittedLedger` - source of truth
2. `ExecutionSandbox` - deterministic engine
3. `RuntimeLedger` - local / non-consensus state

## Enterprise architecture (correct model)

```text
CommittedLedger (immutable)
        ↓ clone
ExecutionSandbox (isolated)
        ↓ execute
StateRoot
        ↓ verify
Commit -> new CommittedLedger
        ↓
RuntimeLedger sync
```

Codebase mapping:

- `CommittedLedger` = committed execution parent lineage / trusted execution cache
- `ExecutionSandbox` = cloned ledger used for deterministic block execution
- `RuntimeLedger` = current `n.Ledger`

## Step-by-step fix plan

### Step 1 - Ledger separation (mandatory)

Current bad model:

```text
RuntimeLedger = execution base
```

Correct model:

```text
Execution always = CommittedLedger.Clone()
```

Rule:

- runtime kabhi execution ka base nahi hoga

### Step 2 - Execution sandbox engine

Implement isolated execution:

```go
type ExecutionSandbox struct {
    Ledger *Ledger
}

func NewSandbox(parent *Ledger) *ExecutionSandbox {
    return &ExecutionSandbox{
        Ledger: parent.Clone(),
    }
}
```

Intent:

- har block execution isolated environment me hoga
- runtime mutation aur consensus execution mix nahi honge

### Step 3 - Preflight guard (critical control)

Before execution:

```go
if runtime.Hash() != committed.Hash() {
    runtime = committed.Clone()
}
```

Intent:

- drift instantly detect ho
- runtime wrong base banne se pehle repair ho

### Step 4 - Deterministic execution pipeline

Execution flow:

```text
receive proposal
↓
fetch parent committed state
↓
clone sandbox
↓
execute txs
↓
compute root
↓
compare with proposal
```

### Step 5 - Remove runtime mutation

Wrong:

```go
runtimeLedger.ApplyTx()
```

Correct:

```go
sandbox.ApplyTx()
```

### Step 6 - Restart-safe recovery

Startup pe:

```go
ExecutionLedger = LoadSnapshot()
RuntimeLedger   = ExecutionLedger.Clone()
```

Intent:

- restart ke baad deterministic parent state immediately restore ho
- partial runtime rebuild consensus ko poison na kare

### Step 7 - Execution hash stability

Har validator ko same hash produce karna chahiye:

```go
hash := ComputeStateRoot(sandbox)

broadcast(hash)
```

## Config-level improvements

Recommended config direction:

```toml
[execution]
strict_determinism = true
runtime_isolation = true
sandbox_enabled = true
```

## Debug mode (immediate visibility)

Add visibility logs:

```go
log.Printf("EXEC-CHECK parent=%s runtime=%s", parent.Hash(), runtime.Hash())
```

Expected steady state:

```text
parent == runtime
```

Important note:

- before post-block effects, parent/runtime alignment expected hai
- after post-block effects, runtime local mutation allowed ho sakti hai, lekin next consensus execution base phir bhi committed parent hi hona chahiye

## Enterprise safety controls

### 1. Drift auto-recovery

```go
if mismatch_detected {
    reset runtime
}
```

### 2. Execution replay system

```text
block -> re-execute -> compare root
```

### 3. Consensus guard

```go
if root mismatch {
    reject block
    // do not mutate runtime
}
```

## Future-proof upgrade

### Deterministic engine v2

Add:

1. State versioning
   `state_v1 -> state_v2`
2. Merkle tree upgrade
   current simple hash -> future merkle root + proof
3. Parallel execution later
   sandbox shards

## Core strategy (single line)

> Execution ko runtime se alag karo aur har block ko same parent state par run karo.

## Exact kya karna hai (action plan)

### 1. Ledger discipline fix karo

One rule:

- committed state = source of truth
- runtime = ignore for consensus base

Meaning:

- block execution kabhi bhi runtime state se start nahi hoga

### 2. Har block ke liye fresh execution karo

Har block execute karte time:

- parent state uthao
- uska copy banao
- us copy par execution karo

Rules:

- reuse mat karo
- stale cache par blindly depend mat karo

### 3. Runtime drift zero karo

Execution se pehle:

```text
if runtime != committed -> runtime reset
```

Intent:

- restart bug aur mismatch bug dono contain hon

### 4. Execution isolation enforce karo

Execution ke dauran:

- runtime update mat karo
- global consensus state mutate mat karo

Rule:

- execution sandbox me hona chahiye

### 5. Strict preflight rule lagao

Block accept tabhi ho:

```text
calculated root == proposed root
```

Agar mismatch:

- block reject
- runtime touch mat karo

### 6. Restart recovery fix karo

Node restart hone par:

- snapshot load karo
- runtime ko usi trusted execution base se initialize karo

Rule:

- partial rebuild allowed nahi honi chahiye for consensus base

### 7. Consensus safety lock lagao

Agar 2-3 rounds tak mismatch aaye:

- force runtime reset
- fresh execution restart karo

### 8. Snapshot + execution sync align karo

Rule:

- snapshot state = execution base
- snapshot aur execution lineage alag nahi hone chahiye

Clarification:

- runtime post-effects local ho sakte hain
- lekin snapshot parent aur execution parent mismatch nahi karne chahiye

### 9. Execution determinism enforce karo

Goal:

- same input -> same output on every validator

Avoid:

- unordered map dependence
- random logic
- time-based execution logic

### 10. Observability add karo

Track:

- `parent_hash`
- `runtime_hash`
- `execution_hash`

Goal:

- mismatch turant detect ho

## Validation check after fix

Logs me yeh dikhna chahiye:

```text
expected_root == block_root
EXEC-QUORUM reached
Height finalized -> 16 -> 17 -> 18
```

Aur yeh repeated symptom disappear hona chahiye:

```text
reason=state_root_mismatch
startup_execution_snapshot_rebuilt_h_*
round failover loop without finalization
```

## Enterprise-grade fix (mandatory)

Codebase naming note:

- `RuntimeLedger` in this RCA maps to `n.Ledger` in code
- `ExecutionLedger` is the deterministic execution lineage

### Fix 1 - Execution sandbox (critical)

Execution must be isolated from runtime mutation.

```go
func ExecuteDeterministic(parentLedger *Ledger, block *Block) Result {
    // Clone to isolate execution from runtime/post-effects state.
    sandbox := parentLedger.Clone()

    result := ApplyBlock(sandbox, block)

    return result
}
```

Codebase intent:

- deterministic execution must always run from committed parent execution state
- runtime/post-block effects must not become the parent state source for consensus execution

### Fix 2 - Runtime reset guard (very important)

If runtime and deterministic execution state drift too far, runtime should be repairable from execution state.

```go
func EnsureLedgerConsistency(n *Node) {
    if n.RuntimeLedger.Hash() != n.ExecutionLedger.Hash() {
        log.Println("[LEDGER-RESET] fixing drift")

        n.RuntimeLedger = n.ExecutionLedger.Clone()
    }
}
```

Codebase mapping:

```go
// In this codebase:
// RuntimeLedger   == n.Ledger
// ExecutionLedger == n.ExecutionLedger
```

This guard should run only where runtime repair is safe, not blindly inside post-effects paths.

### Fix 3 - Preflight hard check

Preflight should reject or repair obvious runtime drift before using the wrong parent.

```go
if runtimeLedgerHash != parentLedgerHash {
    log.Println("[FATAL] runtime drift detected")

    runtimeLedger = parentLedger.Clone()
}
```

Important clarification:

- consensus execution should still derive from `parentLedger`
- runtime repair is a safety net, not the primary correctness mechanism

### Fix 4 - Execution source lock

Execution must always use committed parent execution state.

Required:

```go
parent := GetCommittedLedger(height - 1)
```

Never:

```go
runtime := n.RuntimeLedger
```

Codebase equivalent:

- use cached/trusted execution parent lineage
- do not derive consensus execution root from mutable `n.Ledger`

### Fix 5 - Disable runtime mutation during consensus

Consensus should not mutate runtime state from uncommitted traffic.

Wrong:

```go
n.RuntimeLedger.ApplyTx(tx)
```

Correct:

```go
n.Mempool.Add(tx)
```

Intent:

- runtime ledger mutates on committed state application and post-block effects
- mempool admission must not mutate committed execution lineage

### Fix 6 - Snapshot restore correction

On startup, deterministic execution state should be restored first, then runtime should align from it.

```go
n.ExecutionLedger = LoadSnapshot()
n.RuntimeLedger = n.ExecutionLedger.Clone()
```

Codebase equivalent:

- snapshot restore now seeds both `n.ExecutionLedger` and `n.Ledger`
- trusted execution snapshots must not corrupt live execution cache ownership

## Production hardening

### 1. Deterministic execution hash audit

```go
if localHash != quorumHash {
    panic("NON-DETERMINISTIC EXECUTION DETECTED")
}
```

### 2. Execution replay engine

block replay

-> state diff

-> hash verify

### 3. Ledger versioning

- `CommittedLedger` (immutable)
- `RuntimeLedger` (mutable)
- `ExecutionSandbox` (isolated)
