# Read-only session-start status line. Any failure exits silently; the
# session must start identically with or without this hook.
try {
    $rd = Join-Path (Get-Location) '.re-discipline'
    if (-not (Test-Path $rd -PathType Container)) { exit 0 }

    $campaigns = @(Get-ChildItem (Join-Path $rd 'active') -Directory -ErrorAction SilentlyContinue)
    $docs = @(Get-ChildItem (Join-Path $rd 'docs') -Recurse -Filter *.md -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -ne 'INDEX.md' })

    $campaignPart = "$($campaigns.Count) active campaign(s)"
    if ($campaigns.Count -gt 0) {
        $names = $campaigns | ForEach-Object {
            $age = [int]((Get-Date) - $_.LastWriteTime).TotalDays
            "$($_.Name) (updated ${age}d ago)"
        }
        $campaignPart = "$($campaigns.Count) active campaign(s) - $($names -join ', ')"
    }
    Write-Output "re-discipline: $campaignPart. $($docs.Count) docs curated."
} catch { }
exit 0
