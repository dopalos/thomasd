$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"

# 0) 디버그 프로세스 중지
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force

# 1) 에러 라인 파싱
if(-not (Test-Path .\build_full.log)){ throw "build_full.log not found. Run a build first." }
$tail = Get-Content .\build_full.log -Raw
$m = [regex]::Match($tail, 'router\.go:(\d+):')
if(-not $m.Success){ throw "could not parse error line from build_full.log" }
$line = [int]$m.Groups[1].Value
"TARGET LINE = $line"

# 2) 파일 로드
[string[]]$lines = Get-Content $router
if($line -lt 1 -or $line -gt $lines.Count){ throw "line $line out of range (1..$($lines.Count))" }
$orig = $lines[$line-1]

# 3) precheckCommit(mux)만 고친다 (그 줄에 한정)
if($orig -notmatch 'precheckCommit\(\s*mux\s*\)'){
  Write-Host "No precheckCommit(mux) on that line; printing context (line-5..line+5)" -ForegroundColor Yellow
  $start=[Math]::Max(1,$line-5); $end=[Math]::Min($lines.Count,$line+5)
  0..($end-$start) | % { "{0,5}: {1}" -f ($start+$_), $lines[$start+$_-1] }
  throw "Expected precheckCommit(mux) on line $line"
}

# 4) 같은 함수 범위 근처(위로 300줄)에서 ServeMux 변수명 찾기
$regexVar = '^\s*([A-Za-z_]\w*)\s*:=\s*http\.NewServeMux\(\)\s*$'
$name = $null
for($i=$line-2; $i -ge 0 -and $i -ge $line-300; $i--){
  if($lines[$i] -match $regexVar){ $name = $Matches[1]; break }
  if($lines[$i] -match '^\s*func\s'){ break } # 함수 경계 넘지 않도록
}

# 5) 치환: 변수 있으면 그 이름으로, 없으면 inline NewServeMux()로
if($name){
  $lines[$line-1] = $orig -replace 'precheckCommit\(\s*mux\s*\)', "precheckCommit($name)"
  "FIXED line $line via VAR:$name"
}else{
  $lines[$line-1] = $orig -replace 'precheckCommit\(\s*mux\s*\)', 'precheckCommit(http.NewServeMux())'
  "FIXED line $line via INLINE http.NewServeMux()"
}

# 6) 저장
Set-Content $router $lines -Encoding UTF8
"router.go saved"

# 7) 빌드
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 120

# 8) 성공 시 재기동 + 바인딩 스모크
if(Test-Path .\thomasd_dbg.exe){
  $sk  = (Get-Content .\dev_sk.b64 -Raw).Trim()
  $pub = (& go run .\sig_util.go pub $sk).Trim()
  $env:THOMAS_PUBKEY_tho1alice = $pub
  $env:THOMAS_REQUIRE_COMMIT   = '1'
  $env:THOMAS_VERIFY_SIG       = '1'

  Start-Process .\thomasd_dbg.exe -RedirectStandardOutput thomasd_out.log -RedirectStandardError thomasd_err.log
  Start-Sleep 1
  .\smoketest_binding_focus.ps1
}
