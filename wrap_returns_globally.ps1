$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"

# 0) 디버그 바이너리 중지
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force

# 1) 소스 로드
[string]$src = Get-Content $router -Raw

# 2) 이미 바인딩이 들어간 호출은 건너뛰고, 없는 것만 래핑
# return precheckSig(precheckCommit(VAR))  ->  return precheckSig(precheckFromBinding(precheckCommit(VAR)))
$patReturn = '(?m)^(?<lead>\s*return\s+)precheckSig\s*\(\s*(?!precheckFromBinding\s*\()\s*precheckCommit\s*\(\s*(?<v>[A-Za-z_]\w*)\s*\)\s*\)\s*$'
$replReturn = '${lead}precheckSig(precheckFromBinding(precheckCommit(${v})))'
$src2 = [regex]::Replace($src, $patReturn, $replReturn)
$cntReturn = ([regex]::Matches($src2, '\[unreachable\]')).Count # dummy 초기화용
# 위 카운트는 Replace 전후 비교로
$cntReturn = (([regex]::Matches($src,    $patReturn)).Count)

# txPrecheckWith(precheckSig(precheckCommit(VAR)),  ->  txPrecheckWith(precheckSig(precheckFromBinding(precheckCommit(VAR))), 
$patTx = 'txPrecheckWith\s*\(\s*precheckSig\s*\(\s*(?!precheckFromBinding\s*\()\s*precheckCommit\s*\(\s*(?<v>[A-Za-z_]\w*)\s*\)\s*\)\s*,'
$src3 = [regex]::Replace($src2, $patTx, { param($m)
  $v = $m.Groups['v'].Value
  "txPrecheckWith(precheckSig(precheckFromBinding(precheckCommit($v))),"
})
$cntTx = (([regex]::Matches($src2, $patTx)).Count)

# 변경 여부 체크
if(($src3 -ne $src)){
  Set-Content $router $src3 -Encoding UTF8
  "router.go saved (wrapped): returns=$cntReturn, txPrecheckWith=$cntTx"
}else{
  "No wrapable patterns found (returns=0, txPrecheckWith=0). Showing candidates..."
  # 힌트용으로 precheckSig(precheckCommit(…)) 라인 40개만 출력
  ($src -split "`r?`n") |
    Where-Object { $_ -match 'precheckSig\s*\(\s*precheckCommit\s*\(' } |
    Select-Object -First 40 |
    ForEach-Object { "  >> $_" }
}

# 3) 빌드
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 120

# 4) 기동 + 바인딩/시그 테스트
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
