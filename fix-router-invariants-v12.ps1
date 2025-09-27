# fix-router-invariants-v12.ps1
param([string]$Root = (Get-Location).Path)
$ErrorActionPreference='Stop'

$File = Join-Path $Root 'internal\rpc\router.go'
if(!(Test-Path $File)){ throw "router.go not found: $File" }

# 0) 백업
$bak = "$File.bak-$(Get-Date -Format yyyyMMddHHmmss)"
Copy-Item $File $bak -Force
Write-Host "[bak] $bak"

# 1) 소스 로드
[string]$s = Get-Content -LiteralPath $File -Raw -Encoding UTF8

# 2) import 보강 (json, time)
function Add-Imports {
  param([string]$code, [string[]]$pkgs)
  $missing = @(); foreach($p in $pkgs){ if($code -notmatch [regex]::Escape($p)){ $missing += $p } }
  if($missing.Count -eq 0){ return $code }
  if($code -match 'import\s*\('){
    return [regex]::Replace($code,'import\s*\(\s*', {
      param($m)
      $ins = ($missing | ForEach-Object { "`t$_`n" }) -join ''
      "import (`n$ins"
    }, 1)
  } else {
    $pkgLine = [regex]::Match($code,'^\s*package\s+[A-Za-z_][A-Za-z0-9_]*\s*$', 'Multiline')
    if(-not $pkgLine.Success){ return $code }
    $ins = "import (`n" + (($missing | ForEach-Object { "`t$_`n" }) -join '') + ")`n"
    return $code.Insert($pkgLine.Index + $pkgLine.Length, "`r`n$ins")
  }
}
$s = Add-Imports $s @('"encoding/json"','"time"')

# 3) 특정 HandleFunc 블록 제거 도우미
function Remove-HandleBlock {
  param([string]$code, [string]$pathLit) # e.g. "/health"
  $pattern = '([A-Za-z_][A-Za-z0-9_]*)\s*\.\s*HandleFunc\(\s*"' + [regex]::Escape($pathLit) + '"\s*,\s*func\s*\('
  while($true){
    $m = [regex]::Match($code, $pattern, 'Singleline')
    if(-not $m.Success){ break }
    # HandleFunc( 의 '(' 위치
    $openCall = $code.IndexOf('(', $m.Index)
    if($openCall -lt 0){ break }

    # 괄호/중괄호 균형으로 끝 ) 찾기
    $paren=0;$brace=0;$inStr=$false;$inRune=$false;$inRaw=$false;$esc=$false;$endCall=-1
    for($i=$openCall; $i -lt $code.Length; $i++){
      $ch = $code[$i]
      if($inRaw){ if($ch -eq '`'){$inRaw=$false}; continue }
      if($inStr){ if($esc){$esc=$false; continue}; if($ch -eq '\'){$esc=$true; continue}; if($ch -eq '"'){$inStr=$false}; continue }
      if($inRune){ if($esc){$esc=$false; continue}; if($ch -eq '\'){$esc=$true; continue}; if($ch -eq "'"){$inRune=$false}; continue }
      if($ch -eq '`'){$inRaw=$true; continue}
      if($ch -eq '"'){$inStr=$true; continue}
      if($ch -eq "'"){$inRune=$true; continue}
      if($ch -eq '('){$paren++; continue}
      if($ch -eq ')'){
        $paren--; if($paren -eq 0 -and $brace -eq 0){ $endCall=$i; break }; continue
      }
      if($ch -eq '{'){$brace++; continue}
      if($ch -eq '}'){$brace--; continue}
    }
    if($endCall -lt 0){ break }

    # 앞 공백 포함 덩어리 제거
    $lead = $m.Index
    while($lead -gt 0 -and [char]::IsWhiteSpace($code[$lead-1])){ $lead-- }
    $code = $code.Remove($lead, ($endCall+1)-$lead)

    # 혹시 남은 고아 '{' 제거
    if($code.Substring($lead, [Math]::Min(40, $code.Length-$lead)) -match '^\{\s*'){
      $code = $code.Remove($lead,1)
    }
  }
  return $code
}

# 4) 깨진 /health, /health/invariants 제거
$s = Remove-HandleBlock $s '/health/invariants'
$s = Remove-HandleBlock $s '/health'

# 5) "/policy" 핸들러 뒤에 삽입 위치/식별자(muxVar) 찾기
$pm = [regex]::Match($s,'([A-Za-z_][A-Za-z0-9_]*)\s*\.\s*HandleFunc\(\s*"/policy"', 'Singleline')
if(-not $pm.Success){ throw 'cannot find HandleFunc("/policy") as anchor' }
$muxVar = $pm.Groups[1].Value

# "/policy" 블록 끝 다음 위치 계산
# 다시 한 번 블록 끝을 균형으로 탐색
$start = $pm.Index
$openCall = $s.IndexOf('(', $start)
$paren=0;$brace=0;$inStr=$false;$inRune=$false;$inRaw=$false;$esc=$false;$endCall=-1
for($i=$openCall; $i -lt $s.Length; $i++){
  $ch = $s[$i]
  if($inRaw){ if($ch -eq '`'){$inRaw=$false}; continue }
  if($inStr){ if($esc){$esc=$false; continue}; if($ch -eq '\'){$esc=$true; continue}; if($ch -eq '"'){$inStr=$false}; continue }
  if($inRune){ if($esc){$esc=$false; continue}; if($ch -eq '\'){$esc=$true; continue}; if($ch -eq "'"){$inRune=$false}; continue }
  if($ch -eq '`'){$inRaw=$true; continue}
  if($ch -eq '"'){$inStr=$true; continue}
  if($ch -eq "'"){$inRune=$true; continue}
  if($ch -eq '('){$paren++; continue}
  if($ch -eq ')'){ $paren--; if($paren -eq 0 -and $brace -eq 0){ $endCall=$i; break }; continue }
  if($ch -eq '{'){$brace++; continue}
  if($ch -eq '}'){$brace--; continue}
}
if($endCall -lt 0){ throw 'cannot find end of HandleFunc("/policy")' }
$insertPos = $endCall + 1

# 6) 올바른 핸들러 스니펫 구성 & 삽입
$snippet = @"
`r
    $muxVar.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{
            "status":  "ok",
            "time_utc": time.Now().UTC().Format(time.RFC3339),
        })
    })
    $muxVar.HandleFunc("/health/invariants", func(w http.ResponseWriter, r *http.Request) {
        h := e.CurrentHeight()
        rc := uint64(e.ReceiptCount())
        ok := (h == rc)
        reason := ""
        if !ok { reason = "height_mismatch" }
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{
            "ok": ok,
            "reason": reason,
            "height": h,
            "receipts_count": rc,
            "time_utc": time.Now().UTC().Format(time.RFC3339),
        })
    })
`r
"@ -replace '\$muxVar', $muxVar

$s = $s.Insert($insertPos, $snippet)

# 7) 저장
Set-Content -LiteralPath $File -Value $s -Encoding UTF8
Write-Host "[ok] router.go patched (mux=$muxVar)"

# 8) 빌드 & 재기동 & 확인
Push-Location $Root
cmd /c "go build -o .\bin\thomasd.exe .\cmd\thomasd > build-router.out.txt 2> build-router.err.txt & exit /b %errorlevel%"
$code = $LASTEXITCODE
Pop-Location
if($code -ne 0){
  Write-Host "== build errors =="
  Get-Content (Join-Path $Root 'build-router.err.txt') -Tail 200
  throw "go build failed"
}

Get-Process thomasd -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Process -NoNewWindow (Join-Path $Root 'bin\thomasd.exe')
Start-Sleep 1

$BASE='http://127.0.0.1:8081'
"`nGET $BASE/health =>"
Invoke-RestMethod "$BASE/health" | Format-List
"`nGET $BASE/health/invariants =>"
Invoke-RestMethod "$BASE/health/invariants" | Format-List
