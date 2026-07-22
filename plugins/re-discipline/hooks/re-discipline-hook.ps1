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

$initialized =
    (Test-Path -LiteralPath (Join-Path $projectDir ".re-discipline/project-profile.md")) -or
    (Test-Path -LiteralPath (Join-Path $projectDir ".claude/project-profile.md")) -or
    (Test-Path -LiteralPath (Join-Path $projectDir "docs/INDEX.md"))

if (-not $initialized) {
    exit 0
}

if ($EventName -eq "session-start") {
    Write-Output "Reminder: invoke the re-discipline onboard skill before substantive work. Read the canonical project profile, docs/INDEX.md, truth and history indexes, and any active CAMPAIGN.md."
}
else {
    Write-Output "Reminder: context is about to compact. If a campaign is active, invoke checkpoint-campaign and preserve Current state plus dead ends. If it is solved, invoke close-campaign."
}

exit 0
