# fix-router-invariants-v8.ps1
param([string]$Root = (Get-Location).Path)
$ErrorActionPreference='Stop'
$File = Join-Path $Root 'internal\rpc\router.go'
if(!(Test-Path $File)){ throw "router.go not found: $File" }

[string]$src = Get-Content -LiteralPath $File -Raw -Encoding UTF8

function Ensure-Import([string]$code,[string]$pkg){
  if($code -notmatch [regex]::Escape($pkg)){
    return [regex]::Replace($code,'import\s*\(\s*',("import (`n`t$pkg`n"),1)
  }
  return $code
}
$src = Ensure-Import $src '"encoding/json"'
$src = Ensure-Import $src '"time"'

# mux 변수 추정
$MuxVar = $null
$m1 = [regex]::Match($src,'(\w+)\s*:=\s*http\.NewServeMux\(\)')
if($m1.Success){ $MuxVar = $m1.Groups[1].Value }
if(-not $MuxVar){
  $m2 = [regex]::Match($src,'(\w+)\.HandleFunc\("/health"\s*,')
  if($m2.Success){ $MuxVar = $m2.Groups[1].Value }
}
if(-not $MuxVar){ throw "cannot infer mux variable" }

# 기존 invariants 흔적 제거 (중복/깨진 블록 방지)
$src = [regex]::Replace($src,'(?s)\s*\{\s*'+[regex]::Escape($MuxVar)+'\.HandleFunc\("/health/invariants".*?\}\)\s*',"`n")
$src = [regex]::Replace($src,'(?s)\s*'+[regex]::Escape($MuxVar)+'\.HandleFunc\("/health/invariants".*?\}\)\s*',"`n")

# NewRouter 함수 범위 찾기 (중괄호 스캔)
$fn = [regex]::Match($src,'func\s+NewRouter\s*\([^)]*\)\s*\*?http\.ServeMux\s*\{')
if(-not $fn.Success){ throw "cannot find NewRouter signature" }
$start = $fn.Index
# 본문 여는 '{' 위치
$openIdx = $src.IndexOf('{', $fn.Index)
if($openIdx -lt 0){ throw "cannot find NewRouter opening brace" }

# 중괄호 스캔으로 끝 '}' 찾기
$depth = 0
$endIdx = -1
for($i=$openIdx; $i -lt $src.Length; $i++){
  $ch = $src[$i]
  if($ch -eq '{'){ $depth++ }
  elseif($ch -eq '}'){
    $depth--
    if($depth -eq 0){ $endIdx = $i; break }
  }
}
if($endIdx -lt 0){ throw "brace scan failed for NewRouter" }

# NewRouter 본문 서브스트링
$body = $src.Substring($openIdx+1, $endIdx-($openIdx+1))

# 본문 안의 마지막 'return mux' 찾기
$rtPattern = 'return\s+'+[regex]::Escape($MuxVar)+'\b'
$rtMatches = [regex]::Matches($body, $rtPattern)
if($rtMatches.Count -eq 0){ throw "cannot find 'return $MuxVar' inside NewRouter" }
$rtLast = $rtMatches[$rtMatches.Count-1]
$insertInBodyAt = $rtLast.Index  # return 바로 앞

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

# 본문에 삽입
$bodyPatched = $body.Insert($insertInBodyAt, "`n$snippet`n")
# 원문 재조합
$patched = $src.Substring(0,$openIdx+1) + $bodyPatched + $src.Substring($endIdx)

# 백업 및 저장
$bak = "$File.bak-$(Get-Date -Format 'yyyyMMddHHmmss')"
Copy-Item $File $bak -Force
Set-Content -LiteralPath $File -Value $patched -Encoding UTF8
Write-Host "[ok] router.go patched (bak=$bak) - mux=$MuxVar"

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
