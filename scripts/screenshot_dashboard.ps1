<#
.SYNOPSIS
    Starts examples/httpcache with the in-process load generator, waits until
    the dashboard history is filled, takes real screenshots with headless
    Chrome and saves the /stats JSON, then stops the server.

.EXAMPLE
    .\scripts\screenshot_dashboard.ps1 -Backend goarena -Out ..\work\figures\fig4_dashboard.png
#>
param(
    [string]$Backend = "goarena",
    [int]$Keys = 2000000,
    [int]$Workers = 4,
    [int]$WaitSeconds = 60,
    [int]$Port = 8080,
    [string]$Out = "results\dashboard_$Backend.png",
    [string]$Chrome = "C:\Program Files\Google\Chrome\Application\chrome.exe"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
$mingw = "C:\Users\Vlad\mingw64\bin"
if (Test-Path $mingw) { $env:PATH = "$mingw;$env:PATH" }
$env:CGO_ENABLED = "1"

New-Item -ItemType Directory -Force bin, results\logs | Out-Null
go build -o bin\httpcache.exe .\examples\httpcache
if ($LASTEXITCODE -ne 0) { throw "build failed" }

$log = "results\logs\httpcache_$Backend.txt"
$proc = Start-Process -FilePath "bin\httpcache.exe" -PassThru -NoNewWindow `
    -ArgumentList "-addr", "127.0.0.1:$Port", "-backend", $Backend, "-selfload", "-keys", $Keys, "-workers", $Workers `
    -RedirectStandardError $log
try {
    Start-Sleep -Seconds $WaitSeconds
    $url = "http://127.0.0.1:$Port"
    Invoke-WebRequest "$url/stats" -UseBasicParsing -OutFile "results\stats_$Backend.json"
    $outAbs = [System.IO.Path]::GetFullPath((Join-Path $root $Out))
    $profileDir = Join-Path $env:TEMP "goarena-chrome-profile"
    & $Chrome --headless=new --disable-gpu --hide-scrollbars --no-first-run "--user-data-dir=$profileDir" `
        --window-size=1400,900 --virtual-time-budget=4000 "--screenshot=$outAbs" "$url/dashboard" 2>&1 | Out-Null
    Write-Host "screenshot: $outAbs"
}
finally {
    Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
}
