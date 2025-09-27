# fix-router-invariants-v14.ps1
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

# 2) import 보강
function Add-Imports {
  param([string]$code, [string[]]$pkgs)
  $need = @()
  foreach($p in $pkgs){ if($code -notmatch [regex]::Escape($p)){ $need += $p } }
  if($need.Count -eq 0){ return $code }

  if($code -match 'import\s*\('){
    return [regex]::Replace($code,'import\s*\(\s*', {
      param($m)
      $ins = ($need | ForEach-Object { "`t$_`r`n" }) -join ''
      "import (`r`n$ins"
    }, 1)
  } else {
    $pkgLine = [regex]::Match($code,'^\s*package\s+[A-Za-z_][A-Za-z0-9_]*\s*$', 'Multiline')
    if(-not $pkgLine.Success){ return $code }
    $ins = "import (`r`n" + (($need | ForEach-Object { "`t$_`r`n" }) -join '') + ")`r`n"
    return $code.Insert($pkgLine.Index + $pkgLine.Length, "`r`n$ins")
  }
}
$s = Add-Imports $s @('"encoding/json"','"time"')

# 3) NewRouter 시그니처/본문 범위 찾기
$fn = [regex]::Match($s,'func\s+NewRouter\s*\([^)]*\)\s*\*http\.ServeMux\s*\{','Singleline')
if(-not $fn.Success){ throw "cannot find NewRouter signature" }
$bodyStart = $s.IndexOf('{', $fn.Index)

# 균형 스캔
$depth=0;$i=$bodyStart; $inStr=$false;$inRune=$false;$inRaw=$false;$esc=$false;$inLineCmt=$false;$inBlkCmt=$false
for(; $i -lt $s.Length; $i++){
  $ch = $s[$i]
  $nxt = if($i+1 -lt $s.Length){ $s[$i+1] } else { [char]0 }

  if($inLineCmt){ if($ch -eq "`n"){ $inLineCmt=$false }; continue }
  if($inBlkCmt){ if($ch -eq '*' -and $nxt -eq '/'){ $inBlkCmt=$false; $i++; }; continue }
  if($inRaw){ if($ch -eq '`'){$inRaw=$false}; continue }
  if($inStr){ if($esc){$esc=$false; continue}; if($ch -eq '\'){$esc=$true; continue}; if($ch -eq '"'){$inStr=$false}; continue }
  if($inRune){ if($esc){$esc=$false; continue}; if($ch -eq '\'){$esc=$true; continue}; if($ch -eq "'"){$inRune=$false}; continue }

  if($ch -eq '/' -and $nxt -eq '/'){ $inLineCmt=$true; $i++; continue }
  if($ch -eq '/' -and $nxt -eq '*'){ $inBlkCmt=$true; $i++; continue }
  if($ch -eq '`'){ $inRaw=$true; continue }
  if($ch -eq '"'){ $inStr=$true; continue }
  if($ch -eq "'"){ $inRune=$true; continue }

  if($ch -eq '{'){ $depth++; continue }
  if($ch -eq '}'){
    $depth--
    if($depth -eq 0){ break }
    continue
  }
}
if($depth -ne 0){ throw "brace scan failed for NewRouter" }
$bodyEnd = $i

# 4) 본문과 return <ident> 찾기
$body = $s.Substring($bodyStart+1, $bodyEnd-$bodyStart-1)
$returns = [regex]::Matches($body,'return\s+([A-Za-z_][A-Za-z0-9_]*)\s*','Singleline')
if($returns.Count -eq 0){ throw "cannot find any return <ident> in NewRouter" }
$muxVar = $returns[$returns.Count-1].Groups[1].Value
$retIdxInBody = $returns[$returns.Count-1].Index
$insertPos = $bodyStart + 1 + $retIdxInBody  # 파일 내 절대 위치

# 5) 이미 invariants 있으면 스킵 (단순 포함 검사로 안전 처리)
if($s.Contains('/health/invariants')){
  Write-Host "[skip] /health/invariants already present (mux=$muxVar)"
} else {
  $snippet = @"
    $muxVar.HandleFunc("/health/invariants", func(w http.ResponseWriter, r *http.Request) {
        h := e.CurrentHeight()
        rc := uint64(e.ReceiptCount())
        ok := (h == rc)
        reason := ""
        if !ok { reason = "height_mismatch" }
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{
            "ok":             ok,
            "reason":         reason,
            "height":         h,
            "receipts_count": rc,
            "time_utc":       time.Now().UTC().Format(time.RFC3339),
        })
    })
"@ -replace '\$muxVar', $muxVar

  $s = $s.Insert($insertPos, $snippet)
  Set-Content -LiteralPath $File -Value $s -Encoding UTF8
  Write-Host "[ok] inserted invariants handler before return ($muxVar)"
}

# 6) 빌드 & 재기동 & 확인
Push-Location $Root
cmd /c "go build -o .\bin\thomasd.exe .\cmd\thomasd > build-router.out.txt 2> build-router.err.txt & exit /b %errorlevel%"
$ecode = $LASTEXITCODE
if($ecode -ne 0){
  Write-Host "== build errors =="; Get-Content .\build-router.err.txt -Tail 200
  throw "go build failed ($ecode)"
}
Pop-Location

Get-Process thomasd -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Process -NoNewWindow (Join-Path $Root 'bin\thomasd.exe')
Start-Sleep 1

$BASE='http://127.0.0.1:8081'
"`nGET $BASE/health =>"
Invoke-RestMethod "$BASE/health" | Format-List
"`nGET $BASE/health/invariants =>"
Invoke-RestMethod "$BASE/health/invariants" | Format-List
