# fix-router-invariants-v11.ps1
param([string]$Root = (Get-Location).Path)
$ErrorActionPreference='Stop'

$File = Join-Path $Root 'internal\rpc\router.go'
if(!(Test-Path $File)){ throw "router.go not found: $File" }

# --- 0) 가장 최근 백업으로 '빌드 가능한' 버전을 찾는다 -------------------------
$backupDir = Join-Path $Root 'internal\rpc'
$bakList = Get-ChildItem $backupDir -Filter 'router.go.bak-*' | Sort-Object LastWriteTime -Descending
$baselineRestored=$false

# 후보: 백업들 + (마지막에) 현재 파일
$targets = @($bakList) + ,(Get-Item $File)

foreach($cand in $targets){
  Copy-Item $cand.FullName $File -Force
  Push-Location $Root
  cmd /c "go build -o .\bin\thomasd.exe .\cmd\thomasd > nul 2> nul & exit /b %errorlevel%"
  $code=$LASTEXITCODE
  Pop-Location
  if($code -eq 0){ 
    Write-Host "[baseline] restored from $($cand.Name)"
    $baselineRestored=$true
    break
  }
}
if(-not $baselineRestored){ throw "no buildable baseline of router.go found (even backups failed)" }

# --- 1) 소스 로드 --------------------------------------------------------------
[string]$src = Get-Content -LiteralPath $File -Raw -Encoding UTF8

function Add-Imports {
  param([string]$code, [string[]]$pkgs)
  $add = @()
  foreach($p in $pkgs){ if($code -notmatch [regex]::Escape($p)){ $add += $p } }
  if($add.Count -eq 0){ return $code }
  if($code -match 'import\s*\('){
    return [regex]::Replace($code,'import\s*\(\s*', {
      param($m)
      $ins = ($add | ForEach-Object { "`t$_`n" }) -join ''
      "import (`n$ins"
    }, 1)
  } else {
    $pkgLine = [regex]::Match($code,'^\s*package\s+[A-Za-z_][A-Za-z0-9_]*\s*$', 'Multiline')
    if(-not $pkgLine.Success){ return $code }
    $ins = "import (`n" + (($add | ForEach-Object { "`t$_`n" }) -join '') + ")`n"
    return $code.Insert($pkgLine.Index + $pkgLine.Length, "`r`n$ins")
  }
}

# import 보강
$src = Add-Imports $src @('"encoding/json"','"time"')

# --- 2) NewRouter 함수 경계/반환식 파악 ----------------------------------------
$fn = [regex]::Match($src,'func\s+NewRouter\s*\([^)]*\)\s*\*http\.ServeMux\s*\{')
if(-not $fn.Success){ throw "cannot find NewRouter signature" }
$startBrace = $fn.Index + $fn.Length - 1

# 본문 끝 '}' 위치 스캔
$depth=0;$inStr=$false;$inRune=$false;$inRaw=$false;$esc=$false
$endBrace=-1
for($i=$startBrace; $i -lt $src.Length; $i++){
  $ch = $src[$i]
  if($inRaw){ if($ch -eq '`'){$inRaw=$false}; continue }
  if($inStr){ if($esc){$esc=$false; continue}; if($ch -eq '\'){$esc=$true; continue}; if($ch -eq '"'){$inStr=$false}; continue }
  if($inRune){ if($esc){$esc=$false; continue}; if($ch -eq '\'){$esc=$true; continue}; if($ch -eq "'"){$inRune=$false}; continue }
  if($ch -eq '`'){$inRaw=$true; continue}
  if($ch -eq '"'){$inStr=$true; continue}
  if($ch -eq "'"){$inRune=$true; continue}
  if($ch -eq '{'){ $depth++; continue }
  if($ch -eq '}'){
    if($depth -eq 0){ $endBrace=$i; break }
    $depth--; continue
  }
}
if($endBrace -lt 0){ throw "brace scan failed for NewRouter" }

$bodyStart = $startBrace + 1
$bodyEnd   = $endBrace - 1
$body = $src.Substring($bodyStart, $bodyEnd - $bodyStart + 1)

# mux 식별자: 마지막 "return <ident>"
$rets = [regex]::Matches($body,'return\s+([A-Za-z_][A-Za-z0-9_]*)','Multiline')
if($rets.Count -eq 0){ throw "cannot find any return <ident> in NewRouter" }
$muxVar = $rets[$rets.Count-1].Groups[1].Value

# --- 3) 기존 /health, /health/invariants 블록 제거(깨진 조각 포함) -------------
function RemoveHandler {
  param([string]$code, [string]$ident, [string]$pathStr)
  $startPat = [regex]::Escape($ident) + '\s*\.\s*HandleFunc\(\s*"' + [regex]::Escape($pathStr) + '"\s*,\s*func\s*\('
  $m = [regex]::Match($code, $startPat, 'Singleline')
  if(-not $m.Success){ return $code }

  # 함수 리터럴의 시작 '(' 위치부터 괄호/중괄호 균형으로 끝까지 제거
  $openCall = $code.IndexOf('(', $m.Index)  # HandleFunc( 의 '('
  if($openCall -lt 0){ return $code }
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
      $paren--;
      if($paren -eq 0 -and $brace -eq 0){ $endCall=$i; break }
      continue
    }
    if($ch -eq '{'){$brace++; continue}
    if($ch -eq '}'){$brace--; continue}
  }
  if($endCall -lt 0){ return $code }

  # 앞의 공백 포함해서 덩어리 제거
  $lead = $m.Index
  while($lead -gt 0 -and [char]::IsWhiteSpace($code[$lead-1])){ $lead-- }
  $new = $code.Remove($lead, ($endCall+1) - $lead)

  # 바로 뒤에 남은 고아 '{' 한 줄 제거 (이전 패치가 남긴 조각 방지)
  $after = $new.Substring($lead, [Math]::Min(80, $new.Length - $lead))
  if($after -match '^\{\s*'){
    $new = $new.Remove($lead,1)
  }
  return $new
}

$body = RemoveHandler $body $muxVar '/health/invariants'
$body = RemoveHandler $body $muxVar '/health'

# --- 4) 정상 핸들러 삽입: 마지막 return 직전에 붙이기 -------------------------
$insertAt = $body.LastIndexOf("return $muxVar")
if($insertAt -lt 0){ throw "cannot find 'return $muxVar' in NewRouter" }

$snippet = @"
    $muxVar.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{
            "status": "ok",
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
"@

$bodyNew = $body.Insert($insertAt, "`r`n" + ($snippet -replace '\$muxVar',$muxVar) + "`r`n")
$srcNew = $src.Substring(0,$bodyStart) + $bodyNew + $src.Substring($bodyEnd+1)

# 백업 후 저장
$bakNow = "$File.bak-$(Get-Date -Format 'yyyyMMddHHmmss')"
Copy-Item $File $bakNow -Force
Set-Content -LiteralPath $File -Value $srcNew -Encoding UTF8
Write-Host "[ok] router.go updated (bak=$bakNow) - mux=$muxVar"

# --- 5) 빌드 & 확인 ------------------------------------------------------------
Push-Location $Root
cmd /c "go build -o .\bin\thomasd.exe .\cmd\thomasd > build-router.out.txt 2> build-router.err.txt & exit /b %errorlevel%"
$code=$LASTEXITCODE
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
"GET $BASE/health =>"
Invoke-RestMethod "$BASE/health" | Format-List
"GET $BASE/health/invariants =>"
Invoke-RestMethod "$BASE/health/invariants" | Format-List
