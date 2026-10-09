# Herdr runs this as the manifest's Windows [[build]] step. It builds
# bin\herdr-dots.exe from the plugin root (this script's parent directory).
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath (Join-Path $PSScriptRoot '..')
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    [Console]::Error.WriteLine('herdr-dots: Go toolchain (>= 1.25) is required to build')
    exit 1
}
New-Item -ItemType Directory -Force -Path 'bin' | Out-Null
& go build -o 'bin\herdr-dots.exe' '.\cmd\herdr-dots'
exit $LASTEXITCODE
