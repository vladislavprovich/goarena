<#
.SYNOPSIS
    Runs the complete verification suite of goarena and stores every console
    output in results\logs (used for SUMMARY.md and the thesis figures).

.DESCRIPTION
    vet + build, unit tests (-v), race detector, coverage (coverage.out and
    per-function report), FuzzStore for -FuzzTime.
#>
param([string]$FuzzTime = "60s")

$ErrorActionPreference = "Continue"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
$mingw = "C:\Users\Vlad\mingw64\bin"
if (Test-Path $mingw) { $env:PATH = "$mingw;$env:PATH" }
$env:CGO_ENABLED = "1"
$logs = "results\logs"
New-Item -ItemType Directory -Force $logs | Out-Null

function Run($log, [string[]]$cmd) {
    Write-Host "==> $($cmd -join ' ')" -ForegroundColor Cyan
    & $cmd[0] $cmd[1..($cmd.Length - 1)] 2>&1 | ForEach-Object { "$_" } | Tee-Object -FilePath "$logs\$log"
}

# go vet and go build print nothing on success, so record explicit status lines.
& {
    '$ go version'; go version
    '$ go vet ./...'; go vet ./... 2>&1 | ForEach-Object { "$_" }; "go vet: exit code $LASTEXITCODE"
    '$ go build ./...'; go build ./... 2>&1 | ForEach-Object { "$_" }; "go build: exit code $LASTEXITCODE"
    '$ gofmt -l .'; $unformatted = @(gofmt -l .); "gofmt: $($unformatted.Count) files need formatting"
} | Tee-Object -FilePath "$logs\vet_build.txt"
Run "test_v.txt"     @("go", "test", "-count=1", "-v", "./...")
Run "test.txt"       @("go", "test", "-count=1", "./...")
Run "race.txt"       @("go", "test", "-race", "-count=1", "-v", "./...")
Run "cover.txt"      @("go", "test", "-count=1", "-coverprofile=results/coverage.out", "./...")
Run "cover_func.txt" @("go", "tool", "cover", "-func=results/coverage.out")
Run "fuzz.txt"       @("go", "test", "-run", "XXX", "-fuzz", "FuzzStore", "-fuzztime", $FuzzTime, "-fuzzminimizetime", "5s", "./store")
