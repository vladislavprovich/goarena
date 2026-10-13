<#
.SYNOPSIS
    Build, check and test goarena on Windows.

.EXAMPLE
    .\build.ps1            # vet + test + build
    .\build.ps1 -Race      # additionally run the race detector
    .\build.ps1 -Cover     # additionally write results\coverage.out
#>
param([switch]$Race, [switch]$Cover)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

# cgo requires gcc in PATH (MinGW-w64 is used for the reference measurements).
$mingw = "C:\Users\Vlad\mingw64\bin"
if ((-not (Get-Command gcc -ErrorAction SilentlyContinue)) -and (Test-Path $mingw)) {
    $env:PATH = "$mingw;$env:PATH"
}
$env:CGO_ENABLED = "1"

function Step($name, [scriptblock]$block) {
    Write-Host "==> $name" -ForegroundColor Cyan
    & $block
    if ($LASTEXITCODE -ne 0) { throw "$name failed (exit code $LASTEXITCODE)" }
}

Step "go vet"   { go vet ./... }
Step "go test"  { go test -count=1 ./... }
if ($Race)  { Step "go test -race" { go test -race -count=1 ./... } }
if ($Cover) {
    New-Item -ItemType Directory -Force results | Out-Null
    Step "go test -cover" { go test -count=1 -coverprofile=results\coverage.out ./... }
    Step "go tool cover"  { go tool cover -func=results\coverage.out }
}
New-Item -ItemType Directory -Force bin | Out-Null
Step "go build" { go build ./... }
Step "build goarena-bench" { go build -o bin\goarena-bench.exe .\cmd\goarena-bench }
Step "build httpcache"     { go build -o bin\httpcache.exe .\examples\httpcache }
Write-Host "done" -ForegroundColor Green
