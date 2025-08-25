$ErrorActionPreference='Stop'

$router = "internal\rpc\router.go"

# 0) 실행 중인 프로세스 종료
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force

# 1) 라인 단위 로드
$lines = Get-Content $router

# 2) 잘못된 재선언 교정: "mux := precheckFromBinding(mux)" -> "mux = precheckFromBinding(mux)"
$fixA=0
for($i=0;$i -lt $lines.Count;$i++){
  if($lines[$i] -match '^\s*mux\s*:=\s*precheckFromBinding\(\s*mux\s*\)\s*$'){
    $lines[$i] = ($lines[$i] -replace ':=','=')
    $fixA++
  }
}
"FIX A (redeclare->assign): $fixA"

# 3) mux 선언 정규화: 첫 "mux := http.NewServeMux()"만 := 유지, 그 외는 = 로 변경
$defIdxs = @()
for($i=0;$i -lt $lines.Count;$i++){
  if($lines[$i] -match '^\s*mux\s*:=\s*http\.NewServeMux\(\)\s*$'){ $defIdxs += $i }
}
if($defIdxs.Count -gt 0){
  for($k=1;$k -lt $defIdxs.Count;$k++){
    $j=$defIdxs[$k]
    $lines[$j] = ($lines[$j] -replace ':=','=')
  }
}else{
  Write-Host "WARN: no 'mux := http.NewServeMux()' found" -ForegroundColor Yellow
}

# 4) 첫 선언 직후 한 번만 precheckFromBinding 적용 (같은 블록 내 5줄 이내에 없으면 삽입)
if($defIdxs.Count -gt 0){
  $j = $defIdxs[0]
  $hasWrap = $false
  for($t=$j+1; $t -le [Math]::Min($j+5, $lines.Count-1); $t++){
    if($lines[$t] -match 'precheckFromBinding\s*\(\s*mux\s*\)'){ $hasWrap=$true; break }
  }
  if(-not $hasWrap){
    $indent = ($lines[$j] -replace '^(\s*).*','$1')
    $ins = "$indent"+"mux = precheckFromBinding(mux)"
    $lines = $lines[0..$j] + $ins + $lines[($j+1)..($lines.Count-1)]
    "INSERTED inline wrap after mux declaration"
  } else {
    "SKIP wrap insert: already present near declaration"
  }
}

# 5) 저장 (라인 기반)
Set-Content $router $lines -Encoding UTF8
"router.go normalized (line-based)"

# 6) 리턴부/태그 복구 (문자열 대체만 사용)
[string]$text = Get-Content $router -Raw
$text = $text -replace 'precheckSig\s*\(\s*precheckFromBinding\s*\(\s*precheckCommit\s*\(\s*mux\s*\)\s*\)\s*\)', 'precheckSig(precheckCommit(mux))'
$text = $text -replace '(?<!`)json:"from"(?!`)', '`json:"from"`'
Set-Content $router $text -Encoding UTF8
"returns & tags restored"

# 7) 빌드
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 120

# 8) 성공 시 재기동 후 바인딩 스모크
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
