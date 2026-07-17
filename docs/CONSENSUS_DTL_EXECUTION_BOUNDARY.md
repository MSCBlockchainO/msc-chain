# Consensus / DTL Execution Boundary

MSC keeps consensus and native DTL in the same validator process, but they are
separate modules with a one-way deterministic interface.

```text
consensus/runtime adapter
  ordered block + committed validator authority
                    |
                    v
deterministic block execution engine
  MSC transfer/stake/update rules
                    |
                    v
native DTL executor
  parent NativeDTLState + ordered DTL txs + height
                    |
                    v
next state + receipts
                    |
                    v
StateRoot + ReceiptsRoot + FeeRoot -> consensus verification
```

## Ownership

- Consensus owns validator-set resolution, proposer/leader selection, rounds,
  votes, quorum certificates, ordering, and finality.
- Block execution owns deterministic MSC balance, nonce, stake, and committed
  validator-update transitions.
- Native DTL owns token, NFT, pool, lending, game, oracle, DTL event-log, DTL
  governance replay, and bridge-event replay state.
- The state persistence coordinator stores the next ledger. Consensus consumes
  execution commitments; it does not use DTL internals for authority decisions.

## Hard invariants

The native DTL executor receives only `NativeDTLState`, ordered transactions,
and block height. It must never read:

- `Node`, blockchain, mempool, P2P, peer, heartbeat, vote, or quorum state;
- local/runtime configuration or presentation maps;
- wall-clock time, randomness, HTTP/RPC, goroutines, or network state.

The block execution engine receives a cloned parent ledger, an ordered block,
and a committed `BlockExecutionAuthority` snapshot. Runtime `Node` reads stop in
`execution_consensus_adapter.go`.

Leader, committee, and quorum functions must never read DTL state. A DTL state
change can change execution commitments, but cannot change proposer selection,
validator membership, voting power, or quorum thresholds.

## Compatibility

`Ledger.UsedBridgeEvents` remains the persisted projection of DTL bridge replay
state, preserving existing snapshot JSON and ledger-hash byte layout. The DTL
executor owns and clones that data through `NativeDTLState`; consensus does not
interpret it.

Legacy public execution helpers remain adapters for tests, RPC, recovery, and
snapshot replay. Proposal construction, block replay, state-root calculation,
and receipt verification all route through the deterministic execution engine.

The regression suite in `advanced_execution_boundary_test.go` enforces module
isolation, parent immutability, deterministic commitments, local-map tamper
resistance, and DTL-independent consensus selection. It also includes a DTL
determinism fuzz target.
