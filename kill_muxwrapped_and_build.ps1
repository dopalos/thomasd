$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"
[string]$code = Get-Content $router -Raw
$chain = 'precheckFromBinding(precheckSig(precheckCommit(mux)))'

# 0) 선언/대입 라인 제거(미사용 변수 방지)
$code = [regex]::Replace($code, '(?m)^\s*muxWrapped\s*:.*\r?\n', '')

# 1) 모든 남은 토큰을 체인으로 치환
$code = [regex]::Replace($code, '\bmuxWrapped\b', $chain)

# 2) 혹시 단독 'return mux'가 남아있으면 체인으로
$code = [regex]::Replace($code, '(?m)^(?<lead>\s*return\s+)mux(\s*)$', { param($m) $m.Groups['lead'].Value + $chain + $m.Groups[2].Value })

Set-Content $router $code -Encoding UTF8
"router.go patched (muxWrapped fully inlined)"

# 빌드 로그 캡처
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){ cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1" }

"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 200
