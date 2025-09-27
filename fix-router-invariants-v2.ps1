# fix-router-invariants-v2.ps1
param([string]$Root = (Get-Location).Path)
$ErrorActionPreference='Stop'

$File = Join-Path $Root 'internal\rpc\router.go'
if(!(Test-Path $File)){ throw "router.go not found: $File" }

function Try-Build($root){
  pushd $root
  & go build -o .\bin\thomasd.exe .\cmd\thomasd *> $null
  $ok = ($LASTEXITCODE -eq 0)
  popd
  return $ok
}

# 0) 현재가 빌드 안되면: 최근 백업들 중 빌드 통과하는 것으로 롤백
if(-not (Try-Build $Root)){
  $backs = Get-ChildItem (Split-Path $File) -Filter 'router.go.bak-*' -ErrorAction SilentlyContinue | Sort-Object LastWriteTime -Descending
  $rolled = $false
  foreach($b in $backs){
    Copy-Item $b.FullName $File -Force
    if(Try-Build $Root){ $rolled=$true; Write-Host "[rolled] -> $($b.Name)"; break }
  }
  if(-not $rolled){ Write-Host "[warn] no compiling backup found; continue with current file" }
}

[string]$src = Get-Content -LiteralPath $File -Raw -Encoding UTF8

# 1) import 보강(중복 방지)
function Ensure-Import([string]$code,[string]$pkg){
  if($code -notmatch [regex]::Escape($pkg)){
    return [regex]::Replace($code,'import\s*\(\s*',("import (`n`t$pkg`n"),1)
  }
  return $code
}
$src = Ensure-Import $src '"encoding/json"'
$src = Ensure-Import $src '"time"'

# 2) NewRouter 함수 블록 추출
$rx = [regex]'(?s)(func\s+NewRouter\s*\([^\)]*\)\s*\*http\.ServeMux\s*\{\s*)([\s\S]*?)(\n\})'
$m = $rx.Match($src)
if(-not $m.Success){ throw "cannot find NewRouter(e *app.Engine) *http.ServeMux function" }
$head  = $src.Substring(0,$m.Groups[1].Index)
$fnHdr = $m.Groups[1].Value
$fnBody= $m.Groups[2].Value
$tail  = $src.Substring($m.Groups[3].Index + $m.Groups[3].Length)

# 3) 기존 잘못 삽입된 invariants 핸들러 제거(함수 내부만)
$fnBody = [regex]::Replace($fnBody,'(?s)\s*mux\.HandleFunc\("/health/invariants"\s*,\s*func\(w http\.ResponseWriter,\s*r \*http\.Request\)\s*\{.*?\}\)\s*',"`n")

# 4) 삽입 위치: /health 뒤가 우선, 없으면 return mux 앞
$afterHealth = [regex]::Match($fnBody,'mux\.HandleFunc\("/health"[\s\S]*?\)\s*')
$insertIdx = 0
if($afterHealth.Success){
  $insertIdx = $afterHealth.Index + $afterHealth.Length
}else{
  $ret = [regex]::Match($fnBody,'\breturn\s+mux\b')
  if(-not $ret.Success){ throw "cannot find insertion point inside NewRouter body" }
  $insertIdx = $ret.Index
}

$snippet = @"
    mux.HandleFunc("/health/invariants", func(w http.ResponseWriter, r *http.Request) {
        h := e.CurrentHeight()
        rc := uint64(e.ReceiptCount())
        ok := (h == rc)
        reason := ""
        if !ok {
            reason = "height_mismatch"
        }
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

# 5) 재조립/저장
$new = $head + $fnHdr + $fnBody + "`n}" + $tail
$bak = "$File.bak-$(Get-Date -Format 'yyyyMMddHHmmss')"
Copy-Item $File $bak -Force
Set-Content -LiteralPath $File -Value $new -Encoding UTF8
Write-Host "[ok] router.go patched (bak=$bak)"

# 6) 빌드/재기동/검증
if(-not (Try-Build $Root)){ throw "go build failed after v2 patch" }
Get-Process thomasd -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Process -NoNewWindow (Join-Path $Root 'bin\thomasd.exe')
Start-Sleep 1

$BASE='http://127.0.0.1:8081'
"GET $BASE/health/invariants =>"
Invoke-RestMethod "$BASE/health/invariants" | Format-List
