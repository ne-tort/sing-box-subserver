# Copy glibc libcronet.so next to host-built linux agent binaries (naive smoke / purego).
# Usage: from repo root (vendor/sing-box-subserver): .\scripts\ensure_libcronet.ps1 [-Arch amd64]
param(
  [string]$Arch = "amd64",
  [string]$OutDir = "dist"
)
$ErrorActionPreference = "Stop"
$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $Root
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$mod = "github.com/sagernet/cronet-go/lib/linux_$Arch"
$dir = (go list -m -f "{{.Dir}}" $mod)
if (-not $dir) { throw "go list failed for $mod" }
$src = Join-Path $dir "libcronet.so"
if (-not (Test-Path $src)) { throw "missing $src" }
$dst = Join-Path $OutDir "libcronet.so"
Copy-Item -Force $src $dst
Write-Host "copied $dst"
