$ErrorActionPreference='Stop'

# --- paths ---
$router = "internal\rpc\router.go"
$bind   = "internal\rpc\precheck_binding.go"

# 0) 정지
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force

# 1) router.go 로드 & 치유
[string]$r = Get-Content $router -Raw

# 1-a) 잘못된 라인: mux := precheckFromBinding(mux) -> mux := http.NewServeMux()
$fixed1 = 0
$r = [regex]::Replace($r, '^\s*mux\s*:=\s*precheckFromBinding\(\s*mux\s*\)\s*$', { param($m) $script:fixed1++; '    mux := http.NewServeMux()' }, [System.Text.RegularExpressions.RegexOptions]::Multiline)
"FIX mux redeclare: $fixed1"

# 1-b) 혹시 남아있을 잘못된 struct 태그 복구 (json:"from" -> `json:"from"`)
$before = [regex]::Matches($r, '(?<!`)json:"from"(?!`)').Count
if($before -gt 0){
  $r = [regex]::Replace($r, '(?<!`)json:"from"(?!`)', '`json:"from"`')
  "FIX struct tag in router.go: $before"
}

# 1-c) 불필요한 주입 블럭 흔적 제거 (이전 마커들)
$removed = 0
$r = [regex]::Replace($r,
  '(?s)^[ \t]*//\s*===\s*begin:\s*from/pub binding check\s*\[[^\]]+\]\s*===.*?//\s*===\s*end:\s*from/pub binding check\s*\[[^\]]+\]\s*===\r?\n?',
  { param($m) $script:removed++; '' },
  [System.Text.RegularExpressions.RegexOptions]::Multiline
)
"REMOVED old injected blocks in router.go: $removed"

Set-Content $router $r -Encoding UTF8
"router.go saved"

# 2) precheck_binding.go: log import 보강
if(Test-Path $bind){
  [string]$b = Get-Content $bind -Raw
  # import (...) 내부에 "log" 추가
  $b = [regex]::Replace($b, '(?s)(\bimport\s*\(\s*)(.*?)\)', {
    param($m)
    $head=$m.Groups[1].Value; $body=$m.Groups[2].Value
    if($body -notmatch "(?m)^\s*`"log`"\s*$"){ $body += "`r`n`t`"log`"" }
    $head + $body + ")"
  }, 1)
  Set-Content $bind $b -Encoding UTF8
  "precheck_binding.go import(log) ensured"
}else{
  "WARN: precheck_binding.go not found"
}

# 3) 빌드
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 120

# 4) 성공 시 재기동 + 스모크
if((Get-Item .\thomasd_dbg.exe -ErrorAction SilentlyContinue)){
  $sk  = (Get-Content .\dev_sk.b64 -Raw).Trim()
  $pub = (& go run .\sig_util.go pub $sk).Trim()
  $env:THOMAS_PUBKEY_tho1alice = $pub
  $env:THOMAS_REQUIRE_COMMIT   = '1'
  $env:THOMAS_VERIFY_SIG       = '1'

  Start-Process .\thomasd_dbg.exe -RedirectStandardOutput thomasd_out.log -RedirectStandardError thomasd_err.log
  Start-Sleep 1

  .\smoketest_binding_focus.ps1

  "`n--- thomasd_err.log (tail 200) ---"
  Get-Content .\thomasd_err.log -Tail 200
  "`n--- thomasd_out.log (tail 200) ---"
  Get-Content .\thomasd_out.log -Tail 200
}else{
  Write-Host "Build failed. See build_full.log above." -ForegroundColor Yellow
}
