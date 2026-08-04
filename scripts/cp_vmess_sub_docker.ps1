# VMess subscription smoke via Docker (catalogsqlite SoT).
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

function Assert-HttpStatus([string]$Method, [string]$Path, [int]$Want) {
  $uri = "$Base$Path"
  $code = & $Curl -sk -o NUL -w "%{http_code}" -H "Authorization: Bearer $Token" -X $Method $uri
  if ([int]$code -ne $Want) { throw "expected HTTP $Want for $Method $Path, got $code" }
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

Write-Host "== case: protocols include sqlite vmess =="
$protos = Invoke-Json GET "/v1/controlplane/protocols?lang=en"
$vmessProto = @($protos.data) | Where-Object { $_.tag -eq "vmess" } | Select-Object -First 1
Assert-True ($null -ne $vmessProto) "vmess protocol missing from API"
$presetTags = @($vmessProto.invariant_tags)
Assert-True ($presetTags -contains "vmess_custom") "missing vmess_custom"
Assert-True ($presetTags -contains "vmess_ws_tls") "missing vmess_ws_tls"
Assert-True ($presetTags -contains "vmess_tls_mux") "missing vmess_tls_mux"

Write-Host "== case: preset API exposes sqlite profiles (no user_variants) =="
$preset = Invoke-Json GET "/v1/controlplane/presets/vmess_ws_tls?lang=en"
Assert-True ($preset.data.custom_preset -eq $true) "ready must expose custom schema"
$availV = @()
if ($null -ne $preset.data.PSObject.Properties['available_user_variants'] -and $null -ne $preset.data.available_user_variants) {
  $availV = @($preset.data.available_user_variants | ForEach-Object { $_.name } | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
}
Assert-True ($availV.Count -eq 0) "vmess must have empty user_variants, got: $($availV -join ',')"
$availP = @($preset.data.available_client_profiles | ForEach-Object { $_.name })
Assert-True ($availP -contains "sec-auto") "missing sec-auto"
Assert-True ($availP -contains "sec-aes128") "missing sec-aes128"
Assert-True ($availP -contains "sec-chacha") "missing sec-chacha"
Assert-True ($availP -contains "pkt-xudp") "missing pkt-xudp"

Write-Host "== case: user + multi VMess sets =="
$user = Invoke-Json POST "/v1/controlplane/users" @{ name = "alice" }
$tok = $user.data.sub_token
Assert-True (-not [string]::IsNullOrEmpty($tok)) "no sub_token"

Write-Host "== case: create+activate sets (materialize inbounds from sqlite) =="
Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "v1"; listen = "0.0.0.0"; listen_port = 10443
  bindings = @(
    @{
      preset = "vmess-tcp"
      subscription_tags = @("mobile")
      enabled_client_profiles = @("sec-auto", "sec-aes128", "pkt-xudp")
    }
  )
} | ConvertTo-Json -Compress -Depth 12) | Out-Null
$act1 = Invoke-Json POST "/v1/controlplane/sets/v1/activate"
Assert-True ($act1.ok -eq $true) "v1 activate failed: $($act1 | ConvertTo-Json -Compress)"

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "vws"; listen = "0.0.0.0"; listen_port = 10444
  presets = @("vmess_ws_tls")
} | ConvertTo-Json -Compress -Depth 8) | Out-Null
Invoke-Json POST "/v1/controlplane/sets/vws/activate" | Out-Null

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "vr"; listen = "0.0.0.0"; listen_port = 10445
  presets = @("vmess_reality")
} | ConvertTo-Json -Compress -Depth 8) | Out-Null
Invoke-Json POST "/v1/controlplane/sets/vr/activate" | Out-Null

Write-Host "== case: inspect applied config for inbound =="
$ErrorActionPreference = "Continue"
$cfgRaw = docker exec subserver-cp-smoke sh -c "grep -rl 'cp-in-v1-vmess-tcp' /var/lib/subserver 2>/dev/null | head -n 1 | xargs -r cat"
$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($cfgRaw)) {
  Write-Host "  WARN: applied config blob with inbound tag not found; relying on subscription + activate success"
} else {
  $cfg = $cfgRaw | ConvertFrom-Json
  $inV1 = @($cfg.inbounds) | Where-Object { $_.tag -eq "cp-in-v1-vmess-tcp" } | Select-Object -First 1
  Assert-True ($null -ne $inV1) "missing inbound cp-in-v1-vmess-tcp in applied config"
  Assert-True ($inV1.type -eq "vmess") "inbound type"
  Assert-True ($null -eq $inV1.PSObject.Properties['packet_encoding'] -or $null -eq $inV1.packet_encoding) "inbound must not carry packet_encoding"

  $inWs = @($cfg.inbounds) | Where-Object { $_.tag -eq "cp-in-vws-vmess_ws_tls" } | Select-Object -First 1
  Assert-True ($null -ne $inWs) "missing ws inbound"
  Assert-True ($inWs.transport.type -eq "ws") "ws transport from base+param_values"
  Assert-True ($inWs.transport.path -eq "/vmess") "ws path from ready param_values"
  Assert-True ($inWs.transport.max_early_data -eq 2048) "ws early data from param_values"
}

Write-Host "== case: full subscription outbounds =="
$sub = (Invoke-Raw "/v1/sub/$tok") | ConvertFrom-Json
$tags = @($sub.outbounds | ForEach-Object { $_.tag })
Write-Host ("  outbound tags: " + ($tags -join ", "))
Assert-True ($tags.Count -ge 4) "expected multiple vmess outbounds, got $($tags.Count)"

$v1 = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-v1-vmess-tcp-*" })
Assert-True ($v1.Count -eq 3) "v1 outbounds=$($v1.Count) want 3 (sec-auto, sec-aes128, pkt-xudp)"
$v1Aes = $v1 | Where-Object { $_.tag -like "*-sec-aes128" } | Select-Object -First 1
Assert-True ($null -ne $v1Aes) "missing sec-aes128 outbound"
Assert-True ($v1Aes.security -eq "aes-128-gcm") "sec-aes128 security"
$v1Xudp = $v1 | Where-Object { $_.tag -like "*-pkt-xudp" } | Select-Object -First 1
Assert-True ($null -ne $v1Xudp) "missing pkt-xudp outbound"
Assert-True ($v1Xudp.packet_encoding -eq "xudp") "pkt-xudp override"

$vws = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-vws-*" })
Assert-True ($vws.Count -eq 1) "ws outbounds=$($vws.Count) want 1"
Assert-True ($vws[0].type -eq "vmess") "ws outbound type"
Assert-True ($vws[0].transport.type -eq "ws") "ws outbound transport"
Assert-True ($vws[0].security -eq "auto") "ws default sec-auto"
Assert-True ($vws[0].tls.insecure -eq $true) "self_signed => insecure"

$vr = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-vr-*" })
Assert-True ($vr.Count -ge 1) "reality outbounds missing"
Assert-True ($null -ne $vr[0].tls.reality) "reality outbound missing tls.reality"

Write-Host "== case: subscription filters (profile/tag) =="
$filt = (Invoke-Raw "/v1/sub/${tok}?profile=sec-aes128&tag=mobile") | ConvertFrom-Json
$fOut = @($filt.outbounds)
Assert-True ($fOut.Count -eq 1) "filtered outbounds=$($fOut.Count) want 1"
Assert-True ($fOut[0].security -eq "aes-128-gcm") "filtered security"

$miss = (Invoke-Raw "/v1/sub/${tok}?profile=sec-chacha&set=v1") | ConvertFrom-Json
Assert-True (@($miss.outbounds).Count -eq 0) "profile mismatch must emit 0"

Write-Host "== case: strict_filters rejects opaque legacy profile names =="
Assert-HttpStatus GET "/v1/sub/${tok}?strict_filters=1&profile=ios" 400

Write-Host "== case: inbound smoke (ephemeral client-box) for all ready VMess =="
$readyPresets = @(
  "vmess_tcp", "vmess_tls", "vmess_reality", "vmess_tls_mux",
  "vmess_ws_tls", "vmess_ws_reality",
  "vmess_grpc_tls", "vmess_grpc_reality",
  "vmess_http_tls", "vmess_http_reality",
  "vmess_httpupgrade_tls", "vmess_httpupgrade_reality",
  "vmess_quic_tls"
)

$port = 11000
foreach ($preset in $readyPresets) {
  $setName = ("s-" + ($preset -replace "_", "-"))
  if ($setName.Length -gt 24) { $setName = $setName.Substring(0, 24) }
  Write-Host ("  activate+smoke $preset ($setName port=$port)")
  $body = @{
    name = $setName
    listen = "0.0.0.0"
    listen_port = $port
    presets = @($preset)
  }
  Invoke-Json POST "/v1/controlplane/sets" ($body | ConvertTo-Json -Compress -Depth 6) | Out-Null
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

Write-Host "== OK vmess subscription docker smoke =="
$ErrorActionPreference = "Continue"
docker compose -f $Compose down -v 2>&1 | ForEach-Object { Write-Host $_ }
