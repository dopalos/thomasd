# scripts\install-proxy-startup.ps1
# 로그인 시 자동으로 signedmsg-proxy 따라붙게 시작프로그램 등록 (PS 5.1 호환)
param(
  [string]$Root   = "C:\thomas-scaffold\thomasd",
  [string]$Listen = ":63081"
)
$ErrorActionPreference = "Stop"

$proxyExe = Join-Path $Root "tools\signedmsg_proxy\signedmsg-proxy.exe"
$taskPs1  = Join-Path $Root "scripts\run-proxy-task.ps1"
$runner   = Join-Path $Root "scripts\start-proxy-task.bat"
$logDir   = Join-Path $Root "run_logs"
$startup  = [Environment]::GetFolderPath('Startup')
$lnkPath  = Join-Path $startup "THO-Proxy.lnk"
$batInStartup = Join-Path $startup "THO-Proxy.bat"

if (-not (Test-Path $proxyExe)) { throw "proxy exe not found: $proxyExe" }
if (-not (Test-Path $taskPs1))  { throw "run-proxy-task.ps1 not found: $taskPs1" }

New-Item -ItemType Directory -Force -Path (Split-Path $runner -Parent) | Out-Null
New-Item -ItemType Directory -Force -Path $logDir | Out-Null

# 실행 배치 파일 생성 (BASE 자동 추적 태스크 실행)
$bat = @"
@echo off
setlocal
set ROOT=$Root
set PS="%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe"
%PS% -NoProfile -ExecutionPolicy Bypass -File "%ROOT%\scripts\run-proxy-task.ps1" -ProxyExe "%ROOT%\tools\signedmsg_proxy\signedmsg-proxy.exe" -ProxyArgs "-base {BASE} -listen $Listen" -Log "%ROOT%\run_logs\proxy.txt"
"@
$bat | Out-File -FilePath $runner -Encoding OEM -Force

# 시작프로그램 등록: 우선 .lnk 시도, 실패하면 .bat 복사
$registered = $false
try {
  $wsh = New-Object -ComObject WScript.Shell
  $sc  = $wsh.CreateShortcut($lnkPath)
  $sc.TargetPath = "cmd.exe"
  $sc.Arguments  = '/c "' + $runner + '"'
  $sc.WorkingDirectory = $Root
  $sc.WindowStyle = 7
  $sc.IconLocation = "$env:SystemRoot\System32\shell32.dll,44"
  $sc.Save()
  if (Test-Path $lnkPath) { $registered = $true }
} catch { }

if (-not $registered) {
  Copy-Item $runner $batInStartup -Force
  if (Test-Path $batInStartup) { $registered = $true }
}

if (-not $registered) { throw "failed to register in Startup folder: $startup" }

Write-Host "Startup entry installed."
Write-Host "  Startup folder: $startup"
if (Test-Path $lnkPath) { Write-Host "  Link: $lnkPath" }
if (Test-Path $batInStartup) { Write-Host "  Batch: $batInStartup" }
Write-Host "Runner: $runner"
Write-Host "Proxy exe: $proxyExe"
