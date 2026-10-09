# Herdr runs this as the manifest's Windows [[build]] step, during
# `herdr plugin install`. It installs bin\herdr-dots.exe, like build.sh:
#   1. the prebuilt release binary for this CPU, downloaded from the GitHub
#      release and checked against release/SHA256SUMS from the cloned tree;
#   2. otherwise a source build with Go 1.25 or newer;
#   3. otherwise it fails with the reason, leaving nothing half-installed.
# HERDR_PLUGIN_BUILD_OFFLINE=1 skips the download.
$ErrorActionPreference = 'Stop'
# Windows PowerShell's progress bar slows Invoke-WebRequest badly.
$ProgressPreference = 'SilentlyContinue'
Set-Location -LiteralPath (Join-Path $PSScriptRoot '..')
$Name = 'herdr-dots'
$Version = (Get-Content -Raw -LiteralPath 'internal/version/VERSION').Trim()

function Say([string]$Message) { [Console]::Error.WriteLine("${Name}: $Message") }

function Get-Platform {
    if ($env:HERDR_DOTS_PLATFORM) {
        if ($env:HERDR_DOTS_PLATFORM -cmatch '^(darwin|linux|windows)_(amd64|arm64)$') { return $env:HERDR_DOTS_PLATFORM }
        Say "ignoring HERDR_DOTS_PLATFORM=$($env:HERDR_DOTS_PLATFORM): not <darwin|linux|windows>_<amd64|arm64>"
        return $null
    }
    switch ($env:PROCESSOR_ARCHITECTURE) {
        'AMD64' { return 'windows_amd64' }
        'ARM64' { return 'windows_arm64' }
    }
    return $null
}

function Get-Repo {
    $r = ''
    # An empty file reads as $null; the string wrap makes it "".
    if (Test-Path -LiteralPath 'release/REPO') { $r = "$(Get-Content -Raw -LiteralPath 'release/REPO')".Trim() }
    if (-not $r -and (Get-Command git -ErrorAction SilentlyContinue)) {
        $url = (& git config --get remote.origin.url 2>$null)
        if ($url -match '^(?:https://github\.com/|git@github\.com:)(.+?)(?:\.git)?$') { $r = $Matches[1] }
    }
    if ($r -match '^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$') { return $r }
    return $null
}

function Get-Checksum([string]$Asset) {
    if (-not (Test-Path -LiteralPath 'release/SHA256SUMS')) { return $null }
    foreach ($line in Get-Content -LiteralPath 'release/SHA256SUMS') {
        $f = $line.Trim() -split '\s+', 2
        if ($f.Count -eq 2 -and $f[1].TrimStart('*') -eq $Asset) { return $f[0].ToLower() }
    }
    return $null
}

function Install-Release([string]$Tmp) {
    if ($env:HERDR_PLUGIN_BUILD_OFFLINE -eq '1') { Say 'HERDR_PLUGIN_BUILD_OFFLINE=1: skipping the download'; return $false }
    $plat = Get-Platform
    if (-not $plat) { Say "no prebuilt binary for $env:PROCESSOR_ARCHITECTURE"; return $false }
    $asset = "${Name}_${Version}_${plat}.zip"
    $want = Get-Checksum $asset
    if (-not $want) { Say "release/SHA256SUMS has no checksum for $asset"; return $false }
    $repo = Get-Repo
    if (-not $repo) { Say 'unknown GitHub repository: put owner/repo in release/REPO'; return $false }
    $base = 'https://github.com'
    if ($env:HERDR_DOTS_RELEASE_BASE) { $base = $env:HERDR_DOTS_RELEASE_BASE }
    $url = "$base/$repo/releases/download/v$Version/$asset"
    $zip = Join-Path $Tmp $asset
    Say "downloading $url"
    try {
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
        Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $zip -TimeoutSec 300
    } catch {
        Say "download failed: $($_.Exception.Message)"
        return $false
    }
    $got = (Get-FileHash -Algorithm SHA256 -LiteralPath $zip).Hash.ToLower()
    if ($got -ne $want) { Say "checksum mismatch for $asset (got $got, want $want): not installing it"; return $false }
    $bundle = $null
    try {
        Invoke-WebRequest -UseBasicParsing -Uri "$url.sigstore.json" -OutFile (Join-Path $Tmp 'bundle') -TimeoutSec 60
        $bundle = Join-Path $Tmp 'bundle'
    } catch { }
    # Stage everything in $Tmp, then install the binary first, the archive
    # (and bundle) verify-release reads next, and the record last, so a
    # failure part way never leaves a record that vouches for this binary.
    # Any failure here returns $false, so the caller falls back to a source build.
    try {
        $x = Join-Path $Tmp 'x'
        Expand-Archive -LiteralPath $zip -DestinationPath $x
        $exe = Join-Path $x "$Name.exe"
        if (-not (Test-Path -LiteralPath $exe -PathType Leaf)) { Say "$asset has no $Name.exe"; return $false }
        $record = Join-Path $Tmp 'INSTALL_SOURCE'
        Set-Content -NoNewline -LiteralPath $record -Value "source=release`nversion=$Version`nrepo=$repo`nasset=$asset`nsha256=$got`n"
        Install-Binary $exe
        New-Item -ItemType Directory -Force -Path 'bin/release' | Out-Null
        Remove-Item -Force -ErrorAction SilentlyContinue -LiteralPath "bin/release/$asset.sigstore.json"
        Move-Item -Force -LiteralPath $zip -Destination "bin/release/$asset"
        if ($bundle) { Move-Item -Force -LiteralPath $bundle -Destination "bin/release/$asset.sigstore.json" }
        Move-Item -Force -LiteralPath $record -Destination 'bin/INSTALL_SOURCE'
    } catch {
        Say "could not install the verified ${asset}: $($_.Exception.Message)"
        return $false
    }
    return $true
}

# Install-Binary moves the new exe to bin\. Once the old binary is gone its
# INSTALL_SOURCE no longer describes bin\, so the record is removed until the
# caller writes the new one.
function Install-Binary([string]$Exe) {
    Move-Item -Force -LiteralPath $Exe -Destination "bin/$Name.exe"
    if (Test-Path -LiteralPath 'bin/INSTALL_SOURCE') { Remove-Item -Force -LiteralPath 'bin/INSTALL_SOURCE' }
}

function Build-Source([string]$Tmp) {
    Say "building $Version from source with $(& go version)"
    $out = Join-Path $Tmp "$Name.exe"
    & go build -trimpath -ldflags '-X herdr-dots/internal/version.Source=source' -o $out '.\cmd\herdr-dots'
    if ($LASTEXITCODE -ne 0) { return $false }
    try {
        $record = Join-Path $Tmp 'INSTALL_SOURCE'
        Set-Content -NoNewline -LiteralPath $record -Value "source=source`nversion=$Version`n"
        Install-Binary $out
        Move-Item -Force -LiteralPath $record -Destination 'bin/INSTALL_SOURCE'
    } catch {
        Say "could not install the source build: $($_.Exception.Message)"
        return $false
    }
    return $true
}

New-Item -ItemType Directory -Force -Path 'bin' | Out-Null
$tmp = Join-Path 'bin' ('.install.' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
$code = 1
try {
    if (Install-Release $tmp) {
        Say "installed the verified $Version release binary"
        $code = 0
    } elseif (Get-Command go -ErrorAction SilentlyContinue) {
        if (Build-Source $tmp) { Say "built $Version from source"; $code = 0 }
        else { Say 'the source build or its install failed (see above)' }
    } else {
        Say 'could not install: no verified prebuilt binary (see above) and no Go toolchain.'
        Say 'Install Go 1.25 or newer (https://go.dev/dl/) and reinstall the plugin, or download'
        Say "${Name}_${Version}_windows_<arch>.zip from the GitHub release, check it against"
        Say "release/SHA256SUMS and put $Name.exe in bin\."
    }
} finally {
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue -LiteralPath $tmp
}
exit $code
