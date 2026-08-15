$ErrorActionPreference = "Stop"

$pluginRoot = $env:PLUGIN_ROOT
if (-not $pluginRoot) {
    $pluginRoot = Split-Path -Parent $PSScriptRoot
}

$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq "Arm64") {
    "arm64"
} else {
    "amd64"
}

$binary = Join-Path $pluginRoot "bin/signposts-windows-$arch.exe"
if (-not (Test-Path $binary)) {
    Write-Error "signposts: unsupported platform or missing binary: windows-$arch"
    exit 1
}

& $binary @args
exit $LASTEXITCODE
