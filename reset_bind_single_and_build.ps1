$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"
if(-not (Test-Path $router)){ throw "router.go not found" }

# 0) 로드
[string]$s = Get-Content $router -Raw

# 1) 기존 주입 블록(모든 버전) 제거: bindchk-v2, bindchk-v3 등 전체 제거
$removed1 = 0
$s = [regex]::Replace($s,
  '(?s)^[ \t]*//\s*===\s*begin:\s*from/pub binding check\s*\[[^\]]+\]\s*===.*?//\s*===\s*end:\s*from/pub binding check\s*\[[^\]]+\]\s*===\r?\n?',
  { param($m) $script:removed1++; '' },
  [System.Text.RegularExpressions.RegexOptions]::Multiline
)
"REMOVED injected blocks: $removed1"

# 2) import 보강 (중복 방지)
$s = [regex]::Replace($s, '(?s)(\bimport\s*\(\s*)(.*?)\)', {
  param($m)
  $head = $m.Groups[1].Value; $body = $m.Groups[2].Value
  foreach($pkg in @('log','bytes','encoding/json','io','os','strings')){
    if($body -notmatch "(?m)^\s*`"$([regex]::Escape($pkg))`"\s*$"){
      $body += "`r`n`t`"$pkg`""
    }
  }
  $head + $body + ")"
}, 1)

# 3) 단일 바인딩 블록 정의 (마커: bindchk-single)
$bind = @"
        // === begin: from/pub binding check [bindchk-single] ===
        pub := strings.TrimSpace(r.Header.Get("X-PubKey"))
        log.Printf("[bindchk-single] pub=%q", pub)
        if pub != "" {
            var tx struct{ From string `json:"from"` }
            var body []byte
            if r.Body != nil {
                b, _ := io.ReadAll(r.Body)
                body = b
                r.Body = io.NopCloser(bytes.NewReader(b))
            }
            if len(body) > 0 { _ = json.Unmarshal(body, &tx) }
            log.Printf("[bindchk-single] from=%q", tx.From)
            if tx.From != "" {
                envKey := "THOMAS_PUBKEY_" + tx.From
                want, ok := os.LookupEnv(envKey)
                log.Printf("[bindchk-single] envKey=%s ok=%t want=%q", envKey, ok, strings.TrimSpace(want))
                if ok && strings.TrimSpace(want) != pub {
                    w.Header().Set("Content-Type", "application/json")
                    w.WriteHeader(http.StatusOK)
                    _, _ = w.Write([]byte(`{"ok":true,"applied":false,"reason":"verify:from_pub_mismatch"}`))
                    log.Printf("[bindchk-single] BLOCKED mismatch: from=%s want=%q pub=%q", tx.From, strings.TrimSpace(want), pub)
                    return
                }
            }
        }
        // === end: from/pub binding check [bindchk-single] ===
"@

# 4) mux.HandleFunc("/tx"...){ 바로 다음 줄에 "단 한 번만" 삽입
$needleRe = '(?s)(mux\.HandleFunc\(\s*"/tx"\s*,\s*func\s*\([^)]*\)\s*\{\s*)'
if($s -notmatch $needleRe){ throw 'cannot find mux.HandleFunc("/tx", func(...){' }
$s = [regex]::Replace($s, $needleRe, { param($m) $m.Groups[1].Value + "`r`n" + $bind }, 1)
"INJECTED bindchk-single into mux.HandleFunc(/tx)"

# 5) 잘못된 struct 태그 전역 복구: json:"from" -> `json:"from"`
$s = [regex]::Replace($s, '(?<!`)json:"from"(?!`)', '`json:"from"`')

# 6) 저장
Set-Content $router $s -Encoding UTF8
"router.go saved (single binding)"

# 7) 빌드
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 150
