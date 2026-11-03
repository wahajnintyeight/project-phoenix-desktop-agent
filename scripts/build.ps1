# Build the Phoenix spike: type-check the UI, bundle it, then build the Windows executable.
# usage: pwsh scripts\build.ps1 [-Run]
param([switch]$Run)

$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)

if (-not (Test-Path node_modules)) { npm install --no-audit --no-fund }

npx tsc --noEmit -p ui
if ($LASTEXITCODE -ne 0) { throw 'TypeScript type-check failed' }

go run ./tools/buildui
if ($LASTEXITCODE -ne 0) { throw 'UI bundle failed' }

go build -ldflags '-s -w -H=windowsgui' -o bin\phoenix-spike.exe ./cmd/spike
if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }

$size = (Get-Item bin\phoenix-spike.exe).Length / 1MB
'built bin\phoenix-spike.exe ({0:N1} MB)' -f $size

if ($Run) { Start-Process .\bin\phoenix-spike.exe }
