# fix-router-invariants.ps1
param([string]$Root = (Get-Location).Path)
$ErrorActionPreference = 'Stop'

$File = Join-Path $Root 'internal\rpc\router.go'
if (!(Test-Path $File)) { throw "router.go not found: $File" }

# 0) 가장 최근 백업들 중 '빌드가 통과되는' 버전으로 자동 롤백
$dir = Split-Path $File
$backups = Get-ChildItem $dir -Filter 'router.go.bak-*' -ErrorAction SilentlyContinue | Sort-Object LastWriteTime -Descending
$restored = $false
foreach($b in $backups){
  try{
    Copy-Item $b.FullName $File -Force
    pushd $Root
    & go build -o .\bin\thomasd.exe .\cmd\thomasd 2>$null
    if ($LASTEXITCODE -eq 0) { $restored = $true; popd; break }
    popd
  } catch { try{ popd }catch{} }
}
if(-not $restored){
  Write-Host "[warn] no compiling backup found or none existed; continue patching current file"
}

# 1) 원본 로드
[string]$s = Get-Content -LiteralPath $File -Raw -Encoding UTF8

# 2) import 보강 (중복 방지)
function Add-ImportIfMissing([string]$src, [string]$pkg){
  if ($src -notmatch [regex]::Escape($pkg)) {
    return [regex]::Replace($src, 'import\s*\(\s*', ("import (`n`t$pkg`n"), 1)
  }
  return $src
}
$s = Add-ImportIfMissing $s '"encoding/json"'
$s = Add-ImportIfMissing $s '"time"'

# 3) 기존에 삽입된 잘못된 invariants 핸들러 제거
$s = [regex]::Replace($s, '(?s)\s*mux\.HandleFunc\("/health/invariants".*?\}\)\s*\)', ')') # 보호적 제거
$s = [regex]::Replace($s, '(?s)\n\s*mux\.HandleFunc\("/health/invariants".*?\}\)\s*\n', "`n")

# 4) 삽입 위치 찾기: /health 바로 뒤 우선, 없으면 return mux 직전
$anchor = [regex]::Match($s, 'mux\.HandleFunc\("/health"\s*,[\s\S]*?\)\s*')
$insertAt = $null
if($anchor.Success){
  $insertAt = $anchor.Index + $anchor.Length
}else{
  $ret = [regex]::Match($s, '\breturn\s+mux\b')
  if(-not $ret.Success){ throw "cannot find insertion point: no /health or 'return mux' found" }
  $insertAt = $ret.Index
}

# 5) 정상 핸들러 스니펫 (map 인코딩, 태그없음)
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

$s = $s.Insert($insertAt, "`n$snippet`n")

# 6) 저장
$stamp = Get-Date -Format 'yyyyMMddHHmmss'
$bak = "$File.bak-$stamp"
Copy-Item -LiteralPath $File -Destination $bak -Force
Set-Content -LiteralPath $File -Value $s -Encoding UTF8
Write-Host "[ok] invariants handler fixed -> $File (bak=$bak)"

# 7) 빌드 & 재기동 & 검증
pushd $Root
& go build -o .\bin\thomasd.exe .\cmd\thomasd
if ($LASTEXITCODE -ne 0) { throw "go build failed after patch" }
Get-Process thomasd -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Process -NoNewWindow .\bin\thomasd.exe
Start-Sleep 1
$BASE = 'http://127.0.0.1:8081'
"GET $BASE/health/invariants =>"
Invoke-RestMethod "$BASE/health/invariants" | Format-List
popd
