# MSC Founding Validators Program

Program: `MSC Founding Validators Program`  
Legacy API key: `[testnet_campaign]`  
Default duration: 4 weeks  
Rewards: reputation points and badges only  
Primary community: Discord  
Announcement mirror: Telegram

## Participant Quick Start

Windows:

```powershell
.\msc-node.exe install candidate --id HOME1 --low-ram --auto-start
.\msc-node.exe doctor --id HOME1 --role candidate --datadir data --json
.\msc-node.exe backup --id HOME1 --datadir data --out D:\msc-backups
```

Ubuntu/Linux:

```bash
./msc install candidate --id HOME1 --low-ram --auto-start
./msc doctor --id HOME1 --role candidate --datadir data --json
./msc backup --id HOME1 --datadir data --out "$HOME/msc-backups"
```

Submit the node ID, validator ID if applicable, operator ID, OS, region/provider/Home-PC status, and `msc doctor --json` output in Discord.

## Useful-Node Scoring

Reputation is based on useful participation, not just a process being online. A node earns online/uptime points only when it has healthy sync, peer health, useful vote/block participation where available, no stale height, and no severe weekly fault.

| Action | Points |
| --- | ---: |
| Useful node online sample/day | 10 |
| Weekly signed ratio at or above 95% | 100 |
| First healthy `msc doctor` proof | 50 |
| Home-PC validator weekly bonus at or above 95% | 50 |

Anti-farming weights:

| Same operator validator | Reputation weight |
| --- | ---: |
| First | 100% |
| Second | 50% |
| Third and later | 10% |

Home-PC bonus is awarded once per operator per week. Slashed or jailed validators remain visible on the leaderboard but cannot earn weekly top badges for that week.

## Bug Reports

Bug reports are reviewed off-chain through Discord or GitHub forms. The node stores reviewed campaign records only; there is no unauthenticated public bug-submit endpoint in v1.

| Severity | Points |
| --- | ---: |
| Critical | 1000 |
| High | 500 |
| Medium | 250 |
| Low | 100 |
| Docs/UI | 50 |
| Duplicate | 0 |
| Duplicate but useful accepted report | max 5 |
| Invalid | 0 |

Reporter cooldown: maximum 3 scored reports per reporter per 24 hours. Extra reports can still be queued for manual review, but they do not receive automatic points.

## Badge Rules

- `MSC Founder`: first 100 eligible validators during the first 30 campaign days, with signed ratio at or above 95%, useful-node health, and no severe fault.
- `MSC Genesis Validator`: early genesis or founder-qualified validator with signed ratio at or above 95%, useful-node health, and no severe fault.
- `MSC Home Validator`: verified Home-PC validator; one bonus per operator per week.
- `MSC Uptime Hero`: useful node with signed ratio at or above 99% and no slash.
- `MSC Bug Hunter`: accepted bug report.
- `Critical Hunter`: accepted critical bug report.
- `Early Builder`: manual review badge for useful tooling or integrations.
- `Community Helper`: manual review badge for support contributions.
- `Documentation Contributor`: accepted docs or UI report/contribution.
- `Governance Pioneer`: manual review badge for governance participation.
- `Decentralization Champion`: high decentralization score without severe fault.

Founder NFT semantics: the award-time rule is strict, but a valid minted Founder NFT is historical proof. Later weekly performance drops can block weekly badges and ranking awards, but they do not silently delete the already-minted Founder NFT.

## Leaderboard Categories

- Overall Reputation
- Most Stable Validator
- Fastest Sync Node
- Best Peer Connectivity
- Home-PC Champion
- Longest Consecutive Uptime
- Bug Hunter
- Community Contributor
- Decentralization Champion

## Snapshot Storage And Audit Log

Weekly leaderboard snapshots are frozen so later score changes do not rewrite published results.

```text
data/
└── campaign/
    └── <season_id>/
        ├── week_1.json
        ├── week_2.json
        ├── week_3.json
        ├── week_4.json
        ├── bug_reports.json
        └── campaign_events.log
```

Audit events are JSONL records. Expected event types include `badge_awarded`, `points_added`, `bug_accepted`, `bug_rejected`, `uptime_bonus`, `snapshot_published`, and `operator_weight_applied`.

## Operator API

Campaign status:

```bash
curl -s https://mscblockexplorer.in/v1/testnet/campaign
```

Weekly snapshot export:

```bash
curl -s "https://mscblockexplorer.in/v1/testnet/campaign/export?format=json&week=1"
curl -s "https://mscblockexplorer.in/v1/testnet/campaign/export?format=csv&week=1"
```

Validator leaderboard:

```bash
curl -s https://mscblockexplorer.in/v1/validators/leaderboard
```

Public validators page:

```text
https://mscblockexplorer.in/validators.html
```

## Discord Structure

- `#start-here`: program rules, install commands, links.
- `#node-support`: setup, sync, backup, and repair help.
- `#bug-reports`: structured bug reports and triage.
- `#leaderboard`: weekly rank snapshots.
- `#announcements`: official notices.
- `#validator-chat`: validator operator coordination.

Telegram mirrors announcements and points users back to Discord or the bug report form for support and triage.

## Weekly Leaderboard Template

```text
MSC Founding Validators Program - Week N

Overall Reputation
1. <validator> - <points> pts
2. <validator> - <points> pts
3. <validator> - <points> pts

Most Stable Validator
1. <validator> - <signed_ratio>%

Home-PC Champion
1. <validator> - <points> pts

Decentralization Champion
1. <validator> - <score>

Bug Hunter
1. <validator> - <bug_points> pts

Notes:
- Snapshot source: /v1/validators/leaderboard
- Campaign source: /v1/testnet/campaign
- Export source: /v1/testnet/campaign/export?format=csv&week=N
- Published snapshot is frozen for this week.
```
