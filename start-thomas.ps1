$ErrorActionPreference='Stop'
$here = Get-Location
# 포트 바꾸고 싶으면 아래를 수정: ":8081" -> ":18081"
$env:THOMAS_HTTP_ADDR=":8081"

# 필요 기능 플래그 (원하면 0/1로 조정)
$env:THOMAS_REQUIRE_COMMIT="1"
$env:THOMAS_VERIFY_SIG="1"
$env:THOMAS_REQUIRE_FROM_PUBKEY="1"
$env:THOMAS_FEAT_ALIAS="1"
$env:THOMAS_FEAT_R2P="1"

# 기존 프로세스 정리
Get-Process thomasd -ErrorAction SilentlyContinue | Stop-Process -Force

# 로그 경로
$out = Join-Path $here 'thomasd.out.log'
$err = Join-Path $here 'thomasd.err.log'

# 실행 + 로그 리다이렉트
Start-Process -NoNewWindow `
  -FilePath (Join-Path $here 'bin\thomasd.exe') `
  -RedirectStandardOutput $out `
  -RedirectStandardError  $err

Start-Sleep 1
Write-Host "== thomasd started =="
Get-Content $out -Tail 10
Write-Host "`n== tail -f (Ctrl+C to stop viewing) =="
Get-Content $out -Wait
