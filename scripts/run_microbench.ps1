<#
.SYNOPSIS
    Experiment E3: go test micro-benchmarks (empty Go call vs empty cgo call,
    Arena.Alloc vs make, Slab Alloc+Free, store Get/Set vs sharded map).
#>
param([int]$Count = 5)

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
$mingw = "C:\Users\Vlad\mingw64\bin"
if (Test-Path $mingw) { $env:PATH = "$mingw;$env:PATH" }
$env:CGO_ENABLED = "1"
New-Item -ItemType Directory -Force results\logs | Out-Null
go test -run XXX -bench . -benchmem -count $Count ./internal/cmem ./internal/tsc ./arena ./store 2>&1 |
    ForEach-Object { "$_" } | Tee-Object -FilePath results\logs\microbench.txt
python scripts\parse_microbench.py results\logs\microbench.txt results\microbench.csv
