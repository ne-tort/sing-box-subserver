# Hysteria2 subscription smoke via Docker (catalogsqlite SoT).
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

Write-Host "== case: protocols include sqlite hysteria2 =="
$protos = Invoke-Json GET "/v1/controlplane/protocols?lang=en"
$hy2Proto = @($protos.data) | Where-Object { $_.tag -eq "hysteria2" } | Select-Object -First 1
Assert-True ($null -ne $hy2Proto) "hysteria2 protocol missing"
$presetTags = @($hy2Proto.invariant_tags)
Assert-True ($presetTags -contains "hy2_custom") "missing hy2_custom"
Assert-True ($presetTags -contains "hy2") "missing hy2"
Assert-True ($presetTags -contains "hy2_salamander") "missing hy2_salamander"

Write-Host "== case: preset API exposes full constructor schema =="
$preset = Invoke-Json GET "/v1/controlplane/presets/hy2?lang=en"
Assert-True ($preset.data.custom_preset -eq $true) "ready must expose custom schema"

Write-Host "== case: user + hy2 sets =="
$user = Invoke-Json POST "/v1/controlplane/users" @{ name = "alice" }
$tok = $user.data.sub_token
Assert-True (-not [string]::IsNullOrEmpty($tok)) "no sub_token"

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "h1"; listen = "0.0.0.0"; listen_port = 10443
  presets = @("hy2")
} | ConvertTo-Json -Compress -Depth 8) | Out-Null
$act1 = Invoke-Json POST "/v1/controlplane/sets/h1/activate"
Assert-True ($act1.ok -eq $true) "h1 activate failed: $($act1 | ConvertTo-Json -Compress)"

Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "hs"; listen = "0.0.0.0"; listen_port = 10444
  presets = @("hy2_salamander")
} | ConvertTo-Json -Compress -Depth 8) | Out-Null
Invoke-Json POST "/v1/controlplane/sets/hs/activate" | Out-Null

Write-Host "== case: inspect applied config =="
$ErrorActionPreference = "Continue"
$cfgRaw = docker exec subserver-cp-smoke sh -c "grep -rl 'cp-in-h1-hy2' /var/lib/subserver 2>/dev/null | head -n 1 | xargs -r cat"
$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($cfgRaw)) {
  Write-Host "  WARN: applied config blob not found"
} else {
  $cfg = $cfgRaw | ConvertFrom-Json
  $inH1 = @($cfg.inbounds) | Where-Object { $_.tag -eq "cp-in-h1-hy2" } | Select-Object -First 1
  Assert-True ($null -ne $inH1) "missing inbound cp-in-h1-hy2"
  Assert-True ($inH1.type -eq "hysteria2") "inbound type"
  Assert-True ($null -eq $inH1.PSObject.Properties['obfs'] -or $null -eq $inH1.obfs) "plain hy2 must not have obfs"
  $inS = @($cfg.inbounds) | Where-Object { $_.tag -eq "cp-in-hs-hy2_salamander" } | Select-Object -First 1
  Assert-True ($null -ne $inS) "missing salamander inbound"
  Assert-True ($inS.obfs.type -eq "salamander") "salamander obfs"
}

Write-Host "== case: subscription outbounds =="
$sub = (Invoke-Raw "/v1/sub/$tok") | ConvertFrom-Json
$tags = @($sub.outbounds | ForEach-Object { $_.tag })
Write-Host ("  outbound tags: " + ($tags -join ", "))
Assert-True ($tags.Count -ge 2) "expected hy2 outbounds"
$h1 = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-h1-*" }) | Select-Object -First 1
Assert-True ($null -ne $h1) "missing h1 outbound"
Assert-True ($h1.type -eq "hysteria2") "h1 type"
Assert-True ($h1.tls.insecure -eq $true) "self_signed => insecure"
$hs = @($sub.outbounds | Where-Object { $_.tag -like "cp-out-hs-*" }) | Select-Object -First 1
Assert-True ($hs.obfs.type -eq "salamander") "sub salamander obfs"

Write-Host "== case: inbound smoke for ready Hy2 (except realm) =="
$readyPresets = @(
  "hy2", "hy2_salamander", "hy2_gecko", "hy2_gecko_compact",
  "hy2_masquerade", "hy2_gecko_masquerade", "hy2_masquerade_proxy"
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

Write-Host "== case: file masquerade with param =="
Invoke-Json POST "/v1/controlplane/sets" (@{
  name = "s-hy2-file"
  listen = "0.0.0.0"
  listen_port = $port
  bindings = @(@{
    preset = "hy2_masquerade_file"
    params = @{ masquerade_dir = "/tmp" }
  })
} | ConvertTo-Json -Compress -Depth 8) | Out-Null
$actF = Invoke-Json POST "/v1/controlplane/sets/s-hy2-file/activate"
Assert-True ($actF.ok -eq $true) "file masquerade activate failed"
$smokeF = Invoke-Json POST "/v1/controlplane/smoke" (@{
  sets = @("s-hy2-file")
  presets = @("hy2_masquerade_file")
  timeout_ms = 3000
  include_variants = $false
  urls = @("http://1.1.1.1/")
} | ConvertTo-Json -Compress -Depth 6)
Assert-True ($smokeF.ok -eq $true) "smoke file envelope"
$okF = @($smokeF.data.results | Where-Object { -not $_.skipped -and $_.ok -eq $true }).Count
Assert-True ($okF -ge 1) "smoke file failed: $($smokeF | ConvertTo-Json -Compress -Depth 8)"
Write-Host ("    ok file masquerade")

Write-Host "== OK hy2 subscription docker smoke =="
$ErrorActionPreference = "Continue"
docker compose -f $Compose down -v 2>&1 | ForEach-Object { Write-Host $_ }
