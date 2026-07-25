param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet("session-start", "pre-compact")]
    [string]$EventName
)

$rawInput = [Console]::In.ReadToEnd()
$projectDir = $null

if (-not [string]::IsNullOrWhiteSpace($rawInput)) {
    try {
        $payload = $rawInput | ConvertFrom-Json
        if ($payload.cwd) {
            $projectDir = [string]$payload.cwd
        }
    }
    catch {
        # Fall through to environment and process-directory discovery.
    }
}

if ([string]::IsNullOrWhiteSpace($projectDir)) {
    $projectDir = $env:CLAUDE_PROJECT_DIR
}
if ([string]::IsNullOrWhiteSpace($projectDir)) {
    $projectDir = (Get-Location).Path
}

function Find-ReDisciplineRoot {
    param(
        [Parameter(Mandatory = $true)]
        [string]$StartPath
    )

    try {
        $current = [System.IO.Path]::GetFullPath($StartPath)
    }
    catch {
        return $null
    }

    while (-not [string]::IsNullOrWhiteSpace($current)) {
        $canonical = Join-Path $current ".re-discipline/project-profile.md"
        $legacyClaude = Join-Path $current ".claude/project-profile.md"
        $legacyCodex = Join-Path $current ".codex/project-profile.md"
        $projectIndex = Join-Path $current "docs/INDEX.md"

        if (
            (Test-Path -LiteralPath $canonical -PathType Leaf) -or
            (Test-Path -LiteralPath $legacyClaude -PathType Leaf) -or
            (Test-Path -LiteralPath $legacyCodex -PathType Leaf) -or
            (Test-Path -LiteralPath $projectIndex -PathType Leaf)
        ) {
            return $current
        }

        $parent = [System.IO.Directory]::GetParent($current)
        if ($null -eq $parent) {
            return $null
        }
        $current = $parent.FullName
    }

    return $null
}

function Write-CodexContext {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Context
    )

    $output = @{
        hookSpecificOutput = @{
            hookEventName = "SessionStart"
            additionalContext = $Context
        }
    }
    Write-Output ($output | ConvertTo-Json -Compress -Depth 4)
}

$projectRoot = Find-ReDisciplineRoot -StartPath $projectDir

if ([string]::IsNullOrWhiteSpace($projectRoot)) {
    exit 0
}

$canonicalProfile = Join-Path $projectRoot ".re-discipline/project-profile.md"
$hasCanonicalProfile = Test-Path -LiteralPath $canonicalProfile -PathType Leaf
$isCodex = -not [string]::IsNullOrWhiteSpace($env:PLUGIN_ROOT)
$onboardReminder = "Reminder: invoke the re-discipline onboard skill before substantive work. Read the canonical project profile, active manager adapter, docs/INDEX.md, truth and history indexes, and any active CAMPAIGN.md."
$recoveryReminder = "Legacy or incomplete re-discipline project detected without .re-discipline/project-profile.md. Invoke init-project in migration or recovery mode before substantive work; legacy host profiles are recovery input only."
$checkpointReminder = "Reminder: context is about to compact. If a campaign is active, invoke checkpoint-campaign and preserve Current state plus dead ends. If it is solved, invoke close-campaign."

if ($EventName -eq "session-start") {
    if ($isCodex) {
        if ($hasCanonicalProfile) {
            $profileBody = [System.IO.File]::ReadAllText(
                $canonicalProfile,
                [System.Text.Encoding]::UTF8
            )
            $profileBody = $profileBody.Replace("`r`n", "`n")
            Write-CodexContext -Context $profileBody
        }
        else {
            Write-CodexContext -Context $recoveryReminder
        }
    }
    elseif ($hasCanonicalProfile) {
        Write-Output $onboardReminder
    }
    else {
        Write-Output $recoveryReminder
    }
}
else {
    if ($isCodex) {
        Write-Output (@{ systemMessage = $checkpointReminder } | ConvertTo-Json -Compress)
    }
    else {
        Write-Output $checkpointReminder
    }
}

exit 0
