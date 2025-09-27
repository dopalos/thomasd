# fix-router-invariants-v7.ps1
param([string]$Root = (Get-Location).Path)
$ErrorActionPreference='Stop'
$File = Join-Path $Root 'internal\rpc\router.go'
if(!(Test-Path $File)){ throw "router.go not found: $File" }

[string]$src = Get-Content -LiteralPath $File -Raw -Encoding UTF8

function Ensure-Import([string]$code,[string]$pkg){
  if ($code -notmatch [regex]::Escape($pkg)) {
    return [regex]::Replace($code,'import\s*\(\s*',("import (`n`t$pkg`n"),1)
  }
  return $code
}
$src = Ensure-Import $src '"encoding/json"'
$src = Ensure-Import $src '"time"'

# mux 변수 추정
$MuxVar = $null
$m1 = [regex]::Match($src, '(\w+)\s*:=\s*http\.NewServeMux\(\)')
if($m1.Success){ $MuxVar = $m1.Groups[1].Value }
if(-not $MuxVar){
  $m2 = [regex]::Match($src, '(\w+)\.HandleFunc\("/health"\s*,')
  if($m2.Success){ $MuxVar = $m2.Groups[1].Value }
}
if(-not $MuxVar){ throw "cannot infer mux variable" }

# 기존 invariants 및 그 앞의 고아 '{' 제거
$src = [regex]::Replace(
  $src,
  '(?s)\s*\{\s*\r?\n\s*'+[regex]::Escape($MuxVar)+'\.HandleFunc\("/health/invariants".*?\}\)\s*',
  "`n"
)
$src = [regex]::Replace(
  $src,
  '(?s)\s*'+[regex]::Escape($MuxVar)+'\.HandleFunc\("/health/invariants".*?\}\)\s*',
  "`n"
)

# /health 핸들러 전체 블록 끝 위치(… { … \n}) 뒤에 삽입
$pat = '(?s)'+[regex]::Escape($MuxVar)+'\.HandleFunc\("/health",\s*func\([^)]*\)\s*\{.*?\n\}\)\s*'
$mh = [regex]::Match($src,$pat)
if(-not $mh.Success){ throw "cannot find complete /health handler block" }
$insertPos = $mh.Index + $mh.Length

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

$patched = $src.Insert($insertPos, "`n$snippet`n")
$bak="$File.bak-$(Get-Date -Format 'yyyyMMddHHmmss')"
Copy-Item $File $bak -Force
Set-Content -LiteralPath $File -Value $patched -Encoding UTF8
Write-Host "[ok] router.go fixed and patched (bak=$bak) - mux=$MuxVar"

# 빌드/재기동/검증
pushd $Root
& go build -o .\bin\thomasd.exe .\cmd\thomasd 2>&1 | Tee-Object -Variable buildOut | Out-Null
if($LASTEXITCODE -ne 0){ Write-Host "`n[go build output]"; Write-Host $buildOut; throw "go build failed" }
popd

Get-Process thomasd -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Process -NoNewWindow (Join-Path $Root 'bin\thomasd.exe')
Start-Sleep 1

$BASE='http://127.0.0.1:8081'
"GET $BASE/health/invariants =>"
Invoke-RestMethod "$BASE/health/invariants" | Format-List
