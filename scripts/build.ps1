# Canonical re-search build. This exact command is shared by release and
# CI; any drift breaks the stale-binary check by design.
param(
    [string]$Output = "bin/re-search.exe"
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot

$claude = (Get-Content (Join-Path $repo '.claude-plugin/plugin.json') -Raw | ConvertFrom-Json).version
$codex  = (Get-Content (Join-Path $repo '.codex-plugin/plugin.json')  -Raw | ConvertFrom-Json).version
if ($claude -ne $codex) { throw "version mismatch: .claude-plugin=$claude .codex-plugin=$codex" }

# Marketplace entries must match too (spec §11) — this check is what
# replaced the 0.x version sync/guard scripts.
foreach ($mp in @('.claude-plugin/marketplace.json', '.agents/plugins/marketplace.json')) {
    $mpPath = Join-Path $repo $mp
    if (-not (Test-Path $mpPath)) { continue }
    $entry = (Get-Content $mpPath -Raw | ConvertFrom-Json).plugins |
        Where-Object { $_.name -eq 're-discipline' }
    if ($entry -and $entry.version -and $entry.version -ne $claude) {
        throw "version mismatch: $mp=$($entry.version) plugin.json=$claude"
    }
}

# Force the exact compiler from go.mod's toolchain directive. Without
# this, GOTOOLCHAIN=auto silently uses any NEWER installed Go (CI's
# go1.26.5 vs local go1.26.4 produced different binaries and broke the
# stale-binary check).
$tc = (Select-String -Path (Join-Path $repo 'retrieval/go.mod') -Pattern '^toolchain (\S+)').Matches[0].Groups[1].Value
if (-not $tc) { throw "no toolchain directive in retrieval/go.mod" }
$env:GOTOOLCHAIN = $tc

# cgo is forbidden in this project, and CGO_ENABLED is part of the build
# ID baked into the binary — machines with a C compiler default it to 1
# and produce a different file hash for identical code. Pin it.
$env:CGO_ENABLED = '0'

$out = Join-Path $repo $Output
New-Item -ItemType Directory -Force (Split-Path -Parent $out) | Out-Null
Push-Location (Join-Path $repo 'retrieval')
try {
    go build -trimpath -buildvcs=false -ldflags "-X main.version=$claude" -o $out ./cmd/re-search
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally { Pop-Location }
Write-Output "built $out (version $claude)"
