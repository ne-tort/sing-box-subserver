# Client-shaped DNS / route / outbounds against live CP agent (Docker).
# Uses curl.exe -k (Windows PowerShell 5.x cannot skip self-signed TLS reliably).
$ErrorActionPreference = "Stop"
$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $Root
$Token = "smoke-token-not-for-prod"
$Base = if ($env:BASE_URL) { $env:BASE_URL } else { "https://127.0.0.1:18081" }
$Compose = "docker-compose.cp-smoke.yml"
$Auth = "Authorization: Bearer $Token"

function Invoke-Curl([string[]]$ExtraArgs) {
  $out = & curl.exe -sk @ExtraArgs 2>&1
  $ec = $LASTEXITCODE
  if ($ec -ne 0) { throw "curl failed ($ec): $out" }
  return ($out | Out-String).Trim()
}

function Assert-Http([string]$Method, [string]$Path, [string]$Body = $null, [int]$Want = 200) {
  $tmp = [System.IO.Path]::GetTempFileName()
  try {
    $args = @("-X", $Method, "-o", $tmp, "-w", "%{http_code}", "-H", $Auth)
    if ($null -ne $Body) {
      $bodyFile = [System.IO.Path]::GetTempFileName()
      [System.IO.File]::WriteAllText($bodyFile, $Body, [System.Text.UTF8Encoding]::new($false))
      $args += @("-H", "Content-Type: application/json", "--data-binary", "@$bodyFile")
      $code = Invoke-Curl ($args + @("$Base$Path"))
      Remove-Item $bodyFile -Force -ErrorAction SilentlyContinue
    } else {
      $code = Invoke-Curl ($args + @("$Base$Path"))
    }
    $content = [System.IO.File]::ReadAllText($tmp)
    if ([int]$code -ne $Want) { throw "$Method $Path → $code $content" }
    return $content
  } finally {
    Remove-Item $tmp -Force -ErrorAction SilentlyContinue
  }
}

function Test-Healthy {
  try {
    $code = & curl.exe -sk -o NUL -w "%{http_code}" --max-time 2 "$Base/v1/health"
    return $code -eq "200"
  } catch { return $false }
}

$skipBuild = ($env:CP_SMOKE_SKIP_BUILD -eq "1") -or (Test-Healthy)
if (-not $skipBuild) {
  Write-Host "== host-build linux binary =="
  New-Item -ItemType Directory -Force -Path dist | Out-Null
  $tags = ((Get-Content build/tags.server.controlplane -Raw) -replace "`r|`n", "").Trim()
  $env:CGO_ENABLED = "0"
  $env:GOOS = "linux"
  $env:GOARCH = "amd64"
  go build -trimpath -ldflags="-s -w -checklinkname=0" -tags $tags -o dist/subserver-cp-linux ./cmd/subserver
  $ec = $LASTEXITCODE
  Remove-Item Env:GOOS -ErrorAction SilentlyContinue
  Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
  if ($ec -ne 0) { throw "go build failed" }
  & "$PSScriptRoot\ensure_libcronet.ps1" -Arch amd64
  if ($LASTEXITCODE -ne 0) { throw "ensure_libcronet failed" }

  Write-Host "== compose up =="
  $prevEa = $ErrorActionPreference
  $ErrorActionPreference = "Continue"
  docker compose -f $Compose down -v | Out-Null
  docker compose -f $Compose up -d --build
  $composeEc = $LASTEXITCODE
  $ErrorActionPreference = $prevEa
  if ($composeEc -ne 0) { throw "compose up failed ($composeEc)" }
}

Write-Host "== wait health =="
$ok = $false
for ($i = 0; $i -lt 120; $i++) {
  if (Test-Healthy) { $ok = $true; break }
  Start-Sleep -Seconds 2
}
if (-not $ok) { throw "health timeout" }

Write-Host "== client DNS PUT =="
$dns = @'
{"dns":{"independent_cache":false,"disable_expire":true,"final":"dns-remote","servers":[{"tag":"dns-local","type":"local"},{"tag":"dns-bootstrap-0","type":"udp","server":"1.1.1.1","domain_resolver":"dns-local"},{"tag":"dns-bootstrap","type":"group","servers":["dns-bootstrap-0"],"mode":"stable","error_ttl":"2m"},{"tag":"dns-remote-0","type":"local","domain_resolver":"dns-bootstrap","detour":"direct"},{"tag":"dns-remote","type":"group","servers":["dns-remote-0"],"mode":"stable","error_ttl":"2m"},{"tag":"dns-fake","type":"fakeip","inet4_range":"198.18.0.0/15","inet6_range":"fc00::/18"}],"rules":[{"query_type":["A","AAAA"],"server":"dns-fake","strategy":"prefer_ipv4","disable_cache":true},{"server":"dns-remote","strategy":"prefer_ipv4"}]}}
'@
Assert-Http PUT "/v1/controlplane/config/dns" $dns | Out-Null

Write-Host "== client Outbounds PUT =="
$out = @'
{"outbounds":[{"type":"selector","tag":"select","outbounds":["lowest","balance","Exit · a","Exit · b"],"default":"balance","interrupt_exist_connections":true},{"type":"balancer","tag":"lowest","outbounds":["Exit · a","Exit · b"],"strategy":"lowest-delay","interrupt_exist_connections":true},{"type":"balancer","tag":"balance","outbounds":["Exit · a","Exit · b"],"strategy":"round-robin","interrupt_exist_connections":true},{"type":"socks","tag":"Exit · a","server":"1.1.1.1","server_port":1080},{"type":"socks","tag":"Exit · b","server":"1.0.0.1","server_port":1080},{"type":"direct","tag":"direct"},{"type":"block","tag":"block"}]}
'@
Assert-Http PUT "/v1/controlplane/config/outbounds" $out | Out-Null

Write-Host "== client Route PUT + rulesets =="
$b64 = [Convert]::ToBase64String([byte[]](1, 2, 3, 4))
$route = '{"route":{"final":"balance","rules":[{"protocol":["dns"],"action":"hijack-dns"},{"ip_is_private":true,"outbound":"direct"},{"rule_set":["local-abc"],"outbound":"balance"},{"protocol":["quic"],"action":"reject"}],"rule_set":[{"type":"local","tag":"local-abc","format":"binary","path":"rp_test_abc.srs"}],"default_domain_resolver":{"server":"dns-bootstrap"}},"rulesets":[{"filename":"rp_test_abc.srs","content_base64":"' + $b64 + '"}]}'
$rr = Assert-Http PUT "/v1/controlplane/config/route" $route
$parsed = $rr | ConvertFrom-Json
if ([int]$parsed.data.rulesets_written -ne 1) {
  throw "rulesets_written=$($parsed.data.rulesets_written) body=$rr"
}

Write-Host "== soft-patch final=direct =="
$patch = @'
{"route":{"final":"direct","rules":[{"protocol":["dns"],"action":"hijack-dns"},{"ip_is_private":true,"outbound":"direct"},{"rule_set":["local-abc"],"outbound":"balance"},{"protocol":["quic"],"action":"reject"}],"rule_set":[{"type":"local","tag":"local-abc","format":"binary","path":"rp_test_abc.srs"}]}}
'@
Assert-Http PUT "/v1/controlplane/config/route" $patch | Out-Null

Write-Host "== DELETE outbounds/dns/route =="
Assert-Http DELETE "/v1/controlplane/config/outbounds" | Out-Null
Assert-Http DELETE "/v1/controlplane/config/dns" | Out-Null
Assert-Http DELETE "/v1/controlplane/config/route" | Out-Null

Write-Host "OK: client net fragments accepted by live agent"
