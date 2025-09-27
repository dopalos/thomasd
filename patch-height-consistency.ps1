# patch-height-consistency.ps1
param([string]$RootPath = (Get-Location).Path)
$ErrorActionPreference = 'Stop'

$File = Join-Path $RootPath 'internal\app\engine.go'
if (!(Test-Path $File)) { throw "engine.go not found: $File" }

# 백업
$bak = "$File.bak-$(Get-Date -Format 'yyyyMMddHHmmss')"
Copy-Item -LiteralPath $File -Destination $bak -Force
Write-Host "[bak] $bak"

# 원본 로드
[string]$s = Get-Content -LiteralPath $File -Raw -Encoding UTF8

# (A) 이전 GUARD 핫픽스 주석 블록 제거: 주석~Lock() 사이를 Lock() 하나로 축약
$s = [regex]::Replace(
  $s,
  '(?s)//\s*----\s*GUARD.*?\r?\n\s*e\.mu\.Lock\(\)',
  'e.mu.Lock()'
)

# (B) CommitRound: from/to 를 항상 receipts 개수 기반으로 정규화
$pattern1 = 'from\s*:=\s*e\.lastCommittedHeight\s*\+\s*1\s*\r?\n\s*to\s*:=\s*e\.height'
$repl1 = ('rc := uint64(len(e.leaves))' + "`n" +
          'if e.height != rc { e.height = rc }' + "`n" +
          'from := e.lastCommittedHeight + 1' + "`n" +
          'to := e.height' + "`n")
if ($s -match $pattern1) {
  $s = [regex]::Replace($s, $pattern1, [System.Text.RegularExpressions.MatchEvaluator]{ param($m) $repl1 }, 1)
  Write-Host "[ok] normalized from/to"
} else { Write-Warning "[skip] pattern1 not found (already patched?)" }

# (C) 슬라이스 복사 안전화: copy(...) → append(... from-1:to ... )
$pattern2 = 'sub\s*:=\s*make\(\[\]\[\]\s*byte,\s*to-from\+1\)\s*\r?\n\s*copy\(sub,\s*e\.leaves\[from-1:to\]\)'
$repl2 = 'sub := append([][]byte(nil), e.leaves[from-1:to]...)'
if ($s -match $pattern2) {
  $s = [regex]::Replace($s, $pattern2, $repl2, 1)
  Write-Host "[ok] safe slice copy"
} else { Write-Warning "[skip] pattern2 not found (already patched?)" }

# (D) saveLedger: Height를 항상 len(leaves) 기준으로 저장
$pattern3 = 'Height\s*:\s*e\.height\s*,'
$repl3    = 'Height: uint64(len(e.leaves)),'
if ($s -match $pattern3) {
  $s = [regex]::Replace($s, $pattern3, $repl3, 1)
  Write-Host "[ok] saveLedger height(len(leaves))"
} else { Write-Warning "[skip] pattern3 not found (already patched?)" }

# (E) loadLedger: 저장값 대신 receipts 개수로 재계산
$pattern4 = 'e\.height\s*=\s*led\.Height'
$repl4    = 'e.height = uint64(len(led.Receipts))'
if ($s -match $pattern4) {
  $s = [regex]::Replace($s, $pattern4, $repl4, 1)
  Write-Host "[ok] loadLedger recompute height"
} else { Write-Warning "[skip] pattern4 not found (already patched?)" }

Set-Content -LiteralPath $File -Value $s -Encoding UTF8
Write-Host "[ok] wrote $File"
