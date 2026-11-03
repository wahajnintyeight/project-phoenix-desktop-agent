# Build Phoenix with the Wails 3 CLI: type-check the UI, compile its CSS (Tailwind), bundle it,
# then build bin\phoenix.exe. Wraps `wails3 build` so it also installs the CLI when missing.
# usage: powershell -File scripts\build.ps1 [-Run]
param([switch]$Run)

$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)

if (-not (Get-Command wails3 -ErrorAction SilentlyContinue)) {
  go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
  if ($LASTEXITCODE -ne 0) { throw 'could not install the wails3 CLI' }
}

wails3 build
if ($LASTEXITCODE -ne 0) { throw 'wails3 build failed' }

$size = (Get-Item bin\phoenix.exe).Length / 1MB
'built bin\phoenix.exe ({0:N1} MB)' -f $size

if ($Run) { Start-Process .\bin\phoenix.exe }
