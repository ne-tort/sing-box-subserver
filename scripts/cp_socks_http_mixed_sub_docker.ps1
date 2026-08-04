# SOCKS / HTTP / Mixed subscription smoke via Docker (catalogsqlite SoT).
$ErrorActionPreference = "Stop"
$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $Root
$Token = "smoke-token-not-for-prod"
$Base = if ($env:BASE_URL) { $env:BASE_URL } else { "https://127.0.0.1:18081" }
$Compose = "docker-compose.cp-smoke.yml"
$Curl = "curl.exe"

function Invoke-Json([string]$Method, [string]$Path, $Body = $null) {
  $uri = "$Base$Path"
  $outFile = [System.IO.Path]::GetTempFileName()
  try {
    $args = @("-sk", "-o", $outFile, "-w", "%{http_code}", "-H", "Authorization: Bearer $Token", "-H", "Accept: application/json", "-X", $Method)
    $bodyFile = $null
    if ($null -ne $Body) {
      $raw = if ($Body -is [string]) { $Body } else { ($Body | ConvertTo-Json -Compress -Depth 12) }
      $bodyFile = [System.IO.Path]::GetTempFileName()
      [System.IO.File]::WriteAllText($bodyFile, $raw, [System.Text.UTF8Encoding]::new($false))
      $args += @("-H", "Content-Type: application/json", "--data-binary", "@$bodyFile")
    }
    $args += @($uri)
    $code = (& $Curl @args | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { throw "curl $Method ${Path} failed ($LASTEXITCODE)" }
    $payload = [System.IO.File]::ReadAllText($outFile)
    if ([int]$code -lt 200 -or [int]$code -ge 300) { throw "curl $Method ${Path} HTTP ${code}: $payload" }
    if ([string]::IsNullOrWhiteSpace($payload)) { return $null }
    return ($payload | ConvertFrom-Json)
  } finally {
    Remove-Item $outFile -ErrorAction SilentlyContinue
    if ($bodyFile) { Remove-Item $bodyFile -ErrorAction SilentlyContinue }
  }
}

function Invoke-Raw([string]$Path) {
  $uri = "$Base$Path"
  $outFile = [System.IO.Path]::GetTempFileName()
  try {
    $code = (& $Curl -sk -o $outFile -w "%{http_code}" -H "Authorization: Bearer $Token" -H "Accept: application/json" $uri | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { throw "curl GET ${Path} failed ($LASTEXITCODE)" }
    $payload = [System.IO.File]::ReadAllText($outFile)
    if ([int]$code -lt 200 -or [int]$code -ge 300) { throw "curl GET ${Path} HTTP ${code}: $payload" }
    return $payload
  } finally {
    Remove-Item $outFile -ErrorAction SilentlyContinue
  }
}

function Assert-True([bool]$Cond, [string]$Msg) {
  if (-not $Cond) { throw $Msg }
}

Write-Host "== regen catalogsqlite seed =="
go run -tags with_controlplane ./cmd/gen-catalogsqlite
if ($LASTEXITCODE -ne 0) { throw "gen catalogsqlite failed" }

Write-Host "== host-build linux binary (controlplane tags) =="
New-Item -ItemType Directory -Force -Path dist | Out-Null
$tags = ((Get-Content build/tags.server.controlplane -Raw) -replace "`r|`n", "").Trim()
$env:CGO_ENABLED = "0"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -trimpath -ldflags="-s -w -checklinkname=0" -tags $tags -o dist/subserver-cp-linux ./cmd/subserver
$buildEc = $LASTEXITCODE
Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
if ($buildEc -ne 0) { throw "host go build failed" }
& "$PSScriptRoot\ensure_libcronet.ps1" -Arch amd64
if ($LASTEXITCODE -ne 0) { throw "ensure_libcronet failed" }

Write-Host "== compose up =="
$prevEap = $ErrorActionPreference
$ErrorActionPreference = "Continue"
docker compose -f $Compose down -v 2>&1 | ForEach-Object { Write-Host $_ }
docker compose -f $Compose up -d --build 2>&1 | ForEach-Object { Write-Host $_ }
$upEc = $LASTEXITCODE
$ErrorActionPreference = $prevEap
if ($upEc -ne 0) { throw "compose up failed ($upEc)" }

Write-Host "== wait health =="
$ok = $false
for ($i = 0; $i -lt 90; $i++) {
  $code = & $Curl -sk -o NUL -w "%{http_code}" "$Base/v1/health"
  if ($code -eq "200") { $ok = $true; break }
  Start-Sleep -Seconds 2
}
if (-not $ok) { throw "health timeout" }

Write-Host "== case: protocols include socks/http/mixed =="
$protos = Invoke-Json GET "/v1/controlplane/protocols?lang=en"
foreach ($want in @("socks", "http", "mixed")) {
  $p = @($protos.data) | Where-Object { $_.tag -eq $want } | Select-Object -First 1
  Assert-True ($null -ne $p) "$want protocol missing"
}

Write-Host "== case: user + sample sets =="
$user = Invoke-Json POST "/v1/controlplane/users" @{ name = "alice" }
$tok = $user.data.sub_token
Assert-True (-not [string]::IsNullOrEmpty($tok)) "no sub_token"

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "sk"; listen = "0.0.0.0"; listen_port = 11080
  presets = @("socks")
} | ConvertTo-Json -Compress -Depth 6) | Out-Null
Assert-True ((Invoke-Json POST "/v1/controlplane/sets/sk/activate").ok -eq $true) "socks activate"

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "ht"; listen = "0.0.0.0"; listen_port = 11081
  presets = @("http")
} | ConvertTo-Json -Compress -Depth 6) | Out-Null
Assert-True ((Invoke-Json POST "/v1/controlplane/sets/ht/activate").ok -eq $true) "http activate"

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "mx"; listen = "0.0.0.0"; listen_port = 11082
  presets = @("mixed")
} | ConvertTo-Json -Compress -Depth 6) | Out-Null
Assert-True ((Invoke-Json POST "/v1/controlplane/sets/mx/activate").ok -eq $true) "mixed activate"

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "htls"; listen = "0.0.0.0"; listen_port = 11443
  presets = @("http_tls")
} | ConvertTo-Json -Compress -Depth 6) | Out-Null
Assert-True ((Invoke-Json POST "/v1/controlplane/sets/htls/activate").ok -eq $true) "http_tls activate"

Write-Host "== case: subscription outbounds =="
$sub = (Invoke-Raw "/v1/sub/$tok") | ConvertFrom-Json
$tags = @($sub.outbounds | ForEach-Object { $_.tag })
Write-Host ("  outbound tags: " + ($tags -join ", "))
Assert-True ($tags.Count -ge 4) "expected socks/http/mixed/http_tls outbounds"

$sk = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-sk-*" }) | Select-Object -First 1
Assert-True ($sk.type -eq "socks") "socks type"
$ht = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-ht-*" }) | Select-Object -First 1
Assert-True ($ht.type -eq "http") "http type"
Assert-True ($null -eq $ht.PSObject.Properties['tls'] -or $null -eq $ht.tls) "plain http no tls"
$mx = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-mx-*" }) | Select-Object -First 1
Assert-True ($mx.type -eq "socks") "mixed_auth outbound socks"
$htls = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-htls-*" }) | Select-Object -First 1
Assert-True ($htls.tls.enabled -eq $true) "http_tls outbound tls"
Assert-True ($htls.tls.insecure -eq $true) "self_signed => insecure"

Write-Host "== case: inbound smoke for ready utility presets =="
$readyPresets = @(
  "socks", "socks_uot",
  "http", "http_tls", "http_tls_path",
  "mixed_auth", "mixed_tls"
)
$port = 12000
foreach ($preset in $readyPresets) {
  $setName = ("u-" + ($preset -replace "_", "-"))
  if ($setName.Length -gt 24) { $setName = $setName.Substring(0, 24) }
  Write-Host ("  activate+smoke $preset ($setName port=$port)")
  Invoke-Json POST "/v1/controlplane/sets" (@{
    name = $setName
    listen = "0.0.0.0"
    listen_port = $port
    presets = @($preset)
  } | ConvertTo-Json -Compress -Depth 6) | Out-Null
  $act = Invoke-Json POST "/v1/controlplane/sets/$setName/activate"
  Assert-True ($act.ok -eq $true) "activate $preset failed: $($act | ConvertTo-Json -Compress)"
  $smoke = Invoke-Json POST "/v1/controlplane/smoke" (@{
    sets = @($setName)
    presets = @($preset)
    timeout_ms = 3000
    include_variants = $false
    urls = @("http://1.1.1.1/")
  } | ConvertTo-Json -Compress -Depth 6)
  Assert-True ($smoke.ok -eq $true) "smoke envelope $preset"
  $probeable = @($smoke.data.results | Where-Object { -not $_.skipped })
  Assert-True ($probeable.Count -ge 1) "no probeable results for $preset"
  $okCount = @($probeable | Where-Object { $_.ok -eq $true }).Count
  if ($okCount -lt 1) {
    throw "smoke $preset failed: $($smoke | ConvertTo-Json -Compress -Depth 8)"
  }
  Write-Host ("    ok=$okCount / $($probeable.Count) duration_ms=$($smoke.data.duration_ms)")
  $port++
}

Write-Host "== OK socks/http/mixed subscription docker smoke =="
$ErrorActionPreference = "Continue"
docker compose -f $Compose down -v 2>&1 | ForEach-Object { Write-Host $_ }
