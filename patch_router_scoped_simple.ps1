$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"
if(-not (Test-Path $router)){ throw "router.go not found" }

# 0) 원본 읽기
[string]$code = Get-Content $router -Raw

# 1) 기존 muxWrapped 선언 라인들 제거(정확히 그 라인만)
$code = [regex]::Replace($code, '(?m)^\s*muxWrapped\s*:.*\r?\n', '')

# 2) mux := http.NewServeMux() 찾고, 그 바로 아래에 선언 1줄 삽입
$vm = [regex]::Match($code,'(?m)^(?<indent>\s*)mux\s*:=\s*http\.NewServeMux\(\)\s*$')
if(-not $vm.Success){ throw "mux := http.NewServeMux() not found" }
$indent = $vm.Groups['indent'].Value
$insertAt = $vm.Index + $vm.Length
$decl = "`r`n$indent" + 'muxWrapped := precheckFromBinding(precheckSig(precheckCommit(mux)))'
$code = $code.Insert($insertAt, $decl)

# 3) 함수 호출을 모두 muxWrapped로 통일 (오직 mux 관련 체인만)
# 3a) return 라인 안의 체인들 → muxWrapped (여러 변형 대응)
$patterns = @(
  'precheckSig\s*\(\s*precheckCommit\(\s*mux\s*\)\s*\)',
  'precheckSig\s*\(\s*precheckFromBinding\s*\(\s*precheckCommit\(\s*mux\s*\)\s*\)\s*\)',
  'precheckFromBinding\s*\(\s*precheckSig\s*\(\s*precheckCommit\(\s*mux\s*\)\s*\)\s*\)'
)
foreach($p in $patterns){
  $code = [regex]::Replace($code, $p, 'muxWrapped')
}

# 3b) txPrecheckWith( <체인>, ... ) → txPrecheckWith(muxWrapped, ... )
$txPats = @(
  'txPrecheckWith\(\s*precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)\s*,',
  'txPrecheckWith\(\s*precheckSig\(\s*precheckFromBinding\(\s*precheckCommit\(\s*mux\s*\)\s*\)\s*\)\s*,',
  'txPrecheckWith\(\s*precheckFromBinding\(\s*precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)\s*\)\s*,'
)
foreach($p in $txPats){
  $code = [regex]::Replace($code, $p, 'txPrecheckWith(muxWrapped,')
}

# 4) 저장
Set-Content $router $code -Encoding UTF8
"router.go patched (scoped text replacements)"

# 5) 간단 검증 출력
Select-String $router -Pattern 'NewServeMux|muxWrapped|txPrecheckWith|precheckFromBinding|precheckCommit\(|precheckSig\('

# 6) 빌드 로그 캡처 (stdout+stderr)
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 200
