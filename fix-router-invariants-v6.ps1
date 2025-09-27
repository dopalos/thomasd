# fix-router-invariants-v6.ps1
param([string]$Root = (Get-Location).Path)
$ErrorActionPreference = 'Stop'

$File = Join-Path $Root 'internal\rpc\router.go'
if (!(Test-Path $File)) { throw "router.go not found: $File" }

# ---------- 유틸: go build ----------
function Try-Build([string]$root, [ref]$outText){
  pushd $root
  $out = & go build -o .\bin\thomasd.exe .\cmd\thomasd 2>&1
  $code = $LASTEXITCODE
  popd
  $outText.Value = ($out -join "`r`n")
  return ($code -eq 0)
}

# ---------- 원본 읽기 & import 보강 ----------
[string]$src = Get-Content -LiteralPath $File -Raw -Encoding UTF8

function Ensure-Import([string]$code,[string]$pkg){
  if ($code -notmatch [regex]::Escape($pkg)) {
    return [regex]::Replace($code,'import\s*\(\s*',("import (`n`t$pkg`n"),1)
  }
  return $code
}
$src = Ensure-Import $src '"encoding/json"'
$src = Ensure-Import $src '"time"'

# ---------- mux 변수 추출 ----------
# 1) mux := http.NewServeMux()
$m1 = [regex]::Match($src, '(\w+)\s*:=\s*http\.NewServeMux\(\)')
$MuxVar = $null
if ($m1.Success) { $MuxVar = $m1.Groups[1].Value }

# 2) 못 찾으면 /health 등록하는 변수로 추정
if (-not $MuxVar) {
  $m2 = [regex]::Match($src, '(\w+)\.HandleFunc\("/health"\s*,')
  if ($m2.Success) { $MuxVar = $m2.Groups[1].Value }
}

if (-not $MuxVar) { throw "cannot infer mux variable (no NewServeMux or /health registration found)" }

# ---------- 삽입 위치 계산 ----------
# 우선순위: /health 핸들러 바로 뒤 → return <mux> 직전
$afterHealth = [regex]::Match($src, '(?s)'+[regex]::Escape($MuxVar)+'\.HandleFunc\("/health"[^)]*\)\s*')
$insertPos = -1
if ($afterHealth.Success) {
  $insertPos = $afterHealth.Index + $afterHealth.Length
} else {
  $ret = [regex]::Match($src, '\breturn\s+'+[regex]::Escape($MuxVar)+'\b')
  if ($ret.Success) { $insertPos = $ret.Index } else { throw "cannot find insertion point (neither /health nor return $MuxVar)" }
}

# ---------- 기존 invariants 핸들러 제거(중복 방지) ----------
$src = [regex]::Replace(
  $src,
  '(?s)\s*'+[regex]::Escape($MuxVar)+'\.HandleFunc\("/health/invariants"\s*,\s*func\(w http\.ResponseWriter,\s*r \*http\.Request\)\s*\{.*?\}\)\s*',
  "`n"
)

# ---------- 핸들러 스니펫 ----------
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

# ---------- 코드 삽입 & 저장 ----------
$patched = $src.Insert($insertPos, "`n$snippet`n")
$bak = "$File.bak-$(Get-Date -Format 'yyyyMMddHHmmss')"
Copy-Item $File $bak -Force
Set-Content -LiteralPath $File -Value $patched -Encoding UTF8
Write-Host "[ok] router.go patched (bak=$bak) - mux var = $MuxVar"

# ---------- 빌드/재기동/검증 ----------
$buildOut = ""
if (-not (Try-Build $Root ([ref]$buildOut))) {
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
