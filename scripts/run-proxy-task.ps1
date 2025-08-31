# scripts\run-proxy-task.ps1
# PURPOSE: thomasd BASE를 주기적으로 감지하여 signedmsg-proxy를 올바른 인자로 유지
# NOTE   : PS 5.1 호환(삼항 연산자/예약변수 사용 안 함)

param(
  [Parameter(Mandatory=$true)]
  [string]$ProxyExe,                  # 예: C:\thomas-scaffold\thomasd\tools\signedmsg_proxy\signedmsg-proxy.exe

  [Parameter(Mandatory=$true)]
  [string]$ProxyArgs,                 # 예: -base {BASE} -listen :63081  (반드시 {BASE} 자리표시자 포함)

  [string]$Log,                       # 비우면 자동으로 ROOT\run_logs\proxy.txt 사용
  [int]$PollSec = 3                   # BASE 감지 주기(초)
)

$ErrorActionPreference = "Stop"

# ── 경로/로그 준비 ──────────────────────────────────────────────────────────────
if (-not (Test-Path $ProxyExe)) { throw "proxy exe not found: $ProxyExe" }

$toolsDir = Split-Path $ProxyExe -Parent                    # ...\tools\signedmsg_proxy
$root     = Split-Path $toolsDir -Parent                    # ...\thomasd
if (-not $Log -or $Log -eq "") {
  $Log = Join-Path $root "run_logs\proxy.txt"
}
New-Item -ItemType Directory -Force -Path (Split-Path $Log -Parent) | Out-Null

function LogLine([string]$msg) {
  $ts = Get-Date -f 'yyyy-MM-dd HH:mm:ss'
  "$ts $msg" | Out-File -FilePath $Log -Append -Encoding UTF8
}

# ── 유틸 함수 ──────────────────────────────────────────────────────────────────
function Get-ThomasBase {
  # 1) 환경변수 우선
  if ($env:THOMAS_BASE) { return $env:THOMAS_BASE }

  # 2) 프로세스에서 포트 추출
  $p = Get-Process thomasd -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($p) {
    try {
      $c = Get-NetTCPConnection -State Listen -ErrorAction Stop |
           Where-Object OwningProcess -eq $p.Id | Select-Object -First 1
      if ($c) { return ("http://127.0.0.1:{0}" -f $c.LocalPort) }
    } catch {}
    # 2b) 로그에서 추출
    $exeDir = Split-Path $p.Path -Parent
    $root2  = Split-Path $exeDir -Parent
    $logDir = Join-Path $root2 'run_logs'
    if (Test-Path $logDir) {
      $log = Get-ChildItem $logDir -Filter 'out_*.txt' | Sort-Object LastWriteTime | Select-Object -Last 1
      if ($log) {
        $m = Select-String -Path $log.FullName -Pattern 'listening on (http://[0-9\.]+:\d+)'
        if ($m -and $m.Matches.Count -gt 0) { return $m.Matches[0].Groups[1].Value.Trim() }
      }
    }
  }

  # 3) 루트 고정 폴백
  $logDir3 = Join-Path $root 'run_logs'
  if (Test-Path $logDir3) {
    $log3 = Get-ChildItem $logDir3 -Filter 'out_*.txt' | Sort-Object LastWriteTime | Select-Object -Last 1
    if ($log3) {
      $m3 = Select-String -Path $log3.FullName -Pattern 'listening on (http://[0-9\.]+:\d+)'
      if ($m3 -and $m3.Matches.Count -gt 0) { return $m3.Matches[0].Groups[1].Value.Trim() }
    }
  }
  return $null
}

function Get-ListenPortFromArgs([string]$args) {
  # -listen :63081 또는 127.0.0.1:63081 등에서 포트 파싱
  $m = [regex]::Match($args, ':(\d{2,5})')
  if ($m.Success) { return [int]$m.Groups[1].Value }
  return 63081
}

function IsListening([int]$procId, [int]$port) {
  $conn = Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue |
          Where-Object { $_.OwningProcess -eq $procId -and $_.LocalPort -eq $port } |
          Select-Object -First 1
  if ($conn) { return $true } else { return $false }
}

function KillExisting([string]$exePath, [int]$port) {
  try {
    $ex = Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($ex) { Stop-Process -Id $ex.OwningProcess -Force }
  } catch {}
  try {
    Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $exePath } | Stop-Process -Force
  } catch {}
}

# ── 메인 루프 ──────────────────────────────────────────────────────────────────
$listenPort = Get-ListenPortFromArgs $ProxyArgs
$lastBase   = $null
$proc       = $null

LogLine "=== run-proxy-task start. exe=$ProxyExe, listenPort=$listenPort, poll=${PollSec}s ==="

while ($true) {
  $base = $null
  try { $base = Get-ThomasBase } catch { $base = $null }

  if ($base -and $base -ne $lastBase) {
    LogLine ("detected BASE change: {0} -> {1}" -f $lastBase, $base)

    # 기존 종료
    KillExisting -exePath $ProxyExe -port $listenPort

    # 인자 치환 및 실행
    $execArgs = $ProxyArgs.Replace("{BASE}", $base)
    LogLine ("start proxy: {0} {1}" -f $ProxyExe, $execArgs)
    $proc = Start-Process -FilePath $ProxyExe -ArgumentList $execArgs -WindowStyle Hidden -PassThru

    # 리슨 대기(최대 5초)
    $ok = $false
    $deadline = (Get-Date).AddSeconds(5)
    while ((Get-Date) -lt $deadline) {
      Start-Sleep -Milliseconds 250
      if (IsListening -procId $proc.Id -port $listenPort) { $ok = $true; break }
      if ($proc.HasExited) { break }
      $proc = Get-Process -Id $proc.Id -ErrorAction SilentlyContinue
      if (-not $proc) { break }
    }
    if ($ok) { LogLine ("listening on :{0} (pid={1})" -f $listenPort, $proc.Id) }
    else     { LogLine ("WARN: not listening on :{0} (pid={1})" -f $listenPort, $proc.Id) }

    $lastBase = $base
  }

  # 프로세스가 죽었으면 재시도 트리거
  if ($proc) {
    $alive = $true
    try { $null = Get-Process -Id $proc.Id -ErrorAction Stop } catch { $alive = $false }
    if (-not $alive) {
      LogLine "proxy process exited; will re-evaluate"
      $lastBase = $null
      $proc = $null
    }
  }

  Start-Sleep -Seconds $PollSec
}
