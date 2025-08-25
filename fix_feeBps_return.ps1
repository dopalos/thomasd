$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"

# 0) 노드 중지
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force

# 1) 파일 라인 로드
$lines = Get-Content $router

# 2) feeBps 블록 안의 "return mux"만 찾아서 "return 0"으로 교체
$start = ($lines | Select-String -Pattern '^\s*var\s+feeBps\s*=\s*func\(\s*\)\s*int\s*\{' -List).LineNumber
if(-not $start){ throw "feeBps block not found" }
$startIdx = $start - 1

# 블록 끝(해당 func의 닫는 '}()')까지 탐색
$endIdx = $null
for($i=$startIdx; $i -lt $lines.Count; $i++){
  if($lines[$i] -match '^\s*\}\s*\(\s*\)\s*$'){ $endIdx = $i; break }
}
if($endIdx -eq $null){ throw "feeBps block end not found" }

# 블록 내부에서 'return mux' 교체
$replaced = $false
for($i=$startIdx; $i -le $endIdx; $i++){
  if($lines[$i] -match '^\s*return\s+mux\s*$'){
    $indent = ($lines[$i] -replace '(return.*)$','')
    $lines[$i] = "${indent}return 0"
    $replaced = $true
    break
  }
}
if(-not $replaced){ throw "'return mux' inside feeBps block not found" }

# 3) 저장
Set-Content $router $lines -Encoding UTF8
"router.go fixed: replaced 'return mux' -> 'return 0' in feeBps block"

# 4) 빌드
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 80

# 5) 성공 시 재기동 + 바인딩 스모크
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
