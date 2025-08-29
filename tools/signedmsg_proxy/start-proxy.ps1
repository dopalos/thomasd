param(
  [string]$Listen=":63080"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# 경로
$ROOT   = "C:\thomas-scaffold\thomasd"
$RUNLOG = Join-Path $ROOT "run_logs\out.txt"
$EXE    = Join-Path $ROOT "tools\signedmsg_proxy\signedmsg-proxy.exe"

# 이전 인스턴스 종료(있으면)
Get-Process signedmsg-proxy -ErrorAction SilentlyContinue | Stop-Process -Force

# BASE 추출 (최대 20초 대기, 없으면 기본값)
$deadline = (Get-Date).AddSeconds(20)
$BASE = ""
while (-not (Test-Path $RUNLOG) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 500 }
if (Test-Path $RUNLOG) {
  $m = Select-String -Path $RUNLOG -Pattern "listening on (http://[0-9\.]+:\d+)"
  if ($m) { $BASE = $m[-1].Matches[0].Groups[1].Value.Trim() }
}
if (-not $BASE) { $BASE = "http://127.0.0.1:62533" }  # Fallback

# 환경변수
$env:THOMAS_BASE = $BASE

# 실행 파일 체크
if (-not (Test-Path $EXE)) { throw "signedmsg-proxy.exe not found: $EXE" }

# 기동 (출력 리다이렉트 없이)
Start-Process -FilePath $EXE `
  -ArgumentList @("-base",$BASE,"-listen",$Listen) `
  -WorkingDirectory (Split-Path $EXE) `
  -WindowStyle Minimized

"started signedmsg-proxy"
"  base   : $BASE"
"  listen : $Listen"
