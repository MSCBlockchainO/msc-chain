param(
    [switch]$Full,
    [switch]$Race
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot

function Invoke-GoTestPhase {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    Write-Host ""
    Write-Host "==> $Name" -ForegroundColor Cyan
    & go @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Name failed with exit code $LASTEXITCODE"
    }
}

Push-Location $repoRoot
try {
    Invoke-GoTestPhase -Name "Compile all packages" -Arguments @(
        "test", "-run", "^$", "./..."
    )

    $startupPattern = "^Test(RuntimeStatus|LegacyRuntimeStatus|StartupNetworkValidatorSetSample|StartupValidatorSetSelfCheck|StartupSelfCheck)"
    Invoke-GoTestPhase -Name "Validator startup and admission safety" -Arguments @(
        "test", ".", "-run", $startupPattern, "-count=1"
    )

    $consensusPattern = "^(TestFinalizeExecutionResultAlreadyCommittedStillMovesToNextHeight|TestDuplicateExecutionVoteIngressRetryCanLandAfterQueuedUnresolvedVote|TestHandleLeaderBlockVotesForIncomingProposalDirectly|TestFutureLeaderBlockQueuesAndReplaysQueuedExecutionVote|TestPostCommitConsensusDrainReplaysQueuedNextEpochEvidence|TestCommittedUnresolvedExecutionVoteIsNotQueued|TestCoordinatedByzantineConflictingQuorumPropagationDoesNotPoisonValidQuorum)$"
    Invoke-GoTestPhase -Name "Consensus commit and replay liveness" -Arguments @(
        "test", ".", "-run", $consensusPattern, "-count=1"
    )

    $recoveryPattern = "^Test(AutoHeal|Autoheal|ConsensusDetector|DetectConsensusMode|ValidatorCrashRecovery|ValidatorLiveness|SnapshotSafetyLock|SyncCatchup|SyncController|ResourceExhaustion|DDoSSimulationResourceQuotas|RuntimeMemory|LedgerCache|SignatureCPU)"
    Invoke-GoTestPhase -Name "Autoheal, recovery, sync, and resource safety" -Arguments @(
        "test", ".", "-run", $recoveryPattern, "-count=1"
    )

    if ($Race) {
        $cgoEnabled = (& go env CGO_ENABLED).Trim()
        if ($cgoEnabled -ne "1") {
            throw "Race gate requires CGO_ENABLED=1 and a working C compiler; current CGO_ENABLED=$cgoEnabled"
        }
        Invoke-GoTestPhase -Name "Startup and consensus race detector" -Arguments @(
            "test", "-race", ".", "-run", "($startupPattern|$consensusPattern)", "-count=1"
        )
    }

    if ($Full) {
        Invoke-GoTestPhase -Name "Full repository test suite" -Arguments @(
            "test", "./...", "-count=1"
        )
    }

    Write-Host ""
    Write-Host "Chain and validator stability gate passed." -ForegroundColor Green
}
finally {
    Pop-Location
}
