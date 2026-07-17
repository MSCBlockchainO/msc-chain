# MSC Mainnet Genesis Freeze

This file records the production genesis artifact for MSC mainnet. It is a launch lock, not a test fixture.

## Frozen Chain Identity

| Field | Value |
| --- | --- |
| Network | MSC Mainnet |
| Chain ID | `91938` |
| Genesis file | `genesis.json` |
| Genesis SHA256 | `d6d7d96ea1a70d2aca31389ce7ef7953794ce77b4c933828295269702768fa3c` |

`config.toml` and the compiled `GenesisHashExpected` default must both point at this hash.

## Frozen Validator Set

| Validator | Consensus public key | Reward wallet | Genesis stake | Lock epochs |
| --- | --- | --- | --- | --- |
| `A` | `f180a970fa11c67b961d79b9fe4cd362da47e5f2816ab1654d4032af0b23658b` | `MSC017d78d2c1920db5321271a2d594a4995a3c5ba99d` | `100` | `19872000` |
| `B` | `bbd7aac5cf70150dd2565a67342950e79f7eeb7a3fbd2ebc353b1d95302d0a88` | `MSC01102bdf87789381354be6ec8af1f49688306ea83c` | `100` | `19872000` |
| `C` | `d3d2c0a3201f85f83c857103803915200616378263e48da0fe973e7e6ff6fa88` | `MSC01dc7b2c81d1211199f209a52a9688a31352f3b800` | `100` | `19872000` |
| `D` | `e26e21281f1adf98dfde8c76cd858edd21b0c323e55f6bd80623bb0354eafec4` | `MSC01d8f4952c11e683aac3cf6652513cd90982e4a938` | `100` | `19872000` |

## Frozen Allocation

| Account | Balance |
| --- | ---: |
| `MSC017d78d2c1920db5321271a2d594a4995a3c5ba99d` | `1379173540` |
| `MSC01102bdf87789381354be6ec8af1f49688306ea83c` | `4597011801` |
| `MSC01dc7b2c81d1211199f209a52a9688a31352f3b800` | `100000` |
| `MSC01d8f4952c11e683aac3cf6652513cd90982e4a938` | `100000` |
| `USER_REW` | `1000000` |

Frozen explicit genesis balance total: `5977385341`.

## Frozen Foundation And Treasury

| Pool | Wallet | Allocation | Lock |
| --- | --- | ---: | --- |
| Foundation | `MSC017d78d2c1920db5321271a2d594a4995a3c5ba99d` | `1379073540` | locked for `19872000` epochs |
| Treasury | `MSC01102bdf87789381354be6ec8af1f49688306ea83c` | `4596911801` | locked, governance-only |

## Production Rules

- Do not generate validator keys during production genesis startup.
- Do not use unapproved local chaos-test or backup validator keys in `genesis.json`.
- Do not mutate `genesis.json` after launch. Any change creates a different chain.
- If final pre-launch validator keys or allocations change, update `genesis.json`, `config.toml`, `var.go`, this document, and `production_genesis_test.go` together, then publish the new hash.
- Operators must verify `sha256(genesis.json)` before starting a mainnet node.

Guard test:

```powershell
go test . -run "TestProductionGenesis" -count=1
```

Release gate:

```powershell
.\scripts\build_mainnet_release.ps1 -VersionTag v1.0.0-mainnet
```

Publish `genesis.json` SHA256 together with the binary checksums from `dist/<version>/checksums.txt`.
