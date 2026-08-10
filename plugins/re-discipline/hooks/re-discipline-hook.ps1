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

function Get-ApplyPatchTargets {
    param($ToolInput)
    if ($null -eq $ToolInput) {
        return [pscustomobject]@{ Valid = $false; Targets = @(); Reason = 'tool_input is missing' }
    }
    $command = Get-Field $ToolInput @('command') ''
    if ([string]::IsNullOrWhiteSpace($command)) {
        return [pscustomobject]@{ Valid = $false; Targets = @(); Reason = 'patch command is missing' }
    }

    $targets = @()
    $beginCount = 0
    $endCount = 0
    $inside = $false
    $ended = $false
    foreach ($rawLine in [regex]::Split($command, "\r?\n")) {
        $line = $rawLine.TrimEnd("`r")
        if ($line -ceq '*** Begin Patch') {
            $beginCount++
            $inside = $true
            $ended = $false
            continue
        }
        if ($line -ceq '*** End Patch') {
            $endCount++
            $inside = $false
            $ended = $true
            continue
        }
        if ($line -cmatch '^\*\*\* (Add|Update|Delete) File: (.+)$' -or
            $line -cmatch '^\*\*\* (Move to): (.+)$') {
            if (-not $inside -or $ended) {
                return [pscustomobject]@{ Valid = $false; Targets = @(); Reason = 'patch target is outside the patch envelope' }
            }
            $target = if ($Matches[1] -ceq 'Move to') { $Matches[2] } else { $Matches[2] }
            $target = $target.Trim()
            if ([string]::IsNullOrWhiteSpace($target) -or
                [System.IO.Path]::IsPathRooted($target) -or
                $target -match '^[A-Za-z]:[\\/]' -or
                $target.StartsWith('\\') -or
                $target -match '[<>:"|?*]' -or
                $target -match '[\x00-\x1f]') {
                return [pscustomobject]@{ Valid = $false; Targets = @(); Reason = "patch target '$target' is not a project-relative path" }
            }
            $normalized = $target.Replace('\', '/')
            $segments = @($normalized -split '/')
            if ($segments.Count -eq 0 -or @($segments | Where-Object { $_ -eq '' -or $_ -eq '.' -or $_ -eq '..' }).Count -gt 0) {
                return [pscustomobject]@{ Valid = $false; Targets = @(); Reason = "patch target '$target' is not canonical" }
            }
            $targets += $normalized
            continue
        }
        if ($line -cmatch '^\*\*\* .* (File|to):') {
            return [pscustomobject]@{ Valid = $false; Targets = @(); Reason = "unsupported patch target header '$line'" }
        }
    }
    if ($beginCount -ne 1 -or $endCount -ne 1 -or $targets.Count -eq 0) {
        return [pscustomobject]@{ Valid = $false; Targets = @(); Reason = 'patch envelope or target list is malformed' }
    }
    return [pscustomobject]@{ Valid = $true; Targets = @($targets | Select-Object -Unique); Reason = '' }
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
        '^docs/truth(?:/.*)?$',
        '^docs/history/campaigns(?:/.*)?$',
        '^\.re-discipline/state(?:/.*)?$',
        '^\.re-discipline/migration/0\.8(?:/.*)?$',
        '^\.re-discipline/knowledge/(?:migration(?:/.*)?|normalization-queue\.json(?:\.lock)?|\.re-discipline-tmp-.*)$'
    )
    foreach ($pattern in $patterns) { if ($Relative -match $pattern) { return $true } }
    return $false
}

function Test-LegacyCanonicalPath {
    param([string]$Relative)
    if ([string]::IsNullOrWhiteSpace($Relative)) { return $false }
    $patterns = @(
        '^active/[^/]+/(?:CAMPAIGN|REVIEWS)\.md$',
        '^\.re-discipline/config\.json$',
        '^\.re-discipline/knowledge/(?:policy\.jsonc|retrieval-profile\.json)$'
    )
    foreach ($pattern in $patterns) { if ($Relative -match $pattern) { return $true } }
    return $false
}

function Get-ProjectStateVersion {
    param([string]$Root)
    $pluginRoot = Split-Path -Parent $PSScriptRoot
    $runtimePath = Join-Path $pluginRoot 'knowledge\bin\re-discipline-knowledge.exe'
    if (Test-Path -LiteralPath $runtimePath -PathType Leaf) {
        try {
            $raw = (& $runtimePath project-version --project-root $Root 2>$null | Out-String)
            if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($raw)) {
                $result = $raw | ConvertFrom-Json
                $version = [string]$result.projectStateVersion
                if ($version -eq '0.7' -or $version -eq '0.8') { return $version }
            }
        }
        catch { }
    }

    # A stale or unavailable packaged runtime must not make SessionStart mutate
    # or mislabel a project. This bounded read-only fallback recognizes only an
    # unambiguous shared-laws marker with no opposite-version control-plane
    # shape; partial and mixed trees remain unknown and fail closed.
    $profilePath = Join-Path $Root '.re-discipline\project-profile.md'
    try { $profile = Get-Content -Raw -LiteralPath $profilePath }
    catch { return 'unknown' }
    $legacyProfile = $profile -match 're-discipline:shared-laws v0\.7\.'
    $currentProfile = $profile -match 're-discipline:shared-laws v0\.8\.'
    $legacyShape = $false
    $currentShape = (Test-Path -LiteralPath (Join-Path $Root '.re-discipline\state')) -or
        (Test-Path -LiteralPath (Join-Path $Root 'docs\history\campaigns'))
    $activeRoot = Join-Path $Root 'active'
    if (Test-Path -LiteralPath $activeRoot -PathType Container) {
        foreach ($campaign in @(Get-ChildItem -LiteralPath $activeRoot -Directory -ErrorAction SilentlyContinue)) {
            if ((Test-Path -LiteralPath (Join-Path $campaign.FullName 'CAMPAIGN.md') -PathType Leaf) -or
                (Test-Path -LiteralPath (Join-Path $campaign.FullName 'REVIEWS.md') -PathType Leaf)) {
                $legacyShape = $true
            }
            if ((Test-Path -LiteralPath (Join-Path $campaign.FullName 'campaign.json') -PathType Leaf) -or
                (Test-Path -LiteralPath (Join-Path $campaign.FullName 'work-items') -PathType Container) -or
                (Test-Path -LiteralPath (Join-Path $campaign.FullName 'runs') -PathType Container) -or
                (Test-Path -LiteralPath (Join-Path $campaign.FullName 'events') -PathType Container) -or
                (Test-Path -LiteralPath (Join-Path $campaign.FullName 'closure') -PathType Container)) {
                $currentShape = $true
            }
        }
    }
    if ($legacyProfile -and -not $currentProfile -and -not $currentShape) { return '0.7' }
    if ($currentProfile -and -not $legacyProfile -and -not $legacyShape) { return '0.8' }
    return 'unknown'
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
    # The contract line, not the patch level. A hook that pins the exact patch
    # reports a mismatch on every release it did not itself change, which is the
    # same trap that stranded the published version behind its source.
    if ([string]$manifest.runtime.version -notmatch '^0\.8(?:\.|$)') { return 'runtime version mismatch' }
    try {
        $null = & $runtimePath preflight --asset-root (Join-Path $pluginRoot 'knowledge') --project-root $Root 2>$null
        if ($LASTEXITCODE -eq 0) { return 'preflight passed' }
        return 'preflight needs attention'
    }
    catch { return 'preflight unavailable' }
}

function Get-DeclaredRunId {
    param($InputObject)
    $runId = Get-Field $InputObject @('runId', 'run_id') $env:RE_DISCIPLINE_RUN_ID
    if ([string]::IsNullOrWhiteSpace($runId)) {
        $runPath = Get-Field $InputObject @('runPath', 'run_path') $env:RE_DISCIPLINE_RUN_PATH
        if (-not [string]::IsNullOrWhiteSpace($runPath)) {
            $leaf = Split-Path -Leaf $runPath.TrimEnd('\', '/')
            if ($leaf -match '^R-[0-9]{8}-[0-9]{4,}$') { $runId = $leaf }
        }
    }
    return $runId
}

function Get-RegisteredRun {
    param([string]$Root, [string]$RunId)
    if ($RunId -notmatch '^R-[0-9]{8}-[0-9]{4,}$') { return $null }
    $matches = @()
    $activeRoot = Join-Path $Root 'active'
    if (Test-Path -LiteralPath $activeRoot -PathType Container) {
        foreach ($campaign in @(Get-ChildItem -LiteralPath $activeRoot -Directory -ErrorAction SilentlyContinue)) {
            $candidate = Join-Path $campaign.FullName "runs\$RunId\run.json"
            if (Test-Path -LiteralPath $candidate -PathType Leaf) { $matches += $candidate }
        }
    }
    if ($matches.Count -ne 1) { return $null }
    try {
        $record = Get-Content -Raw -LiteralPath $matches[0] | ConvertFrom-Json
    }
    catch { return $null }
    if ([string]$record.id -cne $RunId) { return $null }
    return [pscustomobject]@{ Record = $record; Path = $matches[0] }
}

function Get-ValidatedDraftRun {
    param(
        [string]$Root,
        [string]$RunId,
        [string]$RunPath,
        [string]$WorkItemId,
        [string]$PackDigest,
        [switch]$AllowReturned
    )
    if ([string]::IsNullOrWhiteSpace($RunId) -or
        [string]::IsNullOrWhiteSpace($RunPath) -or
        $PackDigest -cnotmatch '^sha256:[0-9a-f]{64}$') {
        return $null
    }
    try { $absoluteRunPath = [System.IO.Path]::GetFullPath($RunPath) }
    catch { return $null }
    if (-not (Test-Path -LiteralPath $absoluteRunPath -PathType Container)) { return $null }
    $relativeRunPath = Get-RelativeProjectPath $absoluteRunPath $Root
    $activeRun = $relativeRunPath -cmatch '^active/[^/]+/runs/[^/]+$'
    $recruitingRun = $relativeRunPath -cmatch '^\.re-discipline/agents/recruiting/[^/]+/runs/[^/]+$'
    if (-not $activeRun -and -not $recruitingRun) { return $null }
    if ((Split-Path -Leaf $absoluteRunPath) -cne $RunId -or
        $RunId -cnotmatch '^[A-Za-z0-9][A-Za-z0-9._-]{1,124}$') {
        return $null
    }

    $recordPath = Join-Path $absoluteRunPath 'run.json'
    try { $record = Get-Content -Raw -LiteralPath $recordPath | ConvertFrom-Json }
    catch { return $null }
    if ([string]$record.id -cne $RunId) { return $null }
    $allowedStatuses = if ($AllowReturned) { @('prepared', 'running', 'returned') } else { @('prepared', 'running') }
    if ($allowedStatuses -notcontains [string]$record.status) { return $null }

    if ($activeRun) {
        if ([string]::IsNullOrWhiteSpace($WorkItemId) -or
            [string]$record.primaryWorkItemId -cne $WorkItemId) {
            return $null
        }
        $registered = Get-RegisteredRun $Root $RunId
        $registeredRunPath = if ($null -ne $registered) {
            [System.IO.Path]::GetFullPath((Split-Path -Parent $registered.Path))
        }
        else { '' }
        if ($null -eq $registered -or
            -not $registeredRunPath.Equals($absoluteRunPath, [System.StringComparison]::OrdinalIgnoreCase)) {
            return $null
        }
    }

    $packRelative = "$relativeRunPath/context-pack.json"
    if ($null -eq $record.contextPack -or [string]$record.contextPack.path -cne $packRelative) { return $null }
    $packPath = Join-Path $absoluteRunPath 'context-pack.json'
    try { $pack = Get-Content -Raw -LiteralPath $packPath | ConvertFrom-Json }
    catch { return $null }
    if ([string]$pack.digest -cne $PackDigest -or
        [string]::IsNullOrWhiteSpace([string]$pack.packId)) {
        return $null
    }
    return [pscustomobject]@{
        Record = $record
        Path = $absoluteRunPath
        RelativePath = $relativeRunPath
        WorkItemId = if ($activeRun) { [string]$record.primaryWorkItemId } else { 'none' }
    }
}

function Test-RegisteredRunWrite {
    param([string]$Root, [string]$RunId, [string]$Relative)
    $registered = Get-RegisteredRun $Root $RunId
    if ($null -eq $registered) {
        return [pscustomobject]@{ Allowed = $false; Reason = "run '$RunId' is not uniquely registered" }
    }
    $record = $registered.Record
    if ([string]$record.status -notmatch '^(prepared|running|returned)$') {
        return [pscustomobject]@{ Allowed = $false; Reason = "run '$RunId' is not writable in status '$($record.status)'" }
    }
    $runDirectory = Split-Path -Parent $registered.Path
    $runRelative = Get-RelativeProjectPath $runDirectory $Root
    if ($Relative -eq "$runRelative/report.md" -or $Relative -match ('^' + [regex]::Escape("$runRelative/payload/") + '.+')) {
        return [pscustomobject]@{ Allowed = $true; Reason = '' }
    }
    foreach ($grant in @($record.writeGrants)) {
        $mode = [string]$grant.mode
        $grantPath = [string]$grant.path
        if ($mode -notmatch '^(exact|directory)$' -or
            $grantPath -notmatch '^[A-Za-z0-9._@+#() -]+(?:/[A-Za-z0-9._@+#() -]+)*$') {
            return [pscustomobject]@{ Allowed = $false; Reason = "run '$RunId' has an invalid registered write grant" }
        }
        if (($mode -eq 'exact' -and $Relative -ceq $grantPath) -or
            ($mode -eq 'directory' -and ($Relative -ceq $grantPath -or $Relative.StartsWith($grantPath + '/', [System.StringComparison]::Ordinal)))) {
            return [pscustomobject]@{ Allowed = $true; Reason = '' }
        }
    }
    return [pscustomobject]@{ Allowed = $false; Reason = "path '$Relative' is outside run '$RunId' write grants" }
}

$inputObject = Read-HookInput
$cwd = Get-Field $inputObject @('cwd', 'projectRoot', 'project_root') (Get-Location).Path
$projectRoot = Find-ProjectRoot $cwd

if ($Event -eq 'pre-tool-use') {
    $toolName = Get-Field $inputObject @('tool_name', 'toolName') ''
    $toolInput = $inputObject.PSObject.Properties['tool_input']
    if ($null -eq $toolInput) { $toolInput = $inputObject.PSObject.Properties['toolInput'] }
    $toolInputValue = if ($null -ne $toolInput) { $toolInput.Value } else { $null }
    $targets = @()
    $operationLabel = 'Write/Edit'
    if ($toolName -match '^(Write|Edit)$') {
        $path = ''
        if ($null -ne $toolInput) { $path = Get-Field $toolInput.Value @('file_path', 'filePath', 'path') '' }
        if ([string]::IsNullOrWhiteSpace($path)) { $path = Get-Field $inputObject @('file_path', 'filePath', 'path') '' }
        if (-not [string]::IsNullOrWhiteSpace($path)) { $targets = @($path) }
    }
    elseif ($toolName -ieq 'apply_patch') {
        $operationLabel = 'apply_patch'
        $patchTargets = Get-ApplyPatchTargets $toolInputValue
        if (-not $patchTargets.Valid) {
            Write-Denial "Direct apply_patch denied: $($patchTargets.Reason). A write hook must identify every project-relative target before allowing the patch."
            exit 0
        }
        $targets = @($patchTargets.Targets)
    }
    else {
        Write-JsonObject ([ordered]@{})
        exit 0
    }
    if ([string]::IsNullOrWhiteSpace($projectRoot)) {
        Write-JsonObject ([ordered]@{})
        exit 0
    }
    if ($targets.Count -eq 0) {
        if ($toolName -ieq 'apply_patch') {
            Write-Denial 'Direct apply_patch denied: no write target could be identified.'
            exit 0
        }
        Write-JsonObject ([ordered]@{})
        exit 0
    }

    $declaredRunId = Get-DeclaredRunId $inputObject
    $projectVersion = Get-ProjectStateVersion $projectRoot
    foreach ($target in $targets) {
        $relative = Get-RelativeProjectPath $target $projectRoot
        if ([string]::IsNullOrWhiteSpace($relative)) {
            if ($toolName -ieq 'apply_patch') {
                Write-Denial "Direct apply_patch denied: target '$target' is outside the verified project root or cannot be normalized."
                exit 0
            }
            continue
        }
        $isRunOutput = $relative -match '^active/[^/]+/runs/[^/]+/(?:report\.md|payload/.+)$'
        if ($isRunOutput -and [string]::IsNullOrWhiteSpace($declaredRunId)) {
            Write-Denial "Direct $operationLabel denied: run output '$relative' requires a uniquely registered writable run identity. Run grants are an accident boundary, not host-attested authority."
            exit 0
        }
        if (-not [string]::IsNullOrWhiteSpace($declaredRunId)) {
            $decision = Test-RegisteredRunWrite $projectRoot $declaredRunId $relative
            if (-not $decision.Allowed) {
                Write-Denial "Direct $operationLabel denied: $($decision.Reason). Run grants are an accident boundary, not host-attested authority."
                exit 0
            }
        }
        if ($projectVersion -ne '0.8' -and (Test-LegacyCanonicalPath $relative)) {
            Write-Denial "Direct $operationLabel to '$relative' is blocked while the project is legacy or unverified. Use migrate-project for the approved conversion; no 0.8 operation may mutate prior-version state."
            exit 0
        }
        if ((-not $isRunOutput) -and (Test-ProtectedPath $relative)) {
            Write-Denial "Direct $operationLabel to '$relative' is blocked. Use the re-discipline shared state engine; use migrate-project only for prior-version inputs."
            exit 0
        }
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
        $projectVersion = Get-ProjectStateVersion $projectRoot
        if ($projectVersion -eq '0.7') {
            Write-Context 'SessionStart' 'Legacy re-discipline 0.7 project detected. Do not invoke 0.8 lifecycle operations or edit managed state directly. Inspect migrate-project status and create a read-only preview; migration requires explicit approval of the exact plan digest and never runs at session start.'
        }
        elseif ($projectVersion -ne '0.8') {
            Write-Context 'SessionStart' 'A re-discipline project marker was found, but its state version could not be verified. Do not invoke lifecycle mutations or edit managed state directly. Repair runtime availability or inspect migrate-project status before continuing.'
        }
        else {
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
    }
    'pre-compact' {
        Write-Context 'PreCompact' "No semantic save is required. Persist only an already-started atomic engine transaction. Resume handles: campaign=$campaign workItem=$workItem generation=$generation lastEvent=$eventHead."
    }
    'post-compact' {
        Write-Context 'PostCompact' "Rehydrate with bounded state mode orient, then state mode resume for campaign=$campaign since generation=$generation or lastEvent=$eventHead. Expand only cited handles needed for the next decision."
    }
    'subagent-start' {
        $declaredRunId = Get-Field $inputObject @('runId', 'run_id') ''
        $declaredRunPath = Get-Field $inputObject @('runPath', 'run_path') ''
        $declaredWorkItem = Get-Field $inputObject @('workItemId', 'work_item_id') ''
        $declaredPackDigest = Get-Field $inputObject @('contextPackDigest', 'context_pack_digest') ''
        $draftRun = Get-ValidatedDraftRun $projectRoot $declaredRunId $declaredRunPath $declaredWorkItem $declaredPackDigest
        if ($null -eq $draftRun) {
            Write-JsonObject ([ordered]@{})
        }
        else {
            Write-Context 'SubagentStart' "Assigned run=$declaredRunId workItem=$($draftRun.WorkItemId) path=$($draftRun.Path) contextPackDigest=$declaredPackDigest. Read the exact brief and verify the immutable pack. Write only report.md, lazy payload/, and explicit project grants. Do not mutate canonical state or ratify findings."
        }
    }
    'subagent-stop' {
        $declaredRunId = Get-Field $inputObject @('runId', 'run_id') ''
        $declaredRunPath = Get-Field $inputObject @('runPath', 'run_path') ''
        $declaredWorkItem = Get-Field $inputObject @('workItemId', 'work_item_id') ''
        $declaredPackDigest = Get-Field $inputObject @('contextPackDigest', 'context_pack_digest') ''
        $draftRun = Get-ValidatedDraftRun $projectRoot $declaredRunId $declaredRunPath $declaredWorkItem $declaredPackDigest -AllowReturned
        if ($null -eq $draftRun) {
            Write-JsonObject ([ordered]@{})
        }
        else {
            $report = Join-Path $draftRun.Path 'report.md'
            $reportStatus = if (Test-Path -LiteralPath $report -PathType Leaf) { 'report present' } else { 'report missing' }
            Write-Context 'SubagentStop' "Run return check for ${declaredRunId}: $reportStatus. Submit run.return through the shared engine to freeze the report digest and queue curation. Return does not imply review or ratification."
        }
    }
    'stop' {
        $inFlight = Get-Field $inputObject @('transactionInFlight', 'transaction_in_flight') $env:RE_DISCIPLINE_TRANSACTION_IN_FLIGHT
        if ($inFlight -match '^(1|true|yes)$') {
            Write-Context 'Stop' 'A shared-engine transaction is reported in flight. Let it publish or recover atomically before ending; do not edit state files directly.'
        }
        else { Write-JsonObject ([ordered]@{}) }
    }
}
