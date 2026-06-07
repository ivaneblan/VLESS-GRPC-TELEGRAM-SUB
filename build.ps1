param(
    [Parameter(Position = 0)]
    [ValidateSet('build', 'build-linux', 'build-windows', 'build-all', 'build-bot-linux', 'build-bot-windows', 'tidy')]
    [string]$Target = 'build'
)

$ErrorActionPreference = 'Stop'
$env:CGO_ENABLED = '0'

function Resolve-Go {
    if (Get-Command go -ErrorAction SilentlyContinue) {
        return
    }
    $goBin = 'C:\Program Files\Go\bin'
    if (Test-Path "$goBin\go.exe") {
        $env:Path += ";$goBin"
        return
    }
    throw 'go not found. Install Go from https://go.dev/dl/ or add Go\bin to PATH.'
}

Resolve-Go

function Get-VersionInfo {
    $ver = 'dev'
    $commit = 'none'
    $date = (Get-Date -Format 'yyyy-MM-dd')
    if (Get-Command git -ErrorAction SilentlyContinue) {
        $tag = git describe --tags --always --dirty 2>$null
        if ($LASTEXITCODE -eq 0 -and $tag) { $ver = $tag.Trim() }
        $sha = git rev-parse --short HEAD 2>$null
        if ($LASTEXITCODE -eq 0 -and $sha) { $commit = $sha.Trim() }
    }
    $pkg = 'main'
    return "-X $pkg.version=$ver -X $pkg.commit=$commit -X $pkg.date=$date"
}

$LdFlags = Get-VersionInfo

function Ensure-Dist {
    New-Item -ItemType Directory -Force -Path dist | Out-Null
}

function Build-Local {
    if ($IsWindows -or $env:OS -eq 'Windows_NT') {
        go build -ldflags $LdFlags -o vpnctl.exe ./cmd/vpnctl
        go build -o tgbot.exe ./cmd/tgbot
    } else {
        go build -ldflags $LdFlags -o vpnctl ./cmd/vpnctl
        go build -o tgbot ./cmd/tgbot
    }
}

function Build-Linux {
    Ensure-Dist
    $env:GOOS = 'linux'
    $env:GOARCH = 'amd64'
    go build -ldflags $LdFlags -o dist/vpnctl-linux-amd64 ./cmd/vpnctl
    go build -o dist/tgbot-linux-amd64 ./cmd/tgbot
    Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
}

function Build-Windows {
    Ensure-Dist
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    go build -ldflags $LdFlags -o dist/vpnctl-windows-amd64.exe ./cmd/vpnctl
    go build -o dist/tgbot-windows-amd64.exe ./cmd/tgbot
    Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
}

function Build-BotLinux {
    Ensure-Dist
    $env:GOOS = 'linux'
    $env:GOARCH = 'amd64'
    go build -o dist/tgbot-linux-amd64 ./cmd/tgbot
    Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
}

function Build-BotWindows {
    Ensure-Dist
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    go build -o dist/tgbot-windows-amd64.exe ./cmd/tgbot
    Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
}

switch ($Target) {
    'build' { Build-Local }
    'build-linux' { Build-Linux }
    'build-windows' { Build-Windows }
    'build-all' {
        Build-Linux
        Build-Windows
        Build-Local
        Write-Host "built dist/vpnctl-* and dist/tgbot-* + local vpnctl/tgbot"
    }
    'build-bot-linux' { Build-BotLinux }
    'build-bot-windows' { Build-BotWindows }
    'tidy' { go mod tidy }
}
