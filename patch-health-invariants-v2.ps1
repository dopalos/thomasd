# patch-health-invariants-v2.ps1
param([string]$Root=(Get-Location).Path)
$ErrorActionPreference='Stop'

$File = Join-Path $Root 'internal\rpc\router.go'
if (!(Test-Path $File)) { throw "router.go not found: $File" }

# 백업
$stamp = Get-Date -Format 'yyyyMMddHHmmss'
$bak = "$File.bak-$stamp"
Copy-Item $File $bak -Force
Write-Host "[bak] $bak"

# 원본 로드
$s = Get-Content -LiteralPath $File -Raw -Encoding UTF8

# import 보강
if ($s -notmatch [regex]::Escape('"encoding/json"')) {
  $s = [regex]::Replace($s, 'import\s*\(\s*', "import (`n`t""encoding/json""`n", 1)
}
if ($s -notmatch '(^|\s)"time"(\s|$)') {
  $s = [regex]::Replace($s, 'import\s*\(\s*', "import (`n`t""time""`n", 1)
}

# 기존 중복(/health/invariants) 핸들러 제거
$s = [regex]::Replace($s, '(?s)\n\s*mux\.HandleFunc\("/health/invariants".*?\)\n', "`n")

# mux 생성 지점 찾기
$m = [regex]::Match($s, 'mux\s*:=\s*http\.NewServeMux\(\)')
if (-not $m.Success) { throw "cannot find 'mux := http.NewServeMux()' in $File" }

# 핸들러 본문
$insertion = @"
    mux.HandleFunc("/health/invariants", func(w http.ResponseWriter, r *http.Request) {
        type resp struct {
            OK            bool   `json:"ok"`
            Reason        string `json:"reason"`
            Height        uint64 `json:"height"`
            ReceiptsCount uint64 `json:"receipts_count"`
            TimeUTC       string `json:"time_utc"`
        }
        h := e.CurrentHeight()
        rc := uint64(e.ReceiptCount())
        ok := (h == rc)
        reason := ""
        if !ok { reason = "height_mismatch" }

        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(resp{
            OK: ok, Reason: reason, Height: h, ReceiptsCount: rc,
            TimeUTC: time.Now().UTC().Format(time.RFC3339),
        })
    })
"@

# mux 생성 직후에 주입
$s = $s.Insert($m.Index + $m.Length, "`n$insertion")

Set-Content -LiteralPath $File -Value $s -Encoding UTF8
Write-Host "[ok] invariants handler inserted after mux creation -> $File"
