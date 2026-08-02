param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('session-start', 'pre-tool-use', 'pre-compact', 'post-compact', 'subagent-start', 'subagent-stop', 'stop')]
    [string]$Event
)

$ErrorActionPreference = 'Stop'

function Read-HookInput {
    $raw = [Console]::In.ReadToEnd()
    if ([string]::IsNullOrWhiteSpace($raw)) { return [pscustomobject]@{} }
    try { return $raw | ConvertFrom-Json }
    catch { return [pscustomobject]@{} }
}

function Get-Field {
    param($Object, [string[]]$Names, [string]$Default = '')
    foreach ($name in $Names) {
        $property = $Object.PSObject.Properties[$name]
        if ($null -ne $property -and -not [string]::IsNullOrWhiteSpace([string]$property.Value)) {
            return [string]$property.Value
        }
    }
    return $Default
}

function Find-ProjectRoot {
    param([string]$Start)
    if ([string]::IsNullOrWhiteSpace($Start)) { $Start = (Get-Location).Path }
    try { $current = [System.IO.Path]::GetFullPath($Start) }
    catch { $current = (Get-Location).Path }
    if (Test-Path -LiteralPath $current -PathType Leaf) { $current = Split-Path -Parent $current }
    while (-not [string]::IsNullOrWhiteSpace($current)) {
        $profile = Join-Path $current '.re-discipline\project-profile.md'
        if (Test-Path -LiteralPath $profile -PathType Leaf) { return $current }
        $parent = Split-Path -Parent $current
        if ($parent -eq $current) { break }
        $current = $parent
    }
    return ''
}

function Write-JsonObject {
    param($Object)
    $Object | ConvertTo-Json -Compress -Depth 8
}

function Write-Context {
    param([string]$HookEventName, [string]$Context)
    Write-JsonObject ([ordered]@{
        hookSpecificOutput = [ordered]@{
            hookEventName = $HookEventName
            additionalContext = $Context
        }
    })
}

function Write-Denial {
    param([string]$Reason)
    Write-JsonObject ([ordered]@{
        hookSpecificOutput = [ordered]@{
            hookEventName = 'PreToolUse'
            permissionDecision = 'deny'
            permissionDecisionReason = $Reason
        }
    })
}

function Get-RelativeProjectPath {
    param([string]$Path, [string]$Root)
    if ([string]::IsNullOrWhiteSpace($Path) -or [string]::IsNullOrWhiteSpace($Root)) { return '' }
    try {
        $absolute = if ([System.IO.Path]::IsPathRooted($Path)) {
            [System.IO.Path]::GetFullPath($Path)
        }
        else {
            [System.IO.Path]::GetFullPath((Join-Path $Root $Path))
        }
    }
    catch { return '' }
    $prefix = $Root.TrimEnd('\', '/') + [System.IO.Path]::DirectorySeparatorChar
    if (-not $absolute.StartsWith($prefix, [System.StringComparison]::OrdinalIgnoreCase)) { return '' }
    return $absolute.Substring($prefix.Length).Replace('\', '/')
}

function Test-ProtectedPath {
    param([string]$Relative)
    if ([string]::IsNullOrWhiteSpace($Relative)) { return $false }
    if ($Relative -match '^active/[^/]+/runs/[^/]+/(report\.md|payload(?:/.*)?)$') { return $false }
    $patterns = @(
        '^active/[^/]+/campaign\.json$',
        '^active/[^/]+/STATE\.md$',
        '^active/[^/]+/work-items(?:/.*)?$',
        '^active/[^/]+/runs(?:/.*)?$',
        '^active/[^/]+/findings(?:/.*)?$',
        '^active/[^/]+/intake(?:/.*)?$',
        '^active/[^/]+/reviews(?:/.*)?$',
        '^active/[^/]+/events(?:/.*)?$',
        '^active/[^/]+/closure(?:/.*)?$',
        '^docs/truth(?:/.*)?$'
    )
    foreach ($pattern in $patterns) { if ($Relative -match $pattern) { return $true } }
    return $false
}

function Get-ServerHealth {
    param([string]$Root)
    $pluginRoot = Split-Path -Parent $PSScriptRoot
    $manifestPath = Join-Path $pluginRoot 'knowledge\bin\manifest.json'
    $runtimePath = Join-Path $pluginRoot 'knowledge\bin\re-discipline-knowledge.exe'
    if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf) -or -not (Test-Path -LiteralPath $runtimePath -PathType Leaf)) {
        return 'runtime unavailable'
    }
    try { $manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json }
    catch { return 'runtime manifest invalid' }
    if ([string]$manifest.runtime.version -ne '0.8.0') { return 'runtime version mismatch' }
    try {
        $null = & $runtimePath preflight --asset-root (Join-Path $pluginRoot 'knowledge') --project-root $Root 2>$null
        if ($LASTEXITCODE -eq 0) { return 'preflight passed' }
        return 'preflight needs attention'
    }
    catch { return 'preflight unavailable' }
}

$inputObject = Read-HookInput
$cwd = Get-Field $inputObject @('cwd', 'projectRoot', 'project_root') (Get-Location).Path
$projectRoot = Find-ProjectRoot $cwd

if ($Event -eq 'pre-tool-use') {
    $toolName = Get-Field $inputObject @('tool_name', 'toolName') ''
    $toolInput = $inputObject.PSObject.Properties['tool_input']
    if ($null -eq $toolInput) { $toolInput = $inputObject.PSObject.Properties['toolInput'] }
    $path = ''
    if ($null -ne $toolInput) { $path = Get-Field $toolInput.Value @('file_path', 'filePath', 'path') '' }
    if ([string]::IsNullOrWhiteSpace($path)) { $path = Get-Field $inputObject @('file_path', 'filePath', 'path') '' }
    if ($toolName -notmatch '^(Write|Edit)$' -or [string]::IsNullOrWhiteSpace($projectRoot)) {
        Write-JsonObject ([ordered]@{})
        exit 0
    }
    $relative = Get-RelativeProjectPath $path $projectRoot
    if (Test-ProtectedPath $relative) {
        Write-Denial "Direct Write/Edit to '$relative' is blocked. Use the re-discipline shared state engine; use migrate-project only for prior-version inputs."
        exit 0
    }
    Write-JsonObject ([ordered]@{})
    exit 0
}

if ([string]::IsNullOrWhiteSpace($projectRoot)) {
    Write-JsonObject ([ordered]@{})
    exit 0
}

$campaign = Get-Field $inputObject @('campaignId', 'campaign_id') $env:RE_DISCIPLINE_CAMPAIGN_ID
$workItem = Get-Field $inputObject @('workItemId', 'work_item_id') $env:RE_DISCIPLINE_WORK_ITEM_ID
$generation = Get-Field $inputObject @('generation', 'generationId', 'generation_id') $env:RE_DISCIPLINE_GENERATION_ID
$eventHead = Get-Field $inputObject @('lastEventId', 'last_event_id', 'eventHead') $env:RE_DISCIPLINE_LAST_EVENT_ID
$runId = Get-Field $inputObject @('runId', 'run_id') $env:RE_DISCIPLINE_RUN_ID
$runPath = Get-Field $inputObject @('runPath', 'run_path') $env:RE_DISCIPLINE_RUN_PATH
$packDigest = Get-Field $inputObject @('contextPackDigest', 'context_pack_digest') $env:RE_DISCIPLINE_CONTEXT_PACK_DIGEST

switch ($Event) {
    'session-start' {
        $serverHealth = Get-ServerHealth $projectRoot
        $handles = @()
        $activeRoot = Join-Path $projectRoot 'active'
        if (Test-Path -LiteralPath $activeRoot -PathType Container) {
            $handles = @(Get-ChildItem -LiteralPath $activeRoot -Directory -ErrorAction SilentlyContinue |
                Where-Object { Test-Path -LiteralPath (Join-Path $_.FullName 'campaign.json') -PathType Leaf } |
                Sort-Object Name | Select-Object -First 8 | ForEach-Object { $_.Name })
        }
        $handleText = if ($handles.Count -gt 0) { $handles -join ', ' } else { 'none' }
        Write-Context 'SessionStart' "Re-discipline 0.8 project detected; server $serverHealth. Invoke onboard and call bounded state mode orient before substantive work. Active campaign handles: $handleText. Canonical records are engine-owned; generated views and caches are derived."
    }
    'pre-compact' {
        Write-Context 'PreCompact' "No semantic save is required. Persist only an already-started atomic engine transaction. Resume handles: campaign=$campaign workItem=$workItem generation=$generation lastEvent=$eventHead."
    }
    'post-compact' {
        Write-Context 'PostCompact' "Rehydrate with bounded state mode orient, then state mode resume for campaign=$campaign since generation=$generation or lastEvent=$eventHead. Expand only cited handles needed for the next decision."
    }
    'subagent-start' {
        Write-Context 'SubagentStart' "Assigned run=$runId workItem=$workItem path=$runPath contextPackDigest=$packDigest. Read the exact brief and verify the immutable pack. Write only report.md, lazy payload/, and explicit project grants. Do not mutate canonical state or ratify findings."
    }
    'subagent-stop' {
        $reportStatus = 'run path unavailable'
        if (-not [string]::IsNullOrWhiteSpace($runPath)) {
            $relativeRun = Get-RelativeProjectPath $runPath $projectRoot
            if ($relativeRun -match '^(active/[^/]+/runs/[^/]+|\.re-discipline/agents/recruiting/[^/]+/runs/[^/]+)$') {
                $report = Join-Path ([System.IO.Path]::GetFullPath($runPath)) 'report.md'
                $reportStatus = if (Test-Path -LiteralPath $report -PathType Leaf) { 'report present' } else { 'report missing' }
            }
            else { $reportStatus = 'run path outside registered run roots' }
        }
        Write-Context 'SubagentStop' "Run return check: $reportStatus. Submit run.return through the shared engine to freeze the report digest and queue curation. Return does not imply review or ratification."
    }
    'stop' {
        $inFlight = Get-Field $inputObject @('transactionInFlight', 'transaction_in_flight') $env:RE_DISCIPLINE_TRANSACTION_IN_FLIGHT
        if ($inFlight -match '^(1|true|yes)$') {
            Write-Context 'Stop' 'A shared-engine transaction is reported in flight. Let it publish or recover atomically before ending; do not edit state files directly.'
        }
        else { Write-JsonObject ([ordered]@{}) }
    }
}
