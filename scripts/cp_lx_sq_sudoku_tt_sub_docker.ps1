# LX shadowquic / sudoku / trusttunnel subscription smoke via Docker (catalogsqlite SoT).
# Requires with_shadowquic,with_sudoku,with_trusttunnel in build/tags.server.controlplane.
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

Write-Host "== host-build linux binary (controlplane + LX tags) =="
New-Item -ItemType Directory -Force -Path dist | Out-Null
$tags = ((Get-Content build/tags.server.controlplane -Raw) -replace "`r|`n", "").Trim()
Assert-True ($tags -match "with_shadowquic") "tags missing with_shadowquic"
Assert-True ($tags -match "with_sudoku") "tags missing with_sudoku"
Assert-True ($tags -match "with_trusttunnel") "tags missing with_trusttunnel"
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

Write-Host "== case: protocols include LX SQ/sudoku/TT =="
$protos = Invoke-Json GET "/v1/controlplane/protocols?lang=en"
foreach ($want in @("shadowquic", "sudoku", "trusttunnel")) {
  $p = @($protos.data) | Where-Object { $_.tag -eq $want } | Select-Object -First 1
  Assert-True ($null -ne $p) "$want protocol missing"
}

Write-Host "== case: user =="
$user = Invoke-Json POST "/v1/controlplane/users" @{ name = "alice" }
$tok = $user.data.sub_token
Assert-True (-not [string]::IsNullOrEmpty($tok)) "no sub_token"

Write-Host "== case: activate+smoke LX ready =="
$readyPresets = @(
  "shadowquic_jls", "shadowquic_uot", "shadowquic_0rtt",
  "sudoku_pad", "sudoku_aes", "sudoku_httpmask",
  "trusttunnel_h2", "trusttunnel_h3", "trusttunnel_auto"
)
$port = 15000
foreach ($preset in $readyPresets) {
  $setName = ("lx2-" + ($preset -replace "_", "-"))
  if ($setName.Length -gt 24) { $setName = $setName.Substring(0, 24) }
  Write-Host ("  activate+smoke $preset ($setName port=$port)")
  Invoke-Json POST "/v1/controlplane/sets" (@{
    name = $setName
    listen = "0.0.0.0"
    listen_port = $port
    presets = @($preset)
  } | ConvertTo-Json -Compress -Depth 6) | Out-Null
  $act = Invoke-Json POST "/v1/controlplane/sets/$setName/activate"
  Assert-True ($act.ok -eq $true) ("activate {0} failed: {1}" -f $preset, ($act | ConvertTo-Json -Compress))

  $smoke = Invoke-Json POST "/v1/controlplane/smoke" (@{
    sets = @($setName)
    presets = @($preset)
    timeout_ms = 6000
    include_variants = $false
    urls = @("http://1.1.1.1/")
  } | ConvertTo-Json -Compress -Depth 6)
  Assert-True ($smoke.ok -eq $true) "smoke envelope $preset"
  $probeable = @($smoke.data.results | Where-Object { -not $_.skipped })
  Assert-True ($probeable.Count -ge 1) "no probeable results for $preset"
  $okCount = @($probeable | Where-Object { $_.ok -eq $true }).Count
  if ($okCount -lt 1) {
    throw ("smoke {0} failed: {1}" -f $preset, ($smoke | ConvertTo-Json -Compress -Depth 8))
  }
  Write-Host ("    ok=$okCount / $($probeable.Count) duration_ms=$($smoke.data.duration_ms)")

  $subRaw = & $Curl -sk "$Base/v1/sub/$tok"
  if ($LASTEXITCODE -ne 0) { throw "sub curl failed" }
  $sub = $subRaw | ConvertFrom-Json
  Assert-True ($sub.meta.matched -ge 1) ("sub matched=0 for {0}" -f $preset)
  $first = @($sub.outbounds)[0]
  $obType = [string]$first.PSObject.Properties['type'].Value
  if ([string]::IsNullOrEmpty($obType)) { $obType = [string]$first.type }
  $wantType = switch -Regex ($preset) {
    '^shadowquic' { 'shadowquic' }
    '^sudoku' { 'sudoku' }
    '^trusttunnel' { 'trusttunnel' }
    default { '' }
  }
  # After many activates, first outbound may be from an earlier set — find matching type.
  $hit = $null
  foreach ($ob in @($sub.outbounds)) {
    $t = [string]$ob.PSObject.Properties['type'].Value
    if ([string]::IsNullOrEmpty($t)) { $t = [string]$ob.type }
    if ($t -eq $wantType) { $hit = $ob; break }
  }
  Assert-True ($null -ne $hit) ("no {0} outbound in sub for {1}" -f $wantType, $preset)
  Write-Host ("    sub type=$wantType tag=$($hit.tag)")
  $port++
}

Write-Host "== OK LX SQ/sudoku/TT subscription docker smoke =="
$ErrorActionPreference = "Continue"
docker compose -f $Compose down -v 2>&1 | ForEach-Object { Write-Host $_ }
