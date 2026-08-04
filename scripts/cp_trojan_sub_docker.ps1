# Trojan subscription smoke via Docker (catalogsqlite SoT).
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

Write-Host "== case: protocols include sqlite trojan =="
$protos = Invoke-Json GET "/v1/controlplane/protocols?lang=en"
$trojanProto = @($protos.data) | Where-Object { $_.tag -eq "trojan" } | Select-Object -First 1
Assert-True ($null -ne $trojanProto) "trojan protocol missing from API"
$presetTags = @($trojanProto.invariant_tags)
Assert-True ($presetTags -contains "trojan_custom") "missing trojan_custom"
Assert-True ($presetTags -contains "trojan_tls") "missing trojan_tls"
Assert-True ($presetTags -contains "trojan_tls_fallback") "missing trojan_tls_fallback"

Write-Host "== case: preset API exposes empty variants/profiles + fallback schema =="
$preset = Invoke-Json GET "/v1/controlplane/presets/trojan_tls?lang=en"
Assert-True ($preset.data.custom_preset -eq $true) "ready must expose custom schema"
$availV = @()
if ($null -ne $preset.data.PSObject.Properties['available_user_variants'] -and $null -ne $preset.data.available_user_variants) {
  $availV = @($preset.data.available_user_variants | ForEach-Object { $_.name } | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
}
Assert-True ($availV.Count -eq 0) "trojan must have empty user_variants, got: $($availV -join ',')"
$availP = @()
if ($null -ne $preset.data.PSObject.Properties['available_client_profiles'] -and $null -ne $preset.data.available_client_profiles) {
  $availP = @($preset.data.available_client_profiles | ForEach-Object { $_.name } | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
}
Assert-True ($availP.Count -eq 0) "trojan must have empty client_profiles, got: $($availP -join ',')"

Write-Host "== case: user + multi Trojan sets =="
$user = Invoke-Json POST "/v1/controlplane/users" @{ name = "alice" }
$tok = $user.data.sub_token
Assert-True (-not [string]::IsNullOrEmpty($tok)) "no sub_token"

Write-Host "== case: create+activate sets (materialize inbounds from sqlite) =="
Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "t1"; listen = "0.0.0.0"; listen_port = 10443
  bindings = @(
    @{
      preset = "trojan-tcp"
      subscription_tags = @("mobile")
    }
  )
} | ConvertTo-Json -Compress -Depth 12) | Out-Null
$act1 = Invoke-Json POST "/v1/controlplane/sets/t1/activate"
Assert-True ($act1.ok -eq $true) "t1 activate failed: $($act1 | ConvertTo-Json -Compress)"

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "tws"; listen = "0.0.0.0"; listen_port = 10444
  presets = @("trojan_ws_tls")
} | ConvertTo-Json -Compress -Depth 8) | Out-Null
Invoke-Json POST "/v1/controlplane/sets/tws/activate" | Out-Null

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "tr"; listen = "0.0.0.0"; listen_port = 10445
  presets = @("trojan_reality")
} | ConvertTo-Json -Compress -Depth 8) | Out-Null
Invoke-Json POST "/v1/controlplane/sets/tr/activate" | Out-Null

Write-Host "== case: inspect applied config for inbound =="
$ErrorActionPreference = "Continue"
$cfgRaw = docker exec subserver-cp-smoke sh -c "grep -rl 'cp-in-t1-trojan-tcp' /var/lib/subserver 2>/dev/null | head -n 1 | xargs -r cat"
$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($cfgRaw)) {
  Write-Host "  WARN: applied config blob with inbound tag not found; relying on subscription + activate success"
} else {
  $cfg = $cfgRaw | ConvertFrom-Json
  $inT1 = @($cfg.inbounds) | Where-Object { $_.tag -eq "cp-in-t1-trojan-tcp" } | Select-Object -First 1
  Assert-True ($null -ne $inT1) "missing inbound cp-in-t1-trojan-tcp in applied config"
  Assert-True ($inT1.type -eq "trojan") "inbound type"

  $inWs = @($cfg.inbounds) | Where-Object { $_.tag -eq "cp-in-tws-trojan_ws_tls" } | Select-Object -First 1
  Assert-True ($null -ne $inWs) "missing ws inbound"
  Assert-True ($inWs.transport.type -eq "ws") "ws transport from base+param_values"
  Assert-True ($inWs.transport.path -eq "/trojanws") "ws path from ready param_values"
  Assert-True ($inWs.transport.max_early_data -eq 2048) "ws early data from param_values"
}

Write-Host "== case: full subscription outbounds =="
$sub = (Invoke-Raw "/v1/sub/$tok") | ConvertFrom-Json
$tags = @($sub.outbounds | ForEach-Object { $_.tag })
Write-Host ("  outbound tags: " + ($tags -join ", "))
Assert-True ($tags.Count -ge 3) "expected multiple trojan outbounds, got $($tags.Count)"

$t1 = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-t1-*" })
Assert-True ($t1.Count -eq 1) "t1 outbounds=$($t1.Count) want 1"
Assert-True ($t1[0].type -eq "trojan") "t1 outbound type"

$tws = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-tws-*" })
Assert-True ($tws.Count -eq 1) "ws outbounds=$($tws.Count) want 1"
Assert-True ($tws[0].transport.type -eq "ws") "ws outbound transport"
Assert-True ($tws[0].tls.insecure -eq $true) "self_signed => insecure"

$tr = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-tr-*" })
Assert-True ($tr.Count -ge 1) "reality outbounds missing"
Assert-True ($null -ne $tr[0].tls.reality) "reality outbound missing tls.reality"

Write-Host "== case: subscription filters (tag) =="
$filt = (Invoke-Raw "/v1/sub/${tok}?tag=mobile") | ConvertFrom-Json
$fOut = @($filt.outbounds)
Assert-True ($fOut.Count -eq 1) "filtered outbounds=$($fOut.Count) want 1"

Write-Host "== case: strict_filters rejects opaque legacy profile names =="
Assert-HttpStatus GET "/v1/sub/${tok}?strict_filters=1&profile=ios" 400

Write-Host "== case: inbound smoke (ephemeral client-box) for all ready Trojan =="
$readyPresets = @(
  "trojan_tls", "trojan_reality", "trojan_tls_mux", "trojan_tls_fallback",
  "trojan_ws_tls", "trojan_ws_reality",
  "trojan_grpc_tls", "trojan_grpc_reality",
  "trojan_http_tls", "trojan_http_reality",
  "trojan_httpupgrade_tls", "trojan_httpupgrade_reality",
  "trojan_quic_tls"
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

Write-Host "== OK trojan subscription docker smoke =="
$ErrorActionPreference = "Continue"
docker compose -f $Compose down -v 2>&1 | ForEach-Object { Write-Host $_ }
