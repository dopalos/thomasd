# fix-router-invariants-v3.ps1
param([string]$Root = (Get-Location).Path)
$ErrorActionPreference='Stop'

$File = Join-Path $Root 'internal\rpc\router.go'
if(!(Test-Path $File)){ throw "router.go not found: $File" }

function Try-Build([string]$root, [ref]$outText){
  pushd $root
  $out = & go build -o .\bin\thomasd.exe .\cmd\thomasd 2>&1
  $code = $LASTEXITCODE
  popd
  $outText.Value = $out -join "`r`n"
  return ($code -eq 0)
}

# 원본 로드
[string]$src = Get-Content -LiteralPath $File -Raw -Encoding UTF8

# import 보강(중복 방지)
function Ensure-Import([string]$code,[string]$pkg){
  if($code -notmatch [regex]::Escape($pkg)){
    return [regex]::Replace($code,'import\s*\(\s*',("import (`n`t$pkg`n"),1)
  }
  return $code
}
$src = Ensure-Import $src '"encoding/json"'
$src = Ensure-Import $src '"time"'

# NewRouter 함수 추출
$rx = [regex]'(?s)(func\s+NewRouter\s*\([^\)]*\)\s*\*http\.ServeMux\s*\{\s*)([\s\S]*?)(\n\})'
$m = $rx.Match($src)
if(-not $m.Success){ throw "cannot find NewRouter(e *app.Engine) *http.ServeMux" }
$head  = $src.Substring(0,$m.Groups[1].Index)
$fnHdr = $m.Groups[1].Value
$fnBody= $m.Groups[2].Value
$tail  = $src.Substring($m.Groups[3].Index + $m.Groups[3].Length)

# 기존 잘못된/중복 invariants 제거(함수 내부만)
$fnBody = [regex]::Replace(
  $fnBody,
  '(?s)\s*mux\.HandleFunc\("/health/invariants"\s*,\s*func\(w http\.ResponseWriter,\s*r \*http\.Request\)\s*\{.*?\}\)\s*',
  "`n"
)

# 삽입 위치 결정: /health 뒤 또는 return mux 직전
$afterHealth = [regex]::Match($fnBody,'mux\.HandleFunc\("/health"[\s\S]*?\)\s*')
$insertIdx = 0
if($afterHealth.Success){
  $insertIdx = $afterHealth.Index + $afterHealth.Length
}else{
  $ret = [regex]::Match($fnBody,'\breturn\s+mux\b')
  if(-not $ret.Success){ throw "cannot find insertion point inside NewRouter body" }
  $insertIdx = $ret.Index
}

# 정상 핸들러 스니펫
$snippet = @"
    mux.HandleFunc("/health/invariants", func(w http.ResponseWriter, r *http.Request) {
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
"@

# 삽입
$fnBody = $fnBody.Insert($insertIdx, "`n$snippet`n")
$new = $head + $fnHdr + $fnBody + "`n}" + $tail

# 백업/저장
$bak = "$File.bak-$(Get-Date -Format 'yyyyMMddHHmmss')"
Copy-Item $File $bak -Force
Set-Content -LiteralPath $File -Value $new -Encoding UTF8
Write-Host "[ok] router.go patched (bak=$bak)"

# 빌드
$buildOut = ""
if(-not (Try-Build $Root ([ref]$buildOut))){
  Write-Host "`n[go build output] ============================="
  $buildOut | Out-Host
  Write-Host "==============================================="
  # 컨텍스트 출력 (/health/invariants 근처 80줄)
  $lines = $new -split "`r?`n"
  $hit = ($lines | Select-String -Pattern '/health/invariants' -SimpleMatch | Select-Object -First 1)
  if($hit){
    $start = [Math]::Max(0, $hit.LineNumber-40)
    $end   = [Math]::Min($lines.Count-1, $hit.LineNumber+40)
    Write-Host "`n[router.go context lines $start..$end] --------"
    for($i=$start;$i -le $end;$i++){ '{0,5} {1}' -f $i, $lines[$i] | Out-Host }
    Write-Host "------------------------------------------------"
  }
  throw "go build failed (see output above)"
}

# 재기동 & 확인
Get-Process thomasd -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Process -NoNewWindow (Join-Path $Root 'bin\thomasd.exe')
Start-Sleep 1

$BASE='http://127.0.0.1:8081'
"GET $BASE/health/invariants =>"
Invoke-RestMethod "$BASE/health/invariants" | Format-List
