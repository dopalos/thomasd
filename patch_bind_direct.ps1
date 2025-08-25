$ErrorActionPreference='Stop'

$router = "internal\rpc\router.go"
if(-not (Test-Path $router)){ throw "router.go not found" }

# 0) 파일 읽기
[string]$code = Get-Content $router -Raw

# 1) precheckSig(precheckCommit(mux)) -> precheckSig(precheckFromBinding(precheckCommit(mux)))
#    (이미 precheckFromBinding이 끼워진 건 건드리지 않음)
$pattern = 'precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)'
$matches = [regex]::Matches($code, $pattern, 'Singleline')
$before = $matches.Count
if($before -gt 0){
  $code = [regex]::Replace($code, $pattern, 'precheckSig(precheckFromBinding(precheckCommit(mux)))', 'Singleline')
  "REPLACED plain calls count: $before"
}else{
  "No plain precheckSig(precheckCommit(mux)) calls found."
}

# 2) txPrecheckWith(precheckSig(precheckCommit(mux)), ...) 형태 보호 교체(혹시 누락된 변형 잡기)
$pattern2 = 'txPrecheckWith\(\s*precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)\s*,'
$matches2 = [regex]::Matches($code, $pattern2, 'Singleline')
$before2 = $matches2.Count
if($before2 -gt 0){
  $code = [regex]::Replace($code, $pattern2, 'txPrecheckWith(precheckSig(precheckFromBinding(precheckCommit(mux))),', 'Singleline')
  "REPLACED txPrecheckWith count: $before2"
}

# 3) muxWrapped 선언이 남아있고 실제로 사용 안되면 제거 (declared and not used 방지)
$declPattern = '(?m)^\s*muxWrapped\s*:='
$hasDecl = [regex]::IsMatch($code, $declPattern)
$useCount = ([regex]::Matches($code, '(?m)\bmuxWrapped\b')).Count
if($hasDecl -and $useCount -le 1){
  # 선언 라인 제거
  $code = [regex]::Replace($code, '(?m)^\s*muxWrapped\s*:.*\r?\n', '')
  "REMOVED unused muxWrapped declaration"
}

# 4) 저장
Set-Content $router $code -Encoding UTF8
"router.go patched (direct insert of precheckFromBinding)"

# 5) 빌드 로그 캡처
go clean -cache | Out-Null
& go build -tags "cbor blake3" -o .\thomasd_dbg.exe .\cmd\thomasd *>&1 | Tee-Object build_full.log
$ec = $LASTEXITCODE
if($ec -ne 0){
  "RETRY with 'cbor' only" | Write-Host -ForegroundColor Yellow
  go clean -cache | Out-Null
  & go build -tags "cbor" -o .\thomasd_dbg.exe .\cmd\thomasd *>&1 | Tee-Object build_full.log
  $ec = $LASTEXITCODE
}
if($ec -ne 0){
  Write-Host "`n--- build_full.log (tail) ---" -ForegroundColor Yellow
  Get-Content .\build_full.log -Tail 200
  throw "BUILD FAILED"
}
"BUILD OK"
