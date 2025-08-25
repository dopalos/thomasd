$ErrorActionPreference='Stop'

# 0) 미들웨어 파일은 있는지 보증
$mid = "internal\rpc\precheck_binding.go"
if(-not (Test-Path $mid)){ throw "missing $mid (expected to exist)" }

# 1) router.go 읽기
$router = "internal\rpc\router.go"
if(-not (Test-Path $router)){ throw "router.go not found" }
[string]$code = Get-Content $router -Raw

# 2) mux 선언 라인 찾기
$vm = [regex]::Match($code,'(?m)^(?<indent>\s*)mux\s*:=\s*http\.NewServeMux\(\)\s*$')
if(-not $vm.Success){ throw "mux := http.NewServeMux() not found" }
$indent = $vm.Groups['indent'].Value

# 3) 선언 직후에 wrapper 변수 삽입 (없을 때만)
if($code -notmatch '(?m)^\s*muxWrapped\s*:=' ){
  $insert = "`r`n$($indent)muxWrapped := precheckSig(precheckFromBinding(precheckCommit(mux)))"
  $code = $code.Insert($vm.Index + $vm.Length, $insert)
  "INSERT: muxWrapped := precheckSig(precheckFromBinding(precheckCommit(mux)))"
}else{
  "SKIP insert: muxWrapped already exists"
}

# 4) 파일 내 모든 체인 호출을 muxWrapped로 치환
#    - 이전/이후 어떤 상태든 한 방에 정리
$before1 = ([regex]::Matches($code,'precheckSig\(\s*precheckFromBinding\(\s*precheckCommit\(\s*mux\s*\)\s*\)\s*\)')).Count
$code = [regex]::Replace($code,'precheckSig\(\s*precheckFromBinding\(\s*precheckCommit\(\s*mux\s*\)\s*\)\s*\)','muxWrapped')

$before2 = ([regex]::Matches($code,'precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)')).Count
$code = [regex]::Replace($code,'precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)','muxWrapped')

"REPLACED calls: with-binding=$before1, no-binding=$before2"

# 5) 저장
Set-Content $router $code -Encoding UTF8
"router.go patched"

# 6) 빌드 (로그 저장)
go clean -cache | Out-Null
& go build -tags "cbor blake3" -o .\thomasd_dbg.exe .\cmd\thomasd *>&1 | Tee-Object build_full.log
if($LASTEXITCODE -ne 0){
  "RETRY with 'cbor' only" | Write-Host -ForegroundColor Yellow
  go clean -cache | Out-Null
  & go build -tags "cbor" -o .\thomasd_dbg.exe .\cmd\thomasd *>&1 | Tee-Object build_full.log
}
if($LASTEXITCODE -ne 0){
  Write-Host "`n--- build_full.log (tail) ---" -ForegroundColor Yellow
  Get-Content .\build_full.log -Tail 120
  throw "BUILD FAILED"
}
"BUILD OK"
