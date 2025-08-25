# Thomas chain dev: router.go 자동패치 + 빌드 + 간단 스모크
$ErrorActionPreference = "Stop"

# -- repo root --
if (-not (Test-Path ".\go.mod")) {
  if (Test-Path "C:\thomas-scaffold\thomasd\go.mod") { Set-Location "C:\thomas-scaffold\thomasd" }
  else { throw "go.mod not found. Run at repo root." }
}
$target = "internal\rpc\router.go"
if (-not (Test-Path $target)) { throw "router.go 없음: $target" }

function Try-Build {
  $old = $ErrorActionPreference
  $ErrorActionPreference = "Continue"   # NativeCommandError 방지

  go clean -cache | Out-Null
  $out  = & go build -v -x -tags "cbor blake3" -o .\thomasd_dbg.exe .\cmd\thomasd 2>&1
  $code = $LASTEXITCODE

  $ErrorActionPreference = $old

  $out | Set-Content build_full.log -Encoding UTF8
  if ($code -ne 0) {
    Write-Host "`n--- go build failed: exit=$code (tail) ---`n" -ForegroundColor Yellow
    $out | Select-Object -Last 120 | ForEach-Object { Write-Host # Thomas chain dev: router.go 자동패치 + 빌드 + 간단 스모크
$ErrorActionPreference = "Stop"

# -- repo root --
if (-not (Test-Path ".\go.mod")) {
  if (Test-Path "C:\thomas-scaffold\thomasd\go.mod") { Set-Location "C:\thomas-scaffold\thomasd" }
  else { throw "go.mod not found. Run at repo root." }
}
$target = "internal\rpc\router.go"
if (-not (Test-Path $target)) { throw "router.go 없음: $target" }

function Try-Build {
  go clean -cache | Out-Null
  & go build -tags "cbor blake3" -o .\thomasd_dbg.exe .\cmd\thomasd 2>&1 | Tee-Object -Variable buildOut | Out-Null
  if ($LASTEXITCODE -ne 0) {
    "`n--- go build failed (tail) ---`n" + ($buildOut | Select-Object -Last 80 -ea SilentlyContinue) -join "`n" | Write-Host -ForegroundColor Yellow
    return $false
  }
  return $true
}

function Ensure-Import([string]$code,[string]$path){
  $hasBlk=[regex]::IsMatch($code,'(?s)import\s*\((.*?)\)')
  $line = "(?m)^\s*(?:\w+\s+)?`"$([Regex]::Escape($path))`"\s*$"
  if($hasBlk){
    if(-not [regex]::IsMatch($code,$line)){
      $re=[regex]::new('(?s)import\s*\((.*?)\)')
      $code=$re.Replace($code,{ param($m) $b=$m.Groups[1].Value; $b=($b.TrimEnd()+"`r`n`t`"$path`"`r`n"); "import (`r`n$($b.TrimEnd())`r`n)" },1)
    }
  } else {
    $code = $code -replace 'package\s+rpc', "package rpc`r`n`r`nimport (`r`n`t`"$path`"`r`n)"
  }
  return $code
}

function Find-NewRouterSpan([string]$code){
  $m = [regex]::Match($code,'func\s+NewRouter\s*\([^\)]*\)\s*[^\{]*\{',[System.Text.RegularExpressions.RegexOptions]::Singleline)
  if(-not $m.Success){ throw "NewRouter signature not found" }
  $i = $code.IndexOf('{', $m.Index)
  if($i -lt 0){ throw "opening brace for NewRouter not found" }

  $depth=1; $j=$i+1
  $inLine=$false; $inBlock=$false; $inDQ=$false; $inRaw=$false; $inRune=$false; $esc=$false

  while($j -lt $code.Length){
    $ch = $code[$j]
    $nxt = if($j+1 -lt $code.Length){ $code[$j+1] } else { [char]0 }

    if($inLine){ if($ch -eq "`n"){ $inLine=$false }; $j++; continue }
    if($inBlock){ if($ch -eq '*' -and $nxt -eq '/'){ $inBlock=$false; $j+=2; continue }; $j++; continue }

    if($inDQ){
      if($esc){ $esc=$false; $j++; continue }
      if($ch -eq [char]92){ $esc=$true; $j++; continue }  # backslash
      if($ch -eq '"'){ $inDQ=$false }
      $j++; continue
    }
    if($inRaw){ if($ch -eq '`'){ $inRaw=$false }; $j++; continue }

    if($inRune){
      if($esc){ $esc=$false; $j++; continue }
      if($ch -eq [char]92){ $esc=$true; $j++; continue }  # backslash
      if($ch -eq [char]39){ $inRune=$false }              # single quote '
      $j++; continue
    }

    if($ch -eq '/' -and $nxt -eq '/'){ $inLine=$true; $j+=2; continue }
    if($ch -eq '/' -and $nxt -eq '*'){ $inBlock=$true; $j+=2; continue }
    if($ch -eq '"'){ $inDQ=$true; $j++; continue }
    if($ch -eq '`'){ $inRaw=$true; $j++; continue }
    if($ch -eq [char]39){ $inRune=$true; $j++; continue } # enter rune

    if($ch -eq '{'){ $depth++; $j++; continue }
    if($ch -eq '}'){ $depth--; if($depth -eq 0){ return @{ Open=$i; Close=$j } }; $j++; continue }
    $j++
  }
  throw "closing brace for NewRouter not found"
}

function Remove-DuplicateBlocks([string]$code,[string]$startPat){
  $re=[regex]::new($startPat,[System.Text.RegularExpressions.RegexOptions]::Singleline)
  $ms=$re.Matches($code) | Sort-Object Index
  if($ms.Count -le 1){ return $code }
  for($k=$ms.Count-1; $k -ge 1; $k--){
    $m=$ms[$k]; $open=$code.IndexOf('{',$m.Index); if($open -lt 0){ continue }
    $d=1; $end=$open+1
    while($end -lt $code.Length){ $ch=$code[$end]; if($ch -eq '{'){$d++} elseif($ch -eq '}'){ $d--; if($d -eq 0){ $end++; break } }; $end++ }
    $ls=($code.LastIndexOf("`n",$m.Index)+1); if($ls -lt 0){$ls=0}
    $code=$code.Remove($ls, ($end-$ls))
  }
  return $code
}
function Remove-VarDup([string]$code,[string]$name){
  $re=[regex]::new("var\s+$name\s*=\s*func\([\s\S]*?\)\s*\{[\s\S]*?\}\(\)",[System.Text.RegularExpressions.RegexOptions]::Singleline)
  $ms=$re.Matches($code) | Sort-Object Index
  for($k=$ms.Count-1; $k -ge 1; $k--){ $m=$ms[$k]; $code=$code.Remove($m.Index,$m.Length) }
  return $code
}

# 1) 백업 & 로드
$bk = "$target.bak_$(Get-Date -Format 'yyyyMMdd_HHmmss')"; Copy-Item $target $bk -Force; "BACKUP: $bk"
[string]$src = Get-Content $target -Raw

# 2) 찌꺼기/오타 정리 + 중복 제거
$src = [regex]::Replace($src,'(?s)// === AUTO: .*?// === END AUTO ===','')
$src = $src -replace 'v!="";\s*\{','v != "" {'
$src = $src -replace 'v!="";\s*\)','v != "" )'
$src = $src -replace 'THOMAS_FEE_BPS"\);\s*\{','THOMAS_FEE_BPS"); {'
$src = Remove-DuplicateBlocks $src 'type\s+txPre\s+struct\s*\{'
$src = Remove-DuplicateBlocks $src 'func\s+minFeeMas\s*\('
$src = Remove-DuplicateBlocks $src 'func\s+txPrecheck(?:With)?\s*\('
$src = Remove-DuplicateBlocks $src 'func\s+precheckCommit\s*\('
$src = Remove-DuplicateBlocks $src 'func\s+precheckSig\s*\('
$src = Remove-VarDup $src 'expectedChainID'
$src = Remove-VarDup $src 'feeBps'

# 3) import 보강
foreach($im in @('net/http','time','encoding/json','bytes','io','os','strconv','fmt','crypto/sha256','encoding/hex','encoding/base64','crypto/ed25519','runtime')){
  $src = Ensure-Import $src $im
}

# 4) NewRouter 범위
$span = Find-NewRouterSpan $src
$before = $src.Substring(0,$span.Open+1)
$inside = $src.Substring($span.Open+1, $span.Close-($span.Open+1))
$after  = $src.Substring($span.Close)

# 라우터 변수
$routerVar='mux'
if ($inside -match '([A-Za-z_]\w*)\s*:=\s*http\.NewServeMux\(\)'){ $routerVar=$matches[1] }

# 5) 기본 핸들러들 (없을 때만)
if ($inside -notmatch 'HandleFunc\("/stats\.json"') {
  $inside += @"
    $routerVar.HandleFunc("/stats.json", func(w http.ResponseWriter, r *http.Request) {
      w.Header().Set("Content-Type","application/json")
      _ = json.NewEncoder(w).Encode(map[string]any{
        "height": eng.CurrentHeight(),
        "receipts_count": eng.ReceiptCount(),
        "time_utc": time.Now().UTC().Format(time.RFC3339),
      })
    })
"@
}
if ($inside -notmatch 'HandleFunc\("/metrics"') {
  $inside += @"
    $routerVar.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
      w.Header().Set("Content-Type","text/plain; version=0.0.4")
      fmt.Fprintf(w, "# TYPE thomas_height gauge\nthomas_height %d\n", eng.CurrentHeight())
      fmt.Fprintf(w, "# TYPE thomas_receipts_total counter\nthomas_receipts_total %d\n", eng.ReceiptCount())
      var m runtime.MemStats; runtime.ReadMemStats(&m)
      fmt.Fprintf(w, "# TYPE thomas_uptime_seconds gauge\nthomas_uptime_seconds %d\n", eng.UptimeSeconds())
      fmt.Fprintf(w, "# TYPE thomas_goroutines gauge\nthomas_goroutines %d\n", runtime.NumGoroutine())
      fmt.Fprintf(w, "# TYPE thomas_mem_alloc_bytes gauge\nthomas_mem_alloc_bytes %d\n", m.Alloc)
    })
"@
}
if ($inside -notmatch 'HandleFunc\("/readyz"') {
  $inside += @"
    $routerVar.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
      w.Header().Set("Content-Type","application/json")
      _ = json.NewEncoder(w).Encode(map[string]any{
        "status":"ready",
        "time_utc": time.Now().UTC().Format(time.RFC3339),
        "height": eng.CurrentHeight(),
        "uptime_secs": eng.UptimeSeconds(),
      })
    })
"@
}
if ($src -notmatch 'func\s+debuglogRateLimit\(') {
  $src += @"
type tokenBucket struct{ cap, tokens, refillPerSec int; last time.Time }
func newBucket(c, r int) *tokenBucket { return &tokenBucket{cap:c, tokens:c, refillPerSec:r, last:time.Now()} }
func (b *tokenBucket) take(n int) bool {
  now := time.Now()
  el := int(now.Sub(b.last).Seconds()) * b.refillPerSec
  if el>0 { b.tokens = imin(b.cap, b.tokens+el); b.last = now }
  if b.tokens>=n { b.tokens-=n; return true }
  return false
}
func imin(a,b int)int{ if a<b {return a}; return b }
var dbgBucket = newBucket(100,50)
func debuglogRateLimit(next http.Handler) http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method==http.MethodPost && r.URL.Path=="/tx" && r.URL.RawQuery=="debuglog=1" {
      if !dbgBucket.take(1) {
        w.WriteHeader(http.StatusTooManyRequests)
        _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"debuglog_ratelimited"})
        return
      }
    }
    next.ServeHTTP(w,r)
  })
}
func debuglogStatus(w http.ResponseWriter, r *http.Request){
  w.Header().Set("Content-Type","application/json")
  _ = json.NewEncoder(w).Encode(map[string]any{
    "capacity": dbgBucket.cap, "tokens": dbgBucket.tokens, "refill_per_sec": dbgBucket.refillPerSec,
    "time_utc": time.Now().UTC().Format(time.RFC3339),
  })
}
"@
}
if ($inside -notmatch 'HandleFunc\("/ratelimit\.debuglog"') {
  $inside += @"
    $routerVar.HandleFunc("/ratelimit.debuglog", debuglogStatus)
"@
}

# 6) tx 미들웨어 세트(없을 때만 추가)
$append = ""
if (-not [regex]::IsMatch($src,'(?m)^\s*type\s+txPre\s+struct')) {
$append += @"
type txPre struct {
  Type          int    `json:"type"`
  From          string `json:"from"`
  To            string `json:"to"`
  AmountMas     int64  `json:"amount_mas"`
  FeeMas        int64  `json:"fee_mas"`
  Nonce         int64  `json:"nonce"`
  ChainID       string `json:"chain_id"`
  ExpiryHeight  int64  `json:"expiry_height"`
  MsgCommitment string `json:"msg_commitment"`
  Sig           string `json:"sig,omitempty"`
}
"@
}
if (-not [regex]::IsMatch($src,'(?m)^\s*func\s+minFeeMas\s*\(')) {
$append += @"
func minFeeMas(amount int64, bps int) int64 { f:=(amount*int64(bps))/10000; if f<1 {f=1}; return f }
"@
}
if (-not [regex]::IsMatch($src,'(?m)^\s*var\s+expectedChainID\s*=')) {
$append += @"
var expectedChainID = func() string {
  if v := os.Getenv("THOMAS_CHAIN_ID"); v != "" { return v }
  return "thomas-dev-1"
}()
"@
}
if (-not [regex]::IsMatch($src,'(?m)^\s*var\s+feeBps\s*=')) {
$append += @"
var feeBps = func() int {
  if v := os.Getenv("THOMAS_FEE_BPS"); v != "" {
    if n,err := strconv.Atoi(v); err==nil && n>0 { return n }
  }
  return 10
}()
"@
}
if (-not [regex]::IsMatch($src,'(?m)^\s*func\s+txPrecheckWith\s*\(')) {
$append += @"
func txPrecheckWith(next http.Handler, getHeight func() int64) http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method==http.MethodPost && r.URL.Path=="/tx" {
      buf,_:=io.ReadAll(r.Body); r.Body.Close()
      var in txPre
      if err:=json.Unmarshal(buf,&in); err!=nil {
        w.WriteHeader(http.StatusBadRequest)
        _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"malformed_json"})
        return
      }
      errs:=make([]string,0,4)
      if in.ChainID!="" && in.ChainID!=expectedChainID { errs=append(errs,"bad_chain_id") }
      if in.AmountMas<=0 { errs=append(errs,"amount_le_0") }
      expFee:=minFeeMas(in.AmountMas, feeBps)
      if in.FeeMas<expFee { errs=append(errs,"fee_below_min") }
      if len(in.From)<4 || len(in.To)<4 || in.From[:4]!="tho1" || in.To[:4]!="tho1" { errs=append(errs,"addr_format") }
      h:=getHeight(); if in.ExpiryHeight>0 && in.ExpiryHeight<=h { errs=append(errs,"expired_height") }
      if len(errs)>0 {
        w.Header().Set("Content-Type","application/json")
        w.WriteHeader(http.StatusBadRequest)
        _=json.NewEncoder(w).Encode(map[string]any{
          "ok":false,"reason":"tx_precheck_failed","errors":errs,
          "expected_fee_mas":expFee,"expected_chain_id":expectedChainID,"current_height":h,
        })
        return
      }
      r.Body=io.NopCloser(bytes.NewReader(buf))
    }
    next.ServeHTTP(w,r)
  })
}
"@
}
if (-not [regex]::IsMatch($src,'(?m)^\s*func\s+calcCommit\s*\(')) {
$append += @"
func calcCommit(in txPre) string {
  s := fmt.Sprintf("%d|%s|%s|%d|%d|%d|%s|%d", in.Type,in.From,in.To,in.AmountMas,in.FeeMas,in.Nonce,in.ChainID,in.ExpiryHeight)
  sum := sha256.Sum256([]byte(s))
  return hex.EncodeToString(sum[:])
}
"@
}
if (-not [regex]::IsMatch($src,'(?m)^\s*func\s+precheckCommit\s*\(')) {
$append += @"
func precheckCommit(next http.Handler) http.Handler {
  must := os.Getenv("THOMAS_REQUIRE_COMMIT") == "1"
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method==http.MethodPost && r.URL.Path=="/tx" {
      buf,_:=io.ReadAll(r.Body); r.Body.Close()
      var in txPre
      if err:=json.Unmarshal(buf,&in); err!=nil {
        w.WriteHeader(http.StatusBadRequest)
        _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"malformed_json"})
        return
      }
      expect := calcCommit(in)
      if in.MsgCommitment=="" {
        if must {
          w.WriteHeader(http.StatusBadRequest)
          _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"commitment_required","expected_message":expect})
          return
        }
      } else if in.MsgCommitment!=expect {
        w.WriteHeader(http.StatusBadRequest)
        _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"bad_commitment","expected_message":expect})
        return
      }
      r.Body=io.NopCloser(bytes.NewReader(buf))
    }
    next.ServeHTTP(w,r)
  })
}
"@
}
if (-not [regex]::IsMatch($src,'(?m)^\s*func\s+precheckSig\s*\(')) {
$append += @"
func precheckSig(next http.Handler) http.Handler {
  must := os.Getenv("THOMAS_VERIFY_SIG") == "1"
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method==http.MethodPost && r.URL.Path=="/tx" {
      buf,_:=io.ReadAll(r.Body); r.Body.Close()
      var in txPre
      if err:=json.Unmarshal(buf,&in); err!=nil {
        w.WriteHeader(http.StatusBadRequest)
        _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"malformed_json"})
        return
      }
      commitHex := calcCommit(in)
      pkB64 := r.Header.Get("X-PubKey")
      sgB64 := r.Header.Get("X-Sig")
      if sgB64=="" && in.Sig!="" { sgB64=in.Sig }
      if pkB64=="" || sgB64=="" {
        if must {
          w.WriteHeader(http.StatusBadRequest)
          _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"signature_required","expected_message":commitHex})
          return
        }
      } else {
        pk, e1 := base64.StdEncoding.DecodeString(pkB64)
        sg, e2 := base64.StdEncoding.DecodeString(sgB64)
        if e1!=nil || e2!=nil || len(pk)!=ed25519.PublicKeySize || len(sg)!=ed25519.SignatureSize {
          w.WriteHeader(http.StatusBadRequest)
          _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"bad_signature_encoding"})
          return
        }
        if !ed25519.Verify(ed25519.PublicKey(pk), []byte(commitHex), sg) {
          w.WriteHeader(http.StatusBadRequest)
          _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"bad_signature","expected_message":commitHex})
          return
        }
      }
      r.Body=io.NopCloser(bytes.NewReader(buf))
    }
    next.ServeHTTP(w,r)
  })
}
"@
}
if($append.Length -gt 0){ $src = $src + "`r`n" + $append }

# 7) NewRouter 내부 return 체인 정규화
$chain = "return debuglogRateLimit(txPrecheckWith(precheckSig(precheckCommit($routerVar)), func() int64 { return int64(eng.CurrentHeight()) }))"
$lines = ($inside -split "\r?\n"); $lastReturn=-1
for($k=0;$k -lt $lines.Count;$k++){ if($lines[$k].TrimStart().StartsWith('return ')){ $lastReturn=$k } }
if($lastReturn -ge 0){ $indent = $lines[$lastReturn] -replace '^( *).*','$1'; $lines[$lastReturn] = $indent + $chain }
else { $lines += "    $chain" }
$inside2 = ($lines -join "`r`n")
$src = $before + "`r`n" + $inside2 + $after

# 8) 저장 & 빌드
Set-Content $target $src -Encoding UTF8
"PATCHED & SAVED: $target"
if (-not (Try-Build)) { throw "go build failed after autopatch" }

# 9) (옵션) 서버 띄우고 readyz 확인
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force
$env:THOMAS_REQUIRE_COMMIT='1'; $env:THOMAS_VERIFY_SIG='1'
$p = Start-Process .\thomasd_dbg.exe -RedirectStandardOutput .\thomasd_out.log -RedirectStandardError .\thomasd_err.log -PassThru
for($i=0;$i -lt 60;$i++){
  $c = Get-NetTCPConnection -State Listen -OwningProcess $p.Id -ea SilentlyContinue | ? LocalAddress -in @('127.0.0.1','::1')
  if($c){ $port=$c[0].LocalPort; break }
  if($p.HasExited){ throw "proc exited`n$(Get-Content .\thomasd_err.log -Tail 80 -ea SilentlyContinue -join "`n")" }
  Start-Sleep -Milliseconds 200
}
if(-not $port){ throw "listen timeout" }
$host = '127.0.0.1'
"LISTEN: http://$($host):$port"
try { (Invoke-WebRequest "http://$($host):$port/readyz" -TimeoutSec 3).Content | Write-Output } catch { "readyz failed: $($_.Exception.Message)" }


 -ForegroundColor Yellow }
    return $false
  }
  return $true
}
  return $true
}

function Ensure-Import([string]$code,[string]$path){
  $hasBlk=[regex]::IsMatch($code,'(?s)import\s*\((.*?)\)')
  $line = "(?m)^\s*(?:\w+\s+)?`"$([Regex]::Escape($path))`"\s*$"
  if($hasBlk){
    if(-not [regex]::IsMatch($code,$line)){
      $re=[regex]::new('(?s)import\s*\((.*?)\)')
      $code=$re.Replace($code,{ param($m) $b=$m.Groups[1].Value; $b=($b.TrimEnd()+"`r`n`t`"$path`"`r`n"); "import (`r`n$($b.TrimEnd())`r`n)" },1)
    }
  } else {
    $code = $code -replace 'package\s+rpc', "package rpc`r`n`r`nimport (`r`n`t`"$path`"`r`n)"
  }
  return $code
}

function Find-NewRouterSpan([string]$code){
  $m = [regex]::Match($code,'func\s+NewRouter\s*\([^\)]*\)\s*[^\{]*\{',[System.Text.RegularExpressions.RegexOptions]::Singleline)
  if(-not $m.Success){ throw "NewRouter signature not found" }
  $i = $code.IndexOf('{', $m.Index)
  if($i -lt 0){ throw "opening brace for NewRouter not found" }

  $depth=1; $j=$i+1
  $inLine=$false; $inBlock=$false; $inDQ=$false; $inRaw=$false; $inRune=$false; $esc=$false

  while($j -lt $code.Length){
    $ch = $code[$j]
    $nxt = if($j+1 -lt $code.Length){ $code[$j+1] } else { [char]0 }

    if($inLine){ if($ch -eq "`n"){ $inLine=$false }; $j++; continue }
    if($inBlock){ if($ch -eq '*' -and $nxt -eq '/'){ $inBlock=$false; $j+=2; continue }; $j++; continue }

    if($inDQ){
      if($esc){ $esc=$false; $j++; continue }
      if($ch -eq [char]92){ $esc=$true; $j++; continue }  # backslash
      if($ch -eq '"'){ $inDQ=$false }
      $j++; continue
    }
    if($inRaw){ if($ch -eq '`'){ $inRaw=$false }; $j++; continue }

    if($inRune){
      if($esc){ $esc=$false; $j++; continue }
      if($ch -eq [char]92){ $esc=$true; $j++; continue }  # backslash
      if($ch -eq [char]39){ $inRune=$false }              # single quote '
      $j++; continue
    }

    if($ch -eq '/' -and $nxt -eq '/'){ $inLine=$true; $j+=2; continue }
    if($ch -eq '/' -and $nxt -eq '*'){ $inBlock=$true; $j+=2; continue }
    if($ch -eq '"'){ $inDQ=$true; $j++; continue }
    if($ch -eq '`'){ $inRaw=$true; $j++; continue }
    if($ch -eq [char]39){ $inRune=$true; $j++; continue } # enter rune

    if($ch -eq '{'){ $depth++; $j++; continue }
    if($ch -eq '}'){ $depth--; if($depth -eq 0){ return @{ Open=$i; Close=$j } }; $j++; continue }
    $j++
  }
  throw "closing brace for NewRouter not found"
}

function Remove-DuplicateBlocks([string]$code,[string]$startPat){
  $re=[regex]::new($startPat,[System.Text.RegularExpressions.RegexOptions]::Singleline)
  $ms=$re.Matches($code) | Sort-Object Index
  if($ms.Count -le 1){ return $code }
  for($k=$ms.Count-1; $k -ge 1; $k--){
    $m=$ms[$k]; $open=$code.IndexOf('{',$m.Index); if($open -lt 0){ continue }
    $d=1; $end=$open+1
    while($end -lt $code.Length){ $ch=$code[$end]; if($ch -eq '{'){$d++} elseif($ch -eq '}'){ $d--; if($d -eq 0){ $end++; break } }; $end++ }
    $ls=($code.LastIndexOf("`n",$m.Index)+1); if($ls -lt 0){$ls=0}
    $code=$code.Remove($ls, ($end-$ls))
  }
  return $code
}
function Remove-VarDup([string]$code,[string]$name){
  $re=[regex]::new("var\s+$name\s*=\s*func\([\s\S]*?\)\s*\{[\s\S]*?\}\(\)",[System.Text.RegularExpressions.RegexOptions]::Singleline)
  $ms=$re.Matches($code) | Sort-Object Index
  for($k=$ms.Count-1; $k -ge 1; $k--){ $m=$ms[$k]; $code=$code.Remove($m.Index,$m.Length) }
  return $code
}

# 1) 백업 & 로드
$bk = "$target.bak_$(Get-Date -Format 'yyyyMMdd_HHmmss')"; Copy-Item $target $bk -Force; "BACKUP: $bk"
[string]$src = Get-Content $target -Raw

# 2) 찌꺼기/오타 정리 + 중복 제거
$src = [regex]::Replace($src,'(?s)// === AUTO: .*?// === END AUTO ===','')
$src = $src -replace 'v!="";\s*\{','v != "" {'
$src = $src -replace 'v!="";\s*\)','v != "" )'
$src = $src -replace 'THOMAS_FEE_BPS"\);\s*\{','THOMAS_FEE_BPS"); {'
$src = Remove-DuplicateBlocks $src 'type\s+txPre\s+struct\s*\{'
$src = Remove-DuplicateBlocks $src 'func\s+minFeeMas\s*\('
$src = Remove-DuplicateBlocks $src 'func\s+txPrecheck(?:With)?\s*\('
$src = Remove-DuplicateBlocks $src 'func\s+precheckCommit\s*\('
$src = Remove-DuplicateBlocks $src 'func\s+precheckSig\s*\('
$src = Remove-VarDup $src 'expectedChainID'
$src = Remove-VarDup $src 'feeBps'

# 3) import 보강
foreach($im in @('net/http','time','encoding/json','bytes','io','os','strconv','fmt','crypto/sha256','encoding/hex','encoding/base64','crypto/ed25519','runtime')){
  $src = Ensure-Import $src $im
}

# 4) NewRouter 범위
$span = Find-NewRouterSpan $src
$before = $src.Substring(0,$span.Open+1)
$inside = $src.Substring($span.Open+1, $span.Close-($span.Open+1))
$after  = $src.Substring($span.Close)

# 라우터 변수
$routerVar='mux'
if ($inside -match '([A-Za-z_]\w*)\s*:=\s*http\.NewServeMux\(\)'){ $routerVar=$matches[1] }

# 5) 기본 핸들러들 (없을 때만)
if ($inside -notmatch 'HandleFunc\("/stats\.json"') {
  $inside += @"
    $routerVar.HandleFunc("/stats.json", func(w http.ResponseWriter, r *http.Request) {
      w.Header().Set("Content-Type","application/json")
      _ = json.NewEncoder(w).Encode(map[string]any{
        "height": eng.CurrentHeight(),
        "receipts_count": eng.ReceiptCount(),
        "time_utc": time.Now().UTC().Format(time.RFC3339),
      })
    })
"@
}
if ($inside -notmatch 'HandleFunc\("/metrics"') {
  $inside += @"
    $routerVar.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
      w.Header().Set("Content-Type","text/plain; version=0.0.4")
      fmt.Fprintf(w, "# TYPE thomas_height gauge\nthomas_height %d\n", eng.CurrentHeight())
      fmt.Fprintf(w, "# TYPE thomas_receipts_total counter\nthomas_receipts_total %d\n", eng.ReceiptCount())
      var m runtime.MemStats; runtime.ReadMemStats(&m)
      fmt.Fprintf(w, "# TYPE thomas_uptime_seconds gauge\nthomas_uptime_seconds %d\n", eng.UptimeSeconds())
      fmt.Fprintf(w, "# TYPE thomas_goroutines gauge\nthomas_goroutines %d\n", runtime.NumGoroutine())
      fmt.Fprintf(w, "# TYPE thomas_mem_alloc_bytes gauge\nthomas_mem_alloc_bytes %d\n", m.Alloc)
    })
"@
}
if ($inside -notmatch 'HandleFunc\("/readyz"') {
  $inside += @"
    $routerVar.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
      w.Header().Set("Content-Type","application/json")
      _ = json.NewEncoder(w).Encode(map[string]any{
        "status":"ready",
        "time_utc": time.Now().UTC().Format(time.RFC3339),
        "height": eng.CurrentHeight(),
        "uptime_secs": eng.UptimeSeconds(),
      })
    })
"@
}
if ($src -notmatch 'func\s+debuglogRateLimit\(') {
  $src += @"
type tokenBucket struct{ cap, tokens, refillPerSec int; last time.Time }
func newBucket(c, r int) *tokenBucket { return &tokenBucket{cap:c, tokens:c, refillPerSec:r, last:time.Now()} }
func (b *tokenBucket) take(n int) bool {
  now := time.Now()
  el := int(now.Sub(b.last).Seconds()) * b.refillPerSec
  if el>0 { b.tokens = imin(b.cap, b.tokens+el); b.last = now }
  if b.tokens>=n { b.tokens-=n; return true }
  return false
}
func imin(a,b int)int{ if a<b {return a}; return b }
var dbgBucket = newBucket(100,50)
func debuglogRateLimit(next http.Handler) http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method==http.MethodPost && r.URL.Path=="/tx" && r.URL.RawQuery=="debuglog=1" {
      if !dbgBucket.take(1) {
        w.WriteHeader(http.StatusTooManyRequests)
        _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"debuglog_ratelimited"})
        return
      }
    }
    next.ServeHTTP(w,r)
  })
}
func debuglogStatus(w http.ResponseWriter, r *http.Request){
  w.Header().Set("Content-Type","application/json")
  _ = json.NewEncoder(w).Encode(map[string]any{
    "capacity": dbgBucket.cap, "tokens": dbgBucket.tokens, "refill_per_sec": dbgBucket.refillPerSec,
    "time_utc": time.Now().UTC().Format(time.RFC3339),
  })
}
"@
}
if ($inside -notmatch 'HandleFunc\("/ratelimit\.debuglog"') {
  $inside += @"
    $routerVar.HandleFunc("/ratelimit.debuglog", debuglogStatus)
"@
}

# 6) tx 미들웨어 세트(없을 때만 추가)
$append = ""
if (-not [regex]::IsMatch($src,'(?m)^\s*type\s+txPre\s+struct')) {
$append += @"
type txPre struct {
  Type          int    `json:"type"`
  From          string `json:"from"`
  To            string `json:"to"`
  AmountMas     int64  `json:"amount_mas"`
  FeeMas        int64  `json:"fee_mas"`
  Nonce         int64  `json:"nonce"`
  ChainID       string `json:"chain_id"`
  ExpiryHeight  int64  `json:"expiry_height"`
  MsgCommitment string `json:"msg_commitment"`
  Sig           string `json:"sig,omitempty"`
}
"@
}
if (-not [regex]::IsMatch($src,'(?m)^\s*func\s+minFeeMas\s*\(')) {
$append += @"
func minFeeMas(amount int64, bps int) int64 { f:=(amount*int64(bps))/10000; if f<1 {f=1}; return f }
"@
}
if (-not [regex]::IsMatch($src,'(?m)^\s*var\s+expectedChainID\s*=')) {
$append += @"
var expectedChainID = func() string {
  if v := os.Getenv("THOMAS_CHAIN_ID"); v != "" { return v }
  return "thomas-dev-1"
}()
"@
}
if (-not [regex]::IsMatch($src,'(?m)^\s*var\s+feeBps\s*=')) {
$append += @"
var feeBps = func() int {
  if v := os.Getenv("THOMAS_FEE_BPS"); v != "" {
    if n,err := strconv.Atoi(v); err==nil && n>0 { return n }
  }
  return 10
}()
"@
}
if (-not [regex]::IsMatch($src,'(?m)^\s*func\s+txPrecheckWith\s*\(')) {
$append += @"
func txPrecheckWith(next http.Handler, getHeight func() int64) http.Handler {
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method==http.MethodPost && r.URL.Path=="/tx" {
      buf,_:=io.ReadAll(r.Body); r.Body.Close()
      var in txPre
      if err:=json.Unmarshal(buf,&in); err!=nil {
        w.WriteHeader(http.StatusBadRequest)
        _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"malformed_json"})
        return
      }
      errs:=make([]string,0,4)
      if in.ChainID!="" && in.ChainID!=expectedChainID { errs=append(errs,"bad_chain_id") }
      if in.AmountMas<=0 { errs=append(errs,"amount_le_0") }
      expFee:=minFeeMas(in.AmountMas, feeBps)
      if in.FeeMas<expFee { errs=append(errs,"fee_below_min") }
      if len(in.From)<4 || len(in.To)<4 || in.From[:4]!="tho1" || in.To[:4]!="tho1" { errs=append(errs,"addr_format") }
      h:=getHeight(); if in.ExpiryHeight>0 && in.ExpiryHeight<=h { errs=append(errs,"expired_height") }
      if len(errs)>0 {
        w.Header().Set("Content-Type","application/json")
        w.WriteHeader(http.StatusBadRequest)
        _=json.NewEncoder(w).Encode(map[string]any{
          "ok":false,"reason":"tx_precheck_failed","errors":errs,
          "expected_fee_mas":expFee,"expected_chain_id":expectedChainID,"current_height":h,
        })
        return
      }
      r.Body=io.NopCloser(bytes.NewReader(buf))
    }
    next.ServeHTTP(w,r)
  })
}
"@
}
if (-not [regex]::IsMatch($src,'(?m)^\s*func\s+calcCommit\s*\(')) {
$append += @"
func calcCommit(in txPre) string {
  s := fmt.Sprintf("%d|%s|%s|%d|%d|%d|%s|%d", in.Type,in.From,in.To,in.AmountMas,in.FeeMas,in.Nonce,in.ChainID,in.ExpiryHeight)
  sum := sha256.Sum256([]byte(s))
  return hex.EncodeToString(sum[:])
}
"@
}
if (-not [regex]::IsMatch($src,'(?m)^\s*func\s+precheckCommit\s*\(')) {
$append += @"
func precheckCommit(next http.Handler) http.Handler {
  must := os.Getenv("THOMAS_REQUIRE_COMMIT") == "1"
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method==http.MethodPost && r.URL.Path=="/tx" {
      buf,_:=io.ReadAll(r.Body); r.Body.Close()
      var in txPre
      if err:=json.Unmarshal(buf,&in); err!=nil {
        w.WriteHeader(http.StatusBadRequest)
        _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"malformed_json"})
        return
      }
      expect := calcCommit(in)
      if in.MsgCommitment=="" {
        if must {
          w.WriteHeader(http.StatusBadRequest)
          _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"commitment_required","expected_message":expect})
          return
        }
      } else if in.MsgCommitment!=expect {
        w.WriteHeader(http.StatusBadRequest)
        _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"bad_commitment","expected_message":expect})
        return
      }
      r.Body=io.NopCloser(bytes.NewReader(buf))
    }
    next.ServeHTTP(w,r)
  })
}
"@
}
if (-not [regex]::IsMatch($src,'(?m)^\s*func\s+precheckSig\s*\(')) {
$append += @"
func precheckSig(next http.Handler) http.Handler {
  must := os.Getenv("THOMAS_VERIFY_SIG") == "1"
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method==http.MethodPost && r.URL.Path=="/tx" {
      buf,_:=io.ReadAll(r.Body); r.Body.Close()
      var in txPre
      if err:=json.Unmarshal(buf,&in); err!=nil {
        w.WriteHeader(http.StatusBadRequest)
        _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"malformed_json"})
        return
      }
      commitHex := calcCommit(in)
      pkB64 := r.Header.Get("X-PubKey")
      sgB64 := r.Header.Get("X-Sig")
      if sgB64=="" && in.Sig!="" { sgB64=in.Sig }
      if pkB64=="" || sgB64=="" {
        if must {
          w.WriteHeader(http.StatusBadRequest)
          _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"signature_required","expected_message":commitHex})
          return
        }
      } else {
        pk, e1 := base64.StdEncoding.DecodeString(pkB64)
        sg, e2 := base64.StdEncoding.DecodeString(sgB64)
        if e1!=nil || e2!=nil || len(pk)!=ed25519.PublicKeySize || len(sg)!=ed25519.SignatureSize {
          w.WriteHeader(http.StatusBadRequest)
          _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"bad_signature_encoding"})
          return
        }
        if !ed25519.Verify(ed25519.PublicKey(pk), []byte(commitHex), sg) {
          w.WriteHeader(http.StatusBadRequest)
          _=json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"bad_signature","expected_message":commitHex})
          return
        }
      }
      r.Body=io.NopCloser(bytes.NewReader(buf))
    }
    next.ServeHTTP(w,r)
  })
}
"@
}
if($append.Length -gt 0){ $src = $src + "`r`n" + $append }

# 7) NewRouter 내부 return 체인 정규화
$chain = "return debuglogRateLimit(txPrecheckWith(precheckSig(precheckCommit($routerVar)), func() int64 { return int64(eng.CurrentHeight()) }))"
$lines = ($inside -split "\r?\n"); $lastReturn=-1
for($k=0;$k -lt $lines.Count;$k++){ if($lines[$k].TrimStart().StartsWith('return ')){ $lastReturn=$k } }
if($lastReturn -ge 0){ $indent = $lines[$lastReturn] -replace '^( *).*','$1'; $lines[$lastReturn] = $indent + $chain }
else { $lines += "    $chain" }
$inside2 = ($lines -join "`r`n")
$src = $before + "`r`n" + $inside2 + $after

# 8) 저장 & 빌드
Set-Content $target $src -Encoding UTF8
"PATCHED & SAVED: $target"
if (-not (Try-Build)) { throw "go build failed after autopatch" }

# 9) (옵션) 서버 띄우고 readyz 확인
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force
$env:THOMAS_REQUIRE_COMMIT='1'; $env:THOMAS_VERIFY_SIG='1'
$p = Start-Process .\thomasd_dbg.exe -RedirectStandardOutput .\thomasd_out.log -RedirectStandardError .\thomasd_err.log -PassThru
for($i=0;$i -lt 60;$i++){
  $c = Get-NetTCPConnection -State Listen -OwningProcess $p.Id -ea SilentlyContinue | ? LocalAddress -in @('127.0.0.1','::1')
  if($c){ $port=$c[0].LocalPort; break }
  if($p.HasExited){ throw "proc exited`n$(Get-Content .\thomasd_err.log -Tail 80 -ea SilentlyContinue -join "`n")" }
  Start-Sleep -Milliseconds 200
}
if(-not $port){ throw "listen timeout" }
$host = '127.0.0.1'
"LISTEN: http://$($host):$port"
try { (Invoke-WebRequest "http://$($host):$port/readyz" -TimeoutSec 3).Content | Write-Output } catch { "readyz failed: $($_.Exception.Message)" }



