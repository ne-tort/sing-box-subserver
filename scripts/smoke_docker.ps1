# Smoke scenarios against docker-compose.smoke.yml (Windows/Linux PowerShell)
$ErrorActionPreference = "Stop"
$Root = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
if (Test-Path (Join-Path $PSScriptRoot "..\docker-compose.smoke.yml")) {
  $Root = Resolve-Path (Join-Path $PSScriptRoot "..")
}
Set-Location $Root
$Token = "smoke-token-not-for-prod"
$Base = if ($env:BASE_URL) { $env:BASE_URL } else { "http://127.0.0.1:18080" }

Write-Host "== compose up =="
docker compose -f docker-compose.smoke.yml down -v
if ($LASTEXITCODE -ne 0 -and $LASTEXITCODE -ne $null) { Write-Host "down exit=$LASTEXITCODE (ok if fresh)" }
docker compose -f docker-compose.smoke.yml up -d --build
if ($LASTEXITCODE -ne 0) { throw "compose up failed" }

Write-Host "== wait health =="
$ok = $false
for ($i = 0; $i -lt 90; $i++) {
  try {
    $r = Invoke-WebRequest -Uri "$Base/v1/health" -UseBasicParsing -TimeoutSec 2
    if ($r.StatusCode -eq 200) { $ok = $true; break }
  } catch { Start-Sleep -Seconds 2 }
}
if (-not $ok) { throw "health timeout" }

Write-Host "== unauthorized status =="
try {
  Invoke-WebRequest -Uri "$Base/v1/status" -UseBasicParsing -TimeoutSec 5 | Out-Null
  throw "expected 401"
} catch {
  if ($_.Exception.Response.StatusCode.value__ -ne 401) { throw $_ }
}

$headers = @{ Authorization = "Bearer $Token"; "Content-Type" = "application/json" }
$cfg = '{"log":{"level":"error"},"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}]}'
Write-Host "== put config =="
$put = Invoke-RestMethod -Method Put -Uri "$Base/v1/config" -Headers $headers -Body $cfg
if (-not $put.ok) { throw "put failed: $($put | ConvertTo-Json)" }

Write-Host "== ready =="
Invoke-RestMethod -Uri "$Base/v1/ready" -Headers @{ Authorization = "Bearer $Token" } | Out-Null

Write-Host "== reject clash =="
$clash = '{"experimental":{"clash_api":{"external_controller":"0.0.0.0:9090"}},"outbounds":[{"type":"direct","tag":"d"}]}'
try {
  Invoke-WebRequest -Method Post -Uri "$Base/v1/validate" -Headers $headers -Body $clash -UseBasicParsing | Out-Null
  throw "expected 422"
} catch {
  if ($_.Exception.Response.StatusCode.value__ -ne 422) { throw $_ }
}

Write-Host "== box stop/start =="
Invoke-RestMethod -Method Post -Uri "$Base/v1/box/stop" -Headers @{ Authorization = "Bearer $Token" } | Out-Null
try {
  Invoke-WebRequest -Uri "$Base/v1/ready" -Headers @{ Authorization = "Bearer $Token" } -UseBasicParsing | Out-Null
  throw "expected 503 after stop"
} catch {
  if ($_.Exception.Response.StatusCode.value__ -ne 503) { throw $_ }
}
Invoke-RestMethod -Method Post -Uri "$Base/v1/box/start" -Headers @{ Authorization = "Bearer $Token" } | Out-Null
Invoke-RestMethod -Uri "$Base/v1/ready" -Headers @{ Authorization = "Bearer $Token" } | Out-Null

Write-Host "== metrics =="
Invoke-RestMethod -Uri "$Base/v1/metrics?format=json" -Headers @{ Authorization = "Bearer $Token" } | Out-Null

Write-Host "== OK smoke =="
docker compose -f docker-compose.smoke.yml down
