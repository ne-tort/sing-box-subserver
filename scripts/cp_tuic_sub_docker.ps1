# TUIC subscription smoke via Docker (catalogsqlite SoT).
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

Write-Host "== case: protocols include sqlite tuic =="
$protos = Invoke-Json GET "/v1/controlplane/protocols?lang=en"
$tuicProto = @($protos.data) | Where-Object { $_.tag -eq "tuic" } | Select-Object -First 1
Assert-True ($null -ne $tuicProto) "tuic protocol missing from API"
$presetTags = @($tuicProto.invariant_tags)
Assert-True ($presetTags -contains "tuic_custom") "missing tuic_custom"
Assert-True ($presetTags -contains "tuic") "missing tuic"
Assert-True ($presetTags -contains "tuic_0rtt") "missing tuic_0rtt"

Write-Host "== case: preset API exposes sqlite profiles =="
$preset = Invoke-Json GET "/v1/controlplane/presets/tuic?lang=en"
Assert-True ($preset.data.custom_preset -eq $true) "ready must expose custom schema"
$availP = @($preset.data.available_client_profiles | ForEach-Object { $_.name })
Assert-True ($availP -contains "udp-native") "missing udp-native"
Assert-True ($availP -contains "udp-quic") "missing udp-quic"

Write-Host "== case: user + multi TUIC sets =="
$user = Invoke-Json POST "/v1/controlplane/users" @{ name = "alice" }
$tok = $user.data.sub_token
Assert-True (-not [string]::IsNullOrEmpty($tok)) "no sub_token"

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "t1"; listen = "0.0.0.0"; listen_port = 10443
  bindings = @(
    @{
      preset = "tuic"
      subscription_tags = @("mobile")
      enabled_client_profiles = @("udp-native", "udp-quic")
    }
  )
} | ConvertTo-Json -Compress -Depth 12) | Out-Null
$act1 = Invoke-Json POST "/v1/controlplane/sets/t1/activate"
Assert-True ($act1.ok -eq $true) "t1 activate failed: $($act1 | ConvertTo-Json -Compress)"

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "t0"; listen = "0.0.0.0"; listen_port = 10444
  presets = @("tuic_0rtt")
} | ConvertTo-Json -Compress -Depth 8) | Out-Null
Invoke-Json POST "/v1/controlplane/sets/t0/activate" | Out-Null

Write-Host "== case: inspect applied config =="
$ErrorActionPreference = "Continue"
$cfgRaw = docker exec subserver-cp-smoke sh -c "grep -rl 'cp-in-t1-tuic' /var/lib/subserver 2>/dev/null | head -n 1 | xargs -r cat"
$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($cfgRaw)) {
  Write-Host "  WARN: applied config blob not found; relying on subscription + activate"
} else {
  $cfg = $cfgRaw | ConvertFrom-Json
  $inT1 = @($cfg.inbounds) | Where-Object { $_.tag -eq "cp-in-t1-tuic" } | Select-Object -First 1
  Assert-True ($null -ne $inT1) "missing inbound cp-in-t1-tuic"
  Assert-True ($inT1.type -eq "tuic") "inbound type"
  Assert-True ($inT1.congestion_control -eq "bbr") "congestion from param_values"
  Assert-True ($null -eq $inT1.PSObject.Properties['zero_rtt_handshake'] -or $null -eq $inT1.zero_rtt_handshake -or $inT1.zero_rtt_handshake -eq $false) "stock tuic must not enable 0-rtt"

  $in0 = @($cfg.inbounds) | Where-Object { $_.tag -eq "cp-in-t0-tuic_0rtt" } | Select-Object -First 1
  Assert-True ($null -ne $in0) "missing 0rtt inbound"
  Assert-True ($in0.zero_rtt_handshake -eq $true) "0rtt inbound flag"
}

Write-Host "== case: full subscription outbounds =="
$sub = (Invoke-Raw "/v1/sub/$tok") | ConvertFrom-Json
$tags = @($sub.outbounds | ForEach-Object { $_.tag })
Write-Host ("  outbound tags: " + ($tags -join ", "))
Assert-True ($tags.Count -ge 3) "expected multiple tuic outbounds, got $($tags.Count)"

$t1 = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-t1-tuic-*" })
Assert-True ($t1.Count -eq 2) "t1 outbounds=$($t1.Count) want 2"
$native = $t1 | Where-Object { $_.tag -like "*-udp-native" } | Select-Object -First 1
$quic = $t1 | Where-Object { $_.tag -like "*-udp-quic" } | Select-Object -First 1
Assert-True ($null -ne $native) "missing udp-native"
Assert-True ($native.udp_relay_mode -eq "native") "native mode"
Assert-True ($null -ne $quic) "missing udp-quic"
Assert-True ($quic.udp_relay_mode -eq "quic") "quic mode"

$t0 = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-t0-*" })
Assert-True ($t0.Count -ge 1) "0rtt outbounds missing"
Assert-True ($t0[0].zero_rtt_handshake -eq $true) "0rtt outbound flag"
Assert-True ($t0[0].tls.insecure -eq $true) "self_signed => insecure"

Write-Host "== case: subscription filters (profile/tag) =="
$filt = (Invoke-Raw "/v1/sub/${tok}?profile=udp-quic&tag=mobile") | ConvertFrom-Json
$fOut = @($filt.outbounds)
Assert-True ($fOut.Count -eq 1) "filtered outbounds=$($fOut.Count) want 1"
Assert-True ($fOut[0].udp_relay_mode -eq "quic") "filtered profile"

Write-Host "== case: strict_filters rejects opaque legacy profile names =="
Assert-HttpStatus GET "/v1/sub/${tok}?strict_filters=1&profile=ios" 400

Write-Host "== case: inbound smoke for ready TUIC =="
$readyPresets = @("tuic", "tuic_0rtt")
$port = 11000
foreach ($preset in $readyPresets) {
  $setName = ("s-" + ($preset -replace "_", "-"))
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

Write-Host "== OK tuic subscription docker smoke =="
$ErrorActionPreference = "Continue"
docker compose -f $Compose down -v 2>&1 | ForEach-Object { Write-Host $_ }
