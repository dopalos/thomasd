$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"
if(-not (Test-Path $router)){ throw "router.go not found" }

[string]$code = Get-Content $router -Raw

# 1) muxWrapped := precheckSig(precheckFromBinding(precheckCommit(mux)))  ->  precheckFromBinding(precheckSig(precheckCommit(mux)))
$patWrap = '(?m)^\s*muxWrapped\s*:=\s*precheckSig\s*\(\s*precheckFromBinding\s*\(\s*precheckCommit\s*\(\s*mux\s*\)\s*\)\s*\)\s*$'
$replWrap = 'muxWrapped := precheckFromBinding(precheckSig(precheckCommit(mux)))'
$cntWrap = [regex]::Matches($code, $patWrap).Count
if($cntWrap -gt 0){
  $code = [regex]::Replace($code, $patWrap, $replWrap)
  "REPLACED wrapper init: $cntWrap"
}

# 2) 모든 직접 호출도 같은 순서로 교체
#   a) precheckSig(precheckFromBinding(precheckCommit(mux))) -> precheckFromBinding(precheckSig(precheckCommit(mux)))
$pat1 = '(?s)precheckSig\s*\(\s*precheckFromBinding\s*\(\s*precheckCommit\s*\(\s*mux\s*\)\s*\)\s*\)'
$cnt1 = [regex]::Matches($code, $pat1).Count
if($cnt1 -gt 0){
  $code = [regex]::Replace($code, $pat1, 'precheckFromBinding(precheckSig(precheckCommit(mux)))')
  "REPLACED nested calls: $cnt1"
}

#   b) 혹시 남아있을 수 있는 precheckSig(precheckCommit(mux)) -> precheckFromBinding(precheckSig(precheckCommit(mux)))
$pat2 = '(?s)precheckSig\s*\(\s*precheckCommit\s*\(\s*mux\s*\)\s*\)'
$cnt2 = [regex]::Matches($code, $pat2).Count
if($cnt2 -gt 0){
  $code = [regex]::Replace($code, $pat2, 'precheckFromBinding(precheckSig(precheckCommit(mux)))')
  "REPLACED plain calls: $cnt2"
}

# 저장
Set-Content $router $code -Encoding UTF8
"router.go patched (binding runs first)"

# 빌드 로그를 cmd.exe로 강제 캡처 (stdout+stderr 모두)
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
Write-Host "`n--- build_full.log (tail) ---" -ForegroundColor Yellow
Get-Content .\build_full.log -Tail 200
