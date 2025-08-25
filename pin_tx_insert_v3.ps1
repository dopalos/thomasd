$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"
if(-not (Test-Path $router)){ throw "router.go not found" }

# 소스 로드
[string]$src = Get-Content $router -Raw

# 0) muxWrapped 참조 정리 (컴파일 에러 방지)
$wrapped = [regex]::Matches($src, '\bmuxWrapped\b').Count
if($wrapped -gt 0){
  $src = [regex]::Replace($src, '\bmuxWrapped\b', 'mux')
  "CLEANED muxWrapped refs: $wrapped"
}

# 1) import 보강 (log, bytes, encoding/json, io, os, strings)
$src = [regex]::Replace($src, '(?s)(\bimport\s*\(\s*)(.*?)\)', {
  param($m)
  $head = $m.Groups[1].Value; $body = $m.Groups[2].Value
  foreach($pkg in @('log','bytes','encoding/json','io','os','strings')){
    if($body -notmatch "(?m)^\s*`"$([regex]::Escape($pkg))`"\s*$"){
      $body += "`r`n`t`"$pkg`""
    }
  }
  $head + $body + ")"
}, 1)

# 2) 주입 블록 (마커: [bindchk-v3])
$bind = @"
        // === begin: from/pub binding check [bindchk-v3] ===
        pub := strings.TrimSpace(r.Header.Get("X-PubKey"))
        log.Printf("[bindchk-v3] pub=%q", pub)
        if pub != "" {
            var tx struct{ From string `json:"from"` }
            var body []byte
            if r.Body != nil {
                b, _ := io.ReadAll(r.Body)
                body = b
                r.Body = io.NopCloser(bytes.NewReader(b))
            }
            if len(body) > 0 { _ = json.Unmarshal(body, &tx) }
            log.Printf("[bindchk-v3] from=%q", tx.From)
            if tx.From != "" {
                envKey := "THOMAS_PUBKEY_" + tx.From
                want, ok := os.LookupEnv(envKey)
                log.Printf("[bindchk-v3] envKey=%s ok=%t want=%q", envKey, ok, strings.TrimSpace(want))
                if ok && strings.TrimSpace(want) != pub {
                    w.Header().Set("Content-Type", "application/json")
                    w.WriteHeader(http.StatusOK)
                    _, _ = w.Write([]byte(`{"ok":true,"applied":false,"reason":"verify:from_pub_mismatch"}`))
                    log.Printf("[bindchk-v3] BLOCKED mismatch: from=%s want=%q pub=%q", tx.From, strings.TrimSpace(want), pub)
                    return
                }
            }
        }
        // === end: from/pub binding check [bindchk-v3] ===
"@

# 3) mux.HandleFunc("/tx", func(w http.ResponseWriter, r *http.Request) {  바로 다음 줄에 삽입
$needle1 = 'mux.HandleFunc("/tx", func(w http.ResponseWriter, r *http.Request) {'
if($src -match [regex]::Escape($needle1)){
  if($src -notmatch '\[bindchk-v3\]'){
    $src = $src -replace [regex]::Escape($needle1), ($needle1 + "`r`n" + $bind)
    "INJECT mux.HandleFunc(/tx): OK"
  } else {
    "SKIP mux.HandleFunc(/tx): already injected"
  }
} else {
  "WARN: mux.HandleFunc(/tx) signature not found; skipping that path"
}

# 4) if r.Method==http.MethodPost && r.URL.Path=="/tx" {  모든 수작업 경로에 삽입
#    (사용자 파일에 정확히 이 문자열이 존재함)
$needle2 = 'if r.Method==http.MethodPost && r.URL.Path=="/tx" {'
$cnt = 0
$src = [regex]::Replace($src, [regex]::Escape($needle2), {
  param($m)
  $script:cnt++
  $needle2 + "`r`n" + $bind
})
"INJECT manual if(/tx) blocks: $cnt"

# 5) 저장
Set-Content $router $src -Encoding UTF8
"router.go patched (bindchk-v3)"

# 6) 마커 확인
Select-String $router -Pattern '\[bindchk-v3\]' -Context 0,1

# 7) 빌드
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){ cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1" }
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 120
