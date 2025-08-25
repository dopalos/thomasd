$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"

# 0) 정지
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force

# 1) 소스 로드
[string[]]$lines = Get-Content $router

# 2) 파일 내 첫 ServeMux 변수 선언 찾기
$varName = $null; $muxLine = $null
for($i=0; $i -lt $lines.Count; $i++){
  if($lines[$i] -match '^\s*([A-Za-z_]\w*)\s*:=\s*http\.NewServeMux\(\)\s*$'){
    $varName = $Matches[1]; $muxLine = $i; break
  }
}
if(-not $varName){ throw "No 'X := http.NewServeMux()' found in $router" }

# 3) 해당 선언이 포함된 함수 범위 계산 (func 시그니처~중괄호 짝 맞춰 끝까지)
#    시그니처 찾기
$funcStart = $null
for($j=$muxLine; $j -ge 0; $j--){
  if($lines[$j] -match '^\s*func\s'){ $funcStart=$j; break }
}
if(-not $funcStart){ throw "Owning function for mux not found" }
#    본문 시작 '{' 라인
$bodyOpen = $null
for($k=$funcStart; $k -lt $lines.Count; $k++){
  if($lines[$k] -match '\{'){ $bodyOpen=$k; break }
}
if(-not $bodyOpen){ throw "Opening brace not found" }
#    본문 끝 계산 (brace balance)
$bal=0; $funcEnd=$null
for($m=$bodyOpen; $m -lt $lines.Count; $m++){
  $bal += ([regex]::Matches($lines[$m], '\{').Count)
  $bal -= ([regex]::Matches($lines[$m], '\}').Count)
  if($bal -eq 0){ $funcEnd=$m; break }
}
if(-not $funcEnd){ throw "Function end not found" }

# 4) 함수 범위 내에서 반환/체인 래핑 강제
$replaced1=0; $replaced2=0
for($n=$funcStart; $n -le $funcEnd; $n++){
  # pattern A: return precheckSig(precheckCommit(<var>))
  $patA = 'return\s+precheckSig\s*\(\s*precheckCommit\s*\(\s*'+[regex]::Escape($varName)+'\s*\)\s*\)\s*$'
  if($lines[$n] -match $patA){
    $lines[$n] = $lines[$n] -replace 'precheckSig\s*\(\s*precheckCommit\s*\(\s*'+[regex]::Escape($varName)+'\s*\)\s*\)',
                                     "precheckSig(precheckFromBinding(precheckCommit($varName)))"
    $replaced1++
  }
  # pattern B: txPrecheckWith(precheckSig(precheckCommit(<var>)),   ...)
  $patB = 'txPrecheckWith\s*\(\s*precheckSig\s*\(\s*precheckCommit\s*\(\s*'+[regex]::Escape($varName)+'\s*\)\s*\)\s*,'
  if($lines[$n] -match $patB){
    $lines[$n] = $lines[$n] -replace $patB,
                 "txPrecheckWith(precheckSig(precheckFromBinding(precheckCommit($varName))),"
    $replaced2++
  }
}
"WRAPPED in mux-builder function: returns=$replaced1, txPrecheckWith=$replaced2 (var=$varName, lines $($funcStart+1)-$($funcEnd+1))"

# 5) 저장
Set-Content $router $lines -Encoding UTF8
"router.go saved"

# 6) 빌드
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){ cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1" }
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 120

# 7) 기동 + invalid-sig 확인
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
