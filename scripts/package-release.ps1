param(
  [ValidateSet("windows", "linux", "darwin")]
  [string]$TargetOS = $env:GOOS,
  [string]$TargetArch = $(if ($env:GOARCH) { $env:GOARCH } else { "amd64" }),
  [string]$OutDir = "dist"
)

if (-not $TargetOS) { $TargetOS = "windows" }
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$packageName = "vivurouter-$TargetOS-$TargetArch"
$packageDir = Join-Path $OutDir $packageName
New-Item -ItemType Directory -Force $packageDir | Out-Null

$exeName = if ($TargetOS -eq "windows") { "vivurouter.exe" } else { "vivurouter" }
$rtkName = if ($TargetOS -eq "windows") { "rtk.exe" } else { "rtk" }

$env:GOOS = $TargetOS
$env:GOARCH = $TargetArch
go build -o (Join-Path $packageDir $exeName) ./cmd/vivurouter-go

$rtkSource = Join-Path $root $rtkName
if (Test-Path $rtkSource) {
  Copy-Item $rtkSource (Join-Path $packageDir $rtkName) -Force
} else {
  Write-Warning "RTK binary '$rtkName' not found at project root. Package will rely on PATH or user-provided RTK path."
}

Copy-Item (Join-Path $root "README.md") $packageDir -Force -ErrorAction SilentlyContinue
Copy-Item (Join-Path $root "docs") (Join-Path $packageDir "docs") -Recurse -Force -ErrorAction SilentlyContinue

Write-Host "Package created: $packageDir"
Write-Host "Expected RTK binary for ${TargetOS}: $rtkName"
