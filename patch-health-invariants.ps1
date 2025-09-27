# patch-health-invariants.ps1 (fixed)
param([string]$Root = (Get-Location).Path)
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

# import 블록에 encoding/json, time 추가
if ($s -notmatch [regex]::Escape('"encoding/json"')) {
  $s = [regex]::Replace($s, 'import\s*\(\s*', "import (`n`t""encoding/json""`n", 1)
}
if ($s -notmatch '(^|\s)"time"(\s|$)') {
  $s = [regex]::Replace($s, 'import\s*\(\s*', "import (`n`t""time""`n", 1)
}

# 이미 추가돼 있으면 재삽입 안 함
if ($s -notmatch '/health/invariants') {
  $handler = @"
    // /health/invariants — invariant checks for height vs receipts
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
        if !ok {
            reason = "height_mismatch"
        }
        out := resp{
            OK:            ok,
            Reason:        reason,
            Height:        h,
            ReceiptsCount: rc,
            TimeUTC:       time.Now().UTC().Format(time.RFC3339),
        }
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(out)
    })
"@
  # NewRouter 마지막의 'return mux' 직전에 주입
  $s = $s -replace '(?s)(return\s+mux)', ($handler + "`r`n$1")
}

Set-Content -LiteralPath $File -Value $s -Encoding UTF8
Write-Host "[ok] router invariants endpoint patched -> $File"
