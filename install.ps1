# Skyline MCP Installer for Windows
# Usage: irm https://skyline.projex.cc/install.ps1 | iex

$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "  Skyline MCP Installer" -ForegroundColor Cyan
Write-Host "  =====================" -ForegroundColor Cyan
Write-Host ""

# Detect architecture
$Arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
    "X64"  { "amd64" }
    "Arm64" { "arm64" }
    default {
        Write-Host "Unsupported architecture: $_" -ForegroundColor Red
        exit 1
    }
}

Write-Host "  Platform: windows-$Arch"
Write-Host ""

# Check for existing installation
$ExistingPath = Get-Command skyline -ErrorAction SilentlyContinue
if ($ExistingPath) {
    $ExistingVersion = & skyline --version 2>$null | Select-Object -First 1
    Write-Host "  Existing installation found:" -ForegroundColor Yellow
    Write-Host "    Location: $($ExistingPath.Source)"
    Write-Host "    Version: $ExistingVersion"
    Write-Host "    Status: Will be replaced"
    Write-Host ""
}

# Download
$Binary = "skyline-windows-$Arch.exe"
$Url = "https://github.com/emadomedher/skyline-mcp/releases/latest/download/$Binary"
$InstallDir = "$env:LOCALAPPDATA\Skyline"
$InstallPath = "$InstallDir\skyline.exe"

Write-Host "  Downloading from GitHub releases..." -ForegroundColor Blue

if (!(Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    Invoke-WebRequest -Uri $Url -OutFile $InstallPath -UseBasicParsing
} catch {
    Write-Host "  Download failed: $_" -ForegroundColor Red
    Write-Host "  URL: $Url" -ForegroundColor Red
    exit 1
}

Write-Host "  Installed to $InstallPath" -ForegroundColor Green
Write-Host ""

# Add to PATH if not already there
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
    $env:Path = "$env:Path;$InstallDir"
    Write-Host "  Added $InstallDir to PATH" -ForegroundColor Green
} else {
    Write-Host "  Already in PATH" -ForegroundColor Green
}

Write-Host ""

# Verify installation
try {
    $NewVersion = & $InstallPath --version 2>$null | Select-Object -First 1
    Write-Host "  Skyline MCP installed successfully!" -ForegroundColor Green
    Write-Host ""
    Write-Host "    Version: $NewVersion"
} catch {
    Write-Host "  Skyline MCP installed successfully!" -ForegroundColor Green
}

Write-Host ""
Write-Host "  Next steps:" -ForegroundColor Cyan
Write-Host "    1. Open a new terminal (for PATH changes)"
Write-Host "    2. Run: skyline"
Write-Host "    3. Open Admin UI at https://localhost:8191/ui"
Write-Host ""
Write-Host "  Documentation: https://skyline.projex.cc/docs" -ForegroundColor Blue
Write-Host ""
