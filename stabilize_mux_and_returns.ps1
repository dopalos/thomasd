$ErrorActionPreference='Stop'

$router = "internal\rpc\router.go"

# 0) 노드 중지
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force

# 1) 소스 로드
[string]$src = Get-Content $router -Raw

# 2) (안전) 'mux := precheckFromBinding(mux)' 는 모두 'mux = precheckFromBinding(mux)' 로 교체
$src = [regex]::Replace($src, '^\s*mux\s*:=\s*precheckFromBinding\(\s*mux\s*\)\s*$', '    mux = precheckFromBinding(mux)', 'Multiline')

# 3) 'mux := http.NewServeMux()'를 일단 모두 재할당 형태로 바꿈 → 첫 번째만 다시 선언으로 되돌림
$src = [regex]::Replace($src, '^\s*mux\s*:=\s*http\.NewServeMux\(\)\s*$', '    mux = http.NewServeMux()', 'Multiline')
# 첫 번째 한 건만 := 로 복구
$src = [regex]::Replace($src, '^\s*mux\s*=\s*http\.NewServeMux\(\)\s*$', '    mux := http.NewServeMux()', 'Multiline', 1)

# 4) return 라인에서 프리체크를 mux 자체에 적용하도록 원복
#    precheckSig(precheckFromBinding(precheckCommit(mux))) -> precheckSig(precheckCommit(mux))
$src = [regex]::Replace($src, 'precheckSig\s*\(\s*precheckFromBinding\s*\(\s*precheckCommit\s*\(\s*mux\s*\)\s*\)\s*\)', 'precheckSig(precheckCommit(mux))')

# 5) (혹시 남았을) json:"from" 태그 백틱 복구
$src = [regex]::Replace($src, '(?<!`)json:"from"(?!`)', '`json:"from"`')

# 6) mux 선언 직후에 딱 한 번 미들웨어를 인라인 적용 (없으면 삽입)
#    패턴: 첫 mux := http.NewServeMux() 뒤에 같은 블록 안에서 precheckFromBinding 적용이 없으면 삽입
if($src -notmatch 'mux\s*=\s*precheckFromBinding\s*\(\s*mux\s*\)'){
  $src = [regex]::Replace($src,
    '(?s)(^\s*mux\s*:=\s*http\.NewServeMux\(\)\s*$)',
    '$1' + "`r`n" + '    mux = precheckFromBinding(mux)',
    [System.Text.RegularExpressions.RegexOptions]::Multiline,
    1
  )
}

# 7) 저장
Set-Content $router $src -Encoding UTF8
"router.go normalized (single :=, rest =) and returns restored"

# 8) 빌드
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 120

# 9) 성공 시 재기동 + 바인딩 스모크
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
