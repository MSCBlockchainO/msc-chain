param(
    [string]$UiRoot = (Join-Path $PSScriptRoot "..\ui")
)

$ErrorActionPreference = "Stop"
$UiRoot = (Resolve-Path -LiteralPath $UiRoot).Path

function Page {
    param(
        [string]$Title,
        [string]$Description,
        [string]$Canonical,
        [string]$Heading,
        [string]$Robots = "index,follow,max-image-preview:large,max-snippet:-1,max-video-preview:-1"
    )
    return @{
        Title       = $Title
        Description = $Description
        Canonical   = $Canonical
        Heading     = $Heading
        Robots      = $Robots
    }
}

$pages = [ordered]@{
    "address-book.html" = Page "MSC Address Book - Trusted Wallet Contacts" "Save and manage trusted MSC Chain wallet addresses for safer transfers from the official self-custody wallet." "https://wallet.mscblockexplorer.in/address-book.html" "MSC Address Book" "noindex,follow"
    "bridge.html" = Page "MSC Bridge Status and Transfer Readiness" "Review MSC Chain bridge availability, supported routes, transfer readiness, and safety checks from the official wallet." "https://wallet.mscblockexplorer.in/bridge.html" "MSC Bridge"
    "create-wallet.html" = Page "Create an MSC Wallet - Secure Self-Custody Setup" "Create an encrypted MSC Chain wallet, protect the recovery phrase offline, and begin receiving MSC securely." "https://wallet.mscblockexplorer.in/create-wallet.html" "Create an MSC Wallet" "noindex,follow"
    "dashboard.html" = Page "MSC Wallet - Official Self-Custody Dashboard" "Open the official MSC wallet dashboard to review balances, receive MSC, send transactions, stake, and monitor network status." "https://wallet.mscblockexplorer.in/" "MSC Wallet"
    "dtl_ide.html" = Page "MSC DTL IDE - Protected Developer Workspace" "Protected MSC Chain developer workspace for testing DTL operations and inspecting development payloads." "https://explorer.mscblockexplorer.in/dtl_ide.html" "MSC DTL IDE" "noindex,nofollow,noarchive"
    "explorer.html" = Page "MSC Block Explorer - Live Blocks, Transactions and Validators" "Explore live MSC Chain blocks, transactions, addresses, validators, finality, network health, and governance data." "https://explorer.mscblockexplorer.in/" "MSC Block Explorer"
    "explorer-addresses.html" = Page "MSC Address Explorer - Search Wallet Activity" "Search MSC Chain addresses and inspect balances, transaction history, transfers, and related on-chain activity." "https://explorer.mscblockexplorer.in/explorer-addresses.html" "MSC Address Explorer"
    "explorer-analytics.html" = Page "MSC Chain Analytics - Network Activity and Trends" "Analyze MSC Chain block production, transaction activity, validators, finality, and network performance trends." "https://explorer.mscblockexplorer.in/explorer-analytics.html" "MSC Chain Analytics"
    "explorer-api.html" = Page "MSC Chain API - RPC, REST and WebSocket Endpoints" "Access official MSC Chain RPC, REST, explorer, public-node, and WebSocket endpoint documentation for developers." "https://explorer.mscblockexplorer.in/explorer-api.html" "MSC Chain API"
    "explorer-blocks.html" = Page "MSC Blocks - Latest Heights, Proposers and Quorum" "Browse recent MSC Chain blocks with height, proposer, type, transactions, execution results, quorum, and block hash." "https://explorer.mscblockexplorer.in/explorer-blocks.html" "MSC Blocks"
    "explorer-bridge.html" = Page "MSC Bridge Explorer - Cross-Chain Status and Safety" "Inspect MSC Chain bridge status, transfer controls, route readiness, and cross-chain security information." "https://explorer.mscblockexplorer.in/explorer-bridge.html" "MSC Bridge Explorer"
    "explorer-charts.html" = Page "MSC Chain Charts - Blocks, TPS and Validator Data" "View MSC Chain charts for block production, transaction throughput, validator participation, finality, and supply." "https://explorer.mscblockexplorer.in/explorer-charts.html" "MSC Chain Charts"
    "explorer-council.html" = Page "MSC Governance Council - Network Oversight" "Review MSC Chain governance council information, authority boundaries, proposals, and transparent network oversight." "https://explorer.mscblockexplorer.in/explorer-council.html" "MSC Governance Council"
    "explorer-epochs.html" = Page "MSC Epoch Explorer - Progress and Validator Rotation" "Track MSC Chain epoch progress, validator-set commitments, block ranges, activation heights, and consensus context." "https://explorer.mscblockexplorer.in/explorer-epochs.html" "MSC Epoch Explorer"
    "explorer-governance.html" = Page "MSC Governance Explorer - Proposals and Voting" "Inspect MSC Chain governance proposals, voting status, upgrade decisions, treasury context, and participation." "https://explorer.mscblockexplorer.in/explorer-governance.html" "MSC Governance Explorer"
    "explorer-mempool.html" = Page "MSC Mempool - Pending Transaction Activity" "Monitor pending MSC Chain transactions, queue pressure, block inclusion activity, and current network conditions." "https://explorer.mscblockexplorer.in/explorer-mempool.html" "MSC Mempool"
    "explorer-network.html" = Page "MSC Network Explorer - Peers, Health and Finality" "Monitor MSC Chain peer connectivity, public nodes, consensus mode, finality, quorum, and network health." "https://explorer.mscblockexplorer.in/explorer-network.html" "MSC Network Explorer"
    "explorer-nodes.html" = Page "MSC Nodes - Public RPC and Infrastructure Health" "Review MSC Chain public nodes, RPC health, latency, block height, consensus mode, and infrastructure availability." "https://explorer.mscblockexplorer.in/explorer-nodes.html" "MSC Public Nodes"
    "explorer-rich-list.html" = Page "MSC Rich List - Coin Distribution Overview" "Explore MSC coin distribution, leading balances, allocation context, and concentration indicators on MSC Chain." "https://explorer.mscblockexplorer.in/explorer-rich-list.html" "MSC Rich List"
    "explorer-search.html" = Page "Search MSC Chain - Blocks, Transactions and Addresses" "Search the MSC Chain explorer by block height, transaction ID, address, validator, hash, or governance proposal." "https://explorer.mscblockexplorer.in/explorer-search.html" "Search MSC Chain" "noindex,follow"
    "explorer-security.html" = Page "MSC Chain Security - Invariants and Validator Evidence" "Review MSC Chain security status, runtime invariants, validator incidents, consensus safety, and verification evidence." "https://explorer.mscblockexplorer.in/explorer-security.html" "MSC Chain Security"
    "explorer-settings.html" = Page "MSC Explorer Settings - Display and RPC Preferences" "Configure local MSC Explorer display, language, theme, notification, and RPC preferences." "https://explorer.mscblockexplorer.in/explorer-settings.html" "MSC Explorer Settings" "noindex,follow"
    "explorer-snapshots.html" = Page "MSC Snapshots - Verified Chain State Checkpoints" "Inspect MSC Chain snapshot availability, checkpoint height, state commitments, providers, and recovery metadata." "https://explorer.mscblockexplorer.in/explorer-snapshots.html" "MSC Chain Snapshots"
    "explorer-staking.html" = Page "MSC Staking Explorer - Validators and Delegation Data" "Review MSC Chain staking totals, validator participation, rewards context, activation, and delegation statistics." "https://explorer.mscblockexplorer.in/explorer-staking.html" "MSC Staking Explorer"
    "explorer-tokenomics.html" = Page "MSC Tokenomics - Supply, Fees, Rewards and Burn" "Explore MSC coin supply, issuance, fees, staking rewards, burn policy, allocations, and economic parameters." "https://explorer.mscblockexplorer.in/explorer-tokenomics.html" "MSC Tokenomics"
    "explorer-transactions.html" = Page "MSC Transactions - Search Transfers and Receipts" "Browse and search MSC Chain transactions, transfer details, execution results, confirmations, and receipts." "https://explorer.mscblockexplorer.in/explorer-transactions.html" "MSC Transactions"
    "explorer-treasury.html" = Page "MSC Treasury - Governance Funds and Transparency" "Review MSC Chain treasury policy, governance context, public allocations, and transparent fund activity." "https://explorer.mscblockexplorer.in/explorer-treasury.html" "MSC Treasury"
    "explorer-validators.html" = Page "MSC Validators - Performance, Quorum and Uptime" "Compare MSC Chain validators by status, participation, stake, performance, quorum readiness, and reliability." "https://explorer.mscblockexplorer.in/explorer-validators.html" "MSC Validators"
    "faucet.html" = Page "MSC Testnet Faucet - Request Test MSC" "Request test MSC for wallet, transaction, staking, and application testing with cooldown and network safety limits." "https://wallet.mscblockexplorer.in/faucet.html" "MSC Testnet Faucet"
    "governance.html" = Page "MSC Wallet Governance - Review and Vote" "Review MSC Chain governance proposals and prepare wallet-based voting actions with clear network context." "https://wallet.mscblockexplorer.in/governance.html" "MSC Wallet Governance"
    "index.html" = Page "MSC Wallet Start - Official Wallet Access" "Start with the official MSC Chain wallet to create or unlock an account and access self-custody tools." "https://wallet.mscblockexplorer.in/" "MSC Wallet Start" "noindex,follow"
    "login.html" = Page "Unlock MSC Wallet - Secure Local Access" "Unlock an encrypted MSC wallet locally and access balances, transfers, staking, and wallet activity." "https://wallet.mscblockexplorer.in/login.html" "Unlock MSC Wallet" "noindex,follow"
    "msc_wallet.html" = Page "MSC Wallet Legacy Entry" "Legacy redirect page for the official MSC Chain wallet dashboard, balances, transfers, staking, security, and network status." "https://wallet.mscblockexplorer.in/" "MSC Wallet" "noindex,follow"
    "nfts.html" = Page "MSC NFTs - Wallet Collectibles and Token Records" "View NFT and collectible records associated with the active MSC Chain wallet." "https://wallet.mscblockexplorer.in/nfts.html" "MSC NFTs"
    "receive.html" = Page "Receive MSC - Wallet Address and QR Code" "Display an MSC Chain wallet address and QR code for receiving MSC securely." "https://wallet.mscblockexplorer.in/receive.html" "Receive MSC" "noindex,follow"
    "security.html" = Page "MSC Wallet Security - Protect Keys and Recovery Words" "Learn how to secure an MSC wallet, recovery phrase, private keys, passwords, devices, and transaction approvals." "https://wallet.mscblockexplorer.in/security.html" "MSC Wallet Security"
    "send.html" = Page "Send MSC - Prepare a Secure Wallet Transfer" "Prepare and review an MSC Chain transfer with recipient, amount, fee, password, and confirmation checks." "https://wallet.mscblockexplorer.in/send.html" "Send MSC" "noindex,follow"
    "settings.html" = Page "MSC Wallet Settings - Security and Network Preferences" "Manage local MSC wallet security, network, display, and account preferences." "https://wallet.mscblockexplorer.in/settings.html" "MSC Wallet Settings" "noindex,follow"
    "staking.html" = Page "MSC Staking - Validators, Delegation and Rewards" "Explore MSC Chain staking, validator selection, delegation readiness, rewards, activation, and unstaking guidance." "https://wallet.mscblockexplorer.in/staking.html" "MSC Staking"
    "status.html" = Page "MSC Wallet Network Status - RPC, Nodes and Finality" "Check MSC wallet connectivity, RPC health, public nodes, finality, block age, and consensus status." "https://wallet.mscblockexplorer.in/status.html" "MSC Wallet Network Status"
    "swap.html" = Page "MSC Swap - Token Route Readiness" "Review token swap route availability, input and output assets, network status, and transaction readiness on MSC Chain." "https://wallet.mscblockexplorer.in/swap.html" "MSC Swap"
    "transactions.html" = Page "MSC Wallet Transactions - Account History" "Review transaction history, transfers, confirmations, and activity for the active MSC Chain wallet." "https://wallet.mscblockexplorer.in/transactions.html" "MSC Wallet Transactions" "noindex,follow"
    "validators.html" = Page "MSC Wallet Validators - Choose a Staking Operator" "Compare MSC Chain validators from the wallet before staking or delegation decisions." "https://wallet.mscblockexplorer.in/validators.html" "MSC Wallet Validators"
    "wallet.html" = Page "MSC Wallet Account - Balance and Self-Custody Tools" "Manage an unlocked MSC Chain wallet account, balance, transfers, security, staking, and activity." "https://wallet.mscblockexplorer.in/wallet.html" "MSC Wallet Account" "noindex,follow"
    "portal\campaign.html" = Page "MSC Founding Validators Program - Campaign Guide" "Learn about the MSC founding validators program, participation, reputation, badges, reporting, and testnet contribution." "https://explorer.mscblockexplorer.in/portal/campaign.html" "MSC Founding Validators Program"
    "portal\community.html" = Page "MSC Chain Community - Developer and Validator Links" "Find official MSC Chain community, source code, validator, developer, announcement, and support destinations." "https://explorer.mscblockexplorer.in/portal/community.html" "MSC Chain Community"
    "portal\contact.html" = Page "Contact MSC Chain - Technical and Validator Support" "Contact MSC Chain for technical support, validator operations, security reports, ecosystem, and community questions." "https://explorer.mscblockexplorer.in/portal/contact.html" "Contact MSC Chain"
    "portal\docs.html" = Page "MSC Documentation Portal - Operators and Developers" "Browse MSC Chain documentation for wallets, nodes, validators, APIs, security, governance, and network operations." "https://mscblockexplorer.in/docs/" "MSC Documentation Portal" "noindex,follow"
    "portal\explorer.html" = Page "MSC Explorer Portal - Chain Data Overview" "Open MSC Chain explorer tools for blocks, transactions, addresses, validators, finality, and network health." "https://explorer.mscblockexplorer.in/portal/explorer.html" "MSC Explorer Portal"
    "portal\index.html" = Page "MSC Chain Operations Portal - Nodes and Validators" "Access MSC Chain network operations, validator resources, node setup, documentation, transparency, and status." "https://explorer.mscblockexplorer.in/portal/index.html" "MSC Chain Operations Portal"
    "portal\node-setup.html" = Page "Run an MSC Node - Installation and Operations" "Set up and operate an MSC Chain full node, validator, sentry, archive, or RPC service with security guidance." "https://explorer.mscblockexplorer.in/portal/node-setup.html" "Run an MSC Node"
    "portal\status.html" = Page "MSC Network Operations Status - Services and Nodes" "Monitor MSC Chain public services, validator network, node availability, finality, and operational health." "https://explorer.mscblockexplorer.in/portal/status.html" "MSC Network Operations Status"
    "portal\testnet.html" = Page "MSC Testnet - Genesis, Nodes and Developer Access" "Access MSC testnet connection details, genesis information, public endpoints, snapshots, and operator resources." "https://explorer.mscblockexplorer.in/portal/testnet.html" "MSC Testnet"
    "portal\transparency.html" = Page "MSC Chain Transparency - Roadmap and Reports" "Review MSC Chain roadmap, releases, network incidents, validator operations, and public transparency resources." "https://explorer.mscblockexplorer.in/portal/transparency.html" "MSC Chain Transparency"
    "portal\validator.html" = Page "MSC Validator Profile - Performance and Metadata" "Inspect an MSC Chain validator profile, status, uptime, voting power, version, badges, and performance." "https://explorer.mscblockexplorer.in/portal/validator.html" "MSC Validator Profile"
    "portal\validators.html" = Page "MSC Validator Leaderboard - Reliability and Stake" "Compare MSC Chain validators by reliability, participation, stake, decentralization, status, and operator metadata." "https://explorer.mscblockexplorer.in/portal/validators.html" "MSC Validator Leaderboard"
}

$image = "https://mscblockexplorer.in/assets/msc-logo-512.png"
$googleFontPatterns = @(
    '\s*<link rel="preconnect" href="https://fonts\.googleapis\.com"\s*/?>',
    '\s*<link rel="preconnect" href="https://fonts\.gstatic\.com" crossorigin\s*/?>',
    '\s*<link href="https://fonts\.googleapis\.com/css2[^"]*" rel="stylesheet"\s*/?>'
)

foreach ($relative in $pages.Keys) {
    $path = Join-Path $UiRoot $relative
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "SEO page not found: $relative"
    }
    $meta = $pages[$relative]
    $raw = Get-Content -Raw -LiteralPath $path
    $raw = [regex]::Replace($raw, '\s*<title>.*?</title>', '', [Text.RegularExpressions.RegexOptions]::Singleline)
    foreach ($name in @("description", "robots", "theme-color", "twitter:card", "twitter:title", "twitter:description", "twitter:image")) {
        $raw = [regex]::Replace($raw, "\s*<meta name=`"$([regex]::Escape($name))`"[^>]*?/?>", '', [Text.RegularExpressions.RegexOptions]::IgnoreCase)
    }
    foreach ($property in @("og:type", "og:site_name", "og:title", "og:description", "og:url", "og:image", "og:image:alt")) {
        $raw = [regex]::Replace($raw, "\s*<meta property=`"$([regex]::Escape($property))`"[^>]*?/?>", '', [Text.RegularExpressions.RegexOptions]::IgnoreCase)
    }
    $raw = [regex]::Replace($raw, '\s*<link rel="canonical"[^>]*?/?>', '', [Text.RegularExpressions.RegexOptions]::IgnoreCase)
    foreach ($pattern in $googleFontPatterns) {
        $raw = [regex]::Replace($raw, $pattern, '', [Text.RegularExpressions.RegexOptions]::IgnoreCase)
    }

    $tags = @"
  <title>$($meta.Title)</title>
  <meta name="description" content="$($meta.Description)" />
  <meta name="robots" content="$($meta.Robots)" />
  <meta name="theme-color" content="#05070c" />
  <link rel="canonical" href="$($meta.Canonical)" />
  <meta property="og:type" content="website" />
  <meta property="og:site_name" content="MSC Chain" />
  <meta property="og:title" content="$($meta.Title)" />
  <meta property="og:description" content="$($meta.Description)" />
  <meta property="og:url" content="$($meta.Canonical)" />
  <meta property="og:image" content="$image" />
  <meta property="og:image:alt" content="MSC Chain logo" />
  <meta name="twitter:card" content="summary_large_image" />
  <meta name="twitter:title" content="$($meta.Title)" />
  <meta name="twitter:description" content="$($meta.Description)" />
  <meta name="twitter:image" content="$image" />
"@
    $viewportPattern = '(<meta name="viewport"[^>]*?/?>)'
    if (-not [regex]::IsMatch($raw, $viewportPattern, [Text.RegularExpressions.RegexOptions]::IgnoreCase)) {
        throw "Viewport metadata not found: $relative"
    }
    $raw = [regex]::Replace(
        $raw,
        $viewportPattern,
        { param($match) $match.Value + "`r`n" + $tags.TrimEnd() },
        [Text.RegularExpressions.RegexOptions]::IgnoreCase
    )

    if ($relative -like "explorer-*.html" -and $raw -match '<main class="explorer-content"></main>') {
        $preRender = "<main class=`"explorer-content`"><section class=`"seo-prerender`"><h1>$($meta.Heading)</h1><p>$($meta.Description)</p></section></main>"
        $raw = $raw.Replace('<main class="explorer-content"></main>', $preRender)
    }
    if ($relative -like "portal\*.html" -and $raw -match '<body([^>]*)>\s*<script') {
        $preRender = "<main class=`"seo-prerender portal-seo-prerender`"><h1>$($meta.Heading)</h1><p>$($meta.Description)</p></main>"
        $raw = [regex]::Replace($raw, '(<body[^>]*>)\s*(<script)', "`$1`r`n  $preRender`r`n  `$2", 1)
    }

    $raw = $raw.Replace('<script src="https://unpkg.com/lucide@0.468.0/dist/umd/lucide.min.js"></script>', '<script defer src="https://unpkg.com/lucide@0.468.0/dist/umd/lucide.min.js"></script>')
    $raw = $raw.Replace('<script src="explorer.js?', '<script defer src="explorer.js?')
    $raw = $raw.Replace('<script src="portal.js?', '<script defer src="portal.js?')
    [IO.File]::WriteAllText($path, $raw, [Text.UTF8Encoding]::new($false))
}

Write-Host "Applied SEO metadata to $($pages.Count) HTML pages."
