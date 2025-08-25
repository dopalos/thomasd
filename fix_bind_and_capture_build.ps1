$ErrorActionPreference='Stop'

$router = "internal\rpc\router.go"
if(-not (Test-Path $router)){ throw "router.go not found" }

# --- 0) 파일 로드
[string]$code = Get-Content $router -Raw

# --- 1) precheck 체인 강제 삽입 (줄바꿈/공백 다양성 대응: DOTALL 패턴 사용)
# a) precheckSig(precheckCommit(mux))  -> precheckSig(precheckFromBinding(precheckCommit(mux)))
$pat1 = '(?s)precheckSig\s*\(\s*precheckCommit\s*\(\s*mux\s*\)\s*\)'
$cnt1 = [regex]::Matches($code, $pat1).Count
if($cnt1 -gt 0){
  $code = [regex]::Replace($code, $pat1, 'precheckSig(precheckFromBinding(precheckCommit(mux)))')
  "REPLACED pat1 (plain precheck): $cnt1"
} else {
  "No matches for pat1"
}

# b) txPrecheckWith(precheckSig(precheckCommit(mux)), ...) -> txPrecheckWith(precheckSig(precheckFromBinding(precheckCommit(mux))), ...)
$pat2 = '(?s)txPrecheckWith\s*\(\s*precheckSig\s*\(\s*precheckCommit\s*\(\s*mux\s*\)\s*\)\s*,'
$cnt2 = [regex]::Matches($code, $pat2).Count
if($cnt2 -gt 0){
  $code = [regex]::Replace($code, $pat2, 'txPrecheckWith(precheckSig(precheckFromBinding(precheckCommit(mux))),')
  "REPLACED pat2 (txPrecheckWith): $cnt2"
} else {
  "No matches for pat2"
}

# --- 2) muxWrapped 선언이 있되 실제로 쓰지 않으면 제거 (declared and not used 방지)
$declRx = '(?m)^\s*muxWrapped\s*:.*\r?\n'
$hasDecl = [regex]::IsMatch($code, $declRx)
$useCnt  = ([regex]::Matches($code, '(?m)\bmuxWrapped\b')).Count
if($hasDecl -and $useCnt -le 1){
  $code = [regex]::Replace($code, $declRx, '')
  "REMOVED unused muxWrapped declaration"
}

# --- 3) 저장
Set-Content $router $code -Encoding UTF8
"router.go patched"

# --- 4) 빌드: 모든 스트림을 로그로 캡처하고 꼭 tail 출력
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
# PowerShell 전용 전체 스트림 리다이렉션
& go build -tags "cbor blake3" -o .\thomasd_dbg.exe .\cmd\thomasd *> .\build_full.log
$ec = $LASTEXITCODE
if($ec -ne 0){
  # 태그 축소 재시도
  & go build -tags "cbor" -o .\thomasd_dbg.exe .\cmd\thomasd *> .\build_full.log
  $ec = $LASTEXITCODE
}
if($ec -ne 0){
  Write-Host "`n--- build_full.log (tail) ---" -ForegroundColor Yellow
  Get-Content .\build_full.log -Tail 200
  throw "BUILD FAILED ($ec)"
}

"BUILD OK"

# --- 5) 확인: 체인 들어갔는지 프린트
""
"--- grep check ---"
Select-String .\internal\rpc\router.go -Pattern 'precheckFromBinding|precheckCommit\(mux\)|txPrecheckWith|muxWrapped' -Context 0,0 | ForEach-Object { $_.Line }
