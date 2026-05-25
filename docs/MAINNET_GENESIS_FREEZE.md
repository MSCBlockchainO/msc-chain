# MSC Mainnet Genesis Freeze

This file records the production genesis artifact for MSC mainnet. It is a launch lock, not a test fixture.

## Frozen Chain Identity

| Field | Value |
| --- | --- |
| Network | MSC Mainnet |
| Chain ID | `91938` |
| Genesis file | `genesis.json` |
| Genesis SHA256 | `757cfedec4d164c077a5efaaa7a85e0386940cbfbe955812651e406acf09e0a0` |

`config.toml` and the compiled `GenesisHashExpected` default must both point at this hash.

## Frozen Validator Set

| Validator | Consensus public key | Reward wallet | Genesis stake | Lock epochs |
| --- | --- | --- | --- | --- |
| `A` | `ee8d74edce9d8b17f814be3d76eb8b1c47ea4aec85db9d0b69eb1c6d3123e897` | `MSC017d78d2c1920db5321271a2d594a4995a3c5ba99d` | `100` | `19872000` |
| `B` | `fa810f44ad831ed6be3ab7e1ccece48972eb2572d521369f9f4055a9972d3932` | `MSC01102bdf87789381354be6ec8af1f49688306ea83c` | `100` | `19872000` |
| `C` | `0f71ba143c9a7b2f614733888774c6113aea766402ad5e2c2848af205446fd3a` | `MSC01dc7b2c81d1211199f209a52a9688a31352f3b800` | `100` | `19872000` |
| `D` | `d6766aec7323b5d425bdb861ee3b8b34794fd07bed9a6b92606c64ad18e28ce8` | `MSC01d8f4952c11e683aac3cf6652513cd90982e4a938` | `100` | `19872000` |

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
- Do not use temporary EC2, local chaos-test, or backup validator keys in `genesis.json`.
- Do not mutate `genesis.json` after launch. Any change creates a different chain.
- If final pre-launch validator keys or allocations change, update `genesis.json`, `config.toml`, `var.go`, this document, and `production_genesis_test.go` together, then publish the new hash.
- Operators must verify `sha256(genesis.json)` before starting a mainnet node.

Guard test:

```powershell
go test . -run "TestProductionGenesis" -count=1
```
