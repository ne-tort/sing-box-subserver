# Carrier (sing-box-lx with_carrier) subscription smoke via Docker (catalogsqlite SoT).
# Peer provider only — SFU ready presets need external room URLs.
# shadowquic/sudoku/trusttunnel: see cp_lx_sq_sudoku_tt_sub_docker.ps1.
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

Write-Host "== case: protocols include carrier =="
$protos = Invoke-Json GET "/v1/controlplane/protocols?lang=en"
$carrier = @($protos.data) | Where-Object { $_.tag -eq "carrier" } | Select-Object -First 1
Assert-True ($null -ne $carrier) "carrier protocol missing"

Write-Host "== case: catalog owns peer ready =="
$presets = Invoke-Json GET "/v1/controlplane/presets?protocol=carrier&lang=en"
$list = if ($presets.data -is [System.Array]) { @($presets.data) } elseif ($presets.data.presets) { @($presets.data.presets) } else { @($presets.data) }
foreach ($want in @("carrier_peer_shared", "carrier_peer_users", "carrier_custom")) {
  $hit = @($list) | Where-Object { $_.tag -eq $want -or $_.name -eq $want } | Select-Object -First 1
  Assert-True ($null -ne $hit) "preset $want missing from carrier catalog"
}
$peerMeta = Invoke-Json GET "/v1/controlplane/presets/carrier_peer_shared?lang=en"
Assert-True ($peerMeta.data.protocol -eq "carrier" -or $peerMeta.data.name -eq "carrier_peer_shared" -or $peerMeta.data.tag -eq "carrier_peer_shared") "carrier_peer_shared preset meta missing"

Write-Host "== case: user =="
$user = Invoke-Json POST "/v1/controlplane/users" @{ name = "alice" }
$tok = $user.data.sub_token
Assert-True (-not [string]::IsNullOrEmpty($tok)) "no sub_token"

Write-Host "== case: activate + subscription for carrier peer ready =="
$readyPresets = @("carrier_peer_shared", "carrier_peer_users")
$port = 14000
foreach ($preset in $readyPresets) {
  $setName = ("car-" + ($preset -replace "_", "-"))
  if ($setName.Length -gt 24) { $setName = $setName.Substring(0, 24) }
  Write-Host ("  activate $preset ($setName port=$port)")
  Invoke-Json POST "/v1/controlplane/sets" (@{
    name = $setName
    listen = "0.0.0.0"
    listen_port = $port
    presets = @($preset)
  } | ConvertTo-Json -Compress -Depth 6) | Out-Null
  $act = Invoke-Json POST "/v1/controlplane/sets/$setName/activate"
  Assert-True ($act.ok -eq $true) "activate $preset failed: $($act | ConvertTo-Json -Compress)"

  # Peer carrier is not hairpin-probeable like TCP proxies; smoke may skip.
  $smoke = Invoke-Json POST "/v1/controlplane/smoke" (@{
    sets = @($setName)
    presets = @($preset)
    timeout_ms = 4000
    include_variants = $false
    urls = @("http://1.1.1.1/")
  } | ConvertTo-Json -Compress -Depth 6)
  Assert-True ($smoke.ok -eq $true) "smoke envelope $preset"
  $probeable = @($smoke.data.results | Where-Object { -not $_.skipped })
  if ($probeable.Count -ge 1) {
    $okCount = @($probeable | Where-Object { $_.ok -eq $true }).Count
    if ($okCount -lt 1) {
      throw "smoke $preset failed: $($smoke | ConvertTo-Json -Compress -Depth 8)"
    }
    Write-Host ("    smoke ok=$okCount / $($probeable.Count)")
  } else {
    Write-Host ("    smoke skipped (expected for carrier peer underlay)")
  }

  Write-Host ("  sub $preset")
  $subRaw = & $Curl -sk "$Base/v1/sub/$tok"
  if ($LASTEXITCODE -ne 0) { throw "sub curl failed" }
  if ($subRaw -match "page not found" -or [string]::IsNullOrWhiteSpace($subRaw)) {
    throw ("sub empty/404 for {0}: {1}" -f $preset, $subRaw)
  }
  $sub = $subRaw | ConvertFrom-Json
  Assert-True ($sub.meta.matched -ge 1) ("sub matched=0 for {0}" -f $preset)
  $first = @($sub.outbounds)[0]
  Assert-True ($null -ne $first) ("no outbounds for {0}" -f $preset)
  $obType = [string]$first.PSObject.Properties['type'].Value
  if ([string]::IsNullOrEmpty($obType)) { $obType = [string]$first.type }
  Assert-True ($obType -eq "carrier") ("outbound type={0} for {1}" -f $obType, $preset)
  $link = $first.link
  Assert-True ($null -ne $link.peer -and "$($link.peer)" -ne "") "sub missing link.peer for $preset"
  $prov = [string]$first.PSObject.Properties['provider'].Value
  if ([string]::IsNullOrEmpty($prov)) { $prov = [string]$first.provider }
  Assert-True ($prov -eq "peer") ("provider={0}" -f $prov)
  Write-Host ("    peer=$($link.peer) provider=$prov type=$obType")
  $port++
}

Write-Host "== OK carrier peer subscription docker smoke =="
$ErrorActionPreference = "Continue"
docker compose -f $Compose down -v 2>&1 | ForEach-Object { Write-Host $_ }
