# Traffic + controlplane scenario matrix (quota / shaping / reset).
$ErrorActionPreference = "Stop"
[System.Net.ServicePointManager]::ServerCertificateValidationCallback = { $true }
$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $Root
$Token = "smoke-token-not-for-prod"
$Base = if ($env:BASE_URL) { $env:BASE_URL } else { "https://127.0.0.1:18082" }
$Compose = "docker-compose.traffic-smoke.yml"

Write-Host "== host-build linux binary (traffic+controlplane tags) =="
New-Item -ItemType Directory -Force -Path dist | Out-Null
$tags = ((Get-Content build/tags.server.traffic.controlplane -Raw) -replace "`r|`n", "").Trim()
$env:CGO_ENABLED = "0"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w -checklinkname=0" -tags $tags -o dist/subserver-traffic-cp-linux ./cmd/subserver
$buildEc = $LASTEXITCODE
Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
if ($buildEc -ne 0) { throw "host go build failed" }
& "$PSScriptRoot\ensure_libcronet.ps1" -Arch amd64
if ($LASTEXITCODE -ne 0) { throw "ensure_libcronet failed" }

Write-Host "== compose up (runtime image + prebuilt binary) =="
docker compose -f $Compose down -v 2>$null | Out-Null
docker compose -f $Compose up -d --build
if ($LASTEXITCODE -ne 0) { throw "compose up failed" }

Write-Host "== wait health =="
$ok = $false
for ($i = 0; $i -lt 120; $i++) {
  & curl.exe -fkSs "$Base/v1/health" -o NUL 2>$null
  if ($LASTEXITCODE -eq 0) { $ok = $true; break }
  Start-Sleep -Seconds 2
}
if (-not $ok) {
  docker compose -f $Compose logs --tail 80
  throw "health timeout"
}

Write-Host "== python traffic scenarios =="
python scripts/smoke_traffic_scenarios.py --base $Base --token $Token --insecure
if ($LASTEXITCODE -ne 0) {
  docker compose -f $Compose logs --tail 120
  throw "smoke_traffic_scenarios failed"
}

Write-Host "== OK traffic+controlplane matrix =="
docker compose -f $Compose down -v | Out-Null
