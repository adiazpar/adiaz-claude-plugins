<#
Provider-neutral external drafter dispatcher.

The delegate skill owns policy and creates the workspace. This script expands
a configured provider's command template and ensures the standard report
lands.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[a-z0-9][a-z0-9-]{1,49}$')]
    [string]$Provider,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[a-z0-9][a-z0-9-]{2,49}$')]
    [string]$Slug,

    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[a-z0-9][a-z0-9-]{1,49}$')]
    [string]$Name,

    [string]$BriefPath,
    [string]$ConfigPath,
    [string]$Model,
    [switch]$Bypass,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
$root = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$canonicalProfile = Join-Path $root '.re-discipline\project-profile.md'
if (-not (Test-Path -LiteralPath $canonicalProfile -PathType Leaf)) {
    throw 'Canonical profile not found: .re-discipline/project-profile.md'
}

function Resolve-FromRoot {
    param([Parameter(Mandatory = $true)][string]$Path)

    if ([System.IO.Path]::IsPathRooted($Path)) {
        return [System.IO.Path]::GetFullPath($Path)
    }
    return [System.IO.Path]::GetFullPath((Join-Path $script:root $Path))
}

function Assert-AllowedProperties {
    param(
        [Parameter(Mandatory = $true)]$Object,
        [Parameter(Mandatory = $true)][string[]]$Allowed,
        [Parameter(Mandatory = $true)][string]$Label
    )

    foreach ($property in @($Object.PSObject.Properties.Name)) {
        if ($Allowed -notcontains $property) {
            throw "Unsupported $Label key '$property'."
        }
    }
}

function Get-OptionalArray {
    param(
        [Parameter(Mandatory = $true)]$Object,
        [Parameter(Mandatory = $true)][string]$Name
    )

    if ($null -eq $Object.PSObject.Properties[$Name]) {
        return @()
    }
    return @($Object.$Name)
}

$configFile = if ($ConfigPath) {
    Resolve-FromRoot $ConfigPath
}
else {
    Join-Path $PSScriptRoot 'config.json'
}

if (-not (Test-Path -LiteralPath $configFile -PathType Leaf)) {
    throw "Config not found: $configFile"
}

$config = Get-Content -LiteralPath $configFile -Raw | ConvertFrom-Json
Assert-AllowedProperties $config @('backend', 'providers') 'config'
if ([string]::IsNullOrWhiteSpace([string]$config.backend)) {
    throw "Config requires a non-empty 'backend'."
}
if ($null -eq $config.providers) {
    throw "Config requires a 'providers' object."
}

$backend = [string]$config.backend
$configuredProviderNames = @($config.providers.PSObject.Properties.Name)
if ($backend -ne 'native' -and $configuredProviderNames -notcontains $backend) {
    throw "Backend '$backend' is not native or a configured provider."
}

$providerProperty = $config.providers.PSObject.Properties[$Provider]
if ($null -eq $providerProperty) {
    throw "Unknown provider '$Provider'."
}
$providerConfig = $providerProperty.Value
Assert-AllowedProperties $providerConfig @(
    'command',
    'args',
    'model',
    'model_flag',
    'sandbox_args',
    'bypass_args'
) 'provider'

if ([string]::IsNullOrWhiteSpace([string]$providerConfig.command)) {
    throw "Provider '$Provider' requires a command."
}
if ($null -eq $providerConfig.PSObject.Properties['args']) {
    throw "Provider '$Provider' requires args."
}
if (@($providerConfig.args).Count -eq 0) {
    throw "Provider '$Provider' requires at least one argument template."
}
if ($null -eq (Get-Command $providerConfig.command -ErrorAction SilentlyContinue)) {
    throw "CLI '$($providerConfig.command)' was not found on PATH."
}

$providerProfile = if ($ConfigPath) {
    Join-Path (Split-Path -Parent $configFile) 'profile.md'
}
else {
    Join-Path $PSScriptRoot "providers\$Provider\profile.md"
}
if (-not (Test-Path -LiteralPath $providerProfile -PathType Leaf)) {
    throw "Provider profile not found: $providerProfile"
}

$campaign = Resolve-FromRoot "active/$Slug"
if (-not (Test-Path -LiteralPath (Join-Path $campaign 'CAMPAIGN.md') -PathType Leaf)) {
    throw "Campaign not found or missing CAMPAIGN.md: active/$Slug"
}

$dispatchName = if ($Name.StartsWith("$Provider-")) { $Name } else { "$Provider-$Name" }
$workspace = Resolve-FromRoot "active/$Slug/subagents/$dispatchName"
$campaignPrefix = $campaign.TrimEnd('\', '/') + [System.IO.Path]::DirectorySeparatorChar
if (-not $workspace.StartsWith($campaignPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'Resolved workspace escaped the selected campaign.'
}
if (-not (Test-Path -LiteralPath $workspace -PathType Container)) {
    throw "Drafter workspace not found: $workspace"
}

$brief = if ($BriefPath) { Resolve-FromRoot $BriefPath } else { Join-Path $workspace 'brief.md' }
$override = Join-Path $workspace 'AGENTS.override.md'
if (-not (Test-Path -LiteralPath $brief -PathType Leaf)) {
    throw "Brief not found: $brief"
}
if (-not (Test-Path -LiteralPath $override -PathType Leaf)) {
    throw "Drafter AGENTS.override.md not found: $override"
}

$report = Join-Path $workspace 'report.md'
$lastMessage = Join-Path $workspace 'last_message.md'
$log = Join-Path $workspace 'dispatch.log'
$instructionsFile = '.codex/external-drafter-contract.md'
$prompt = "You are an external drafter. Start in '$workspace'. Read AGENTS.override.md, " +
    "$instructionsFile, '$canonicalProfile', '$providerProfile', and '$brief'. " +
    "Carry out only the brief and write the full report to '$report'."

$resolvedModel = if ($Model) {
    $Model
}
elseif ($providerConfig.model) {
    [string]$providerConfig.model
}
else {
    $null
}

if ($Bypass) {
    $policyArgs = Get-OptionalArray $providerConfig 'bypass_args'
    if ($policyArgs.Count -eq 0) {
        throw "Provider '$Provider' has no verified bypass_args."
    }
    $policyLabel = 'BYPASS (explicit)'
}
else {
    $policyArgs = Get-OptionalArray $providerConfig 'sandbox_args'
    $policyLabel = 'SANDBOXED'
}

$modelArgs = @()
if ($resolvedModel) {
    if ([string]::IsNullOrWhiteSpace([string]$providerConfig.model_flag)) {
        throw "Provider '$Provider' has a model but no model_flag."
    }
    $modelArgs = @([string]$providerConfig.model_flag, $resolvedModel)
}

$arguments = @()
foreach ($argument in @($providerConfig.args)) {
    if ($argument -eq '{model_args}') {
        $arguments += $modelArgs
        continue
    }
    if ($argument -eq '{policy_args}') {
        $arguments += $policyArgs
        continue
    }

    $expanded = [string]$argument
    $expanded = $expanded.Replace('{root}', $root)
    $expanded = $expanded.Replace('{workspace}', $workspace)
    $expanded = $expanded.Replace('{brief}', $brief)
    $expanded = $expanded.Replace('{report}', $report)
    $expanded = $expanded.Replace('{lastmsg}', $lastMessage)
    $expanded = $expanded.Replace('{prompt}', $prompt)
    $arguments += $expanded
}

$modelLabel = if ($resolvedModel) { $resolvedModel } else { '<CLI default>' }
Write-Output "[dispatch] provider=$Provider workspace=$dispatchName model=$modelLabel policy=$policyLabel"
Write-Output "[dispatch] command=$($providerConfig.command) $($arguments -join ' ')"
if ($DryRun) {
    Write-Output '[dispatch] DRY RUN - command not executed.'
    exit 0
}

$null | & $providerConfig.command @arguments 2>&1 | Tee-Object -FilePath $log
$agentExit = $LASTEXITCODE

if (-not (Test-Path -LiteralPath $report -PathType Leaf)) {
    if (Test-Path -LiteralPath $lastMessage -PathType Leaf) {
        Copy-Item -LiteralPath $lastMessage -Destination $report
        Write-Output '[dispatch] report.md missing; copied last_message.md.'
    }
    else {
        throw "No report.md or last_message.md was produced. Inspect $log"
    }
}

Write-Output "[dispatch] report ready: $report"
exit $agentExit
