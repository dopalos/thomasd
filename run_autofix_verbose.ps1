$ErrorActionPreference = "Continue"
$ProgressPreference    = "SilentlyContinue"
[System.Net.ServicePointManager]::Expect100Continue = $false

# 1) 대상 스크립트 확인
try {
  $script = (Resolve-Path .\autofix_router_precheck.ps1 -ErrorAction Stop).Path
} catch {
  Write-Host "autofix_router_precheck.ps1 not found in current dir." -ForegroundColor Red
  exit 1
}

Write-Host "== RUN: $script ==" -ForegroundColor Cyan
Remove-Item .\autofix_run.log, .\go_build.stderr.txt, .\go_build.stdout.txt -ErrorAction Ignore

# 2) 중첩 PowerShell 없이 '직접' 실행 + 로그 수집
& $script 2>&1 | Tee-Object .\autofix_run.log
$code = $LASTEXITCODE
Write-Host ("[EXITCODE] " + $code)

# 3) 로그 꼬리
if (Test-Path .\go_build.stderr.txt) {
  Write-Host "`n--- build stderr (tail) ---" -ForegroundColor Yellow
  Get-Content .\go_build.stderr.txt -Tail 120
}
if (Test-Path .\autofix_run.log) {
  Write-Host "`n--- run log (tail) ---" -ForegroundColor Yellow
  Get-Content .\autofix_run.log -Tail 120
}

# 4) 상태 요약
Write-Host "`n== SUMMARY ==" -ForegroundColor Cyan
if (Test-Path ".\internal\rpc\router.go") {
  $hasCommit = Select-String -Path ".\internal\rpc\router.go" -Pattern "func precheckCommit" -Quiet
  $hasSig    = Select-String -Path ".\internal\rpc\router.go" -Pattern "func precheckSig"    -Quiet
  $wrapped   = Select-String -Path ".\internal\rpc\router.go" -Pattern "return\s+precheckSig\s*\(\s*precheckCommit" -Quiet

  $pc = if ($hasCommit) { "OK" } else { "MISSING" }
  $ps = if ($hasSig)    { "OK" } else { "MISSING" }
  $wr = if ($wrapped)   { "OK" } else { "NO" }

  Write-Host ("precheckCommit: " + $pc)
  Write-Host ("precheckSig   : " + $ps)
  Write-Host ("NewRouter wrap: " + $wr)
} else {
  Write-Host "router.go not found"
}
