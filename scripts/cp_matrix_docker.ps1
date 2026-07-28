# Controlplane scenario matrix (self_signed only — no live ACME).
$ErrorActionPreference = "Stop"
$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $Root
$Token = "smoke-token-not-for-prod"
$Base = if ($env:BASE_URL) { $env:BASE_URL } else { "http://127.0.0.1:18081" }
$Compose = "docker-compose.cp-smoke.yml"
$headers = @{ Authorization = "Bearer $Token"; "Content-Type" = "application/json" }

function Invoke-Json([string]$Method, [string]$Path, $Body = $null) {
  $uri = "$Base$Path"
  if ($null -eq $Body) {
    return Invoke-RestMethod -Method $Method -Uri $uri -Headers @{ Authorization = "Bearer $Token" }
  }
  $raw = if ($Body -is [string]) { $Body } else { ($Body | ConvertTo-Json -Compress -Depth 8) }
  return Invoke-RestMethod -Method $Method -Uri $uri -Headers $headers -Body ([System.Text.Encoding]::UTF8.GetBytes($raw))
}

function Assert-HttpStatus([string]$Method, [string]$Path, [int]$Want, $Body = $null) {
  $uri = "$Base$Path"
  try {
    if ($null -eq $Body) {
      Invoke-WebRequest -Method $Method -Uri $uri -Headers @{ Authorization = "Bearer $Token" } -UseBasicParsing | Out-Null
    } else {
      $raw = if ($Body -is [string]) { $Body } else { ($Body | ConvertTo-Json -Compress -Depth 8) }
      Invoke-WebRequest -Method $Method -Uri $uri -Headers $headers -Body ([System.Text.Encoding]::UTF8.GetBytes($raw)) -UseBasicParsing | Out-Null
    }
    if ($Want -ne 200) { throw "expected HTTP $Want, got 200" }
  } catch {
    $code = 0
    if ($_.Exception.Response) { $code = [int]$_.Exception.Response.StatusCode }
    if ($code -ne $Want) { throw "expected HTTP $Want for $Method $Path, got $code ($_)" }
  }
}

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

Write-Host "== compose up (runtime image + prebuilt binary) =="
docker compose -f $Compose down -v 2>$null | Out-Null
docker compose -f $Compose up -d --build
if ($LASTEXITCODE -ne 0) { throw "compose up failed" }

Write-Host "== wait health =="
$ok = $false
for ($i = 0; $i -lt 120; $i++) {
  try {
    $r = Invoke-WebRequest -Uri "$Base/v1/health" -UseBasicParsing -TimeoutSec 2
    if ($r.StatusCode -eq 200) { $ok = $true; break }
  } catch { Start-Sleep -Seconds 2 }
}
if (-not $ok) { throw "health timeout" }

Write-Host "== case: default TLS self_signed + IP SAN =="
$tls = Invoke-Json GET "/v1/controlplane/tls"
if ($tls.data.mode -ne "self_signed") { throw "mode=$($tls.data.mode)" }
$ipSans = @($tls.data.self_signed.ip_sans)
if ($ipSans -notcontains "203.0.113.10") { throw "ip_sans missing public_host" }
if ($tls.data.material_status.self_signed_cert_present -ne $true) { throw "cert missing after bootstrap" }
if ($tls.data.material_status.active_material -ne "self_signed_pem") { throw "active_material" }

Write-Host "== case: validate rejects (no live ACME) =="
Assert-HttpStatus PUT "/v1/controlplane/tls" 400 @{
  mode = "acme_ip"
  acme = @{ email = "a@b.c"; domains = @("203.0.113.10"); provider = "zerossl" }
}
Assert-HttpStatus PUT "/v1/controlplane/tls" 400 @{
  mode = "acme_ip"
  acme = @{
    email = "a@b.c"; domains = @("203.0.113.10"); provider = "letsencrypt"
    dns01_challenge = @{ provider = "cloudflare"; api_token = "x" }
  }
}

Write-Host "== case: user + shadowsocks set =="
$user = Invoke-Json POST "/v1/controlplane/users" @{ name = "alice" }
$tok = $user.data.sub_token
if (-not $tok) { throw "no sub_token" }
Invoke-Json POST "/v1/controlplane/sets" @{
  name = "ss1"; listen = "0.0.0.0"; listen_port = 1080; presets = @("shadowsocks-tcp")
} | Out-Null
$act = Invoke-Json POST "/v1/controlplane/sets/ss1/activate"
if ($act.data.config_mode -ne "controlplane") { throw "config_mode=$($act.data.config_mode)" }

Write-Host "== case: trojan-tcp + PEM paths in live config =="
Invoke-Json POST "/v1/controlplane/sets" @{
  name = "tr1"; listen = "0.0.0.0"; listen_port = 8443; presets = @("trojan-tcp")
} | Out-Null
Invoke-Json POST "/v1/controlplane/sets/tr1/activate" | Out-Null
$cfgResp = Invoke-WebRequest -Uri "$Base/v1/config" -Headers @{ Authorization = "Bearer $Token" } -UseBasicParsing
$cfgText = $cfgResp.Content
if ($cfgText -notmatch "certificate_path") { throw "config missing certificate_path" }
if ($cfgText -match "certificate_provider") { throw "self_signed config must not use certificate_provider" }
if ($cfgText -notmatch "controlplane/tls/server") { throw "unexpected cert path" }

Write-Host "== case: subscription insecure for self_signed =="
$sub = Invoke-RestMethod -Uri "$Base/v1/sub/$tok" -UseBasicParsing
$foundInsecure = $false
foreach ($ob in $sub.outbounds) {
  if ($ob.type -eq "trojan" -and $ob.tls.insecure -eq $true) { $foundInsecure = $true }
}
if (-not $foundInsecure) { throw "trojan outbound missing tls.insecure" }

Write-Host "== case: TLS handshake to trojan port (host) =="
# Give box a moment after activate Apply returns.
Start-Sleep -Seconds 1
$tcp = New-Object System.Net.Sockets.TcpClient
$tcp.ReceiveTimeout = 5000
$tcp.SendTimeout = 5000
$tcp.Connect("127.0.0.1", 18443)
$ssl = New-Object System.Net.Security.SslStream($tcp.GetStream(), $false, { param($s,$c,$ch,$e) $true })
$ssl.AuthenticateAsClient("203.0.113.10")
if (-not $ssl.IsAuthenticated) { throw "TLS not authenticated" }
$cert = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2($ssl.RemoteCertificate)
$thumb1 = $cert.Thumbprint
$ssl.Close(); $tcp.Close()

Write-Host "== case: regenerate reloads PEM =="
$fp1 = (docker exec subserver-cp-smoke sha256sum /var/lib/subserver/controlplane/tls/server.crt).Trim()
$status1 = Invoke-Json GET "/v1/status"
$rev1 = [int64]$status1.data.revision
Invoke-Json POST "/v1/controlplane/tls/regenerate" | Out-Null
$fp2 = (docker exec subserver-cp-smoke sha256sum /var/lib/subserver/controlplane/tls/server.crt).Trim()
if ($fp1 -eq $fp2) { throw "cert fingerprint unchanged after regenerate" }
$status2 = Invoke-Json GET "/v1/status"
$rev2 = [int64]$status2.data.revision
if ($rev2 -le $rev1) { throw "revision did not advance after Force reload ($rev1 -> $rev2)" }
$tcp2 = New-Object System.Net.Sockets.TcpClient
$tcp2.Connect("127.0.0.1", 18443)
$ssl2 = New-Object System.Net.Security.SslStream($tcp2.GetStream(), $false, { param($s,$c,$ch,$e) $true })
$ssl2.AuthenticateAsClient("203.0.113.10")
$cert2 = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2($ssl2.RemoteCertificate)
if ($cert2.Thumbprint -eq $thumb1) { throw "runtime still serves old cert after regenerate" }
$ssl2.Close(); $tcp2.Close()

Write-Host "== OK controlplane matrix =="
docker compose -f $Compose down -v
