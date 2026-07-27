# Smoke: pull/heartbeat REST, dedupe, YAML no-reseed after DELETE
$ErrorActionPreference = "Continue"
$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $Root
$Boot = "smoke-token-not-for-prod"
$Base = if ($env:BASE_URL) { $env:BASE_URL } else { "http://127.0.0.1:18080" }

function Invoke-Api {
  param([string]$Method, [string]$Path, [string]$Token, [string]$Body, [int[]]$Expect = @(200))
  $headers = @{ Authorization = "Bearer $Token" }
  $uri = "$Base$Path"
  try {
    if ($PSBoundParameters.ContainsKey("Body") -and ($null -ne $Body)) {
      $headers["Content-Type"] = "application/json"
      $r = Invoke-WebRequest -Method $Method -Uri $uri -Headers $headers -Body $Body -UseBasicParsing
    } else {
      $r = Invoke-WebRequest -Method $Method -Uri $uri -Headers $headers -UseBasicParsing
    }
  } catch {
    $resp = $_.Exception.Response
    if ($null -eq $resp) { throw $_ }
    $code = [int]$resp.StatusCode
    if ($Expect -notcontains $code) { throw "unexpected $code $Method $Path" }
    return @{ StatusCode = $code }
  }
  if ($Expect -notcontains [int]$r.StatusCode) { throw "unexpected $($r.StatusCode) $Method $Path" }
  return @{ StatusCode = [int]$r.StatusCode; Content = $r.Content }
}

Write-Host "== compose up =="
docker compose -f docker-compose.smoke.yml down -v
docker compose -f docker-compose.smoke.yml up -d
if ($LASTEXITCODE -ne 0) { throw "compose up failed" }

Write-Host "== wait health =="
$ok = $false
for ($i = 0; $i -lt 60; $i++) {
  try {
    $r = Invoke-WebRequest -Uri "$Base/v1/health" -UseBasicParsing -TimeoutSec 2
    if ($r.StatusCode -eq 200) { $ok = $true; break }
  } catch { Start-Sleep -Seconds 2 }
}
if (-not $ok) { throw "health timeout" }

Write-Host "== managed token =="
$created = (Invoke-Api -Method POST -Path /v1/auth/tokens -Token $Boot -Body '{"name":"smoke"}').Content | ConvertFrom-Json
$Tok = $created.data.token

Write-Host "== heartbeat PUT =="
$hbBody = '{"url":"http://127.0.0.1:1/nope","interval_sec":3600,"enabled":true}'
$hb = (Invoke-Api -Method PUT -Path /v1/heartbeat -Token $Tok -Body $hbBody).Content | ConvertFrom-Json
if (-not $hb.data.configured) { throw "heartbeat should be configured" }

Write-Host "== pull alias subscribe =="
$subBody = '{"url":"http://mock-panel:8080/a.json","interval_sec":3600,"jitter_sec":0,"timeout_sec":10}'
$sub = (Invoke-Api -Method PUT -Path /v1/pull -Token $Tok -Body $subBody).Content | ConvertFrom-Json
if (-not $sub.ok) { throw "pull subscribe failed" }
if (-not $sub.data.subscribe.configured) { throw "subscribe configured flag missing" }

Write-Host "== dedupe refresh =="
$rev1 = $sub.data.revision
$ref = (Invoke-Api -Method POST -Path /v1/pull/refresh -Token $Tok).Content | ConvertFrom-Json
$rev2 = $ref.data.revision
if ($rev2 -ne $rev1) { throw "expected same revision on identical body: $rev1 -> $rev2" }
if (-not $ref.data.subscribe.last_noop) { throw "expected last_noop on identical refresh" }

Write-Host "== DELETE pull stays disabled on status =="
Invoke-Api -Method DELETE -Path /v1/pull -Token $Tok | Out-Null
$st = (Invoke-Api -Method GET -Path /v1/status -Token $Tok).Content | ConvertFrom-Json
if ($st.data.subscribe.enabled -eq $true) { throw "subscribe should be disabled" }
if ($st.data.subscribe.configured -ne $true) { throw "configured must remain true after DELETE" }

Write-Host "== DELETE heartbeat =="
Invoke-Api -Method DELETE -Path /v1/heartbeat -Token $Tok | Out-Null
$hb2 = (Invoke-Api -Method GET -Path /v1/heartbeat -Token $Tok).Content | ConvertFrom-Json
if ($hb2.data.enabled -eq $true) { throw "heartbeat should be disabled" }
if ($hb2.data.configured -ne $true) { throw "heartbeat configured must remain true" }

Write-Host "== OK pull/heartbeat smoke =="
docker compose -f docker-compose.smoke.yml down
