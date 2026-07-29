# Multi-mode traffic matrix: CP + edge + VLESS + iperf.
$ErrorActionPreference = "Stop"
$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $Root
$Compose = "docker-compose.traffic-modes.yml"

Write-Host "== host-build linux binaries =="
New-Item -ItemType Directory -Force -Path dist, testdata/docker/client | Out-Null
$env:CGO_ENABLED = "0"
$env:GOOS = "linux"
$env:GOARCH = "amd64"

$tagsCp = ((Get-Content build/tags.server.traffic.controlplane -Raw) -replace "`r|`n", "").Trim()
go build -trimpath -ldflags="-s -w -checklinkname=0" -tags $tagsCp -o dist/subserver-traffic-cp-linux ./cmd/subserver
if ($LASTEXITCODE -ne 0) { throw "traffic+cp build failed" }

$tagsT = ((Get-Content build/tags.server.traffic -Raw) -replace "`r|`n", "").Trim()
go build -trimpath -ldflags="-s -w -checklinkname=0" -tags $tagsT -o dist/subserver-traffic-linux ./cmd/subserver
if ($LASTEXITCODE -ne 0) { throw "traffic-only build failed" }

Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue

Write-Host "== compose up =="
$prevEap = $ErrorActionPreference
$ErrorActionPreference = "Continue"
docker compose -f $Compose down -v 2>&1 | Out-Null
docker compose -f $Compose up -d --build
$upCode = $LASTEXITCODE
$ErrorActionPreference = $prevEap
if ($upCode -ne 0) { throw "compose up failed" }

Write-Host "== wait containers =="
$ok = $false
for ($i = 0; $i -lt 90; $i++) {
  & curl.exe -fkSs "https://127.0.0.1:18082/v1/health" -o NUL 2>$null
  $cpOk = ($LASTEXITCODE -eq 0)
  & curl.exe -fsS "http://127.0.0.1:18083/v1/health" -o NUL 2>$null
  $edgeOk = ($LASTEXITCODE -eq 0)
  if ($cpOk -and $edgeOk) { $ok = $true; break }
  Start-Sleep -Seconds 2
}
if (-not $ok) {
  docker compose -f $Compose ps
  docker compose -f $Compose logs --tail 80
  throw "health timeout"
}

$skip = @()
if ($env:SKIP_IPERF -eq "1") { $skip = @("--skip-iperf") }

Write-Host "== python multi-mode scenarios =="
$env:PYTHONIOENCODING = "utf-8"
python scripts/smoke_traffic_modes.py --insecure @skip
if ($LASTEXITCODE -ne 0) {
  docker compose -f $Compose logs --tail 160
  throw "smoke_traffic_modes failed"
}

Write-Host "== OK traffic modes matrix =="
if ($env:KEEP_UP -ne "1") {
  $ErrorActionPreference = "Continue"
  docker compose -f $Compose down -v 2>&1 | Out-Null
}
