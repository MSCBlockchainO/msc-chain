# MSC Production V2 Topology

Genesis SHA-256: `f6230e42861022d676ca43c57d0f3b6c0984c3cd17bfcd932059c353ab46821c`

Linux amd64 binary SHA-256:
`5eaa0926bd4ab15f31a652b08ee9acdd9bc861e232fea86eb473faa7b089bc33`

All nodes listen for P2P traffic on TCP `7001`. Validator RPC remains bound to
loopback. Node, validator, wallet, and libp2p identities are distinct and are
stored only in each machine's `/home/<user>/.msc/production-v2/data` directory.

| Name | Public IP | Role | Node ID | Validator ID |
| --- | --- | --- | --- | --- |
| V1 | `54.80.4.133` | Validator | `msc_LdcsQerKQABuPcEH5Gbf9nVHatgugP` | `VAL_6741169D69FD8782020C68F6667AF703` |
| V2 | `98.90.205.156` | Validator | `msc_hqXsrNVjurvxa4iTdxhCv87ASbLrmz` | `VAL_80BCBF5246B10BFD9A83C9AAB668F25B` |
| V3 | `3.88.214.207` | Validator | `msc_gCnFa8vzrPp2otHMowcARRupYgGLeV` | `VAL_247E8B0257337C830835C48EDD113C2A` |
| V4 | `34.201.64.103` | Validator | `msc_faQEyTfifRpn5wt3wTWkUpj2Ef3VPR` | `VAL_36EDF7FFA87C6C5E0D0E53BF216F3F20` |
| V5 | `3.109.153.171` | Validator | `msc_FtCqTmDgJtpKSeazpjUwQ6wse5mVwa` | `VAL_B84F325C74FCD2102493B7B778B4DF49` |
| V6 | `47.129.174.99` | Validator | `msc_TvnSqLkaZKSfaBPppfTbaLLtambwes` | `VAL_A8ECC6229EC1C475210D529C775AE7A6` |
| Explorer | `50.19.167.221` | Public full node and web gateway | `msc_E8NrzejsbTZpHWvSyosGAVbN5deHtr` | Observer |
| Archive | `54.89.222.13` | Archive full node | `msc_Zd7yj3DZcM4wRrKsLjdyMSvNfhiZNQ` | Observer |
| Indexer | `18.61.229.92` | Full node and explorer indexer | `msc_STRw8e2qDmSmnZoURMTqBAf3z7ws3f` | Observer |
| Standby | `56.155.122.236` | Snapshot and standby full node | Pending SSH access | Observer |

Public seed nodes:

```text
/ip4/54.80.4.133/tcp/7001/p2p/12D3KooWC3z8AMHZSEoP9WA7J1c5MDqNbfQuXCJv8kusMGGZeKt4
/ip4/3.109.153.171/tcp/7001/p2p/12D3KooWBz6EfQ2d39GoeQSmxxXC8bbZKZRX9kuE46A71xqndd93
/ip4/47.129.174.99/tcp/7001/p2p/12D3KooWRR1uHrvBh29BhhrfGRZzjLGHMJgU9nCY661GLQUuiCWw
/ip4/18.61.229.92/tcp/7001/p2p/12D3KooWSgQjVMthN8VYBkTvYZAdPPDbaJEXX3nCuiWYU5DCPPpS
```

## Runtime Services

| Machines | Service | RPC/API |
| --- | --- | --- |
| V1-V6 | `msc-production-v2-node.service` | Validator RPC on loopback |
| Explorer | `msc-production-v2-explorer-node.service` | `127.0.0.1:26661` |
| Archive | `msc-production-v2-archive-node.service` | `172.31.19.170:26666` |
| Indexer | `msc-production-v2-index-source.service` | `127.0.0.1:26657` |
| Indexer | `msc-production-v2-indexer.service` | `127.0.0.1:26780` |
| Indexer | `msc-production-v2-indexer-tunnel.service` | Restricted reverse tunnel to the gateway |

The gateway exposes the archive through `/archive-rpc/` and the read-only
indexer through `/indexer/`. Both backends use loopback-only reverse tunnels;
validator RPC is not exposed.

The index source host is a 4GB-class full/archive source and has a local systemd
override for `msc-production-v2-index-source.service`:
`MSC_RUNTIME_MEMORY_LIMIT_MIB=2048` and `GOGC=80`. This keeps its runtime guard
less constrained during catch-up while preserving node identity and chain data.

Legacy services and chain data are retained on each reachable host for recovery
but remain disabled. Standby `56.155.122.236` has public P2P `7001` reachable
from the gateway and the port answers with a libp2p multistream banner, but it
is not yet visible in `/v1/peers`. MSC static peer dials require a full
`/p2p/<peer-id>` multiaddr, so TCP `22` access is still required before its
fresh observer identity and service can be installed and verified.
