param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet("session-start", "pre-compact", "post-compact", "subagent-start")]
    [string]$EventName
)

$ErrorActionPreference = "Stop"
$rawInput = [Console]::In.ReadToEnd()
$payload = $null
if (-not [string]::IsNullOrWhiteSpace($rawInput)) {
    try {
        $payload = $rawInput | ConvertFrom-Json
    }
    catch {
        # Hook input is advisory. Fall back to environment discovery.
    }
}

$projectDir = if ($null -ne $payload -and $payload.cwd) {
    [string]$payload.cwd
}
elseif (-not [string]::IsNullOrWhiteSpace($env:CLAUDE_PROJECT_DIR)) {
    $env:CLAUDE_PROJECT_DIR
}
else {
    (Get-Location).Path
}

$pluginRoot = if (-not [string]::IsNullOrWhiteSpace($env:PLUGIN_ROOT)) {
    $env:PLUGIN_ROOT
}
else {
    $env:CLAUDE_PLUGIN_ROOT
}
$isCodex = -not [string]::IsNullOrWhiteSpace($env:PLUGIN_ROOT)

function Find-ReDisciplineRoot {
    param([Parameter(Mandatory = $true)][string]$StartPath)

    try {
        $current = [System.IO.Path]::GetFullPath($StartPath)
    }
    catch {
        return $null
    }

    while (-not [string]::IsNullOrWhiteSpace($current)) {
        foreach ($relative in @(
            ".re-discipline/project-profile.md",
            ".re-discipline/config.json",
            ".claude/project-profile.md",
            ".codex/project-profile.md"
        )) {
            if (Test-Path -LiteralPath (Join-Path $current $relative) -PathType Leaf) {
                return $current
            }
        }

        $parent = [System.IO.Directory]::GetParent($current)
        if ($null -eq $parent) {
            return $null
        }
        $current = $parent.FullName
    }

    return $null
}

function Write-HookContext {
    param(
        [Parameter(Mandatory = $true)][string]$HookEventName,
        [Parameter(Mandatory = $true)][string]$Context
    )

    $output = @{
        hookSpecificOutput = @{
            hookEventName = $HookEventName
            additionalContext = $Context
        }
    }
    Write-Output ($output | ConvertTo-Json -Compress -Depth 4)
}

function Test-ExactProperties {
    param(
        [Parameter(Mandatory = $true)]$Object,
        [Parameter(Mandatory = $true)][string[]]$Names
    )

    if ($null -eq $Object) {
        return $false
    }
    $actual = @($Object.PSObject.Properties.Name | Sort-Object)
    $expected = @($Names | Sort-Object)
    if ($actual.Count -ne $expected.Count) {
        return $false
    }
    return -not (Compare-Object $actual $expected)
}

function Get-ManagedProfileState {
    param([Parameter(Mandatory = $true)][string]$ProfilePath)

    if (-not (Test-Path -LiteralPath $ProfilePath -PathType Leaf)) {
        return @{ Managed = $false; Newer = $false; Version = $null }
    }
    $body = [System.IO.File]::ReadAllText(
        $ProfilePath,
        [System.Text.Encoding]::UTF8
    )
    $match = [regex]::Match(
        $body,
        '<!--\s*re-discipline:shared-laws v(\d+)\.(\d+)\.(\d+)\s*-->'
    )
    if (-not $match.Success) {
        return @{ Managed = $false; Newer = $false; Version = $null }
    }

    $major = [int]$match.Groups[1].Value
    $minor = [int]$match.Groups[2].Value
    $patch = [int]$match.Groups[3].Value
    $version = "$major.$minor.$patch"
    if ($major -gt 0 -or $minor -gt 6) {
        return @{ Managed = $false; Newer = $true; Version = $version }
    }
    return @{
        Managed = ($major -eq 0 -and $minor -eq 6)
        Newer = $false
        Version = $version
    }
}

function Test-ManagedPathSafe {
    param(
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)][string]$Path
    )

    $rootFull = [System.IO.Path]::GetFullPath($Root).TrimEnd("\", "/")
    $pathFull = [System.IO.Path]::GetFullPath($Path)
    $prefix = $rootFull + [System.IO.Path]::DirectorySeparatorChar
    if (-not $pathFull.StartsWith(
        $prefix,
        [System.StringComparison]::OrdinalIgnoreCase
    )) {
        return $false
    }

    $relative = $pathFull.Substring($prefix.Length)
    $current = $rootFull
    foreach ($part in @($relative -split "[\\/]")) {
        if ([string]::IsNullOrWhiteSpace($part)) {
            continue
        }
        $current = Join-Path $current $part
        if (Test-Path -LiteralPath $current) {
            $item = Get-Item -LiteralPath $current -Force
            if (($item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
                return $false
            }
        }
    }
    return $true
}

function Write-AtomicText {
    param(
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Content
    )

    if (-not (Test-ManagedPathSafe -Root $Root -Path $Path)) {
        throw "managed path escapes through a link or outside the project"
    }
    $parent = Split-Path -Parent $Path
    if (-not (Test-Path -LiteralPath $parent -PathType Container)) {
        $null = New-Item -ItemType Directory -Path $parent -Force
    }
    if (Test-Path -LiteralPath $Path) {
        return $false
    }

    $temporary = Join-Path $parent (
        "." + [System.IO.Path]::GetFileName($Path) +
        ".re-discipline-" + [guid]::NewGuid().ToString("N") + ".tmp"
    )
    $encoding = New-Object System.Text.UTF8Encoding($false)
    try {
        [System.IO.File]::WriteAllText($temporary, $Content, $encoding)
        if (Test-Path -LiteralPath $Path) {
            return $false
        }
        [System.IO.File]::Move($temporary, $Path)
        return $true
    }
    finally {
        if (Test-Path -LiteralPath $temporary) {
            Remove-Item -LiteralPath $temporary -Force
        }
    }
}

function Replace-AtomicText {
    param(
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Content
    )

    if (-not (Test-ManagedPathSafe -Root $Root -Path $Path)) {
        throw "managed path escapes through a link or outside the project"
    }
    $parent = Split-Path -Parent $Path
    $temporary = Join-Path $parent (
        "." + [System.IO.Path]::GetFileName($Path) +
        ".re-discipline-" + [guid]::NewGuid().ToString("N") + ".tmp"
    )
    $backup = $temporary + ".backup"
    $encoding = New-Object System.Text.UTF8Encoding($false)
    try {
        [System.IO.File]::WriteAllText($temporary, $Content, $encoding)
        if (Test-Path -LiteralPath $Path -PathType Leaf) {
            [System.IO.File]::Replace($temporary, $Path, $backup, $true)
        }
        elseif (-not (Test-Path -LiteralPath $Path)) {
            [System.IO.File]::Move($temporary, $Path)
        }
        else {
            throw "managed path is not a regular file"
        }
    }
    finally {
        foreach ($cleanup in @($temporary, $backup)) {
            if (Test-Path -LiteralPath $cleanup) {
                Remove-Item -LiteralPath $cleanup -Force
            }
        }
    }
}

function Get-HeadText {
    param(
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)][string]$RelativePath
    )

    if ($null -eq (Get-Command git -ErrorAction SilentlyContinue)) {
        return $null
    }
    $gitPath = $RelativePath.Replace("\", "/")
    $previousPreference = $ErrorActionPreference
    try {
        # A missing repository or untracked file is an expected recovery case.
        # PowerShell 7 promotes native stderr to an exception when the script's
        # fail-closed preference is Stop, so relax it only for this read probe.
        $ErrorActionPreference = "Continue"
        $lines = @(& git -C $Root show "HEAD:$gitPath" 2>$null)
        $gitExitCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $previousPreference
    }
    if ($gitExitCode -ne 0) {
        return $null
    }
    return ([string]::Join("`n", $lines) + "`n")
}

function Restore-ManagedFile {
    param(
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)][string]$RelativePath,
        [string]$TemplateName,
        [AllowNull()][string]$DefaultContent,
        [Parameter(Mandatory = $true)]$Recovered,
        [Parameter(Mandatory = $true)]$Warnings
    )

    $target = Join-Path $Root $RelativePath
    if (Test-Path -LiteralPath $target -PathType Leaf) {
        return
    }
    if (Test-Path -LiteralPath $target) {
        $Warnings.Add("$RelativePath exists but is not a regular file")
        return
    }

    $content = Get-HeadText -Root $Root -RelativePath $RelativePath
    if ($null -eq $content) {
        if (-not [string]::IsNullOrEmpty($DefaultContent)) {
            $content = $DefaultContent
        }
        elseif (
            -not [string]::IsNullOrWhiteSpace($pluginRoot) -and
            -not [string]::IsNullOrWhiteSpace($TemplateName)
        ) {
            $template = Join-Path $pluginRoot "templates/project/$TemplateName"
            if (Test-Path -LiteralPath $template -PathType Leaf) {
                $content = [System.IO.File]::ReadAllText(
                    $template,
                    [System.Text.Encoding]::UTF8
                )
            }
        }
    }
    if ($null -eq $content) {
        $Warnings.Add("$RelativePath is missing and its packaged template is unavailable")
        return
    }

    try {
        if (Write-AtomicText -Root $Root -Path $target -Content $content) {
            $Recovered.Add($RelativePath)
        }
    }
    catch {
        $Warnings.Add("$RelativePath was not recovered: $($_.Exception.Message)")
    }
}

function Get-BootstrapConfig {
    param([Parameter(Mandatory = $true)][string]$Path)

    try {
        $config = Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
    }
    catch {
        return @{ Valid = $false; Message = "config.json is malformed"; Value = $null }
    }
    if (-not (Test-ExactProperties $config @(
        "schemaVersion", "settingsDirectory", "memory", "knowledge"
    ))) {
        return @{ Valid = $false; Message = "config.json has unsupported keys"; Value = $null }
    }
    if ([int]$config.schemaVersion -ne 1) {
        $message = if ([int]$config.schemaVersion -gt 1) {
            "config.json uses a newer schema and was not downgraded"
        }
        else {
            "config.json schemaVersion must be 1"
        }
        return @{ Valid = $false; Message = $message; Value = $null }
    }
    if ([string]$config.settingsDirectory -ne "settings") {
        return @{ Valid = $false; Message = "config.json settingsDirectory is unsafe"; Value = $null }
    }
    if (-not (Test-ExactProperties $config.memory @("mode", "writePolicy"))) {
        return @{ Valid = $false; Message = "config.json memory policy is invalid"; Value = $null }
    }
    if (
        @("shared-only", "hybrid", "native") -notcontains [string]$config.memory.mode -or
        [string]$config.memory.writePolicy -ne "proposal-only"
    ) {
        return @{ Valid = $false; Message = "config.json memory policy is invalid"; Value = $null }
    }
    if (-not (Test-ExactProperties $config.knowledge @(
        "enabled", "profile", "settingsFile", "projectProfile"
    ))) {
        return @{ Valid = $false; Message = "config.json knowledge policy is invalid"; Value = $null }
    }
    if (
        $config.knowledge.enabled -isnot [bool] -or
        [string]$config.knowledge.profile -ne "plugin:balanced-v1" -or
        [string]$config.knowledge.settingsFile -ne "settings/knowledge.jsonc" -or
        [string]$config.knowledge.projectProfile -ne "settings/retrieval-profile.json"
    ) {
        return @{ Valid = $false; Message = "config.json knowledge policy is invalid"; Value = $null }
    }
    return @{ Valid = $true; Message = $null; Value = $config }
}

function Remove-JsonComments {
    param([Parameter(Mandatory = $true)][string]$Text)

    $output = New-Object System.Text.StringBuilder
    $inString = $false
    $escaped = $false
    $lineComment = $false
    $blockComment = $false
    for ($index = 0; $index -lt $Text.Length; $index++) {
        $character = $Text[$index]
        $next = if ($index + 1 -lt $Text.Length) { $Text[$index + 1] } else { [char]0 }
        if ($lineComment) {
            if ($character -eq "`n") {
                $lineComment = $false
                $null = $output.Append($character)
            }
            continue
        }
        if ($blockComment) {
            if ($character -eq "*" -and $next -eq "/") {
                $blockComment = $false
                $index++
            }
            elseif ($character -eq "`n") {
                $null = $output.Append($character)
            }
            continue
        }
        if ($inString) {
            $null = $output.Append($character)
            if ($escaped) {
                $escaped = $false
            }
            elseif ($character -eq "\") {
                $escaped = $true
            }
            elseif ($character -eq '"') {
                $inString = $false
            }
            continue
        }
        if ($character -eq '"') {
            $inString = $true
            $null = $output.Append($character)
        }
        elseif ($character -eq "/" -and $next -eq "/") {
            $lineComment = $true
            $index++
        }
        elseif ($character -eq "/" -and $next -eq "*") {
            $blockComment = $true
            $index++
        }
        else {
            $null = $output.Append($character)
        }
    }
    if ($blockComment -or $inString) {
        throw "unterminated JSONC string or block comment"
    }
    return $output.ToString()
}

function Test-KnowledgeSettings {
    param([Parameter(Mandatory = $true)][string]$Path)

    try {
        $text = [System.IO.File]::ReadAllText($Path, [System.Text.Encoding]::UTF8)
        $settings = (Remove-JsonComments -Text $text) | ConvertFrom-Json
    }
    catch {
        return $false
    }
    return (
        [int]$settings.schemaVersion -eq 1 -and
        [string]$settings.models.execution -eq "local" -and
        @("off", "metrics-only") -contains [string]$settings.telemetry.mode -and
        $null -ne $settings.sources -and
        $null -ne $settings.budgets
    )
}

function Test-RetrievalProfile {
    param([Parameter(Mandatory = $true)][string]$Path)

    try {
        $profile = Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
    }
    catch {
        return $false
    }
    # Mirror profileIdentityRE in the packaged server. A project that has ever
    # calibrated and promoted a candidate carries "project:candidate-<hex>",
    # not the packaged baseline identity.
    return (
        [int]$profile.schemaVersion -eq 1 -and
        [string]$profile.profileId -match
            "^[a-z0-9]+(-[a-z0-9]+)*(:[a-z0-9]+(-[a-z0-9]+)*)?$" -and
        @($profile.effectiveProfiles).Count -ge 3
    )
}

# The Get-Toml* helpers below mirror stripTOMLComment, tomlBracketDelta,
# openTOMLMultiline, and tomlCodeLines in the packaged Go recovery scanner so
# the hook and the server agree on which lines carry TOML structure.
function Get-TomlCommentStripped {
    param([Parameter(Mandatory = $true)][AllowEmptyString()][string]$Line)

    $basic = $false
    $literal = $false
    $escaped = $false
    for ($index = 0; $index -lt $Line.Length; $index++) {
        $character = $Line.Substring($index, 1)
        if ($escaped) {
            $escaped = $false
        }
        elseif ($basic -and $character -eq "\") {
            $escaped = $true
        }
        elseif (-not $literal -and $character -eq '"') {
            $basic = -not $basic
        }
        elseif (-not $basic -and $character -eq "'") {
            $literal = -not $literal
        }
        elseif (-not $basic -and -not $literal -and $character -eq "#") {
            return $Line.Substring(0, $index)
        }
    }
    return $Line
}

function Get-TomlBracketDelta {
    param([Parameter(Mandatory = $true)][AllowEmptyString()][string]$Text)

    $delta = 0
    $basic = $false
    $literal = $false
    $escaped = $false
    for ($index = 0; $index -lt $Text.Length; $index++) {
        $character = $Text.Substring($index, 1)
        if ($escaped) {
            $escaped = $false
        }
        elseif ($basic -and $character -eq "\") {
            $escaped = $true
        }
        elseif (-not $literal -and $character -eq '"') {
            $basic = -not $basic
        }
        elseif (-not $basic -and $character -eq "'") {
            $literal = -not $literal
        }
        elseif (-not $basic -and -not $literal -and @("[", "{") -contains $character) {
            $delta++
        }
        elseif (-not $basic -and -not $literal -and @("]", "}") -contains $character) {
            $delta--
        }
    }
    return $delta
}

function Get-TomlMultilineDelimiter {
    param([Parameter(Mandatory = $true)][AllowEmptyString()][string]$Value)

    foreach ($delimiter in @('"""', "'''")) {
        if (
            $Value.StartsWith($delimiter) -and
            -not $Value.Substring($delimiter.Length).Contains($delimiter)
        ) {
            return $delimiter
        }
    }
    return ""
}

function Get-TomlStructure {
    param(
        [Parameter(Mandatory = $true)]
        [AllowEmptyCollection()]
        [AllowEmptyString()]
        [string[]]$Lines
    )

    $code = New-Object "bool[]" $Lines.Count
    $continued = New-Object "bool[]" $Lines.Count
    $delimiter = ""
    $depth = 0
    for ($index = 0; $index -lt $Lines.Count; $index++) {
        $raw = [string]$Lines[$index]
        if ($delimiter -ne "") {
            $at = $raw.IndexOf($delimiter)
            if ($at -lt 0) {
                continue
            }
            $rest = $raw.Substring($at + $delimiter.Length)
            $delimiter = ""
            $depth += Get-TomlBracketDelta -Text (Get-TomlCommentStripped -Line $rest)
            if ($depth -lt 0) {
                return @{ Valid = $false }
            }
            continue
        }
        if ($depth -gt 0) {
            $depth += Get-TomlBracketDelta -Text (Get-TomlCommentStripped -Line $raw)
            if ($depth -lt 0) {
                return @{ Valid = $false }
            }
            continue
        }
        $code[$index] = $true
        $line = (Get-TomlCommentStripped -Line $raw).Trim()
        $equals = $line.IndexOf("=")
        if ($equals -gt 0) {
            $value = $line.Substring($equals + 1).Trim()
            $opened = Get-TomlMultilineDelimiter -Value $value
            if ($opened -ne "") {
                $delimiter = $opened
                $continued[$index] = $true
                continue
            }
            $depth += Get-TomlBracketDelta -Text $value
            if ($depth -lt 0) {
                return @{ Valid = $false }
            }
            if ($depth -gt 0) {
                $continued[$index] = $true
            }
        }
    }
    if ($delimiter -ne "" -or $depth -ne 0) {
        return @{ Valid = $false }
    }
    return @{ Valid = $true; Code = $code; Continued = $continued }
}

function Get-TomlBoolean {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Section,
        [Parameter(Mandatory = $true)][string]$Key
    )

    $activeSection = ""
    $foundValues = New-Object System.Collections.Generic.List[bool]
    $text = [System.IO.File]::ReadAllText($Path, [System.Text.Encoding]::UTF8)
    $lines = @([regex]::Split($text, "\r?\n"))
    $structure = Get-TomlStructure -Lines $lines
    if (-not $structure.Valid) {
        return @{ Valid = $false; Value = $null }
    }
    for ($index = 0; $index -lt $lines.Count; $index++) {
        if (-not $structure.Code[$index]) {
            continue
        }
        $candidate = (Get-TomlCommentStripped -Line $lines[$index]).Trim()
        if ($candidate -match "^\[([^\]]+)\]$") {
            $activeSection = $matches[1]
            continue
        }
        if (
            $activeSection -eq $Section -and
            $candidate -match ("^" + [regex]::Escape($Key) + "\s*=\s*(true|false)$")
        ) {
            $foundValues.Add($Matches[1] -eq "true")
        }
    }
    if ($foundValues.Count -ne 1) {
        return @{ Valid = $false; Value = $null }
    }
    return @{ Valid = $true; Value = $foundValues[0] }
}

function Repair-ClaudeMemoryPolicy {
    param(
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][bool]$Expected,
        [Parameter(Mandatory = $true)]$Recovered,
        [Parameter(Mandatory = $true)]$Warnings
    )

    $raw = [System.IO.File]::ReadAllText($Path, [System.Text.Encoding]::UTF8)
    try {
        $settings = $raw | ConvertFrom-Json
    }
    catch {
        $Warnings.Add(".claude/settings.json is malformed and was preserved")
        return
    }
    if (-not $raw.TrimStart().StartsWith("{")) {
        $Warnings.Add(".claude/settings.json must be a JSON object and was preserved")
        return
    }

    $property = $settings.PSObject.Properties["autoMemoryEnabled"]
    $expectedText = if ($Expected) { "true" } else { "false" }
    $pattern = [regex]'"autoMemoryEnabled"\s*:\s*(true|false)'
    $matches = @($pattern.Matches($raw))
    if ($null -ne $property -and $matches.Count -ne 1) {
        $Warnings.Add(
            ".claude/settings.json has an escaped, duplicate, or ambiguous autoMemoryEnabled field and was preserved"
        )
        return
    }
    if ($null -eq $property -and $matches.Count -ne 0) {
        $Warnings.Add(
            ".claude/settings.json has an ambiguous nested autoMemoryEnabled field and was preserved"
        )
        return
    }
    if (
        $null -ne $property -and
        $property.Value -is [bool] -and
        [bool]$property.Value -eq $Expected
    ) {
        return
    }

    if ($null -ne $property) {
        if ($property.Value -isnot [bool]) {
            $Warnings.Add(
                ".claude/settings.json has an ambiguous autoMemoryEnabled field and was preserved"
            )
            return
        }
        $updated = $pattern.Replace(
            $raw,
            "`"autoMemoryEnabled`": $expectedText",
            1
        )
    }
    else {
        $closing = $raw.LastIndexOf("}")
        if ($closing -lt 0) {
            $Warnings.Add(".claude/settings.json is malformed and was preserved")
            return
        }
        $newline = if ($raw.Contains("`r`n")) { "`r`n" } else { "`n" }
        $prefix = $raw.Substring(0, $closing).TrimEnd()
        $suffix = $raw.Substring($closing)
        $separator = if ($prefix -match "\{\s*$") { "" } else { "," }
        $updated = (
            $prefix + $separator + $newline +
            "  `"autoMemoryEnabled`": $expectedText" + $newline +
            $suffix
        )
    }

    try {
        $verified = $updated | ConvertFrom-Json
        if (
            $verified.autoMemoryEnabled -isnot [bool] -or
            [bool]$verified.autoMemoryEnabled -ne $Expected
        ) {
            throw "policy verification failed"
        }
        Replace-AtomicText -Root $Root -Path $Path -Content $updated
        $Recovered.Add(".claude/settings.json (memory policy)")
    }
    catch {
        $Warnings.Add(
            ".claude/settings.json memory policy was not repaired: $($_.Exception.Message)"
        )
    }
}

function Set-TomlBooleanField {
    param(
        [Parameter(Mandatory = $true)][string]$Text,
        [Parameter(Mandatory = $true)][string]$Section,
        [Parameter(Mandatory = $true)][string]$Key,
        [Parameter(Mandatory = $true)][bool]$Expected
    )

    $newline = if ($Text.Contains("`r`n")) { "`r`n" } else { "`n" }
    $lines = @([regex]::Split($Text, "\r?\n"))
    $structure = Get-TomlStructure -Lines $lines
    if (-not $structure.Valid) {
        return @{ Valid = $false; Changed = $false; Text = $Text }
    }
    $sectionIndexes = New-Object System.Collections.Generic.List[int]
    for ($index = 0; $index -lt $lines.Count; $index++) {
        if (-not $structure.Code[$index]) {
            continue
        }
        if ($lines[$index] -match "^\s*\[([^\]]+)\]\s*(?:#.*)?$") {
            if ($Matches[1] -eq $Section) {
                $sectionIndexes.Add($index)
            }
        }
    }
    if ($sectionIndexes.Count -gt 1) {
        return @{ Valid = $false; Changed = $false; Text = $Text }
    }

    $expectedText = if ($Expected) { "true" } else { "false" }
    if ($sectionIndexes.Count -eq 0) {
        $trimmed = $Text.TrimEnd("`r", "`n")
        $separator = if ([string]::IsNullOrEmpty($trimmed)) {
            ""
        }
        else {
            $newline + $newline
        }
        return @{
            Valid = $true
            Changed = $true
            Text = (
                $trimmed + $separator + "[$Section]" + $newline +
                "$Key = $expectedText" + $newline
            )
        }
    }

    $start = $sectionIndexes[0]
    $end = $lines.Count
    for ($index = $start + 1; $index -lt $lines.Count; $index++) {
        if ($structure.Code[$index] -and $lines[$index] -match "^\s*\[") {
            $end = $index
            break
        }
    }
    $keyIndexes = New-Object System.Collections.Generic.List[int]
    $keyPattern = (
        "^(\s*" + [regex]::Escape($Key) +
        "\s*=\s*)(true|false)(\s*(?:#.*)?)$"
    )
    for ($index = $start + 1; $index -lt $end; $index++) {
        if (-not $structure.Code[$index]) {
            continue
        }
        $withoutComment = (Get-TomlCommentStripped -Line $lines[$index]).Trim()
        if ($withoutComment -match ("^" + [regex]::Escape($Key) + "\s*=")) {
            if ($lines[$index] -notmatch $keyPattern) {
                return @{ Valid = $false; Changed = $false; Text = $Text }
            }
            $keyIndexes.Add($index)
        }
    }
    if ($keyIndexes.Count -gt 1) {
        return @{ Valid = $false; Changed = $false; Text = $Text }
    }
    if ($keyIndexes.Count -eq 1) {
        $index = $keyIndexes[0]
        $match = [regex]::Match($lines[$index], $keyPattern)
        $replacement = $match.Groups[1].Value + $expectedText +
            $match.Groups[3].Value
        if ($replacement -eq $lines[$index]) {
            return @{ Valid = $true; Changed = $false; Text = $Text }
        }
        $lines[$index] = $replacement
    }
    else {
        $before = @($lines[0..($end - 1)])
        $after = if ($end -lt $lines.Count) {
            @($lines[$end..($lines.Count - 1)])
        }
        else {
            @()
        }
        $lines = @($before + "$Key = $expectedText" + $after)
    }
    return @{
        Valid = $true
        Changed = $true
        Text = [string]::Join($newline, $lines)
    }
}

function Repair-CodexMemoryPolicy {
    param(
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][bool]$Expected,
        [Parameter(Mandatory = $true)]$Recovered,
        [Parameter(Mandatory = $true)]$Warnings
    )

    $text = [System.IO.File]::ReadAllText($Path, [System.Text.Encoding]::UTF8)
    $lines = @([regex]::Split($text, "\r?\n"))
    $structure = Get-TomlStructure -Lines $lines
    if (-not $structure.Valid) {
        $Warnings.Add(
            ".codex/config.toml is malformed or uses unsupported syntax and was preserved"
        )
        return
    }
    $activeSection = ""
    for ($index = 0; $index -lt $lines.Count; $index++) {
        if (-not $structure.Code[$index]) {
            continue
        }
        $candidate = (Get-TomlCommentStripped -Line $lines[$index]).Trim()
        if ([string]::IsNullOrEmpty($candidate)) {
            continue
        }
        $compact = $candidate -replace "\s", ""
        if ($compact.StartsWith("[[")) {
            $Warnings.Add(
                ".codex/config.toml uses an unsupported or ambiguous table header and was preserved"
            )
            return
        }
        if ($compact -match "^\[[A-Za-z0-9_.\-]+\]$") {
            $table = $compact.Substring(1, $compact.Length - 2)
            if (
                $table -match "^(features|memories)(\.|$)" -and
                @("features", "memories") -notcontains $table
            ) {
                $Warnings.Add(
                    ".codex/config.toml uses an ambiguous alternate managed table and was preserved"
                )
                return
            }
            $activeSection = $table
            continue
        }
        if ($compact.StartsWith("[")) {
            $Warnings.Add(
                ".codex/config.toml uses an unsupported or ambiguous table header and was preserved"
            )
            return
        }
        if ($candidate -notmatch "^([A-Za-z0-9_.\-]+)\s*=") {
            $Warnings.Add(
                ".codex/config.toml is malformed or uses unsupported syntax and was preserved"
            )
            return
        }
        $left = $Matches[1]
        $equals = $candidate.IndexOf("=")
        $right = $candidate.Substring($equals + 1).Trim()
        $multiline = (
            $structure.Continued[$index] -or
            $right.StartsWith('"""') -or
            $right.StartsWith("'''")
        )
        if (
            -not $multiline -and
            $right -notmatch '^"(?:[^"\\]|\\.)*"$' -and
            $right -notmatch "^'[^']*'$" -and
            $right -notmatch "^\[[^\[\]]*\]$" -and
            $right -notmatch "^\{[^{}]*\}$" -and
            $right -notmatch "^(true|false|[+-]?(inf|nan)|[+-]?[0-9][0-9A-Za-z_.:+-]*)$"
        ) {
            $Warnings.Add(
                ".codex/config.toml has an unsupported or malformed value and was preserved"
            )
            return
        }
        if (
            [string]::IsNullOrEmpty($activeSection) -and
            $left -match "^(features|memories)(\.|$)"
        ) {
            $Warnings.Add(
                ".codex/config.toml uses an ambiguous root managed key and was preserved"
            )
            return
        }
        if (
            $activeSection -eq "features" -and
            $left -match "^memories\."
        ) {
            $Warnings.Add(
                ".codex/config.toml uses an ambiguous dotted features.memories key and was preserved"
            )
            return
        }
        if (
            $activeSection -eq "memories" -and
            $left -match "^(generate_memories|use_memories)\."
        ) {
            $Warnings.Add(
                ".codex/config.toml uses an ambiguous dotted memories policy key and was preserved"
            )
            return
        }
    }
    $changed = $false
    foreach ($requirement in @(
        @("features", "memories"),
        @("memories", "generate_memories"),
        @("memories", "use_memories")
    )) {
        $result = Set-TomlBooleanField `
            -Text $text `
            -Section $requirement[0] `
            -Key $requirement[1] `
            -Expected $Expected
        if (-not $result.Valid) {
            $Warnings.Add(
                ".codex/config.toml is malformed or ambiguous near " +
                "$($requirement[0]).$($requirement[1]) and was preserved"
            )
            return
        }
        $text = $result.Text
        $changed = $changed -or $result.Changed
    }
    if (-not $changed) {
        return
    }

    try {
        Replace-AtomicText -Root $Root -Path $Path -Content $text
        $Recovered.Add(".codex/config.toml (memory policy)")
    }
    catch {
        $Warnings.Add(
            ".codex/config.toml memory policy was not repaired: $($_.Exception.Message)"
        )
    }
}

function Invoke-ConfigurationRecovery {
    param(
        [Parameter(Mandatory = $true)][string]$Root,
        [Parameter(Mandatory = $true)]$ProfileState
    )

    $recovered = New-Object System.Collections.Generic.List[string]
    $warnings = New-Object System.Collections.Generic.List[string]
    if ($ProfileState.Newer) {
        $warnings.Add(
            "managed profile v$($ProfileState.Version) is newer than this hook; no recovery was attempted"
        )
        return @{ Recovered = $recovered; Warnings = $warnings; Mode = $null }
    }
    if (-not $ProfileState.Managed) {
        return @{ Recovered = $recovered; Warnings = $warnings; Mode = $null }
    }

    Restore-ManagedFile -Root $Root `
        -RelativePath ".re-discipline/config.json" `
        -TemplateName "config.json" `
        -DefaultContent $null `
        -Recovered $recovered `
        -Warnings $warnings

    $configPath = Join-Path $Root ".re-discipline/config.json"
    if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) {
        return @{ Recovered = $recovered; Warnings = $warnings; Mode = $null }
    }
    $result = Get-BootstrapConfig -Path $configPath
    if (-not $result.Valid) {
        $warnings.Add($result.Message)
        return @{ Recovered = $recovered; Warnings = $warnings; Mode = $null }
    }
    $mode = [string]$result.Value.memory.mode

    $required = @(
        @(".re-discipline/settings/README.md", "settings-README.md"),
        @(".re-discipline/settings/knowledge.jsonc", "knowledge.jsonc"),
        @(".re-discipline/settings/retrieval-profile.json", "retrieval-profile.json"),
        @(".re-discipline/memory/INDEX.md", "memory-INDEX.md"),
        @(".re-discipline/knowledge/evals/README.md", "knowledge-evals-README.md")
    )
    foreach ($entry in $required) {
        Restore-ManagedFile -Root $Root `
            -RelativePath $entry[0] `
            -TemplateName $entry[1] `
            -DefaultContent $null `
            -Recovered $recovered `
            -Warnings $warnings
    }

    $enabled = if ($mode -eq "shared-only") { "false" } else { "true" }
    $claudeDefault = "{`n  `"autoMemoryEnabled`": $enabled`n}`n"
    $codexDefault = @"
# re-discipline:codex-memory-policy v1
# Preserve unrelated project settings when changing this managed policy.
[features]
memories = $enabled

[memories]
generate_memories = $enabled
use_memories = $enabled
# re-discipline:codex-memory-policy:end
"@
    Restore-ManagedFile -Root $Root `
        -RelativePath ".claude/settings.json" `
        -TemplateName "claude-settings.json" `
        -DefaultContent $claudeDefault `
        -Recovered $recovered `
        -Warnings $warnings
    Restore-ManagedFile -Root $Root `
        -RelativePath ".codex/config.toml" `
        -TemplateName "codex-config.toml" `
        -DefaultContent $codexDefault `
        -Recovered $recovered `
        -Warnings $warnings

    foreach ($relativeDirectory in @(
        ".re-discipline/memory/proposals",
        ".re-discipline/memory/topics",
        ".re-discipline/knowledge/evals"
    )) {
        $directory = Join-Path $Root $relativeDirectory
        if (-not (Test-ManagedPathSafe -Root $Root -Path $directory)) {
            $warnings.Add("$relativeDirectory escapes through a link")
            continue
        }
        if (-not (Test-Path -LiteralPath $directory -PathType Container)) {
            try {
                $null = New-Item -ItemType Directory -Path $directory -Force
            }
            catch {
                $warnings.Add(
                    "$relativeDirectory could not be created: $($_.Exception.Message)"
                )
            }
        }
    }

    $knowledgePath = Join-Path $Root ".re-discipline/settings/knowledge.jsonc"
    if (
        (Test-Path -LiteralPath $knowledgePath -PathType Leaf) -and
        -not (Test-KnowledgeSettings -Path $knowledgePath)
    ) {
        $warnings.Add("settings/knowledge.jsonc is malformed or unsupported")
    }
    $retrievalPath = Join-Path $Root ".re-discipline/settings/retrieval-profile.json"
    if (
        (Test-Path -LiteralPath $retrievalPath -PathType Leaf) -and
        -not (Test-RetrievalProfile -Path $retrievalPath)
    ) {
        $warnings.Add("settings/retrieval-profile.json is malformed or unsupported")
    }

    $expected = $mode -ne "shared-only"
    $claudePath = Join-Path $Root ".claude/settings.json"
    $codexPath = Join-Path $Root ".codex/config.toml"
    Repair-ClaudeMemoryPolicy `
        -Root $Root `
        -Path $claudePath `
        -Expected $expected `
        -Recovered $recovered `
        -Warnings $warnings
    Repair-CodexMemoryPolicy `
        -Root $Root `
        -Path $codexPath `
        -Expected $expected `
        -Recovered $recovered `
        -Warnings $warnings

    foreach ($requirement in @(
        @("features", "memories"),
        @("memories", "generate_memories"),
        @("memories", "use_memories")
    )) {
        $value = Get-TomlBoolean `
            -Path $codexPath `
            -Section $requirement[0] `
            -Key $requirement[1]
        if (-not $value.Valid -or [bool]$value.Value -ne $expected) {
            $warnings.Add(
                ".codex/config.toml $($requirement[0]).$($requirement[1]) does not match memory mode '$mode'"
            )
        }
    }

    return @{ Recovered = $recovered; Warnings = $warnings; Mode = $mode }
}

function Get-RecoverySummary {
    param([Parameter(Mandatory = $true)]$State)

    $parts = New-Object System.Collections.Generic.List[string]
    if ($State.Recovered.Count -gt 0) {
        $parts.Add(
            "Recovered managed project files: " +
            ([string]::Join(", ", @($State.Recovered)))
        )
    }
    if ($State.Warnings.Count -gt 0) {
        $parts.Add(
            "Re-discipline is in read-only degraded configuration state: " +
            ([string]::Join("; ", @($State.Warnings))) +
            ". Invoke init-project repair; existing malformed files were not replaced."
        )
    }
    return [string]::Join("`n", @($parts))
}

function ConvertTo-NativeProcessArgument {
    param([Parameter(Mandatory = $true)][string]$Value)

    if ($Value.Length -gt 0 -and $Value -notmatch '[\s"]') {
        return $Value
    }

    $builder = New-Object System.Text.StringBuilder
    $null = $builder.Append('"')
    $backslashes = 0
    foreach ($character in $Value.ToCharArray()) {
        if ($character -eq '\') {
            $backslashes += 1
            continue
        }
        if ($character -eq '"') {
            $null = $builder.Append(('\' * (($backslashes * 2) + 1)))
            $null = $builder.Append('"')
            $backslashes = 0
            continue
        }
        if ($backslashes -gt 0) {
            $null = $builder.Append(('\' * $backslashes))
            $backslashes = 0
        }
        $null = $builder.Append($character)
    }
    if ($backslashes -gt 0) {
        $null = $builder.Append(('\' * ($backslashes * 2)))
    }
    $null = $builder.Append('"')
    return $builder.ToString()
}

function Get-KnowledgeHealthSummary {
    param([Parameter(Mandatory = $true)][string]$Root)

    if ([string]::IsNullOrWhiteSpace($pluginRoot)) {
        return "Knowledge health status is unavailable because the plugin root is missing."
    }

    $executable = Join-Path $pluginRoot "knowledge/bin/re-discipline-knowledge.exe"
    $assetRoot = Join-Path $pluginRoot "knowledge"
    if (
        -not (Test-Path -LiteralPath $executable -PathType Leaf) -or
        -not (Test-Path -LiteralPath $assetRoot -PathType Container)
    ) {
        return "Knowledge health status is unavailable because the packaged runtime is missing."
    }

    $process = New-Object System.Diagnostics.Process
    $process.StartInfo = New-Object System.Diagnostics.ProcessStartInfo
    $process.StartInfo.FileName = $executable
    $process.StartInfo.Arguments = [string]::Join(" ", @(
        "status",
        "--asset-root",
        (ConvertTo-NativeProcessArgument -Value $assetRoot),
        "--project-root",
        (ConvertTo-NativeProcessArgument -Value $Root)
    ))
    $process.StartInfo.UseShellExecute = $false
    $process.StartInfo.CreateNoWindow = $true
    $process.StartInfo.RedirectStandardOutput = $true
    $process.StartInfo.RedirectStandardError = $true

    try {
        if (-not $process.Start()) {
            return "Knowledge health status could not start."
        }
        $stdout = $process.StandardOutput.ReadToEndAsync()
        $stderr = $process.StandardError.ReadToEndAsync()
        if (-not $process.WaitForExit(3000)) {
            try {
                $process.Kill()
            }
            catch {
                # The hook still reports the timeout and remains non-blocking.
            }
            return "Knowledge health status timed out; run the packaged status command directly."
        }
        $stdout.Wait()
        $stderr.Wait()
        if ($process.ExitCode -ne 0) {
            return "Knowledge health status reported degraded state; run the packaged status command directly."
        }
        return ""
    }
    catch {
        return "Knowledge health status could not run; run the packaged status command directly."
    }
    finally {
        $process.Dispose()
    }
}

function Get-ActiveCampaignReminder {
    param([Parameter(Mandatory = $true)][string]$Root)

    $activeRoot = Join-Path $Root "active"
    if (-not (Test-Path -LiteralPath $activeRoot -PathType Container)) {
        return ""
    }
    $paths = @(
        Get-ChildItem -LiteralPath $activeRoot -Directory -ErrorAction SilentlyContinue |
            ForEach-Object {
                $candidate = Join-Path $_.FullName "CAMPAIGN.md"
                if (Test-Path -LiteralPath $candidate -PathType Leaf) {
                    "active/$($_.Name)/CAMPAIGN.md"
                }
            } |
            Sort-Object |
            Select-Object -First 5
    )
    if ($paths.Count -eq 0) {
        return ""
    }
    return " Active campaign handles: " + ([string]::Join(", ", $paths)) + "."
}

$projectRoot = Find-ReDisciplineRoot -StartPath $projectDir
if ([string]::IsNullOrWhiteSpace($projectRoot)) {
    exit 0
}

$canonicalProfile = Join-Path $projectRoot ".re-discipline/project-profile.md"
$profileState = Get-ManagedProfileState -ProfilePath $canonicalProfile
$onboardReminder = "Invoke the re-discipline onboard skill before substantive work. Read the canonical profile, active manager adapter, docs indexes, and active campaign masterfiles. Use the shared knowledge server for bounded, citable context."
$recoveryReminder = "Legacy or incomplete re-discipline project detected without a supported managed profile. Invoke init-project in migration or recovery mode before substantive work; legacy host profiles are recovery input only."
$checkpointReminder = "Context is about to compact. If a campaign is active, invoke checkpoint-campaign and preserve current state, decisions, evidence boundaries, source handles, and dead ends. If it is solved, invoke close-campaign."
$subagentReminder = "You are a drafter, not a ratifier. Read the canonical project profile, your exact brief, and its immutable context pack when present. Preserve source citations and epistemic tiers. Write only in the granted workspace; never accept memory, promote truth, change retrieval profiles, close a campaign, commit, push, or spawn another agent."

if ($EventName -eq "pre-compact") {
    Write-Output (@{ systemMessage = $checkpointReminder } | ConvertTo-Json -Compress)
    exit 0
}

if ($EventName -eq "subagent-start") {
    $context = if (Test-Path -LiteralPath $canonicalProfile -PathType Leaf) {
        $subagentReminder
    }
    else {
        $recoveryReminder
    }
    Write-HookContext -HookEventName "SubagentStart" -Context $context
    exit 0
}

if ($EventName -eq "post-compact") {
    $context = "Rehydrate re-discipline after compaction. Re-read the canonical profile and active CAMPAIGN.md before substantive work. Use knowledge status or a bounded orientation pack rather than rereading the corpus." +
        (Get-ActiveCampaignReminder -Root $projectRoot)
    Write-HookContext -HookEventName "PostCompact" -Context $context
    exit 0
}

$recoveryState = Invoke-ConfigurationRecovery `
    -Root $projectRoot `
    -ProfileState $profileState
$summary = Get-RecoverySummary -State $recoveryState
$healthSummary = if ($profileState.Managed -and -not $profileState.Newer) {
    Get-KnowledgeHealthSummary -Root $projectRoot
}
else {
    ""
}
if (-not [string]::IsNullOrWhiteSpace($healthSummary)) {
    if ([string]::IsNullOrWhiteSpace($summary)) {
        $summary = $healthSummary
    }
    else {
        $summary += "`n$healthSummary"
    }
}
$source = if ($null -ne $payload -and $payload.source) {
    [string]$payload.source
}
else {
    ""
}

if ($source -eq "compact") {
    $context = "Rehydrate re-discipline after compaction. Re-read the canonical profile and active CAMPAIGN.md before substantive work. Use knowledge status or a bounded orientation pack rather than rereading the corpus." +
        (Get-ActiveCampaignReminder -Root $projectRoot)
    if (-not [string]::IsNullOrWhiteSpace($summary)) {
        $context += "`n$summary"
    }
    Write-HookContext -HookEventName "SessionStart" -Context $context
    exit 0
}

if (-not (Test-Path -LiteralPath $canonicalProfile -PathType Leaf)) {
    $context = $recoveryReminder
}
elseif ($isCodex) {
    $context = [System.IO.File]::ReadAllText(
        $canonicalProfile,
        [System.Text.Encoding]::UTF8
    ).Replace("`r`n", "`n")
}
else {
    $context = $onboardReminder
}
if (-not [string]::IsNullOrWhiteSpace($summary)) {
    $context += "`n`n$summary"
}
Write-HookContext -HookEventName "SessionStart" -Context $context
exit 0
