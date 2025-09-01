param(
  [string]$Base,                   # 우선순위 1: 명시 인자
  [string]$Listen=":63081"
)

$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [Text.Encoding]::UTF8
$ROOT = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)

function Get-BASE([string]$Hint){
  if ($Hint) { return $Hint }                              # 1) 인자
  if ($env:THOMAS_SELF_BASE) { return $env:THOMAS_SELF_BASE }  # 2) 환경변수
  # 3) 로그 폴백
  $m = Select-String -Path (Join-Path $ROOT "run_logs\out_*.txt") `
       -Pattern 'listening on (http://[0-9\.]+:\d+)' -ErrorAction SilentlyContinue |
       Select-Object -Last 1
  if ($m) { return $m.Matches[0].Groups[1].Value.Trim() }
  throw "BASE not found (pass -Base or set THOMAS_SELF_BASE or have logs)."
}

$BASE = Get-BASE $Base
$EXE  = Join-Path $PSScriptRoot "signedmsg-proxy.exe"
$LOGD = Join-Path $PSScriptRoot "logs"; New-Item -ItemType Directory -Force -Path $LOGD | Out-Null
$ts   = Get-Date -Format yyyyMMdd_HHmmss
$OUT  = Join-Path $LOGD "stdout_$ts.log"
$ERR  = Join-Path $LOGD "stderr_$ts.log"

Write-Host "started signedmsg-proxy (logged)"
Write-Host "  base   : $BASE"
Write-Host "  listen : $Listen"
Write-Host "  stdout : $OUT"
Write-Host "  stderr : $ERR"

# 포그라운드 점유 없이 백그라운드 실행 + 별도 로그
Start-Process -FilePath $EXE `
  -ArgumentList @("-base",$BASE,"-listen",$Listen) `
  -WorkingDirectory $PSScriptRoot `
  -RedirectStandardOutput $OUT -RedirectStandardError $ERR `
  -WindowStyle Hidden

# 빠른 헬스 확인 (최대 3초)
$hostPart = if ($Listen -match "^\:") { "127.0.0.1$Listen" } else { $Listen }
$PROXY = "http://$hostPart"
for($i=0;$i -lt 12;$i++){
  try {
    $ok = (Invoke-RestMethod "$PROXY/round/latest/signed_msg").signature_valid
    if ($ok -eq $true) { break }
  } catch {}
  Start-Sleep -Milliseconds 250
}