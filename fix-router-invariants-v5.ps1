# fix-router-invariants-v5.ps1
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

# 1) import 보강 (encoding/json, time)
function Ensure-Import([string]$code,[string]$pkg){
  if($code -notmatch [regex]::Escape($pkg)){
    return [regex]::Replace($code,'import\s*\(\s*',("import (`n`t$pkg`n"),1)
  }
  return $code
}
$src = Ensure-Import $src '"encoding/json"'
$src = Ensure-Import $src '"time"'

# 2) NewRouter 헤더 탐색(더 느슨한 정규식)
#    - func NewRouter ... {  를 괄호/개행/코멘트 상관없이 포착
$rxHdr = [regex]'(?s)(func\s+NewRouter\b[^{]*\{)'
$mHdr = $rxHdr.Match($src)
if(-not $mHdr.Success){
  throw "cannot find NewRouter body start (func NewRouter ... {)"
}

# 3) 본문 범위 괄호 스캔
$start = $mHdr.Index + $mHdr.Length
$depth = 1; $pos = $start
while($pos -lt $src.Length -and $depth -gt 0){
  $ch = $src[$pos]
  if($ch -eq '{'){ $depth++ } elseif($ch -eq '}'){ $depth-- }
  $pos++
}
if($depth -ne 0){ throw "brace scan failed for NewRouter" }

$head  = $src.Substring(0, $mHdr.Index)
$fnHdr = $mHdr.Value
$fnBody= $src.Substring($start, ($pos-1) - $start)
$tail  = $src.Substring($pos-1)

# 4) mux 변수명 추출 (없으면 mux라 가정)
$mMux   = [regex]::Match($fnBody, '(\w+)\s*:=\s*http\.NewServeMux\(\)')
$MuxVar = if($mMux.Success){ $mMux.Groups[1].Value } else { 'mux' }

# 5) 기존 invariants 핸들러 제거(있다면)
$fnBody = [regex]::Replace(
  $fnBody,
  '(?s)\s*'+[regex]::Escape($MuxVar)+
  '\.HandleFunc\("/health/invariants"\s*,\s*func\(w http\.ResponseWriter,\s*r \*http\.Request\)\s*\{.*?\}\)\s*',
  "`n"
)

# 6) 삽입 위치: /health 뒤 또는 return 직전
$afterHealth = [regex]::Match($fnBody,'(?s)'+[regex]::Escape($MuxVar)+'\.HandleFunc\("/health"[\s\S]*?\)\s*')
$insertIdx = if($afterHealth.Success){ $afterHealth.Index + $afterHealth.Length } else {
  $ret = [regex]::Match($fnBody,'\breturn\s+'+[regex]::Escape($MuxVar)+'\b')
  if(-not $ret.Success){ throw "cannot find insertion point (neither /health nor return $MuxVar)" }
  $ret.Index
}

# 7) 핸들러 스니펫
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

# 8) 저장(백업)
$bak = "$File.bak-$(Get-Date -Format 'yyyyMMddHHmmss')"
Copy-Item $File $bak -Force
Set-Content -LiteralPath $File -Value $new -Encoding UTF8
Write-Host "[ok] router.go patched (bak=$bak) - mux var = $MuxVar"

# 9) 빌드/재기동/검증
$buildOut = ""
if(-not (Try-Build $Root ([ref]$buildOut))){
  Write-Host "`n[go build output] ============================="
  $buildOut | Out-Host
  Write-Host "==============================================="
  throw "go build failed"
}

Get-Process thomasd -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Process -NoNewWindow (Join-Path $Root 'bin\thomasd.exe')
Start-Sleep 1

$BASE='http://127.0.0.1:8081'
"GET $BASE/health/invariants =>"
Invoke-RestMethod "$BASE/health/invariants" | Format-List
