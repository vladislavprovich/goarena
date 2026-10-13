<#
.SYNOPSIS
    Runs the goarena benchmark experiments (E1, E1b, E2, E4, E5, E6, E7) and aggregates
    the results. E3 (go test micro-benchmarks) is run by run_microbench.ps1.

.DESCRIPTION
    Every benchmark run is executed by goarena-bench in a separate OS process,
    so that the heap, the GC state and the C allocator of one implementation
    cannot influence another. Raw JSON results go to results\raw, the
    aggregated CSV files to results, and the console log to results\logs.
    Runs whose JSON file already exists are skipped, so the script can be
    interrupted and resumed.

.EXAMPLE
    .\scripts\run_experiments.ps1                     # full matrix (~45 min)
    .\scripts\run_experiments.ps1 -Experiments E1 -Repeats 1 -Duration 5s
    .\scripts\run_experiments.ps1 -Scale 0.01 -Duration 1s -Short 1s   # smoke test
#>
param(
    [string]$Experiments = "E1,E1b,E2,E4,E5,E6,E7",
    [int]$Repeats = 3,
    [string]$Duration = "15s",
    [string]$Warmup = "3s",
    [string]$Short = "10s",
    [double]$Scale = 1.0,
    [string]$OutDir = "results"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

# cgo needs gcc; prefer the MinGW-w64 toolchain used for the thesis measurements.
$mingw = "C:\Users\Vlad\mingw64\bin"
if (Test-Path $mingw) { $env:PATH = "$mingw;$env:PATH" }
$env:CGO_ENABLED = "1"
Remove-Item Env:GOGC -ErrorAction SilentlyContinue
Remove-Item Env:GOMEMLIMIT -ErrorAction SilentlyContinue

New-Item -ItemType Directory -Force "bin", "$OutDir\raw", "$OutDir\logs" | Out-Null
go build -o bin\goarena-bench.exe .\cmd\goarena-bench
if ($LASTEXITCODE -ne 0) { throw "build failed" }

$log = "$OutDir\logs\bench_matrix.txt"
"# goarena-bench matrix -exp $Experiments -repeats $Repeats -duration $Duration -warmup $Warmup -short $Short -scale $Scale" |
    Tee-Object -FilePath $log -Append
& .\bin\goarena-bench.exe matrix -exp $Experiments -out "$OutDir\raw" -repeats $Repeats `
    -duration $Duration -warmup $Warmup -short $Short -scale $Scale 2>&1 |
    Tee-Object -FilePath $log -Append
& .\bin\goarena-bench.exe aggregate -in "$OutDir\raw" -out $OutDir 2>&1 |
    Tee-Object -FilePath $log -Append
