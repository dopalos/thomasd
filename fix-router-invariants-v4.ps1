# fix-router-invariants-v4.ps1
param([string]$Root = (Get-Location).Path)
$ErrorActionPreference='Stop'

$File = Join-Path $Root 'internal\rpc\router.go'
if(!(Test-Path $File)){ throw "router.go not found: $File" }

function Try-Build([string]$root, [ref]$outText){
  pushd $root
  $out = & go build -o .\bin\thomasd.exe .\cmd\thomasd 2>&1
  $code = $LASTEXITCODE
  popd
  $outText.Value = ($out -join "`r`n")
  return ($code -eq 0)
}

[string]$src = Get-Content -LiteralPath $File -Raw -Encoding UTF8

# 1) import 보강
function Ensure-Import([string]$code,[string]$pkg){
  if($code -notmatch [regex]::Escape($pkg)){
    return [regex]::Replace($code,'import\s*\(\s*',("import (`n`t$pkg`n"),1)
  }
  return $code
}
$src = Ensure-Import $src '"encoding/json"'
$src = Ensure-Import $src '"time"'

# 2) NewRouter 본문 범위 잡기
$rxHdr = [regex]'(?s)(func\s+NewRouter\s*\([^\)]*\)\s*\{)'
$mHdr = $rxHdr.Match($src)
if(-not $mHdr.Success){ throw "cannot find NewRouter body start (func NewRouter(...){)" }

$start = $mHdr.Index + $mHdr.Length
$depth = 1; $pos = $start
while($pos -lt $src.Length - 1 -and $depth -gt 0){
  $ch = $src[$pos]
  if($ch -eq '{'){ $depth++ } elseif($ch -eq '}'){ $depth-- }
  $pos++
}
if($depth -ne 0){ throw "brace scan failed for NewRouter" }

$head  = $src.Substring(0, $mHdr.Index)
$fnHdr = $mHdr.Value
$fnBody= $src.Substring($start, ($pos-1) - $start)
$tail  = $src.Substring($pos-1)

# 3) mux 변수명 추출
$mMux   = [regex]::Match($fnBody, '(\w+)\s*:=\s*http\.NewServeMux\(\)')
$MuxVar = if($mMux.Success){ $mMux.Groups[1].Value } else { 'mux' }

# 4) 기존 invariants 핸들러 제거
$fnBody = [regex]::Replace(
  $fnBody,
  '(?s)\s*'+[regex]::Escape($MuxVar)+
  '\.HandleFunc\("/health/invariants"\s*,\s*func\(w http\.ResponseWriter,\s*r \*http\.Request\)\s*\{.*?\}\)\s*',
  "`n"
)

# 5) 삽입 위치: /health 뒤 또는 return 직전
$afterHealth = [regex]::Match($fnBody,'(?s)'+[regex]::Escape($MuxVar)+'\.HandleFunc\("/health"[\s\S]*?\)\s*')
$insertIdx = if($afterHealth.Success){ $afterHealth.Index + $afterHealth.Length } else {
  $ret = [regex]::Match($fnBody,'\breturn\s+'+[regex]::Escape($MuxVar)+'\b')
  if(-not $ret.Success){ throw "cannot find insertion point (neither /health nor return $MuxVar)" }
  $ret.Index
}

# 6) 핸들러 스니펫
$snippet = @"
    $MuxVar.HandleFunc("/health/invariants", func(w http.ResponseWriter, r *http.Request) {
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
$fnBody = $fnBody.Insert($insertIdx, "`n$snippet`n")

$new = $head + $fnHdr + $fnBody + $tail

# 7) 저장(백업)
$bak = "$File.bak-$(Get-Date -Format 'yyyyMMddHHmmss')"
Copy-Item $File $bak -Force
Set-Content -LiteralPath $File -Value $new -Encoding UTF8
Write-Host "[ok] router.go patched (bak=$bak) - mux var = $MuxVar"

# 8) 빌드/재기동/검증
$buildOut = ""
if(-not (Try-Build $Root ([ref]$buildOut))){
  Write-Host "`n[go build output] ============================="
  $buildOut | Out-Host
  Write-Host "==============================================="
  $lines = $new -split "`r?`n"
  $hit = ($lines | Select-String -Pattern '/health/invariants' -SimpleMatch | Select-Object -First 1)
  if($hit){
    $startL = [Math]::Max(0, $hit.LineNumber-40)
    $endL   = [Math]::Min($lines.Count-1, $hit.LineNumber+40)
    Write-Host "`n[router.go context lines $startL..$endL] --------"
    for($i=$startL;$i -le $endL;$i++){ '{0,5} {1}' -f $i, $lines[$i] | Out-Host }
    Write-Host "------------------------------------------------"
  }
  throw "go build failed (see output above)"
}

Get-Process thomasd -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Process -NoNewWindow (Join-Path $Root 'bin\thomasd.exe')
Start-Sleep 1

$BASE='http://127.0.0.1:8081'
"GET $BASE/health/invariants =>"
Invoke-RestMethod "$BASE/health/invariants" | Format-List
