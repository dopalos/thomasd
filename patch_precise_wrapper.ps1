$ErrorActionPreference='Stop'

$router = "internal\rpc\router.go"
if(-not (Test-Path $router)){ throw "router.go not found" }

[string]$code = Get-Content $router -Raw

# 1) muxWrapped 보장: mux := http.NewServeMux() 바로 아래에 없으면 삽입
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

# 2) 정확 매치 치환들(함수 시그니처/인자 형태 보존)
$replCount = 0

# a) "return precheckSig(precheckCommit(mux))" -> "return muxWrapped"
$code = [regex]::Replace($code,
  "(?m)^(?<p>\s*return\s*)precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)(?<t>\s*)$",
  { param($m) $script:replCount++; $m.Groups['p'].Value + "muxWrapped" + $m.Groups['t'].Value })

# b) "txPrecheckWith(precheckSig(precheckCommit(mux)),"
$code = [regex]::Replace($code,
  "txPrecheckWith\(\s*precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)\s*,",
  { param($m) $script:replCount++; "txPrecheckWith(muxWrapped," })

# c) 다른 곳에 들어있을 수 있는 동일 패턴(괄호/개행 다양성 포함, 콤마/괄호 닫힘)
$code = [regex]::Replace($code,
  "precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)",
  { param($m) $script:replCount++; "muxWrapped" })

"REPLACED total: $replCount"

Set-Content $router $code -Encoding UTF8
"router.go patched (precise)"

# 3) 빌드 로그 파일로 캡처
go clean -cache | Out-Null
& go build -tags "cbor blake3" -o .\thomasd_dbg.exe .\cmd\thomasd *>&1 | Tee-Object build_full.log
if($LASTEXITCODE -ne 0){
  "RETRY with 'cbor' only" | Write-Host -ForegroundColor Yellow
  go clean -cache | Out-Null
  & go build -tags "cbor" -o .\thomasd_dbg.exe .\cmd\thomasd *>&1 | Tee-Object build_full.log
}
if($LASTEXITCODE -ne 0){
  Write-Host "`n--- build_full.log (tail) ---" -ForegroundColor Yellow
  Get-Content .\build_full.log -Tail 200
  throw "BUILD FAILED"
}
"BUILD OK"
