$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"

# 0) 디버그 바이너리 중지
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force

# 1) 소스 로드
[string]$src = Get-Content $router -Raw
$orig = $src

# 2) txPrecheckWith(mux, ...)  ->  txPrecheckWith(precheckSig(precheckFromBinding(precheckCommit(mux))), ...)
$patTx = 'txPrecheckWith\s*\(\s*mux\s*,'
$cntTxBefore = ([regex]::Matches($src, $patTx)).Count
$src = [regex]::Replace($src, $patTx, "txPrecheckWith(precheckSig(precheckFromBinding(precheckCommit(mux))),")
$cntTxAfter  = ([regex]::Matches($src, 'txPrecheckWith\s*\(\s*precheckSig\s*\(\s*precheckFromBinding')).Count

# 3) 혹시 남아있을 수 있는 반환형:  return mux  ->  return precheckSig(precheckFromBinding(precheckCommit(mux)))
$cntRetBefore = ([regex]::Matches($src, '(?m)^\s*return\s+mux\s*$')).Count
$src = [regex]::Replace($src, '(?m)^\s*(return)\s+mux\s*$', '${1} precheckSig(precheckFromBinding(precheckCommit(mux)))')
$cntRetAfter  = ([regex]::Matches($src, '(?m)precheckSig\s*\(\s*precheckFromBinding\s*\(')).Count

if($src -ne $orig){
  Set-Content $router $src -Encoding UTF8
  "router.go saved (txPrecheckWith wrapped: $cntTxBefore → $cntTxAfter, return mux wrapped: $cntRetBefore)"
}else{
  "No changes written (patterns not found). Showing first 3 txPrecheckWith lines for reference:"
  ($src -split "`r?`n" | Where-Object { $_ -match 'txPrecheckWith\(' } | Select-Object -First 3) | ForEach-Object { "  >> $_" }
}

# 4) 빌드
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){ cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1" }
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 120

# 5) 기동 + invalid-sig 확인
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
