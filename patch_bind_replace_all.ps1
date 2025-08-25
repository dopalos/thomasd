$ErrorActionPreference='Stop'

$router = "internal\rpc\router.go"
if(-not (Test-Path $router)){ throw "router.go not found" }

# 0) 파일 읽기
[string]$code = Get-Content $router -Raw

# 1) 모든 호출부에서 precheckCommit(mux) → precheckFromBinding(precheckCommit(mux)) 로 직접 치환
$plain = 'precheckCommit(mux)'
$withB = 'precheckFromBinding(precheckCommit(mux))'
$before = ([regex]::Matches($code, [regex]::Escape($plain))).Count
if($before -gt 0){
  $code = $code -replace [regex]::Escape($plain), $withB
  "REPLACED precheckCommit(mux) → with binding: $before"
}else{
  "No 'precheckCommit(mux)' occurrences found."
}

# 2) 선언만 있고 사용 안 되는 muxWrapped 제거
$decl = '(?m)^\s*muxWrapped\s*:.*\r?\n'
$useCount = ([regex]::Matches($code, '(?m)\bmuxWrapped\b')).Count
if($useCount -gt 0){
  # 사용처가 선언 한 줄뿐이면 제거
  if($useCount -eq 1){
    $code = [regex]::Replace($code, $decl, '')
    "REMOVED unused muxWrapped declaration"
  }
}

# 3) 저장
Set-Content $router $code -Encoding UTF8
"router.go patched (direct binding injection)"

# 4) 빌드 로그 캡처
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
