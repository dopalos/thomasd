$ErrorActionPreference='Stop'

# A) 미들웨어 파일 보증
$mid = "internal\rpc\precheck_binding.go"
if(-not (Test-Path $mid)){
  @"
package rpc

import (
    "bytes"
    "encoding/json"
    "io"
    "net/http"
    "os"
    "strings"
)

type txMinimal struct {
    From string `json:"from"`
}

// X-PubKey ↔ body.from 바인딩 (개발용: THOMAS_PUBKEY_<FROM> 로 기대 pubkey 지정)
func precheckFromBinding(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        pubB64 := strings.TrimSpace(r.Header.Get("X-PubKey"))
        if pubB64 == "" {
            next.ServeHTTP(w, r)
            return
        }

        // body 백업/복구
        var body []byte
        if r.Body != nil {
            b, _ := io.ReadAll(r.Body)
            body = b
            r.Body = io.NopCloser(bytes.NewReader(b))
        }

        var tx txMinimal
        if len(body) > 0 {
            _ = json.Unmarshal(body, &tx)
        }
        if tx.From == "" {
            next.ServeHTTP(w, r)
            return
        }

        envKey := "THOMAS_PUBKEY_" + tx.From
        if want, ok := os.LookupEnv(envKey); ok && strings.TrimSpace(want) != "" && strings.TrimSpace(want) != pubB64 {
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusOK)
            io.WriteString(w, `{"ok":true,"applied":false,"reason":"verify:from_pub_mismatch"}`)
            return
        }

        next.ServeHTTP(w, r)
    })
}
"@ | Set-Content $mid -Encoding UTF8
  "CREATED: $mid"
}else{
  "EXISTS : $mid"
}

# B) router.go 체인 치환
$router = "internal\rpc\router.go"
if(-not (Test-Path $router)){ throw "router.go not found" }
[string]$code = Get-Content $router -Raw

# 치환 대상 카운트
$before = ([regex]::Matches($code, 'precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)')).Count

$code = [regex]::Replace(
  $code,
  'precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)',
  'precheckSig(precheckFromBinding(precheckCommit(mux)))'
)

Set-Content $router $code -Encoding UTF8
$after = ([regex]::Matches((Get-Content $router -Raw), 'precheckSig\(\s*precheckFromBinding\(\s*precheckCommit\(\s*mux\s*\)\s*\)\s*\)')).Count
"REPLACED chains: $before -> $after"

# C) 빌드
& go clean -cache | Out-Null
$build = & go build -tags "cbor blake3" -o .\thomasd_dbg.exe .\cmd\thomasd 2>&1
if($LASTEXITCODE -ne 0){
  "BUILD FAILED (cbor blake3). Retrying with 'cbor' only..." | Write-Host -ForegroundColor Yellow
  & go clean -cache | Out-Null
  $build = & go build -tags "cbor" -o .\thomasd_dbg.exe .\cmd\thomasd 2>&1
}
if($LASTEXITCODE -ne 0){
  Write-Host "`n--- build errors (tail) ---" -ForegroundColor Yellow
  $build | Select-Object -Last 120
  exit 1
}
"BUILD OK"

# D) 재기동 + 바인딩 env 설정
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force

$sk  = (Get-Content .\dev_sk.b64 -Raw).Trim()
$pub = (& go run .\sig_util.go pub $sk).Trim()
$env:THOMAS_PUBKEY_tho1alice = $pub   # 바인딩 핵심
$env:THOMAS_REQUIRE_COMMIT='1'
$env:THOMAS_VERIFY_SIG='1'

Start-Process .\thomasd_dbg.exe -RedirectStandardOutput thomasd_out.log -RedirectStandardError thomasd_err.log
Start-Sleep 1
"RESTARTED thomasd_dbg with THOMAS_PUBKEY_tho1alice=$pub"
