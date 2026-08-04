# Optional: unit gate + hints for docker stats sampling.
# Full matrix: python scripts/demux_groups_matrix/run.py --defaults-only --all

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

Write-Host "Demux cost probe"
Write-Host "1. Unit shape test - no Docker required"
go test -tags with_controlplane ./internal/controlplane/demuxgroups/ -count=1 -run TestBuildInstallAllGroupsDefaults
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host ""
Write-Host "2. Model: listeners ~= 1 public demux + N member inbounds"
Write-Host "   See docs/guides/controlplane-presets/09-demux-cost.md"
Write-Host "   Sample matrix keep mode, then docker stats --no-stream inv-server"
Write-Host ""
Write-Host "Done - unit gate passed."
