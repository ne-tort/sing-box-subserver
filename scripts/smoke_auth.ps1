# Auth CRUD / rotation smoke against docker-compose.smoke.yml
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
    return @{ StatusCode = $code; Content = "" }
  }
  if ($Expect -notcontains [int]$r.StatusCode) { throw "unexpected $($r.StatusCode) $Method $Path" }
  return @{ StatusCode = [int]$r.StatusCode; Content = $r.Content }
}

Write-Host "== compose up =="
docker compose -f docker-compose.smoke.yml down -v
docker compose -f docker-compose.smoke.yml up -d --build
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

Write-Host "== 401 without token =="
Invoke-Api -Method GET -Path /v1/status -Token "wrong" -Expect @(401) | Out-Null

Write-Host "== bootstrap list =="
$list = (Invoke-Api -Method GET -Path /v1/auth/tokens -Token $Boot).Content | ConvertFrom-Json
if (-not $list.data.bootstrap_enabled) { throw "bootstrap should be on" }

Write-Host "== create panel token =="
$created = (Invoke-Api -Method POST -Path /v1/auth/tokens -Token $Boot -Body '{"name":"panel"}').Content | ConvertFrom-Json
$panel = $created.data.token
if (-not $panel) { throw "no token returned" }

Write-Host "== panel token works =="
Invoke-Api -Method GET -Path /v1/status -Token $panel | Out-Null

Write-Host "== disable bootstrap =="
Invoke-Api -Method POST -Path /v1/auth/bootstrap/disable -Token $panel | Out-Null
Invoke-Api -Method GET -Path /v1/status -Token $Boot -Expect @(401) | Out-Null

Write-Host "== rotate revoke_others =="
$rot = (Invoke-Api -Method POST -Path /v1/auth/rotate -Token $panel -Body '{"name":"panel","revoke_others":true}').Content | ConvertFrom-Json
$panel2 = $rot.data.token
Invoke-Api -Method GET -Path /v1/status -Token $panel -Expect @(401) | Out-Null
Invoke-Api -Method GET -Path /v1/status -Token $panel2 | Out-Null

Write-Host "== OK auth smoke =="
docker compose -f docker-compose.smoke.yml down
