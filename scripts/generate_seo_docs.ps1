param(
    [string]$OutputDir = (Join-Path $PSScriptRoot "..\ui\docs"),
    [string]$MainBaseUrl = "https://mscblockexplorer.in",
    [string]$DocsBaseUrl = "https://mscblockexplorer.in/docs"
)

$ErrorActionPreference = "Stop"
$OutputDir = [IO.Path]::GetFullPath($OutputDir)
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null

function NormalizeBaseUrl {
    param(
        [Parameter(Mandatory = $true)][string]$Value,
        [Parameter(Mandatory = $true)][string]$Name
    )
    $normalized = $Value.Trim().TrimEnd("/")
    if (-not [uri]::IsWellFormedUriString($normalized, [UriKind]::Absolute)) {
        throw "$Name must be an absolute URL: $Value"
    }
    $uri = [uri]$normalized
    if ($uri.Scheme -ne "https") {
        throw "$Name must use HTTPS: $Value"
    }
    return $normalized
}

$MainBaseUrl = NormalizeBaseUrl $MainBaseUrl "MainBaseUrl"
$DocsBaseUrl = NormalizeBaseUrl $DocsBaseUrl "DocsBaseUrl"
$DocsPathPrefix = ([uri]$DocsBaseUrl).AbsolutePath.TrimEnd("/")
if ($DocsPathPrefix -eq "/") {
    $DocsPathPrefix = ""
}
$DocsRootUrl = "$DocsBaseUrl/"
$DocsRootHref = if ($DocsPathPrefix) { "$DocsPathPrefix/" } else { "/" }
$DocsStylesheetHref = if ($DocsPathPrefix) { "$DocsPathPrefix/docs.css?v=20260618a" } else { "/docs/docs.css?v=20260618a" }
$AssetLogoUrl = "$MainBaseUrl/assets/msc-logo-512.png"
$FeedXmlUrl = "$MainBaseUrl/feed.xml"
$FeedJsonUrl = "$MainBaseUrl/feed.json"

function DocsUrl([string]$Slug) {
    if ([string]::IsNullOrWhiteSpace($Slug)) {
        return $script:DocsRootUrl
    }
    return "$script:DocsBaseUrl/$Slug.html"
}

function DocsHref([string]$Slug) {
    if ([string]::IsNullOrWhiteSpace($Slug)) {
        return $script:DocsRootHref
    }
    if ($script:DocsPathPrefix) {
        return "$script:DocsPathPrefix/$Slug.html"
    }
    return "/$Slug.html"
}

function XmlEncode([string]$Value) {
    return [Security.SecurityElement]::Escape($Value)
}

function Topic {
    param(
        [string]$Slug,
        [string]$Title,
        [string]$Subject,
        [string]$Description,
        [string]$Category,
        [string]$Audience,
        [string]$Goal,
        [string[]]$Principles,
        [string[]]$Steps,
        [string[]]$Risks
    )
    return @{
        Slug        = $Slug
        Title       = $Title
        Subject     = $Subject
        Description = $Description
        Category    = $Category
        Audience    = $Audience
        Goal        = $Goal
        Principles  = $Principles
        Steps       = $Steps
        Risks       = $Risks
    }
}

$topics = @(
    (Topic "what-is-msc-chain" "What Is MSC Chain? A Practical Network Overview" "MSC Chain" "Understand MSC Chain as a Layer-1 blockchain, including its validators, native MSC coin, explorer, wallet, governance, APIs, and verification model." "Fundamentals" "new users, researchers, operators, developers, and ecosystem partners" "understand what the network does and how to verify its public claims" @("Layer-1 ownership of consensus and state", "Public verification through the explorer and APIs", "Validator participation and deterministic finality", "Self-custody access through the MSC wallet") @("Start with the official portal and identify the canonical domains", "Open the explorer and compare height with finalized height", "Inspect several recent blocks and their proposers", "Review the validator set and public node status", "Read the tokenomics and governance references", "Use testnet tools before committing production value") @("confusing a testnet feature with mainnet policy", "trusting screenshots instead of current chain data", "using unofficial wallet or support links"))
    (Topic "msc-tokenomics" "MSC Tokenomics: Supply, Fees, Rewards and Burn" "MSC tokenomics" "Learn how MSC supply, transaction fees, validator rewards, staking, treasury flows, and burn policy fit into the network economy." "Economics" "coin holders, validators, builders, analysts, and governance participants" "evaluate economic policy using transparent on-chain and configuration evidence" @("Supply must be measured from authoritative chain data", "Fees align transaction demand with network resources", "Validator rewards compensate verifiable security work", "Governance changes require explicit public evidence") @("Record the current total and circulating supply", "Review issuance and reward parameters", "Inspect fee handling for common transaction types", "Compare validator and treasury distributions", "Track burns and blocked reward routing", "Recheck policy after every network upgrade") @("treating estimates as guaranteed returns", "ignoring activation heights for policy changes", "using stale third-party supply figures"))
    (Topic "run-msc-validator" "How to Run an MSC Validator" "an MSC validator" "Plan, install, secure, monitor, and maintain an MSC validator with reliable keys, peers, storage, backups, and upgrade procedures." "Operators" "professional operators, home validators, infrastructure teams, and security reviewers" "operate a validator that participates reliably without exposing signing material" @("Validator identity and signing keys must remain stable", "Consensus readiness matters more than process uptime alone", "Peer diversity reduces correlated network failure", "Backups and recovery drills are operating requirements") @("Choose hardware, region, and connectivity", "Install the verified MSC node binary", "Create or restore the validator identity", "Configure persistent peers and private RPC", "Synchronize fully before enabling participation", "Monitor, back up, upgrade, and test recovery") @("losing or rotating the validator key accidentally", "exposing RPC or password files publicly", "joining consensus before state and validator sets are synchronized"))
    (Topic "run-msc-node" "How to Run an MSC Full Node" "an MSC full node" "Install and operate an MSC full node for independent verification, wallet access, RPC service, explorer support, or network resilience." "Operators" "developers, exchanges, explorers, validators, researchers, and infrastructure teams" "run a synchronized node with safe public and private service boundaries" @("A full node independently verifies blocks and state", "RPC exposure must be separated from validator authority", "Storage planning depends on retention and archive policy", "Health checks must include height, peers, and finality") @("Select hardware and a supported operating system", "Download or build a verified release", "Configure data directory and peer connectivity", "Start initial synchronization", "Restrict and monitor RPC access", "Plan updates, snapshots, and recovery") @("running on an undersized disk", "publishing administrative RPC methods", "assuming a running process is synchronized"))
    (Topic "msc-wallet-guide" "MSC Wallet Guide: Create, Receive and Send Safely" "MSC Wallet" "Use the official MSC self-custody wallet to create an account, protect recovery material, receive MSC, send transactions, and review activity." "Wallet" "new and experienced MSC users who want direct control of their account" "complete common wallet actions without exposing private keys or recovery words" @("Self-custody means the user controls recovery material", "Addresses can be shared but private keys cannot", "Every send should be reviewed before signing", "Explorer verification completes the transaction workflow") @("Open the official wallet domain", "Create a wallet with a strong local password", "Record and verify the recovery phrase offline", "Receive a small test amount first", "Send a small transaction and verify it", "Review security and backup practices regularly") @("sharing a seed phrase with support impersonators", "sending to an unverified address", "depending on a single browser or device backup"))
    (Topic "msc-rpc-api" "MSC RPC API Documentation and Integration Guide" "the MSC RPC API" "Integrate with MSC Chain through public JSON-RPC, REST, explorer, balance, status, validator, governance, and WebSocket endpoints." "Developers" "application developers, indexers, exchanges, wallets, dashboards, and infrastructure engineers" "build reliable read and write integrations with clear validation and retry behavior" @("Read endpoints and write endpoints have different risk", "Versioned APIs improve compatibility", "Clients must validate chain ID, height, and response shape", "Rate limits and failover are part of production design") @("Choose the minimum endpoint set required", "Confirm chain identity and health", "Implement typed response validation", "Add timeout, retry, and backoff behavior", "Handle transaction submission idempotently", "Monitor latency, errors, and chain lag") @("sending signed transactions to an untrusted host", "retrying writes without idempotency controls", "accepting stale data without height checks"))
    (Topic "msc-explorer-guide" "MSC Explorer Guide: Verify Blocks and Transactions" "MSC Explorer" "Learn how to use MSC Explorer to verify blocks, transactions, addresses, validators, network health, tokenomics, and governance." "Fundamentals" "wallet users, developers, validators, analysts, and support teams" "turn explorer data into reproducible verification instead of passive viewing" @("A block height is only one part of chain context", "Finalized data is stronger than an unfinalized head", "Proposer and quorum fields explain block production", "Hashes connect records and reveal inconsistencies") @("Search a recent block height", "Inspect proposer, hash, previous hash, and quorum", "Open a transaction and review its status", "Compare address activity with the wallet", "Review validators and network health", "Save canonical explorer links for incident reports") @("treating pending data as finalized", "copying shortened hashes as identifiers", "using a look-alike explorer domain"))
    (Topic "msc-governance" "MSC Governance: Proposals, Voting and Upgrades" "MSC governance" "Understand MSC Chain proposals, voting, treasury context, council boundaries, upgrade activation, and transparent decision evidence." "Governance" "coin holders, validators, delegates, developers, and ecosystem observers" "evaluate governance decisions from proposal creation through activation and audit" @("Governance authority must be bounded by protocol rules", "Proposal text and executable effect are separate evidence", "Voting windows and quorum determine legitimacy", "Activation height makes approved changes auditable") @("Read the complete proposal and linked rationale", "Identify proposer, scope, and requested action", "Verify voting dates, quorum, and thresholds", "Review implementation and security impact", "Track approval and activation height", "Audit chain behavior after activation") @("voting from a summary without reading the payload", "confusing social consensus with executed governance", "failing to verify activation after approval"))
    (Topic "msc-roadmap" "MSC Chain Roadmap: Network, Ecosystem and Operations" "the MSC Chain roadmap" "Review the MSC Chain roadmap across consensus reliability, validators, wallets, developer tooling, governance, interoperability, and ecosystem growth." "Governance" "community members, builders, operators, partners, and long-term researchers" "interpret roadmap items as measurable commitments rather than guaranteed dates" @("Reliability milestones precede feature expansion", "Public acceptance criteria make progress verifiable", "Security work continues after launch", "Ecosystem growth depends on documentation and tooling") @("Separate completed, active, planned, and research work", "Attach measurable acceptance criteria to each milestone", "Publish dependencies and operational risks", "Link releases and chain activation evidence", "Update status when assumptions change", "Archive previous roadmap versions for accountability") @("treating target dates as protocol guarantees", "hiding dependency or security uncertainty", "measuring progress only by feature count"))
    (Topic "msc-whitepaper" "MSC Chain Whitepaper: Architecture and Economic Design" "the MSC Chain whitepaper" "Read a structured guide to MSC Chain architecture, consensus, networking, storage, execution, validator security, economics, and governance." "Fundamentals" "technical readers, validators, developers, analysts, partners, and governance reviewers" "connect whitepaper claims to current implementation and public verification surfaces" @("Architecture claims should map to implementation components", "Consensus safety and liveness require explicit assumptions", "Economic incentives support but do not replace verification", "Protocol evolution needs versioned activation rules") @("Read the system model and trust assumptions", "Study consensus and validator-set rules", "Review state, storage, and execution commitments", "Examine networking and synchronization behavior", "Evaluate economics and governance controls", "Compare the document with live explorer evidence") @("reading design goals as deployed guarantees", "ignoring version and activation differences", "using the whitepaper instead of operational documentation"))
    (Topic "msc-vs-ethereum" "MSC Chain vs Ethereum: Technical Comparison" "MSC Chain and Ethereum" "Compare MSC Chain and Ethereum across consensus, validators, execution, fees, tooling, decentralization, maturity, and suitable use cases." "Comparisons" "developers, researchers, users, and teams evaluating blockchain platforms" "make a fact-based platform decision without treating different designs as interchangeable" @("Network maturity and ecosystem size affect risk", "Consensus and finality models shape application assumptions", "Execution compatibility is not the same as operational equivalence", "Fees, tooling, and decentralization require current measurements") @("Define application requirements before comparing", "Compare consensus and finality assumptions", "Evaluate wallet and developer tooling", "Measure fees and throughput under relevant load", "Review validator and infrastructure diversity", "Run a prototype on test environments") @("using marketing throughput instead of measured conditions", "ignoring Ethereum ecosystem maturity", "assuming a smaller network has identical security economics"))
    (Topic "msc-vs-solana" "MSC Chain vs Solana: Architecture and Use Cases" "MSC Chain and Solana" "Compare MSC Chain and Solana across consensus, execution, performance, validator requirements, tooling, operations, and ecosystem maturity." "Comparisons" "builders, operators, analysts, and product teams comparing high-throughput networks" "identify meaningful design differences and test them against application requirements" @("Performance depends on workload and hardware", "Validator requirements influence decentralization", "Execution architecture affects developer experience", "Operational history matters alongside benchmark results") @("Document latency and throughput requirements", "Compare state and execution models", "Review validator hardware and networking", "Assess SDK, wallet, and indexer support", "Test failure and recovery behavior", "Choose based on measured product constraints") @("comparing peak numbers from different tests", "ignoring infrastructure cost", "assuming ecosystem tools have equivalent coverage"))
    (Topic "msc-validator-rewards" "MSC Validator Rewards: How Earnings and Costs Work" "MSC validator rewards" "Understand MSC validator reward sources, eligibility, participation, fees, operating costs, penalties, and responsible return estimates." "Economics" "current and prospective validators, delegates, analysts, and governance participants" "estimate validator economics without promising fixed returns" @("Rewards depend on protocol policy and participation", "Gross rewards differ from net operator returns", "Reliability and correct signing affect eligibility", "Policy can change only through visible activation rules") @("Identify current reward parameters", "Measure stake and participation requirements", "Estimate infrastructure and staffing costs", "Model downtime and penalty scenarios", "Track actual payouts with explorer evidence", "Recalculate after upgrades or validator-set changes") @("advertising guaranteed yield", "excluding downtime and infrastructure cost", "using outdated reward parameters"))
    (Topic "build-dapps-on-msc" "Build DApps on MSC Chain: Architecture and Workflow" "DApp development on MSC Chain" "Plan, build, test, secure, and operate decentralized applications using MSC Chain wallets, transactions, RPC, events, and explorer APIs." "Developers" "frontend, backend, protocol, wallet, and product developers" "ship a verifiable application with reliable transaction and data handling" @("Chain state is the source of truth", "Wallet authorization must be explicit and minimal", "Transactions need deterministic status handling", "Observability belongs in the initial architecture") @("Define the on-chain and off-chain boundary", "Connect to testnet RPC and verify chain ID", "Implement wallet address and signing flows", "Submit and track transactions", "Index the minimum required public data", "Test failures, upgrades, and production monitoring") @("trusting client-provided transaction status", "exposing signing secrets to application servers", "building without retry and finality states"))
    (Topic "msc-explorer-tutorial" "MSC Explorer Tutorial: A Step-by-Step Walkthrough" "the MSC Explorer tutorial" "Follow a practical MSC Explorer walkthrough for searching blocks, transactions, addresses, validators, governance, and network status." "Fundamentals" "first-time explorer users, support teams, testers, and community educators" "complete a repeatable chain-verification session from homepage search to finality evidence" @("Search results should lead to canonical records", "Block and transaction context must be read together", "Finality changes the strength of an observation", "Network status explains temporary data behavior") @("Open the official explorer and note current height", "Search a block and inspect its linked fields", "Open a transaction from the block", "Search an address involved in the transfer", "Review proposer and validator participation", "Record canonical URLs and hashes for reference") @("searching partial identifiers", "confusing current height with confirmations", "sharing screenshots without canonical links"))
    (Topic "msc-token-creation-guide" "MSC Token Creation Guide: Design Before Deployment" "MSC token creation" "Plan an MSC-based token with clear supply, permissions, distribution, metadata, security, testing, and governance decisions." "Developers" "token issuers, application teams, governance designers, auditors, and integrators" "turn a token concept into a testable specification before any deployment action" @("Supply and authority rules must be explicit", "Metadata should be stable and verifiable", "Distribution creates security and governance consequences", "Testing must cover privileged and failure paths") @("Write the token purpose and threat model", "Define supply, decimals, mint, and burn authority", "Design allocation and vesting rules", "Prepare metadata and explorer representation", "Test creation and transfer flows on testnet", "Publish documentation and monitor launch activity") @("retaining undocumented mint authority", "launching without distribution transparency", "treating token creation as legal or financial approval"))
    (Topic "msc-testnet-guide" "MSC Testnet Guide: Wallets, Faucet, Nodes and Testing" "MSC testnet" "Use MSC testnet to create wallets, request test MSC, submit transactions, run nodes, test APIs, and validate applications safely." "Developers" "users, developers, validators, QA teams, and infrastructure operators" "reproduce realistic workflows without treating test assets as valuable mainnet funds" @("Testnet assets have no promised monetary value", "Test environments should mirror production behavior where possible", "Failure testing is as important as happy-path testing", "Results must record versions and chain identity") @("Confirm the testnet chain ID and status", "Create a dedicated test wallet", "Request faucet tokens within policy", "Send and verify a transaction", "Test application or node integration", "Record defects with logs, hashes, and versions") @("reusing mainnet recovery phrases", "assuming testnet uptime guarantees", "reporting issues without reproducible evidence"))
    (Topic "msc-staking-guide" "MSC Staking Guide: Validators, Delegation and Risk" "MSC staking" "Understand MSC staking, validator selection, delegation, activation, rewards, unstaking, governance, and operational risk." "Economics" "MSC holders, validators, delegates, analysts, and wallet users" "make an informed staking decision using current validator and protocol data" @("Staking contributes to network security", "Validator selection is a risk decision", "Rewards are variable and policy-dependent", "Activation and unstaking can require waiting periods") @("Read current staking policy", "Compare validator reliability and status", "Confirm commission and reward assumptions", "Delegate a small amount first", "Track activation and rewards", "Plan unstaking and account recovery") @("chasing yield without validator review", "ignoring lock or activation periods", "delegating from an insecure wallet"))
    (Topic "msc-wallet-security" "MSC Wallet Security: Recovery Phrase and Key Protection" "MSC wallet security" "Protect MSC wallet recovery phrases, private keys, passwords, devices, backups, addresses, and transaction approvals." "Wallet" "every MSC wallet user, support team, validator operator, and application integrator" "reduce the likelihood and impact of account compromise or permanent key loss" @("Recovery words control the account", "Offline backups reduce remote compromise", "Transaction review prevents many irreversible errors", "Recovery must be tested without exposing secrets") @("Use the official wallet domain", "Create a unique strong local password", "Record recovery words offline", "Verify receive addresses independently", "Review every transaction before signing", "Test recovery in a safe isolated environment") @("typing recovery words into support forms", "storing screenshots in cloud backups", "installing unverified wallet software"))
    (Topic "msc-node-monitoring" "MSC Node Monitoring: Health, Peers and Finality" "MSC node monitoring" "Monitor MSC node process health, synchronization, peers, finality, consensus mode, storage, memory, latency, and public RPC service." "Operators" "node operators, validators, SRE teams, exchanges, explorers, and incident responders" "detect meaningful chain or infrastructure degradation before users are affected" @("Process uptime alone is not chain health", "Height must be compared with trusted peers", "Finality and consensus mode reveal deeper issues", "Alerts need actionable thresholds and context") @("Collect status and health endpoints", "Track height, finalized height, and block age", "Monitor peer count and peer diversity", "Measure CPU, memory, disk, and latency", "Alert on consensus and RPC failures", "Practice incident diagnosis and recovery") @("alerting only when the process exits", "ignoring disk growth and file descriptors", "using one node as its own source of truth"))
    (Topic "msc-validator-security" "MSC Validator Security: Keys, Sentries and Operations" "MSC validator security" "Secure an MSC validator with protected signing keys, restricted RPC, sentry topology, backups, monitoring, access control, and incident response." "Operators" "validator operators, security engineers, auditors, and infrastructure teams" "protect validator authority while preserving reliable consensus participation" @("Signing material should have the smallest possible exposure", "Public networking and signing roles should be separated", "Identity stability prevents consensus and operational failure", "Incident response must preserve forensic evidence") @("Define the validator threat model", "Harden the host and operator access", "Protect password files and key backups", "Use sentries and private RPC boundaries", "Monitor signing, peers, and consensus behavior", "Prepare key-loss and compromise procedures") @("copying validator keys between uncontrolled hosts", "exposing admin RPC to the internet", "rotating identity during an incident without a protocol plan"))
    (Topic "msc-consensus-explained" "MSC Consensus Explained: Proposers, Votes and Quorum" "MSC consensus" "Understand MSC Chain proposer selection, validator committees, execution results, quorum, block commitment, finality, and recovery behavior." "Protocol" "developers, validators, auditors, researchers, and technically curious users" "interpret consensus evidence shown by the explorer and node status APIs" @("Validator-set authority must be deterministic", "The proposer creates a candidate but quorum secures commitment", "Execution results bind validators to deterministic state", "Recovery rules must preserve safety across delays") @("Identify the validator set for a height", "Determine the expected proposer and round", "Review signatures and execution results", "Compare required quorum with collected evidence", "Confirm block and state commitments", "Track finality and any recovery mode") @("equating proposer authority with unilateral control", "counting signatures without membership checks", "ignoring validator-set activation heights"))
    (Topic "msc-finality-explained" "MSC Finality Explained: Confirmations and Safety" "MSC finality" "Learn how MSC Chain finalized height, confirmation count, quorum evidence, block age, and network health affect transaction confidence." "Protocol" "wallet users, exchanges, developers, operators, and risk teams" "choose confirmation policies based on explicit finality evidence" @("Head height and finalized height are different", "Finality relies on accepted consensus evidence", "Applications should define risk-based confirmation rules", "Stalled finality requires operational attention") @("Read current height and finalized height", "Calculate finality lag", "Inspect the target block and transaction", "Check consensus mode and validator readiness", "Apply the application's confirmation policy", "Monitor for finality progress or incident notices") @("crediting deposits from an unfinalized head", "using a fixed delay without chain evidence", "ignoring degraded consensus mode"))
    (Topic "msc-transaction-lifecycle" "MSC Transaction Lifecycle: From Signing to Finality" "an MSC transaction" "Follow an MSC transaction through creation, signing, submission, mempool admission, block inclusion, execution, receipt, and finality." "Protocol" "wallet developers, users, exchanges, indexers, support teams, and auditors" "diagnose transaction status accurately at every stage" @("A transaction identifier should be deterministic", "Submission acceptance is not final execution", "Block inclusion links the transaction to consensus", "Receipts and finality complete verification") @("Construct the intended transaction", "Review and sign with the correct account", "Submit to a trusted RPC endpoint", "Track mempool or pending status", "Verify block inclusion and execution", "Wait for the required finality policy") @("resubmitting a transaction without nonce checks", "treating an RPC response as final settlement", "failing to inspect execution errors"))
    (Topic "msc-fees-guide" "MSC Transaction Fees: Estimation and Verification" "MSC transaction fees" "Understand MSC Chain fee inputs, estimation, payment, proposer and validator distribution, treasury policy, and explorer verification." "Economics" "wallet users, developers, exchanges, validators, and analysts" "estimate fees accurately and explain the resulting on-chain charge" @("Fees price network resource use", "Estimation should use current policy and transaction shape", "Applications need clear maximum-cost displays", "Actual charges should be verifiable after execution") @("Identify the transaction type and payload", "Query current fee policy where available", "Estimate total cost before signing", "Display amount and fee separately", "Submit and inspect the resulting receipt", "Monitor fee changes after upgrades") @("hard-coding fees indefinitely", "hiding fees inside transfer amounts", "using fee estimates from another network"))
    (Topic "msc-bridge-guide" "MSC Bridge Guide: Routes, Risks and Verification" "the MSC bridge" "Understand MSC bridge readiness, supported routes, lock or mint flows, confirmations, relayers, limits, fees, and security checks." "Wallet" "users, integrators, liquidity operators, developers, and risk reviewers" "evaluate a bridge route and verify each transfer stage before moving value" @("A bridge adds trust and software assumptions", "Source and destination finality both matter", "Route status and limits can change", "Canonical token identity must be verified") @("Confirm the official bridge and route", "Review assets, limits, fees, and status", "Verify source and destination addresses", "Submit a small transfer first", "Track source confirmation and relay state", "Verify destination receipt and token identity") @("using an unofficial bridge interface", "bridging the wrong token representation", "repeating a transfer while relay status is uncertain"))
    (Topic "msc-governance-voting-guide" "MSC Governance Voting Guide" "MSC governance voting" "Review proposals, understand voting choices, verify wallet readiness, submit a vote, and confirm governance participation on MSC Chain." "Governance" "eligible voters, delegates, validators, proposal authors, and community educators" "cast an informed vote and verify that it was recorded correctly" @("Voting begins with reading the full proposal", "Eligibility and timing must be checked before signing", "A vote transaction should express one clear choice", "Participation should be verified on-chain") @("Open the canonical proposal record", "Read scope, rationale, risks, and execution details", "Check eligibility and voting deadline", "Unlock the correct wallet safely", "Review and submit the vote transaction", "Verify inclusion and final governance status") @("voting from social media summaries", "signing from the wrong account", "assuming submission means the vote was finalized"))
    (Topic "msc-public-node-api" "MSC Public Node API and Health Guide" "MSC public node APIs" "Use MSC public node discovery and health data to select RPC services, compare height and latency, and implement safe client failover." "Developers" "wallet developers, exchanges, indexers, monitoring teams, and infrastructure engineers" "consume public RPC infrastructure without depending blindly on one backend" @("Healthy nodes must agree on chain identity", "Height and finality are stronger signals than HTTP status", "Failover needs deterministic selection rules", "Write requests require stricter handling than reads") @("Fetch the public node registry", "Filter by role, health, and chain identity", "Compare height, finality, and latency", "Select a stable read backend", "Pin write requests and track outcomes", "Continuously re-evaluate unhealthy nodes") @("round-robin writes without transaction tracking", "selecting nodes by latency alone", "accepting a node on the wrong chain"))
    (Topic "msc-snapshot-recovery" "MSC Snapshot and Node Recovery Guide" "MSC snapshot recovery" "Use verified MSC snapshots, manifests, chunks, state commitments, backups, and recovery checks to restore a node safely." "Operators" "node operators, validators, SRE teams, archive providers, and disaster-recovery owners" "restore chain state without accepting an unverified or incompatible snapshot" @("Snapshot metadata must bind to state commitments", "Chunks require integrity verification", "Validator and registry commitments protect consensus context", "Recovery should preserve backups and audit evidence") @("Stop the affected node and preserve evidence", "Identify a trusted snapshot height and source", "Verify manifest, hashes, and compatibility", "Download and validate all chunks", "Import into a clean recovery path", "Start, synchronize, and compare chain status") @("overwriting the only recoverable data copy", "importing an unverified snapshot", "starting validator participation before full verification"))
    (Topic "msc-developer-getting-started" "MSC Developer Getting Started Guide" "MSC development" "Set up an MSC Chain development workflow with documentation, testnet wallet, faucet, RPC access, transactions, explorer verification, and monitoring." "Developers" "new MSC developers, hackathon teams, integration engineers, and technical evaluators" "complete a first verified integration and establish production-ready habits" @("Start on testnet with isolated credentials", "Use canonical APIs and validate every response", "Explorer links make debugging reproducible", "Security and observability should begin with the prototype") @("Read the network and transaction model", "Create a dedicated testnet wallet", "Request test MSC from the faucet", "Connect to status and balance APIs", "Submit and verify a test transaction", "Add typed errors, logs, and health monitoring") @("using production keys in development", "copying unchecked example payloads", "shipping without finality and error handling"))
)

function HtmlEncode([string]$Value) {
    return [Net.WebUtility]::HtmlEncode($Value)
}

function CategoryImage([string]$Category) {
    switch ($Category) {
        "Operators" { return "../assets/msc-validator-badge.png" }
        "Wallet" { return "../assets/msc-wallet-icon.png" }
        "Developers" { return "../assets/msc-explorer-icon.png" }
        "Economics" { return "../assets/msc-app-icon-192.png" }
        "Governance" { return "../assets/msc-governance-badge.png" }
        "Protocol" { return "../assets/msc-validator-badge.png" }
        "Comparisons" { return "../assets/msc-logo-192.png" }
        default { return "../assets/msc-logo-192.png" }
    }
}

function NavHtml {
    return @"
<header class="docs-header">
  <nav class="docs-nav" aria-label="Documentation navigation">
    <a class="docs-brand" href="$DocsRootHref"><img src="../assets/msc-logo-64.png" width="38" height="38" alt="MSC Chain logo" /><span>MSC Chain Docs</span></a>
    <div class="docs-links">
      <a href="$DocsRootHref">Docs</a>
      <a href="/blog/">Blog</a>
      <a href="https://explorer.mscblockexplorer.in/">Explorer</a>
      <a href="https://wallet.mscblockexplorer.in/">Wallet</a>
      <a href="https://github.com/MSCBlockchainO/msc-chain">GitHub</a>
    </div>
  </nav>
</header>
"@
}

function FooterHtml {
    return @'
<footer class="docs-footer">
  <div class="docs-footer-inner">
    <span>MSC Chain documentation</span>
    <span>Verify current network data with the official explorer and APIs.</span>
  </div>
</footer>
'@
}

$published = "2026-06-18"
$allLinks = $topics | ForEach-Object { DocsHref $_.Slug }

foreach ($topic in $topics) {
    $canonical = DocsUrl $topic.Slug
    $image = CategoryImage $topic.Category
    $principleHtml = for ($i = 0; $i -lt $topic.Principles.Count; $i++) {
        $principle = HtmlEncode $topic.Principles[$i]
        @"
        <h3>$principle</h3>
        <p>$principle is a practical requirement when working with $($topic.Subject). It should be translated into observable checks, documented assumptions, and evidence that another person can reproduce. For MSC Chain, that evidence normally includes canonical documentation, current status responses, explorer records, validator or node health, hashes, heights, and activation information. Treat a statement as provisional until those sources agree.</p>
        <p>Teams should assign ownership for this principle and decide what happens when evidence is missing or contradictory. A useful process records the network, chain height, software version, endpoint, timestamp, and expected result. That context prevents an old screenshot, cached response, or testnet observation from being mistaken for current mainnet behavior.</p>
"@
    }

    $stepsHtml = for ($i = 0; $i -lt $topic.Steps.Count; $i++) {
        $stepNumber = $i + 1
        $step = HtmlEncode $topic.Steps[$i]
        @"
        <li>
          <strong>Step ${stepNumber}: $step.</strong>
          Complete this step deliberately and retain enough evidence to repeat it. Confirm the canonical domain, relevant chain ID, current height, and expected output before moving forward. When a wallet, node, validator, API, or governance action is involved, begin with the smallest reversible or testnet operation. Record identifiers such as block height, transaction ID, validator ID, endpoint, version, or configuration hash so the outcome can be verified independently.
        </li>
"@
    }

    $riskHtml = $topic.Risks | ForEach-Object {
        "<li><strong>$(HtmlEncode $_):</strong> reduce this risk with explicit verification, least-privilege access, small initial tests, current backups, and a documented rollback or recovery path.</li>"
    }

    $faq = @(
        @{
            Question = "What is the purpose of this $($topic.Subject) guide?"
            Answer = "$($topic.Description) The guide is designed to help $($topic.Audience) reach a specific outcome: $($topic.Goal)."
        },
        @{
            Question = "What should I verify before I begin?"
            Answer = "Confirm that you are using mscblockexplorer.in, explorer.mscblockexplorer.in, or wallet.mscblockexplorer.in as appropriate. Check chain identity, current height, finalized height, software version, and the status of any wallet, node, validator, API, or governance dependency used by the workflow."
        },
        @{
            Question = "How can I confirm the result on MSC Chain?"
            Answer = "Use the official MSC Explorer to inspect the relevant block, transaction, address, validator, proposal, or network record. Save the canonical URL and full identifiers rather than relying only on screenshots or shortened hashes."
        },
        @{
            Question = "What is the safest first action?"
            Answer = "Begin with this step: $($topic.Steps[0]). Use testnet or a small reversible operation where possible, record the result, and continue only after the expected chain evidence is visible."
        }
    )
    $faqHtml = $faq | ForEach-Object {
        "<details><summary>$(HtmlEncode $_.Question)</summary><p>$(HtmlEncode $_.Answer)</p></details>"
    }
    $faqSchema = $faq | ForEach-Object {
        @{
            "@type" = "Question"
            name = $_.Question
            acceptedAnswer = @{
                "@type" = "Answer"
                text = $_.Answer
            }
        }
    }

    $related = @($topics | Where-Object { $_.Slug -ne $topic.Slug -and $_.Category -eq $topic.Category } | Select-Object -First 3)
    if ($related.Count -lt 3) {
        $related += @($topics | Where-Object { $_.Slug -ne $topic.Slug -and $_.Slug -notin $related.Slug } | Select-Object -First (3 - $related.Count))
    }
    $relatedHtml = $related | ForEach-Object { "<a href=`"$(DocsHref $_.Slug)`">$(HtmlEncode $_.Title)</a>" }

    $schema = @{
        "@context" = "https://schema.org"
        "@graph" = @(
            @{
                "@type" = "Article"
                "@id" = "$canonical#article"
                headline = $topic.Title
                description = $topic.Description
                image = $AssetLogoUrl
                datePublished = $published
                dateModified = $published
                mainEntityOfPage = $canonical
                author = @{
                    "@type" = "Organization"
                    name = "MSC Chain"
                    url = "$MainBaseUrl/"
                }
                publisher = @{
                    "@type" = "Organization"
                    name = "MSC Chain"
                    url = "$MainBaseUrl/"
                    logo = @{
                        "@type" = "ImageObject"
                        url = $AssetLogoUrl
                    }
                }
            },
            @{
                "@type" = "BreadcrumbList"
                itemListElement = @(
                    @{"@type" = "ListItem"; position = 1; name = "MSC Chain"; item = "$MainBaseUrl/"},
                    @{"@type" = "ListItem"; position = 2; name = "Documentation"; item = $DocsRootUrl},
                    @{"@type" = "ListItem"; position = 3; name = $topic.Title; item = $canonical}
                )
            },
            @{
                "@type" = "FAQPage"
                mainEntity = $faqSchema
            }
        )
    } | ConvertTo-Json -Depth 12 -Compress

    $article = @"
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>$(HtmlEncode $topic.Title) | MSC Chain Docs</title>
  <meta name="description" content="$(HtmlEncode $topic.Description)" />
  <meta name="robots" content="index,follow,max-image-preview:large,max-snippet:-1,max-video-preview:-1" />
  <meta name="theme-color" content="#07090e" />
  <link rel="canonical" href="$canonical" />
  <link rel="alternate" type="application/rss+xml" title="MSC Chain Blog RSS" href="$FeedXmlUrl" />
  <link rel="alternate" type="application/feed+json" title="MSC Chain Blog JSON Feed" href="$FeedJsonUrl" />
  <link rel="icon" type="image/png" href="../assets/msc-app-icon-64.png" />
  <meta property="og:type" content="article" />
  <meta property="og:site_name" content="MSC Chain" />
  <meta property="og:title" content="$(HtmlEncode $topic.Title)" />
  <meta property="og:description" content="$(HtmlEncode $topic.Description)" />
  <meta property="og:url" content="$canonical" />
  <meta property="og:image" content="$AssetLogoUrl" />
  <meta name="twitter:card" content="summary_large_image" />
  <meta name="twitter:title" content="$(HtmlEncode $topic.Title)" />
  <meta name="twitter:description" content="$(HtmlEncode $topic.Description)" />
  <meta name="twitter:image" content="$AssetLogoUrl" />
  <link rel="stylesheet" href="$DocsStylesheetHref" />
  <script type="application/ld+json">$schema</script>
</head>
<body>
$(NavHtml)
<div class="article-shell">
  <article class="article-body">
    <header class="article-header">
      <p class="article-eyebrow">$(HtmlEncode $topic.Category) guide</p>
      <img src="$image" width="88" height="88" alt="" />
      <h1>$(HtmlEncode $topic.Title)</h1>
      <p class="article-lead">$(HtmlEncode $topic.Description)</p>
      <div class="article-meta"><span>Published $published</span><span>Updated $published</span><span>By MSC Chain</span></div>
    </header>

    <section id="overview">
      <h2>Overview</h2>
      <p>$(HtmlEncode $topic.Description) This reference is written for $($topic.Audience). Its practical goal is to help readers $($topic.Goal). The subject should be approached through current, reproducible evidence rather than assumptions, promotional claims, or copied configuration that has not been checked against the active network.</p>
      <p>MSC Chain exposes several verification surfaces for this work. The main portal establishes the official ecosystem entry point. MSC Explorer shows blocks, transactions, validators, network health, tokenomics, governance, and related records. MSC Wallet supports self-custody workflows. Public status, REST, RPC, explorer, and WebSocket endpoints provide machine-readable context for applications and operators. Documentation explains intended behavior, while the chain and running software show what is active now.</p>
      <p>Before applying any instruction, identify whether it concerns mainnet, testnet, a local development environment, or a future policy. Record the chain ID, current height, finalized height, software version, canonical URL, and date. This small discipline prevents most confusion caused by stale screenshots, old guides, cached APIs, or parameters that changed at a known activation height.</p>
      <div class="callout"><strong>Verification rule</strong><span>Use documentation to understand intent, then use explorer and API evidence to confirm current network behavior.</span></div>
    </section>

    <section id="why-it-matters">
      <h2>Why $($topic.Subject) matters</h2>
      <p>$($topic.Subject) affects more than one screen or command. It can influence user funds, validator participation, application reliability, governance decisions, operational recovery, or the interpretation of public chain data. A strong workflow therefore separates facts, assumptions, configuration, and observed results. It also defines who owns each decision and what evidence is required before the next irreversible step.</p>
      <p>For a new blockchain ecosystem, clear documentation has an additional role: it allows independent users, search engines, AI systems, developers, and reviewers to understand the same entity without relying on private explanations. Consistent names, canonical links, dates, examples, and cross-links create a public knowledge graph around MSC Chain. That improves discoverability, but more importantly it makes technical claims easier to test.</p>
    </section>

    <section id="principles">
      <h2>Core principles</h2>
$($principleHtml -join "`n")
    </section>

    <section id="workflow">
      <h2>Step-by-step workflow</h2>
      <ol>
$($stepsHtml -join "`n")
      </ol>
    </section>

    <section id="security">
      <h2>Security and reliability checklist</h2>
      <p>Security for $($topic.Subject) is not a single setting. It is the combination of canonical software and domains, protected credentials, independent verification, limited privileges, current backups, safe defaults, monitoring, and an incident process. Treat unexpected prompts for recovery words, private keys, validator secrets, password files, remote access, or urgent transfers as hostile until independently verified.</p>
      <ul>
$($riskHtml -join "`n")
        <li><strong>Stale information:</strong> compare the guide date and software version with current status, release, explorer, and governance evidence before production use.</li>
        <li><strong>Insufficient audit trail:</strong> retain full hashes, heights, timestamps, versions, commands, and redacted logs so another operator can reproduce the result.</li>
      </ul>
      <p>When value or consensus authority is involved, use a two-person review for high-impact changes. Separate preparation from execution, verify backups before the change, define rollback criteria, and monitor the chain after completion. A successful command is not enough; the expected network state must also appear.</p>
    </section>

    <section id="verification">
      <h2>How to verify success</h2>
      <p>Verification should answer three questions. First, did the local tool, wallet, node, or interface report success? Second, did an independent MSC endpoint or explorer record show the same identifier and result? Third, did the outcome remain valid after the required confirmations or finality condition? If one answer is missing, describe the state as pending or unverified instead of complete.</p>
      <p>Use full identifiers in reports and support requests. Include the relevant canonical page, block height, transaction ID, validator ID, proposal ID, endpoint, chain ID, software version, and observed timestamp. Remove private keys, recovery phrases, passwords, authorization tokens, private peer addresses, and other secrets. This gives maintainers useful evidence without creating a second security incident.</p>
    </section>

    <section id="faq">
      <h2>Frequently asked questions</h2>
      <div class="faq-list">
$($faqHtml -join "`n")
      </div>
    </section>

    <section id="next">
      <h2>Next steps</h2>
      <p>Continue with the related guides, then validate the workflow in the lowest-risk environment available. For wallet and application work, begin on testnet with dedicated credentials. For node and validator work, use a staging host or non-authoritative node before changing a production signer. For governance and economic analysis, save the proposal, policy, activation, and explorer evidence together.</p>
      <p>MSC Chain documentation will evolve with software releases and protocol activation. Revisit this page after upgrades, compare the modified date, and use the live explorer to confirm that examples still match current behavior.</p>
    </section>
  </article>

  <aside class="article-aside" aria-label="Article navigation">
    <div class="aside-panel">
      <strong>On this page</strong>
      <a href="#overview">Overview</a>
      <a href="#why-it-matters">Why it matters</a>
      <a href="#principles">Core principles</a>
      <a href="#workflow">Workflow</a>
      <a href="#security">Security</a>
      <a href="#verification">Verification</a>
      <a href="#faq">FAQ</a>
    </div>
    <div class="aside-panel">
      <strong>Related guides</strong>
$($relatedHtml -join "`n")
    </div>
  </aside>
</div>
$(FooterHtml)
</body>
</html>
"@

    $plain = [regex]::Replace($article, '<[^>]+>', ' ')
    $wordCount = ([regex]::Matches([Net.WebUtility]::HtmlDecode($plain), "\b[\p{L}\p{N}'-]+\b")).Count
    if ($wordCount -lt 1000 -or $wordCount -gt 2500) {
        throw "Article $($topic.Slug) word count $wordCount is outside 1000-2500"
    }
    [IO.File]::WriteAllText((Join-Path $OutputDir "$($topic.Slug).html"), $article, [Text.UTF8Encoding]::new($false))
}

$categoryOrder = @("Fundamentals", "Wallet", "Operators", "Developers", "Protocol", "Economics", "Governance", "Comparisons")
$categorySections = foreach ($category in $categoryOrder) {
    $items = @($topics | Where-Object Category -eq $category)
    if (-not $items.Count) { continue }
    $cards = $items | ForEach-Object {
        $img = CategoryImage $_.Category
        "<a class=`"article-card`" href=`"$(DocsHref $_.Slug)`"><img src=`"$img`" width=`"46`" height=`"46`" alt=`"`" /><strong>$(HtmlEncode $_.Title)</strong><span>$(HtmlEncode $_.Description)</span></a>"
    }
    @"
    <section class="category-section" id="$($category.ToLowerInvariant())">
      <h2>$category</h2>
      <div class="article-grid">$($cards -join "")</div>
    </section>
"@
}

$indexItems = for ($i = 0; $i -lt $topics.Count; $i++) {
    @{
        "@type" = "ListItem"
        position = $i + 1
        url = (DocsUrl $topics[$i].Slug)
        name = $topics[$i].Title
    }
}
$indexSchema = @{
    "@context" = "https://schema.org"
    "@graph" = @(
        @{
            "@type" = "CollectionPage"
            "@id" = "$DocsRootUrl#page"
            url = $DocsRootUrl
            name = "MSC Chain Documentation"
            description = "Official MSC Chain documentation for users, developers, node operators, validators, governance participants, and researchers."
            isPartOf = @{"@id" = "$MainBaseUrl/#website"}
        },
        @{
            "@type" = "ItemList"
            itemListElement = $indexItems
        }
    )
} | ConvertTo-Json -Depth 10 -Compress

$index = @"
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>MSC Chain Documentation - Wallet, Nodes, Validators and API</title>
  <meta name="description" content="Official MSC Chain documentation for wallets, nodes, validators, staking, tokenomics, governance, explorer, APIs, security, and DApp development." />
  <meta name="robots" content="index,follow,max-image-preview:large,max-snippet:-1,max-video-preview:-1" />
  <meta name="theme-color" content="#07090e" />
  <link rel="canonical" href="$DocsRootUrl" />
  <link rel="alternate" type="application/rss+xml" title="MSC Chain Blog RSS" href="$FeedXmlUrl" />
  <link rel="alternate" type="application/feed+json" title="MSC Chain Blog JSON Feed" href="$FeedJsonUrl" />
  <link rel="icon" type="image/png" href="../assets/msc-app-icon-64.png" />
  <meta property="og:type" content="website" />
  <meta property="og:site_name" content="MSC Chain" />
  <meta property="og:title" content="MSC Chain Documentation" />
  <meta property="og:description" content="Guides for MSC Chain users, developers, node operators, validators, and governance participants." />
  <meta property="og:url" content="$DocsRootUrl" />
  <meta property="og:image" content="$AssetLogoUrl" />
  <meta name="twitter:card" content="summary_large_image" />
  <meta name="twitter:title" content="MSC Chain Documentation" />
  <meta name="twitter:description" content="Wallet, explorer, validator, node, API, tokenomics, governance, and security guides." />
  <meta name="twitter:image" content="$AssetLogoUrl" />
  <link rel="stylesheet" href="$DocsStylesheetHref" />
  <script type="application/ld+json">$indexSchema</script>
</head>
<body>
$(NavHtml)
<main class="docs-main">
  <header class="docs-hero">
    <p class="eyebrow">Official knowledge base</p>
    <h1>MSC Chain Documentation</h1>
    <p>Practical, verifiable guides for using MSC Wallet, exploring the chain, running nodes and validators, integrating APIs, understanding protocol behavior, and evaluating governance and economics.</p>
  </header>
$($categorySections -join "`n")
</main>
$(FooterHtml)
</body>
</html>
"@
[IO.File]::WriteAllText((Join-Path $OutputDir "index.html"), $index, [Text.UTF8Encoding]::new($false))

$llmsLines = @(
    "# MSC Chain",
    "",
    "> Official documentation and public verification surfaces for MSC Chain.",
    "",
    "## Canonical sites",
    "- Main portal: $MainBaseUrl/",
    "- Explorer: https://explorer.mscblockexplorer.in/",
    "- Wallet: https://wallet.mscblockexplorer.in/",
    "- Documentation: $DocsRootUrl",
    "- Blog: $MainBaseUrl/blog/",
    "- RSS feed: $FeedXmlUrl",
    "- JSON feed: $FeedJsonUrl",
    "",
    "## Documentation"
)
foreach ($topic in $topics) {
    $llmsLines += "- [$($topic.Title)]($(DocsUrl $topic.Slug)): $($topic.Description)"
}
$llmsLines += @(
    "",
    "## Verification guidance",
    "- Prefer canonical MSC domains and full identifiers.",
    "- Verify current claims against live explorer and API data.",
    "- Never request or disclose recovery phrases, private keys, validator keys, or passwords."
)
[IO.File]::WriteAllLines((Join-Path (Split-Path $OutputDir -Parent) "llms.txt"), $llmsLines, [Text.UTF8Encoding]::new($false))

$feedItemsXml = foreach ($topic in $topics) {
    $url = DocsUrl $topic.Slug
    $title = XmlEncode $topic.Title
    $description = XmlEncode $topic.Description
    $category = XmlEncode $topic.Category
@"
    <item>
      <title>$title</title>
      <link>$url</link>
      <guid isPermaLink="true">$url</guid>
      <description>$description</description>
      <category>$category</category>
      <pubDate>Thu, 18 Jun 2026 00:00:00 GMT</pubDate>
    </item>
"@
}
$rssFeed = @"
<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>MSC Chain Blog</title>
    <link>$MainBaseUrl/blog/</link>
    <description>Research, guides, explainers, and technical references for MSC Chain.</description>
    <language>en</language>
    <lastBuildDate>Thu, 18 Jun 2026 00:00:00 GMT</lastBuildDate>
    <image>
      <url>$AssetLogoUrl</url>
      <title>MSC Chain Blog</title>
      <link>$MainBaseUrl/blog/</link>
    </image>
$($feedItemsXml -join "`n")
  </channel>
</rss>
"@
[IO.File]::WriteAllText((Join-Path (Split-Path $OutputDir -Parent) "feed.xml"), $rssFeed.TrimEnd() + "`n", [Text.UTF8Encoding]::new($false))

$jsonItems = foreach ($topic in $topics) {
    @{
        id             = DocsUrl $topic.Slug
        url            = DocsUrl $topic.Slug
        title          = $topic.Title
        summary        = $topic.Description
        content_text   = "$($topic.Description) Read the canonical MSC Chain guide for $($topic.Subject), including verification steps, risks, FAQs, and internal links."
        image          = $AssetLogoUrl
        date_published = "2026-06-18T00:00:00Z"
        date_modified  = "2026-06-18T00:00:00Z"
        tags           = @("MSC Chain", $topic.Category)
    }
}
$jsonFeed = @{
    version       = "https://jsonfeed.org/version/1.1"
    title         = "MSC Chain Blog"
    home_page_url = "$MainBaseUrl/blog/"
    feed_url      = $FeedJsonUrl
    description   = "Research, guides, explainers, and technical references for MSC Chain."
    icon          = $AssetLogoUrl
    favicon       = "$MainBaseUrl/assets/msc-app-icon-64.png"
    authors       = @(@{
            name = "MSC Chain"
            url  = "$MainBaseUrl/"
        })
    items         = $jsonItems
} | ConvertTo-Json -Depth 8
[IO.File]::WriteAllText((Join-Path (Split-Path $OutputDir -Parent) "feed.json"), $jsonFeed.TrimEnd() + "`n", [Text.UTF8Encoding]::new($false))

Write-Host "Generated $($topics.Count) MSC documentation articles plus index, llms.txt, RSS feed, and JSON feed."
