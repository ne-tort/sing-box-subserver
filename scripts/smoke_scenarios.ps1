# Full scenario smoke: idle → subscribe → refresh → direct cancels → re-subscribe
$ErrorActionPreference = "Continue"
$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $Root
$Bootstrap = "smoke-token-not-for-prod"
$Token = $Bootstrap
$Base = if ($env:BASE_URL) { $env:BASE_URL } else { "http://127.0.0.1:18080" }

function Invoke-Api {
  param(
    [Parameter(Mandatory=$true)][string]$Method,
    [Parameter(Mandatory=$true)][string]$Path,
    [string]$Body,
    [int[]]$Expect = @(200)
  )
  $uri = "$Base$Path"
  $headers = @{ Authorization = "Bearer $Token" }
  $hasBody = $PSBoundParameters.ContainsKey("Body") -and ($null -ne $Body)
  try {
    if ($hasBody) {
      $headers["Content-Type"] = "application/json"
      $r = Invoke-WebRequest -Method $Method -Uri $uri -Headers $headers -Body $Body -UseBasicParsing
    } else {
      $r = Invoke-WebRequest -Method $Method -Uri $uri -Headers $headers -UseBasicParsing
    }
  } catch {
    $resp = $_.Exception.Response
    if ($null -eq $resp) { throw $_ }
    $code = [int]$resp.StatusCode
    if ($Expect -notcontains $code) { throw "unexpected $code for $Method $Path (want $Expect): $_" }
    $sr = New-Object System.IO.StreamReader($resp.GetResponseStream())
    $text = $sr.ReadToEnd()
    return @{ StatusCode = $code; Content = $text }
  }
  if ($Expect -notcontains [int]$r.StatusCode) {
    throw "unexpected $($r.StatusCode) for $Method $Path"
  }
  return @{ StatusCode = [int]$r.StatusCode; Content = $r.Content }
}

Write-Host "== compose up =="
docker compose -f docker-compose.smoke.yml down -v
if ($LASTEXITCODE -ne 0 -and $LASTEXITCODE -ne $null) { Write-Host "down exit=$LASTEXITCODE (ok if fresh)" }
docker compose -f docker-compose.smoke.yml up -d --build
if ($LASTEXITCODE -ne 0) { throw "compose up failed exit=$LASTEXITCODE" }

Write-Host "== wait health =="
$ok = $false
for ($i = 0; $i -lt 90; $i++) {
  try {
    $r = Invoke-WebRequest -Uri "$Base/v1/health" -UseBasicParsing -TimeoutSec 2
    if ($r.StatusCode -eq 200) { $ok = $true; break }
  } catch { Start-Sleep -Seconds 2 }
}
if (-not $ok) { throw "health timeout" }

Write-Host "== bootstrap -> managed token =="
try {
  $created = (Invoke-Api -Method POST -Path /v1/auth/tokens -Body '{"name":"scenarios"}').Content | ConvertFrom-Json
  if ($created.data.token) {
    $Token = $created.data.token
    Write-Host "   switched to managed token"
  }
} catch {
  Write-Host "   token create skipped (continuing with bootstrap)"
}

Write-Host "== 1) idle: not subscribed, waiting =="
$st = (Invoke-Api -Method GET -Path /v1/status).Content | ConvertFrom-Json
if ($st.data.subscribe.enabled -eq $true) { throw "expected idle subscribe" }
Write-Host ("   config_mode=" + $st.data.config_mode + " state=" + $st.data.state)

Write-Host "== 2) subscribe to mock panel a.json =="
$subBody = '{"url":"http://mock-panel:8080/a.json","interval_sec":3600,"jitter_sec":0,"timeout_sec":10}'
$sub = (Invoke-Api -Method POST -Path /v1/subscribe -Body $subBody).Content | ConvertFrom-Json
if (-not $sub.ok) { throw "subscribe failed: $($sub | ConvertTo-Json -Compress)" }
if ($sub.data.config_mode -ne "subscribed") { throw "want subscribed mode" }
$cfg = (Invoke-Api -Method GET -Path /v1/config).Content
if ($cfg -notmatch "from-sub-a") { throw "config missing from-sub-a: $cfg" }
Write-Host "   applied from-sub-a revision=$($sub.data.revision)"

Write-Host "== 3) force refresh same URL (noop or ok) =="
$ref = (Invoke-Api -Method POST -Path /v1/subscribe/refresh).Content | ConvertFrom-Json
if (-not $ref.ok) { throw "refresh failed" }

Write-Host "== 4) switch subscribe to b.json (overwrite) =="
$subBodyB = '{"url":"http://mock-panel:8080/b.json","interval_sec":3600,"jitter_sec":0}'
$sub2 = (Invoke-Api -Method POST -Path /v1/subscribe -Body $subBodyB).Content | ConvertFrom-Json
if (-not $sub2.ok) { throw "subscribe B failed" }
$cfg2 = (Invoke-Api -Method GET -Path /v1/config).Content
if ($cfg2 -notmatch "from-sub-b") { throw "expected from-sub-b: $cfg2" }

Write-Host "== 5) direct PUT cancels subscription =="
$direct = '{"log":{"level":"error"},"inbounds":[],"outbounds":[{"type":"direct","tag":"direct-push"}]}'
$put = (Invoke-Api -Method PUT -Path /v1/config -Body $direct).Content | ConvertFrom-Json
if ($put.data.config_mode -ne "direct") { throw "want direct mode after PUT" }
$st2 = (Invoke-Api -Method GET -Path /v1/status).Content | ConvertFrom-Json
if ($st2.data.subscribe.enabled -eq $true) { throw "subscribe should be cancelled" }
$cfg3 = (Invoke-Api -Method GET -Path /v1/config).Content
if ($cfg3 -notmatch "direct-push") { throw "direct config missing" }
# refresh must fail while unsubscribed
Invoke-Api -Method POST -Path /v1/subscribe/refresh -Expect @(409) | Out-Null
Write-Host "   direct locked; refresh correctly returns 409"

Write-Host "== 6) re-subscribe overwrites direct =="
$sub3 = (Invoke-Api -Method POST -Path /v1/subscribe -Body $subBody).Content | ConvertFrom-Json
if (-not $sub3.ok) { throw "re-subscribe failed" }
$cfg4 = (Invoke-Api -Method GET -Path /v1/config).Content
if ($cfg4 -notmatch "from-sub-a") { throw "re-subscribe should restore a.json" }

Write-Host "== 7) hot apply: agent process still alive (health) =="
Invoke-Api -Method GET -Path /v1/health | Out-Null
$st3 = (Invoke-Api -Method GET -Path /v1/status).Content | ConvertFrom-Json
if (-not $st3.ok) { throw "status not ok" }
if ($st3.data.config_mode -ne "subscribed") { throw "want subscribed after re-sub" }
Write-Host ("   uptime_sec=" + $st3.data.uptime_sec + " revision=" + $st3.data.revision + " mode=" + $st3.data.config_mode)

Write-Host "== OK scenarios =="
docker compose -f docker-compose.smoke.yml down
