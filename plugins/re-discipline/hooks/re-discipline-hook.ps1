param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('session-start', 'pre-tool-use', 'post-tool-use', 'pre-compact', 'post-compact', 'subagent-start', 'subagent-stop', 'stop')]
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
    if ($null -eq $Object) { return $Default }
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
                $target -match '[<>"|?*]' -or
                $target -match '[\x00-\x1f]') {
                return [pscustomobject]@{ Valid = $false; Targets = @(); Reason = "patch target '$target' is not a supported filesystem path" }
            }
            $normalized = $target.Replace('\', '/')
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
    $windowsHost = [System.IO.Path]::DirectorySeparatorChar -eq '\'
    # Drive-relative paths are ambiguous even on Windows. Drive-rooted and
    # backslash-rooted paths are foreign on native Unix and must not be joined
    # beneath the project root as if they were ordinary relative paths.
    if ($Path -match '^[A-Za-z]:(?:$|[^\\/])') { return '' }
    if (-not $windowsHost -and ($Path -match '^[A-Za-z]:[\\/]' -or $Path.StartsWith('\'))) { return '' }
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
    $pluginManifestPath = Join-Path $pluginRoot '.codex-plugin\plugin.json'
    $runtimePath = Join-Path $pluginRoot 'knowledge\bin\re-discipline-knowledge.exe'
    if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf) -or
        -not (Test-Path -LiteralPath $pluginManifestPath -PathType Leaf) -or
        -not (Test-Path -LiteralPath $runtimePath -PathType Leaf)) {
        return 'runtime unavailable'
    }
    try { $manifest = Get-Content -Raw -LiteralPath $manifestPath | ConvertFrom-Json }
    catch { return 'runtime manifest invalid' }
    try { $pluginManifest = Get-Content -Raw -LiteralPath $pluginManifestPath | ConvertFrom-Json }
    catch { return 'plugin manifest invalid' }
    $runtimeVersion = [string]$manifest.runtime.version
    $pluginVersion = [string]$pluginManifest.version
    $semanticVersion = '^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$'
    if ($runtimeVersion -notmatch $semanticVersion) {
        return "runtime manifest version '$runtimeVersion' is invalid"
    }
    if ($pluginVersion -notmatch $semanticVersion) {
        return "plugin manifest version '$pluginVersion' is invalid"
    }
    # Local cachebusters live in SemVer build metadata and do not change the
    # packaged runtime. Compare the complete release identity before '+'. The
    # project-state format is independently reported by Get-ProjectStateVersion.
    $runtimeRelease = ($runtimeVersion -split '\+', 2)[0]
    $pluginRelease = ($pluginVersion -split '\+', 2)[0]
    if ($runtimeRelease -cne $pluginRelease) {
        return "runtime $runtimeVersion does not match plugin $pluginVersion"
    }
    try {
        $null = & $runtimePath preflight --asset-root (Join-Path $pluginRoot 'knowledge') --project-root $Root 2>$null
        if ($LASTEXITCODE -eq 0) { return "preflight passed (runtime $runtimeVersion)" }
        return "preflight needs attention (runtime $runtimeVersion)"
    }
    catch { return "preflight unavailable (runtime $runtimeVersion)" }
}

function Get-DeclaredRunId {
    param($InputObject, [switch]$IgnoreEnvironment)
    $environmentRunId = if ($IgnoreEnvironment) { '' } else { $env:RE_DISCIPLINE_RUN_ID }
    $environmentRunPath = if ($IgnoreEnvironment) { '' } else { $env:RE_DISCIPLINE_RUN_PATH }
    $runId = Get-Field $InputObject @('runId', 'run_id') $environmentRunId
    if ([string]::IsNullOrWhiteSpace($runId)) {
        $runPath = Get-Field $InputObject @('runPath', 'run_path') $environmentRunPath
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

function Get-SubagentId {
    param($InputObject)
    $agentId = Get-Field $InputObject @('agent_id', 'agentId') ''
    if (-not [string]::IsNullOrWhiteSpace($agentId)) { return $agentId }
    foreach ($containerName in @('subagent', 'subAgent')) {
        $container = $InputObject.PSObject.Properties[$containerName]
        if ($null -eq $container) { continue }
        $agentId = Get-Field $container.Value @('agent_id', 'agentId') ''
        if (-not [string]::IsNullOrWhiteSpace($agentId)) { return $agentId }
    }
    return ''
}

function Test-SafeHostIdentifier {
    param([string]$Value)
    return -not [string]::IsNullOrWhiteSpace($Value) -and
        $Value -cmatch '^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$'
}

function Get-SafeDispatchDirectory {
    param([string]$Root, [string[]]$Components, [switch]$Create)
    try { $current = [System.IO.Path]::GetFullPath($Root) }
    catch { return '' }
    foreach ($component in $Components) {
        if ($component -in @('.', '..') -or
            $component -cnotmatch '^[A-Za-z0-9.][A-Za-z0-9._-]{0,199}$') { return '' }
        $current = Join-Path $current $component
        if (-not (Test-Path -LiteralPath $current)) {
            if (-not $Create) { return '' }
            try { $null = [System.IO.Directory]::CreateDirectory($current) }
            catch { return '' }
        }
        try { $item = Get-Item -Force -LiteralPath $current }
        catch { return '' }
        if (-not $item.PSIsContainer -or
            (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0)) {
            return ''
        }
    }
    return $current
}

function Get-DispatchSessionDirectory {
    param([string]$Root, [string]$SessionId, [switch]$Create)
    if (-not (Test-SafeHostIdentifier $SessionId)) { return '' }
    return Get-SafeDispatchDirectory $Root @('.re-discipline', 'cache', 'hook-dispatch', 'v1', $SessionId) -Create:$Create
}

function Test-FirstSessionStart {
    param([string]$Root, [string]$SessionId)
    # A host that cannot identify its session gets a conservative emission on
    # every real SessionStart event; never collapse unrelated unknown sessions.
    if ($SessionId -eq 'unknown') { return $true }
    $sessionDirectory = Get-DispatchSessionDirectory $Root $SessionId -Create
    if ([string]::IsNullOrWhiteSpace($sessionDirectory)) { return $true }
    $marker = Join-Path $sessionDirectory 'session-start.emitted'
    try {
        $stream = [System.IO.File]::Open(
            $marker,
            [System.IO.FileMode]::CreateNew,
            [System.IO.FileAccess]::Write,
            [System.IO.FileShare]::Read)
        $stream.Dispose()
        return $true
    }
    catch [System.IO.IOException] {
        try {
            $item = Get-Item -Force -LiteralPath $marker
            if (-not $item.PSIsContainer -and
                (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -eq 0)) {
                return $false
            }
        }
        catch {}
        return $true
    }
    catch { return $true }
}

function Get-DispatchMarker {
    param($ToolInput)
    $message = Get-Field $ToolInput @('message', 'prompt') ''
    if ([string]::IsNullOrWhiteSpace($message)) {
        return [pscustomobject]@{ Present = $false; Valid = $false; Reason = ''; RunId = ''; PackDigest = '' }
    }
    $firstLine = ([regex]::Split($message, "\r?\n", 2))[0]
    if (-not $firstLine.StartsWith('re-discipline-run:', [System.StringComparison]::Ordinal)) {
        return [pscustomobject]@{ Present = $false; Valid = $false; Reason = ''; RunId = ''; PackDigest = '' }
    }
    if ($firstLine -cnotmatch '^re-discipline-run: (R-[0-9]{8}-[0-9]{4,}) (sha256:[0-9a-f]{64})$') {
        return [pscustomobject]@{
            Present = $true
            Valid = $false
            Reason = 'the first message line must be re-discipline-run: <R-id> <context-pack-digest>'
            RunId = ''
            PackDigest = ''
        }
    }
    return [pscustomobject]@{
        Present = $true
        Valid = $true
        Reason = ''
        RunId = [string]$Matches[1]
        PackDigest = [string]$Matches[2]
    }
}

function Resolve-DispatchDraftRun {
    param([string]$Root, [string]$RunId, [string]$PackDigest, [switch]$AllowReturned)
    $registered = Get-RegisteredRun $Root $RunId
    if ($null -eq $registered) { return $null }
    $runPath = Split-Path -Parent $registered.Path
    $workItemId = [string]$registered.Record.primaryWorkItemId
    return Get-ValidatedDraftRun $Root $RunId $runPath $workItemId $PackDigest -AllowReturned:$AllowReturned
}

function Read-DispatchTicket {
    param([string]$Path, [string]$ExpectedSessionId)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { return $null }
    try { $item = Get-Item -Force -LiteralPath $Path }
    catch { return [pscustomobject]@{ Valid = $false; Reason = 'dispatch ticket cannot be inspected' } }
    if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0 -or $item.Length -gt 2048) {
        return [pscustomobject]@{ Valid = $false; Reason = 'dispatch ticket is unsafe or oversized' }
    }
    $fields = @{}
    try { $lines = @(Get-Content -LiteralPath $Path -Encoding ASCII) }
    catch { return [pscustomobject]@{ Valid = $false; Reason = 'dispatch ticket cannot be read' } }
    foreach ($line in $lines) {
        if ($line -cnotmatch '^([A-Za-z][A-Za-z0-9]*)=(.*)$' -or $fields.ContainsKey($Matches[1])) {
            return [pscustomobject]@{ Valid = $false; Reason = 'dispatch ticket syntax is invalid' }
        }
        $fields[$Matches[1]] = [string]$Matches[2]
    }
    $kind = 'registered'
    if ($fields.schemaVersion -ceq '1') {
        if ($fields.Count -ne 6) {
            return [pscustomobject]@{ Valid = $false; Reason = 'dispatch ticket identity is invalid' }
        }
    }
    elseif ($fields.schemaVersion -ceq '2') {
        if ($fields.Count -ne 7 -or $fields.kind -cnotmatch '^(ordinary|registered)$') {
            return [pscustomobject]@{ Valid = $false; Reason = 'dispatch ticket identity is invalid' }
        }
        $kind = [string]$fields.kind
    }
    else {
        return [pscustomobject]@{ Valid = $false; Reason = 'dispatch ticket identity is invalid' }
    }
    if ($fields.sessionId -cne $ExpectedSessionId -or
        $fields.toolUseId -cnotmatch '^[A-Za-z0-9._:-]{0,200}$' -or
        $fields.createdUnix -cnotmatch '^[0-9]{1,20}$' -or
        ($kind -ceq 'registered' -and
            ($fields.runId -cnotmatch '^R-[0-9]{8}-[0-9]{4,}$' -or
             $fields.contextPackDigest -cnotmatch '^sha256:[0-9a-f]{64}$')) -or
        ($kind -ceq 'ordinary' -and
            (-not [string]::IsNullOrEmpty([string]$fields.runId) -or
             -not [string]::IsNullOrEmpty([string]$fields.contextPackDigest)))) {
        return [pscustomobject]@{ Valid = $false; Reason = 'dispatch ticket identity is invalid' }
    }
    return [pscustomobject]@{
        Valid = $true
        Reason = ''
        Kind = $kind
        RunId = $fields.runId
        PackDigest = $fields.contextPackDigest
        ToolUseId = $fields.toolUseId
    }
}

function Remove-StalePendingDispatch {
    param([string]$SessionDirectory)
    if ([string]::IsNullOrWhiteSpace($SessionDirectory)) { return }
    $pending = Join-Path $SessionDirectory 'pending.ticket'
    if (-not (Test-Path -LiteralPath $pending -PathType Leaf)) { return }
    try { $item = Get-Item -Force -LiteralPath $pending }
    catch { return }
    if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) { return }
    if ($item.LastWriteTimeUtc -lt [DateTime]::UtcNow.AddSeconds(-30)) {
        try { [System.IO.File]::Delete($pending) }
        catch { }
    }
}

function Reserve-DispatchTicket {
    param(
        [string]$Root,
        [string]$SessionId,
        [string]$Kind,
        [string]$RunId,
        [string]$PackDigest,
        [string]$ToolUseId
    )
    if (-not (Test-SafeHostIdentifier $SessionId)) {
        return [pscustomobject]@{ Success = $false; Reason = 'host session_id is missing or unsafe' }
    }
    if ($ToolUseId -cnotmatch '^[A-Za-z0-9._:-]{0,200}$') {
        return [pscustomobject]@{ Success = $false; Reason = 'host tool_use_id is unsafe' }
    }
    if ($Kind -notin @('ordinary', 'registered') -or
        ($Kind -ceq 'registered' -and
            ($RunId -cnotmatch '^R-[0-9]{8}-[0-9]{4,}$' -or
             $PackDigest -cnotmatch '^sha256:[0-9a-f]{64}$')) -or
        ($Kind -ceq 'ordinary' -and
            (-not [string]::IsNullOrEmpty($RunId) -or
             -not [string]::IsNullOrEmpty($PackDigest)))) {
        return [pscustomobject]@{ Success = $false; Reason = 'dispatch kind or run identity is invalid' }
    }
    $sessionDirectory = Get-DispatchSessionDirectory $Root $SessionId -Create
    if ([string]::IsNullOrWhiteSpace($sessionDirectory)) {
        return [pscustomobject]@{ Success = $false; Reason = 'dispatch cache path is unavailable or unsafe' }
    }
    $pending = Join-Path $sessionDirectory 'pending.ticket'
    $createdUnix = [int64]([DateTime]::UtcNow - [DateTime]'1970-01-01T00:00:00Z').TotalSeconds
    $body = @(
        'schemaVersion=2',
        "sessionId=$SessionId",
        "kind=$Kind",
        "runId=$RunId",
        "contextPackDigest=$PackDigest",
        "toolUseId=$ToolUseId",
        "createdUnix=$createdUnix"
    ) -join "`n"
    $body += "`n"
    $bytes = [System.Text.Encoding]::ASCII.GetBytes($body)
    $wait = [System.Diagnostics.Stopwatch]::StartNew()
    while ($true) {
        Remove-StalePendingDispatch $sessionDirectory
        $stream = $null
        try {
            $stream = [System.IO.File]::Open($pending, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::Write, [System.IO.FileShare]::None)
        }
        catch [System.IO.IOException] {
            if ($wait.ElapsedMilliseconds -ge 3500) {
                return [pscustomobject]@{
                    Success = $false
                    Reason = 'the prior launch handoff did not bind within the internal dispatch window'
                }
            }
            Start-Sleep -Milliseconds 25
            continue
        }
        catch {
            return [pscustomobject]@{ Success = $false; Reason = 'dispatch ticket could not be reserved' }
        }
        try {
            $stream.Write($bytes, 0, $bytes.Length)
            $stream.Flush()
        }
        catch {
            try { $stream.Dispose() } catch {}
            try { [System.IO.File]::Delete($pending) } catch {}
            return [pscustomobject]@{ Success = $false; Reason = 'dispatch ticket could not be written' }
        }
        finally {
            if ($null -ne $stream) { $stream.Dispose() }
        }
        return [pscustomobject]@{ Success = $true; Reason = '' }
    }
}

function Resolve-AgentDispatch {
    param([string]$Root, [string]$SessionId, [string]$AgentId, [switch]$AllowReturned)
    if (-not (Test-SafeHostIdentifier $SessionId) -or -not (Test-SafeHostIdentifier $AgentId)) {
        return [pscustomobject]@{ Found = $false; Valid = $false; Reason = '' }
    }
    $sessionDirectory = Get-DispatchSessionDirectory $Root $SessionId
    if ([string]::IsNullOrWhiteSpace($sessionDirectory)) {
        return [pscustomobject]@{ Found = $false; Valid = $false; Reason = '' }
    }
    $binding = Join-Path (Join-Path $sessionDirectory 'agents') "$AgentId.ticket"
    if (-not (Test-Path -LiteralPath $binding -PathType Leaf)) {
        return [pscustomobject]@{ Found = $false; Valid = $false; Reason = '' }
    }
    $ticket = Read-DispatchTicket $binding $SessionId
    if ($null -eq $ticket -or -not $ticket.Valid) {
        $reason = if ($null -eq $ticket) { 'dispatch binding disappeared' } else { $ticket.Reason }
        return [pscustomobject]@{ Found = $true; Valid = $false; Reason = $reason }
    }
    if ($ticket.Kind -ceq 'ordinary') {
        return [pscustomobject]@{
            Found = $true
            Valid = $true
            Reason = ''
            Kind = 'ordinary'
            RunId = ''
            PackDigest = ''
            Draft = $null
        }
    }
    $draft = Resolve-DispatchDraftRun $Root $ticket.RunId $ticket.PackDigest -AllowReturned:$AllowReturned
    if ($null -eq $draft) {
        return [pscustomobject]@{
            Found = $true
            Valid = $false
            Reason = "bound run '$($ticket.RunId)' no longer matches its registered context pack"
        }
    }
    return [pscustomobject]@{
        Found = $true
        Valid = $true
        Reason = ''
        Kind = 'registered'
        RunId = $ticket.RunId
        PackDigest = $ticket.PackDigest
        Draft = $draft
    }
}

function Claim-AgentDispatch {
    param([string]$Root, [string]$SessionId, [string]$AgentId)
    if (-not (Test-SafeHostIdentifier $SessionId) -or -not (Test-SafeHostIdentifier $AgentId)) {
        return [pscustomobject]@{ Found = $false; Valid = $false; Reason = '' }
    }
    $sessionDirectory = Get-DispatchSessionDirectory $Root $SessionId
    if ([string]::IsNullOrWhiteSpace($sessionDirectory)) {
        return [pscustomobject]@{ Found = $false; Valid = $false; Reason = '' }
    }
    $agentsDirectory = Get-SafeDispatchDirectory $Root @('.re-discipline', 'cache', 'hook-dispatch', 'v1', $SessionId, 'agents') -Create
    if ([string]::IsNullOrWhiteSpace($agentsDirectory)) {
        return [pscustomobject]@{ Found = $true; Valid = $false; Reason = 'agent binding directory is unavailable or unsafe' }
    }
    $pending = Join-Path $sessionDirectory 'pending.ticket'
    $binding = Join-Path $agentsDirectory "$AgentId.ticket"
    if (-not (Test-Path -LiteralPath $binding -PathType Leaf)) {
        if (-not (Test-Path -LiteralPath $pending -PathType Leaf)) {
            return [pscustomobject]@{ Found = $false; Valid = $false; Reason = '' }
        }
        try { [System.IO.File]::Move($pending, $binding) }
        catch {
            return [pscustomobject]@{ Found = $true; Valid = $false; Reason = 'pending dispatch could not be claimed atomically' }
        }
    }
    return Resolve-AgentDispatch $Root $SessionId $AgentId
}

function Clear-PendingDispatchAfterTool {
    param([string]$Root, [string]$SessionId, [string]$ToolUseId)
    $sessionDirectory = Get-DispatchSessionDirectory $Root $SessionId
    if ([string]::IsNullOrWhiteSpace($sessionDirectory)) { return }
    $pending = Join-Path $sessionDirectory 'pending.ticket'
    $ticket = Read-DispatchTicket $pending $SessionId
    if ($null -ne $ticket -and $ticket.Valid -and $ticket.ToolUseId -cne '' -and
        $ticket.ToolUseId -ceq $ToolUseId) {
        try { [System.IO.File]::Delete($pending) }
        catch { }
    }
}

function Test-RegisteredRunWrite {
    param([string]$Root, [string]$RunId, [string]$Relative)
    $registered = Get-RegisteredRun $Root $RunId
    if ($null -eq $registered) {
        return [pscustomobject]@{ Allowed = $false; Reason = "run '$RunId' is not uniquely registered" }
    }
    $record = $registered.Record
    if ([string]$record.status -notmatch '^(prepared|running)$') {
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
    $sessionId = Get-Field $inputObject @('session_id', 'sessionId') ''
    $agentId = Get-SubagentId $inputObject
    $toolUseId = Get-Field $inputObject @('tool_use_id', 'toolUseId') ''
    if ($toolName -ieq 'spawn_agent' -or $toolName -ieq 'Agent') {
        $marker = Get-DispatchMarker $toolInputValue
        if (-not $marker.Present) {
            if ([string]::IsNullOrWhiteSpace($projectRoot)) {
                Write-JsonObject ([ordered]@{})
            }
            else {
                $reservation = Reserve-DispatchTicket $projectRoot $sessionId 'ordinary' '' '' $toolUseId
                if (-not $reservation.Success) {
                    Write-Denial "Subagent launch could not enter the session dispatch boundary: $($reservation.Reason)."
                }
                else { Write-JsonObject ([ordered]@{}) }
            }
            exit 0
        }
        if (-not $marker.Valid) {
            Write-Denial "Registered subagent launch denied: $($marker.Reason)."
            exit 0
        }
        if ([string]::IsNullOrWhiteSpace($projectRoot) -or
            (Get-ProjectStateVersion $projectRoot) -ne '0.8') {
            Write-Denial 'Registered subagent launch denied: the current directory is not a verified re-discipline 0.8 project.'
            exit 0
        }
        $draftRun = Resolve-DispatchDraftRun $projectRoot $marker.RunId $marker.PackDigest
        if ($null -eq $draftRun) {
            Write-Denial "Registered subagent launch denied: run '$($marker.RunId)' is not uniquely writable or does not match context pack '$($marker.PackDigest)'."
            exit 0
        }
        $reservation = Reserve-DispatchTicket $projectRoot $sessionId 'registered' $marker.RunId $marker.PackDigest $toolUseId
        if (-not $reservation.Success) {
            Write-Denial "Registered subagent launch denied: $($reservation.Reason)."
            exit 0
        }
        Write-JsonObject ([ordered]@{})
        exit 0
    }
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
            Write-Denial "Direct apply_patch denied: $($patchTargets.Reason). A write hook must identify every filesystem target before allowing the patch."
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

    $binding = Resolve-AgentDispatch $projectRoot $sessionId $agentId
    if ($binding.Found -and -not $binding.Valid) {
        Write-Denial "Direct $operationLabel denied: $($binding.Reason). The subagent has no usable registered run boundary."
        exit 0
    }
    $declaredRunId = Get-DeclaredRunId $inputObject -IgnoreEnvironment:(-not [string]::IsNullOrWhiteSpace($agentId))
    if ($binding.Valid) {
        if ($binding.Kind -ceq 'ordinary') {
            if (-not [string]::IsNullOrWhiteSpace($declaredRunId)) {
                Write-Denial "Direct $operationLabel denied: an ordinary subagent launch cannot claim registered run '$declaredRunId'."
                exit 0
            }
        }
        else {
            if (-not [string]::IsNullOrWhiteSpace($declaredRunId) -and $declaredRunId -cne $binding.RunId) {
                Write-Denial "Direct $operationLabel denied: host binding names run '$($binding.RunId)' but the tool envelope names '$declaredRunId'."
                exit 0
            }
            $declaredRunId = $binding.RunId
        }
    }
    elseif (-not [string]::IsNullOrWhiteSpace($agentId) -and [string]::IsNullOrWhiteSpace($declaredRunId)) {
        Write-Denial "Direct $operationLabel denied: subagent '$agentId' has no registered run binding in manager session '$sessionId'."
        exit 0
    }
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

if ($Event -eq 'post-tool-use') {
    $toolName = Get-Field $inputObject @('tool_name', 'toolName') ''
    if (($toolName -ieq 'spawn_agent' -or $toolName -ieq 'Agent') -and
        -not [string]::IsNullOrWhiteSpace($projectRoot)) {
        $sessionId = Get-Field $inputObject @('session_id', 'sessionId') ''
        $toolUseId = Get-Field $inputObject @('tool_use_id', 'toolUseId') ''
        Clear-PendingDispatchAfterTool $projectRoot $sessionId $toolUseId
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
$sessionId = Get-Field $inputObject @('session_id', 'sessionId') 'unknown'

switch ($Event) {
    'session-start' {
        if (-not (Test-FirstSessionStart $projectRoot $sessionId)) {
            Write-JsonObject ([ordered]@{})
            break
        }
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
            Write-Context 'SessionStart' "Re-discipline 0.8 project detected; server $serverHealth. Session-start onboarding boundary=$sessionId. If this session has not completed onboarding, invoke onboard and call bounded state mode orient once before substantive work. After the first successful orient, onboarding is satisfied for this session: do not re-invoke the onboard skill for ordinary user messages, tool rounds, or compaction. A PostCompact bounded-state refresh is not onboarding. Re-run onboarding only for a new or resumed host session, or after an explicit runtime/state invalidation. Active campaign handles: $handleText. Canonical records are engine-owned; generated views and caches are derived."
        }
    }
    'pre-compact' {
        Write-Context 'PreCompact' "No semantic save is required. Persist only an already-started atomic engine transaction. Resume handles: campaign=$campaign workItem=$workItem generation=$generation lastEvent=$eventHead."
    }
    'post-compact' {
        Write-Context 'PostCompact' "Rehydrate with bounded state mode orient, then state mode resume for campaign=$campaign since generation=$generation or lastEvent=$eventHead. Expand only cited handles needed for the next decision."
    }
    'subagent-start' {
        $agentId = Get-SubagentId $inputObject
        $dispatch = Claim-AgentDispatch $projectRoot $sessionId $agentId
        if ($dispatch.Found) {
            if (-not $dispatch.Valid) {
                Write-Context 'SubagentStart' "Registered re-discipline dispatch could not be bound for session=$sessionId agent=${agentId}: $($dispatch.Reason). Do not write project or run files; return the binding failure to the manager."
            }
            elseif ($dispatch.Kind -ceq 'ordinary') {
                Write-JsonObject ([ordered]@{})
            }
            else {
                Write-Context 'SubagentStart' "Assigned session=$sessionId agent=$agentId run=$($dispatch.RunId) workItem=$($dispatch.Draft.WorkItemId) path=$($dispatch.Draft.Path) contextPackDigest=$($dispatch.PackDigest). Read the exact brief and verify the immutable pack. Write only report.md, lazy payload/, and explicit project grants. Do not mutate canonical state or ratify findings."
            }
        }
        else {
            $declaredRunId = Get-Field $inputObject @('runId', 'run_id') ''
            $declaredRunPath = Get-Field $inputObject @('runPath', 'run_path') ''
            $declaredWorkItem = Get-Field $inputObject @('workItemId', 'work_item_id') ''
            $declaredPackDigest = Get-Field $inputObject @('contextPackDigest', 'context_pack_digest') ''
            $draftRun = Get-ValidatedDraftRun $projectRoot $declaredRunId $declaredRunPath $declaredWorkItem $declaredPackDigest
            if ($null -eq $draftRun) { Write-JsonObject ([ordered]@{}) }
            else {
                Write-Context 'SubagentStart' "Assigned run=$declaredRunId workItem=$($draftRun.WorkItemId) path=$($draftRun.Path) contextPackDigest=$declaredPackDigest. Read the exact brief and verify the immutable pack. Write only report.md, lazy payload/, and explicit project grants. Do not mutate canonical state or ratify findings."
            }
        }
    }
    'subagent-stop' {
        $agentId = Get-SubagentId $inputObject
        $dispatch = Resolve-AgentDispatch $projectRoot $sessionId $agentId -AllowReturned
        $declaredRunId = ''
        $draftRun = $null
        if ($dispatch.Found) {
            if (-not $dispatch.Valid) {
                Write-Context 'SubagentStop' "Registered re-discipline dispatch binding is invalid for session=$sessionId agent=${agentId}: $($dispatch.Reason). The manager must repair or invalidate the run through the shared engine."
                break
            }
            if ($dispatch.Kind -ceq 'ordinary') {
                Write-JsonObject ([ordered]@{})
                break
            }
            $declaredRunId = $dispatch.RunId
            $draftRun = $dispatch.Draft
        }
        else {
            $declaredRunId = Get-Field $inputObject @('runId', 'run_id') ''
            $declaredRunPath = Get-Field $inputObject @('runPath', 'run_path') ''
            $declaredWorkItem = Get-Field $inputObject @('workItemId', 'work_item_id') ''
            $declaredPackDigest = Get-Field $inputObject @('contextPackDigest', 'context_pack_digest') ''
            $draftRun = Get-ValidatedDraftRun $projectRoot $declaredRunId $declaredRunPath $declaredWorkItem $declaredPackDigest -AllowReturned
        }
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
