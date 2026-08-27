param(
    [ValidateSet(
        "current",
        "windows-amd64",
        "windows-arm64",
        "linux-amd64",
        "linux-arm64",
        "darwin-amd64",
        "darwin-arm64",
        "all",
        "clean"
    )]
    [string]$Target
)

$ErrorActionPreference = "Stop"

$ProjectRoot = Split-Path -Parent $PSCommandPath
$DistDir = Join-Path $ProjectRoot "dist"
$AppName = "sshpilot"

$BuildTargets = @(
    [pscustomobject]@{ Key = "2"; Label = "Windows x64"; GOOS = "windows"; GOARCH = "amd64" }
    [pscustomobject]@{ Key = "3"; Label = "Windows ARM64"; GOOS = "windows"; GOARCH = "arm64" }
    [pscustomobject]@{ Key = "4"; Label = "Linux x64"; GOOS = "linux"; GOARCH = "amd64" }
    [pscustomobject]@{ Key = "5"; Label = "Linux ARM64"; GOOS = "linux"; GOARCH = "arm64" }
    [pscustomobject]@{ Key = "6"; Label = "macOS Intel"; GOOS = "darwin"; GOARCH = "amd64" }
    [pscustomobject]@{ Key = "7"; Label = "macOS Apple Silicon"; GOOS = "darwin"; GOARCH = "arm64" }
)

function Test-GoAvailable {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw "Go is not available in PATH."
    }
}

function Get-CurrentTarget {
    $goos = if ($IsWindows) {
        "windows"
    }
    elseif ($IsLinux) {
        "linux"
    }
    elseif ($IsMacOS) {
        "darwin"
    }
    else {
        throw "Unsupported host OS."
    }

    $goarch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()) {
        "x64" { "amd64" }
        "arm64" { "arm64" }
        "x86" { "386" }
        "arm" { "arm" }
        default { throw "Unsupported host architecture." }
    }

    return [pscustomobject]@{
        GOOS = $goos
        GOARCH = $goarch
    }
}

function Get-ArtifactName {
    param(
        [Parameter(Mandatory = $true)]
        [string]$GOOS,
        [Parameter(Mandatory = $true)]
        [string]$GOARCH,
        [switch]$UsePlainName
    )

    if ($UsePlainName) {
        if ($GOOS -eq "windows") {
            return "$AppName.exe"
        }

        return $AppName
    }

    $suffix = "$AppName-$GOOS-$GOARCH"
    if ($GOOS -eq "windows") {
        return "$suffix.exe"
    }

    return $suffix
}

function Initialize-DistDir {
    if (-not (Test-Path -LiteralPath $DistDir)) {
        New-Item -ItemType Directory -Path $DistDir | Out-Null
    }
}

function Clear-DistDir {
    if (Test-Path -LiteralPath $DistDir) {
        Get-ChildItem -LiteralPath $DistDir -Force | Remove-Item -Recurse -Force
    }

    Write-Host "dist directory is clean." -ForegroundColor Yellow
}

function Invoke-AppBuild {
    param(
        [Parameter(Mandatory = $true)]
        [string]$GOOS,
        [Parameter(Mandatory = $true)]
        [string]$GOARCH,
        [switch]$UsePlainName
    )

    Initialize-DistDir

    $artifactPath = Join-Path $DistDir (Get-ArtifactName -GOOS $GOOS -GOARCH $GOARCH -UsePlainName:$UsePlainName)
    $previousGOOS = $env:GOOS
    $previousGOARCH = $env:GOARCH
    $previousCGOEnabled = $env:CGO_ENABLED

    try {
        $env:GOOS = $GOOS
        $env:GOARCH = $GOARCH
        $env:CGO_ENABLED = "0"

        Write-Host ""
        Write-Host "Building $GOOS/$GOARCH..." -ForegroundColor Cyan
        & go build -trimpath "-ldflags=-s -w" -o $artifactPath .

        if ($LASTEXITCODE -ne 0) {
            throw "go build failed for $GOOS/$GOARCH."
        }

        Write-Host "Created: $artifactPath" -ForegroundColor Green
        return $artifactPath
    }
    finally {
        $env:GOOS = $previousGOOS
        $env:GOARCH = $previousGOARCH
        $env:CGO_ENABLED = $previousCGOEnabled
    }
}

function Invoke-AllBuilds {
    $artifacts = @()
    foreach ($targetItem in $BuildTargets) {
        $artifacts += Invoke-AppBuild -GOOS $targetItem.GOOS -GOARCH $targetItem.GOARCH
    }

    return $artifacts
}

function Show-Menu {
    Write-Host ""
    Write-Host "==== sshpilot build menu ====" -ForegroundColor White
    $current = Get-CurrentTarget
    Write-Host "1. Current platform ($($current.GOOS)/$($current.GOARCH))"
    foreach ($targetItem in $BuildTargets) {
        Write-Host "$($targetItem.Key). $($targetItem.Label) ($($targetItem.GOOS)/$($targetItem.GOARCH))"
    }
    Write-Host "8. Build all targets"
    Write-Host "9. Clean dist"
    Write-Host "0. Exit"
    Write-Host ""
}

function Invoke-MenuAction {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Choice
    )

    switch ($Choice) {
        "1" {
            $current = Get-CurrentTarget
            [void](Invoke-AppBuild -GOOS $current.GOOS -GOARCH $current.GOARCH -UsePlainName)
            return $false
        }
        "2" {
            [void](Invoke-AppBuild -GOOS "windows" -GOARCH "amd64")
            return $false
        }
        "3" {
            [void](Invoke-AppBuild -GOOS "windows" -GOARCH "arm64")
            return $false
        }
        "4" {
            [void](Invoke-AppBuild -GOOS "linux" -GOARCH "amd64")
            return $false
        }
        "5" {
            [void](Invoke-AppBuild -GOOS "linux" -GOARCH "arm64")
            return $false
        }
        "6" {
            [void](Invoke-AppBuild -GOOS "darwin" -GOARCH "amd64")
            return $false
        }
        "7" {
            [void](Invoke-AppBuild -GOOS "darwin" -GOARCH "arm64")
            return $false
        }
        "8" {
            [void](Invoke-AllBuilds)
            return $false
        }
        "9" {
            Clear-DistDir
            return $false
        }
        "0" {
            return $true
        }
        default {
            Write-Host "Unknown option. Try again." -ForegroundColor Red
            return $false
        }
    }
}

function Resolve-ChoiceFromTarget {
    param(
        [Parameter(Mandatory = $true)]
        [string]$RequestedTarget
    )

    switch ($RequestedTarget) {
        "current" { return "1" }
        "windows-amd64" { return "2" }
        "windows-arm64" { return "3" }
        "linux-amd64" { return "4" }
        "linux-arm64" { return "5" }
        "darwin-amd64" { return "6" }
        "darwin-arm64" { return "7" }
        "all" { return "8" }
        "clean" { return "9" }
        default { throw "Unknown target: $RequestedTarget" }
    }
}

Test-GoAvailable
Push-Location -LiteralPath $ProjectRoot

try {
    if ($Target) {
        [void](Invoke-MenuAction -Choice (Resolve-ChoiceFromTarget -RequestedTarget $Target))
        return
    }

    while ($true) {
        Show-Menu
        $choice = Read-Host "Select an option"
        $shouldExit = Invoke-MenuAction -Choice $choice
        if ($shouldExit) {
            break
        }
    }
}
finally {
    Pop-Location
}
