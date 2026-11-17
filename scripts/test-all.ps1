# Runs the repository quality gates locally on Windows.
#
#   .\scripts\test-all.ps1            # core checks
#   .\scripts\test-all.ps1 -Full      # core + race + fuzz smoke
#   .\scripts\test-all.ps1 -E2E       # core + Playwright smoke
#   $env:OPENCODE_BIN='C:\path\opencode.exe'; .\scripts\test-all.ps1
#
# The GitHub workflows run the same checks on Linux, Windows and macOS.
param(
    [switch]$Full,
    [switch]$SkipRace,
    [switch]$E2E
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
$failed = @()
$passed = @()

function Step {
    param([string]$Name, [scriptblock]$Body)
    Write-Host ""
    Write-Host "== $Name" -ForegroundColor Cyan
    try {
        & $Body
        if ($LASTEXITCODE -ne 0) { throw "exit code $LASTEXITCODE" }
        $script:passed += $Name
    } catch {
        Write-Host "FAILED: $Name - $_" -ForegroundColor Red
        $script:failed += $Name
    }
}

function Need([string]$Command) {
    if (-not (Get-Command $Command -ErrorAction SilentlyContinue)) {
        throw "$Command is not installed or not on PATH"
    }
}

Step "toolchain" {
    Need go
    Need node
    Need npm
    go version
    node --version
}

Step "gofmt" {
    $unformatted = gofmt -l .
    if ($unformatted) {
        $unformatted | ForEach-Object { Write-Host "  $_" }
        throw "gofmt found unformatted files"
    }
}

Step "go mod verify" { go mod verify }
Step "go vet" { go vet ./... }
Step "go test" { go test ./... }

if (-not $SkipRace) {
    $onWindows = ($env:OS -eq "Windows_NT")
    $hasCompiler = (Get-Command gcc -ErrorAction SilentlyContinue) -or (Get-Command clang -ErrorAction SilentlyContinue)
    if ($onWindows -and -not $hasCompiler) {
        Write-Host ""
        Write-Host "== go test -race: skipped (no C compiler; CI runs the race job on Linux)" -ForegroundColor Yellow
    } else {
        Step "go test -race" { go test -race ./... }
    }
}

Step "staticcheck" {
    go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
}

Step "govulncheck" {
    go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
}

Step "actionlint" {
    go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
}

Step "JavaScript syntax" {
    node web/scripts/check-syntax.mjs
}

Step "web unit tests" {
    Push-Location web
    try { npm test } finally { Pop-Location }
}

if ($env:OPENCODE_BIN) {
    Step "OpenCode API contract" {
        go test ./internal/host -run TestOpenCodeAPIContract -count=1 -v
    }
} else {
    Write-Host ""
    Write-Host "== OpenCode API contract: skipped (set OPENCODE_BIN)" -ForegroundColor Yellow
}

if ($Full) {
    $targets = @(
        @{ Pkg = "./internal/host"; Name = "FuzzClaudeContent" },
        @{ Pkg = "./internal/host"; Name = "FuzzCodexContent" },
        @{ Pkg = "./internal/host"; Name = "FuzzGeminiText" },
        @{ Pkg = "./internal/host"; Name = "FuzzOpenCodeSnippet" },
        @{ Pkg = "./internal/host"; Name = "FuzzCodexRequestID" },
        @{ Pkg = "./internal/host"; Name = "FuzzDevinText" },
        @{ Pkg = "./internal/host"; Name = "FuzzCursorUserText" },
        @{ Pkg = "./internal/protocol"; Name = "FuzzEnvelopeDecode" }
    )
    foreach ($target in $targets) {
        Step "fuzz $($target.Name)" {
            go test $target.Pkg -run '^$' -fuzz "^$($target.Name)$" -fuzztime=10s
        }
    }
}

if ($E2E) {
    Step "Playwright smoke" {
        if (-not (Test-Path "web/node_modules")) {
            throw "web/node_modules is missing; run: cd web; npm ci"
        }
        New-Item -ItemType Directory -Force -Path bin | Out-Null
        $env:CGO_ENABLED = "0"
        go build -trimpath -o bin/nexus-hub ./cmd/nexus-hub
        Push-Location web
        try {
            $env:E2E_SPAWN_HUB = "1"
            node e2e/smoke.mjs
        } finally {
            Pop-Location
            Remove-Item Env:E2E_SPAWN_HUB -ErrorAction SilentlyContinue
        }
    }
}

Write-Host ""
Write-Host "==============================" -ForegroundColor Cyan
Write-Host "passed: $($passed.Count)"
Write-Host "failed: $($failed.Count)" -ForegroundColor $(if ($failed.Count) { "Red" } else { "Green" })
if ($failed.Count) {
    $failed | ForEach-Object { Write-Host "  - $_" -ForegroundColor Red }
    Pop-Location
    exit 1
}
Write-Host "all selected checks passed" -ForegroundColor Green
Pop-Location
