$ErrorActionPreference = 'Stop'
$Root = Split-Path $PSScriptRoot -Parent
$OSName = if ($env:GOOS) { $env:GOOS } else { (& go env GOOS).Trim() }
$Arch = if ($env:GOARCH) { $env:GOARCH } else { (& go env GOARCH).Trim() }
$Suffix = if ($OSName -eq 'windows') { '.exe' } else { '' }
$Output = Join-Path $Root "dist/core/$OSName-$Arch"
New-Item -ItemType Directory -Force $Output | Out-Null
Push-Location (Join-Path $Root 'core')
try {
 $env:CGO_ENABLED = '0'
 & go build -trimpath '-ldflags=-s -w' -o (Join-Path $Output "svolo-core$Suffix") ./cmd/svolo-core
 if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
} finally { Pop-Location }
