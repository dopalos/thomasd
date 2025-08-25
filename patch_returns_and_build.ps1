$ErrorActionPreference='Stop'

$router = "internal\rpc\router.go"
if(-not (Test-Path $router)){ throw "router.go not found" }

# 파일 읽기
[string]$code = Get-Content $router -Raw

# a) muxWrapped 선언 보장 (이미 있으면 스킵)
$vm = [regex]::Match($code,'(?m)^(?<indent>\s*)mux\s*:=\s*http\.NewServeMux\(\)\s*$')
if(-not $vm.Success){ throw "mux := http.NewServeMux() not found" }
$indent = $vm.Groups['indent'].Value
if($code -notmatch '(?m)^\s*muxWrapped\s*:=' ){
  $insert = "`r`n$($indent)muxWrapped := precheckSig(precheckFromBinding(precheckCommit(mux)))"
  $code = $code.Insert($vm.Index + $vm.Length, $insert)
  "INSERT: muxWrapped"
}else{
  "SKIP: muxWrapped already present"
}

# b) return 라인 안의 호출만 교체 (형태 유연 대응)
$repl = 0
$code = [regex]::Replace(
  $code,
  "(?m)^(?<lead>\s*return\s+.*)\bprecheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)(?<trail>.*)$",
  { param($m) $script:repl++; $m.Groups['lead'].Value + "muxWrapped" + $m.Groups['trail'].Value }
)

# c) txPrecheckWith( precheckSig(precheckCommit(mux)), ... ) 형태 교체
$code = [regex]::Replace(
  $code,
  "txPrecheckWith\(\s*precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)\s*,",
  { param($m) $script:repl++; "txPrecheckWith(muxWrapped," }
)

"REPLACED in returns/calls: $repl"

# 저장
Set-Content $router $code -Encoding UTF8
"router.go patched"

# d) 빌드 로그를 파일로 캡처
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
