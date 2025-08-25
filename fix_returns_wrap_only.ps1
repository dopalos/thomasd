$ErrorActionPreference='Stop'

$router = "internal\rpc\router.go"

# 0) 정지
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force

# 1) router.go 라인 로드
$lines = Get-Content $router

# 2) 잘못 삽입된 "mux = precheckFromBinding(mux)" 라인 제거 (모두)
$del=0
$keep = New-Object System.Collections.Generic.List[string]
foreach($ln in $lines){
  if($ln -match '^\s*mux\s*=\s*precheckFromBinding\s*\(\s*mux\s*\)\s*$'){ $del++ } else { $keep.Add($ln) }
}
"REMOVED inline wrap lines: $del"

# 3) 파일 저장(1차)
Set-Content $router $keep -Encoding UTF8

# 4) 반환부 치환 (정밀-패턴)
[string]$src = Get-Content $router -Raw

# 4-a) 단독 return 형태
$patReturn = '(?m)^\s*return\s+precheckSig\s*\(\s*precheckCommit\s*\(\s*mux\s*\)\s*\)\s*$'
$src = [regex]::Replace($src, $patReturn, '    return precheckSig(precheckFromBinding(precheckCommit(mux)))')

# 4-b) txPrecheckWith(...) 인자 형태
$patTx = 'txPrecheckWith\s*\(\s*precheckSig\s*\(\s*precheckCommit\s*\(\s*mux\s*\)\s*\)\s*,'
$src = [regex]::Replace($src, $patTx, 'txPrecheckWith(precheckSig(precheckFromBinding(precheckCommit(mux))),')

# (안전) 태그 백틱 복구
$src = $src -replace '(?<!`)json:"from"(?!`)', '`json:"from"`'

Set-Content $router $src -Encoding UTF8
"router.go patched at returns only"

# 5) 빌드
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 120

# 6) 성공 시 재기동 + 바인딩 스모크
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
