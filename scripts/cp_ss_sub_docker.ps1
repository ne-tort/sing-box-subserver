# Shadowsocks subscription smoke via Docker (catalogsqlite SoT).
# Uses curl.exe -k for management TLS (PowerShell SslStream is unreliable with self-signed).
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

Write-Host "== case: protocols include sqlite shadowsocks =="
$protos = Invoke-Json GET "/v1/controlplane/protocols?lang=en"
$ssProto = @($protos.data) | Where-Object { $_.tag -eq "shadowsocks" } | Select-Object -First 1
Assert-True ($null -ne $ssProto) "shadowsocks protocol missing from API"
$presetTags = @($ssProto.invariant_tags)
Assert-True ($presetTags -contains "shadowsocks_custom") "missing shadowsocks_custom"
Assert-True ($presetTags -contains "ss_aes128") "missing ss_aes128"
Assert-True ($presetTags -contains "ss_2022_aes128") "missing ss_2022_aes128"

Write-Host "== case: preset API exposes full constructor schema =="
$preset = Invoke-Json GET "/v1/controlplane/presets/ss_aes128?lang=en"
Assert-True ($preset.data.custom_preset -eq $true) "ready must expose custom schema"
Assert-True ($preset.data.params_schema.method -ne $null) "method schema missing"

Write-Host "== case: user + SS sets =="
$user = Invoke-Json POST "/v1/controlplane/users" @{ name = "alice" }
$tok = $user.data.sub_token
Assert-True (-not [string]::IsNullOrEmpty($tok)) "no sub_token"

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "s1"; listen = "0.0.0.0"; listen_port = 18443
  bindings = @(@{ preset = "shadowsocks-tcp"; subscription_tags = @("mobile") })
} | ConvertTo-Json -Compress -Depth 12) | Out-Null
$act1 = Invoke-Json POST "/v1/controlplane/sets/s1/activate"
Assert-True ($act1.ok -eq $true) "s1 activate failed: $($act1 | ConvertTo-Json -Compress)"

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "s2022"; listen = "0.0.0.0"; listen_port = 18444
  presets = @("ss_2022_aes128")
} | ConvertTo-Json -Compress -Depth 8) | Out-Null
Invoke-Json POST "/v1/controlplane/sets/s2022/activate" | Out-Null

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "smux"; listen = "0.0.0.0"; listen_port = 18445
  presets = @("ss_aes128_mux")
} | ConvertTo-Json -Compress -Depth 8) | Out-Null
Invoke-Json POST "/v1/controlplane/sets/smux/activate" | Out-Null

Write-Host "== case: inspect applied config =="
$ErrorActionPreference = "Continue"
$cfgRaw = docker exec subserver-cp-smoke sh -c "grep -rl 'cp-in-s1-shadowsocks-tcp' /var/lib/subserver 2>/dev/null | head -n 1 | xargs -r cat"
$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($cfgRaw)) {
  Write-Host "  WARN: applied config blob not found; relying on subscription + activate"
} else {
  $cfg = $cfgRaw | ConvertFrom-Json
  $inS1 = @($cfg.inbounds) | Where-Object { $_.tag -eq "cp-in-s1-shadowsocks-tcp" } | Select-Object -First 1
  Assert-True ($null -ne $inS1) "missing inbound cp-in-s1-shadowsocks-tcp"
  Assert-True ($inS1.type -eq "shadowsocks") "inbound type"
  Assert-True ($inS1.method -eq "aes-128-gcm") "aes128 method"
  Assert-True ($null -eq $inS1.PSObject.Properties['password'] -or [string]::IsNullOrEmpty([string]$inS1.password)) "classic AEAD must not keep peer password"

  $in2022 = @($cfg.inbounds) | Where-Object { $_.tag -eq "cp-in-s2022-ss_2022_aes128" } | Select-Object -First 1
  Assert-True ($null -ne $in2022) "missing ss2022 inbound"
  Assert-True ($in2022.method -eq "2022-blake3-aes-128-gcm") "ss2022 method"
  Assert-True (-not [string]::IsNullOrEmpty([string]$in2022.password)) "ss2022 needs peer password"

  $inMux = @($cfg.inbounds) | Where-Object { $_.tag -eq "cp-in-smux-ss_aes128_mux" } | Select-Object -First 1
  Assert-True ($null -ne $inMux) "missing mux inbound"
  Assert-True ($inMux.multiplex.enabled -eq $true) "mux enabled on inbound"
}

Write-Host "== case: subscription outbounds =="
$sub = (Invoke-Raw "/v1/sub/$tok") | ConvertFrom-Json
$tags = @($sub.outbounds | ForEach-Object { $_.tag })
Write-Host ("  outbound tags: " + ($tags -join ", "))
Assert-True ($tags.Count -ge 3) "expected multiple SS outbounds"

$o1 = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-s1-*" }) | Select-Object -First 1
Assert-True ($null -ne $o1) "missing s1 outbound"
Assert-True ($o1.type -eq "shadowsocks") "s1 type"
Assert-True ($o1.method -eq "aes-128-gcm") "s1 method"

$o2022 = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-s2022-*" }) | Select-Object -First 1
Assert-True ($null -ne $o2022) "missing s2022 outbound"
Assert-True ($o2022.password -like "*:*") "SIP022 combine peer:user"

$omux = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-smux-*" }) | Select-Object -First 1
Assert-True ($omux.multiplex.enabled -eq $true) "sub mux"

Write-Host "== case: inbound smoke for ready Shadowsocks =="
$readyPresets = @(
  "ss_aes128", "ss_aes128_mux", "ss_aes128_uot",
  "ss_aes256", "ss_chacha20",
  "ss_2022_aes128", "ss_2022_aes128_mux", "ss_2022_aes256", "ss_2022_chacha"
)
$port = 11000
foreach ($preset in $readyPresets) {
  $setName = ("s-" + ($preset -replace "_", "-"))
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

Write-Host "== OK shadowsocks subscription docker smoke =="
$ErrorActionPreference = "Continue"
docker compose -f $Compose down -v 2>&1 | ForEach-Object { Write-Host $_ }
