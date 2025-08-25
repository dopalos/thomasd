$ErrorActionPreference = "Stop"

# --- repo root check ---
if (-not (Test-Path ".\go.mod")) {
  if (Test-Path "C:\thomas-scaffold\thomasd\go.mod") { Set-Location "C:\thomas-scaffold\thomasd" }
  else { throw "go.mod not found. Run at repo root." }
}
$target = "internal\rpc\router.go"
if (-not (Test-Path $target)) { throw "router.go not found: $target" }

function Try-Build {
  go clean -cache | Out-Null
  & go build -tags "cbor blake3" -o .\thomasd_dbg.exe .\cmd\thomasd 2>&1 |
    Tee-Object -Variable buildOut | Out-Null
  if ($LASTEXITCODE -ne 0) {
    "`n--- go build failed (tail) ---`n" + ($buildOut | Select-Object -Last 80) -join "`n" | Write-Host -ForegroundColor Yellow
    return $false
  }
  return $true
}

# --- helpers ---
function Ensure-Import([string]$code,[string]$path){
  $hasBlk=[regex]::IsMatch($code,'(?s)import\s*\((.*?)\)')
  $line = "(?m)^\s*(?:\w+\s+)?`"$([Regex]::Escape($path))`"\s*$"
  if($hasBlk){
    if(-not [regex]::IsMatch($code,$line)){
      $code = [regex]::Replace($code,'(?s)import\s*\((.*?)\)',{
        param($m); $b=$m.Groups[1].Value; $b=($b.TrimEnd()+"`r`n`t`"$path`"`r`n"); "import (`r`n$($b.TrimEnd())`r`n)"
      },1)
    }
  } else {
    $code = $code -replace 'package\s+rpc', "package rpc`r`n`r`nimport (`r`n`t`"$path`"`r`n)"
  }
  return $code
}

function Find-NewRouterSpan([string]$code){
  $m = [regex]::Match($code,'func\s+NewRouter\s*\([^\)]*\)\s*http\.Handler\s*\{')
  if(-not $m.Success){ throw "NewRouter signature not found" }
  $i = $code.IndexOf('{', $m.Index)
  if($i -lt 0){ throw "opening brace for NewRouter not found" }
  $depth=1; $j=$i+1
  $inLine=$false; $inBlock=$false; $inDQ=$false; $inRaw=$false
  while($j -lt $code.Length){
    $ch = $code[$j]
    $nxt = if($j+1 -lt $code.Length){ $code[$j+1] } else { [char]0 }
    if($inLine){ if($ch -eq "`n"){ $inLine=$false }; $j++; continue }
    if($inBlock){ if($ch -eq '*' -and $nxt -eq '/'){ $inBlock=$false; $j+=2; continue }; $j++; continue }
    if($inDQ){ if($ch -eq '\'){ $j+=2; continue }; if($ch -eq '"'){ $inDQ=$false }; $j++; continue }
    if($inRaw){ if($ch -eq '`'){ $inRaw=$false }; $j++; continue }
    if($ch -eq '/' -and $nxt -eq '/'){ $inLine=$true; $j+=2; continue }
    if($ch -eq '/' -and $nxt -eq '*'){ $inBlock=$true; $j+=2; continue }
    if($ch -eq '"'){ $inDQ=$true; $j++; continue }
    if($ch -eq '`'){ $inRaw=$true; $j++; continue }
    if($ch -eq '{'){ $depth++; $j++; continue }
    if($ch -eq '}'){ $depth--; if($depth -eq 0){ return @{ Open=$i; Close=$j } }; $j++; continue }
    $j++
  }
  throw "closing brace for NewRouter not found"
}

# --- load & backup ---
[string]$src = Get-Content $target -Raw
$bk = "$target.bak_$(Get-Date -Format 'yyyyMMdd_HHmmss')"
Copy-Item $target $bk -Force
"BACKUP: $bk"

# --- imports needed for minimal prechecks ---
$imports = @('net/http','encoding/json','bytes','io','os','crypto/sha256','encoding/hex','encoding/base64','crypto/ed25519','fmt')
foreach($im in $imports){ $src = Ensure-Import $src $im }

# --- inject minimal prechecks if missing ---
$needTxPre   = -not [regex]::IsMatch($src,'(?m)^\s*type\s+txPre\s+struct')
$needCalc    = -not [regex]::IsMatch($src,'(?m)^\s*func\s+calcCommit\s*\(')
$needCommit  = -not [regex]::IsMatch($src,'(?m)^\s*func\s+precheckCommit\s*\(')
$needSig     = -not [regex]::IsMatch($src,'(?m)^\s*func\s+precheckSig\s*\(')

$imp = [regex]::Match($src,'(?s)import\s*\((.*?)\)')
$ins = $imp.Index + $imp.Length
$blk = ""

if($needTxPre){
$blk += @"
// === AUTO: txPre minimal ===
type txPre struct {
  Type          int    ` + "`" + `json:"type"` + "`" + `
  From          string ` + "`" + `json:"from"` + "`" + `
  To            string ` + "`" + `json:"to"` + "`" + `
  AmountMas     int64  ` + "`" + `json:"amount_mas"` + "`" + `
  FeeMas        int64  ` + "`" + `json:"fee_mas"` + "`" + `
  Nonce         int64  ` + "`" + `json:"nonce"` + "`" + `
  ChainID       string ` + "`" + `json:"chain_id"` + "`" + `
  ExpiryHeight  int64  ` + "`" + `json:"expiry_height"` + "`" + `
  MsgCommitment string ` + "`" + `json:"msg_commitment"` + "`" + `
  Sig           string ` + "`" + `json:"sig,omitempty"` + "`" + `
}
"
}

if($needCalc){
$blk += @"
// === AUTO: calcCommit ===
func calcCommit(in txPre) string {
  s := fmt.Sprintf("%d|%s|%s|%d|%d|%d|%s|%d",
    in.Type, in.From, in.To, in.AmountMas, in.FeeMas, in.Nonce, in.ChainID, in.ExpiryHeight)
  sum := sha256.Sum256([]byte(s))
  return hex.EncodeToString(sum[:])
}
"
}

if($needCommit){
$blk += @"
// === AUTO: precheckCommit (THOMAS_REQUIRE_COMMIT=1 to enforce) ===
func precheckCommit(next http.Handler) http.Handler {
  must := os.Getenv("THOMAS_REQUIRE_COMMIT") == "1"
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodPost && r.URL.Path == "/tx" {
      buf, _ := io.ReadAll(r.Body); r.Body.Close()
      var in txPre
      if err := json.Unmarshal(buf, &in); err != nil {
        w.WriteHeader(http.StatusBadRequest)
        _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"malformed_json"})
        return
      }
      expect := calcCommit(in)
      if in.MsgCommitment == "" {
        if must {
          w.WriteHeader(http.StatusBadRequest)
          _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"commitment_required","expected_message":expect})
          return
        }
      } else if in.MsgCommitment != expect {
        w.WriteHeader(http.StatusBadRequest)
        _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"bad_commitment","expected_message":expect})
        return
      }
      r.Body = io.NopCloser(bytes.NewReader(buf))
    }
    next.ServeHTTP(w, r)
  })
}
"
}

if($needSig){
$blk += @"
// === AUTO: precheckSig (THOMAS_VERIFY_SIG=1 to enforce) ===
func precheckSig(next http.Handler) http.Handler {
  must := os.Getenv("THOMAS_VERIFY_SIG") == "1"
  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodPost && r.URL.Path == "/tx" {
      buf, _ := io.ReadAll(r.Body); r.Body.Close()
      var in txPre
      if err := json.Unmarshal(buf, &in); err != nil {
        w.WriteHeader(http.StatusBadRequest)
        _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"malformed_json"})
        return
      }
      commitHex := calcCommit(in)
      pkB64 := r.Header.Get("X-PubKey")
      sgB64 := r.Header.Get("X-Sig")
      if sgB64 == "" && in.Sig != "" { sgB64 = in.Sig }
      if pkB64 == "" || sgB64 == "" {
        if must {
          w.WriteHeader(http.StatusBadRequest)
          _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"signature_required","expected_message":commitHex})
          return
        }
      } else {
        pk, e1 := base64.StdEncoding.DecodeString(pkB64)
        sg, e2 := base64.StdEncoding.DecodeString(sgB64)
        if e1 != nil || e2 != nil || len(pk) != ed25519.PublicKeySize || len(sg) != ed25519.SignatureSize {
          w.WriteHeader(http.StatusBadRequest)
          _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"bad_signature_encoding"})
          return
        }
        if !ed25519.Verify(ed25519.PublicKey(pk), []byte(commitHex), sg) {
          w.WriteHeader(http.StatusBadRequest)
          _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"bad_signature","expected_message":commitHex})
          return
        }
      }
      r.Body = io.NopCloser(bytes.NewReader(buf))
    }
    next.ServeHTTP(w, r)
  })
}
"
}

if($blk.Length -gt 0){
  $src = $src.Insert($ins, "`r`n$blk`r`n")
  "INJECTED: minimal prechecks" | Write-Host
} else {
  "SKIP inject: prechecks already present" | Write-Host
}

# --- patch NewRouter return ---
$span = Find-NewRouterSpan $src
$before = $src.Substring(0,$span.Open+1)
$inside = $src.Substring($span.Open+1, $span.Close-($span.Open+1))
$after  = $src.Substring($span.Close)

$routerVar='mux'
if ($inside -match '([A-Za-z_]\w*)\s*:=\s*http\.NewServeMux\(\)'){ $routerVar=$matches[1] }

$chain = "return precheckSig(precheckCommit($routerVar))"
$lines = $inside -split "\r?\n"
$lastReturn=-1
for($k=0;$k -lt $lines.Count;$k++){ if ($lines[$k].TrimStart().StartsWith('return ')) { $lastReturn=$k } }
if($lastReturn -ge 0){
  $indent = $lines[$lastReturn] -replace '^( *).*','$1'
  $lines[$lastReturn] = $indent + $chain
} else {
  $lines += "    $chain"
}
$inside2 = ($lines -join "`r`n")
$src2 = $before + "`r`n" + $inside2 + $after

# --- save & build ---
Set-Content $target $src2 -Encoding UTF8
"PATCHED: $target"
if (-not (Try-Build)) { throw "go build failed" }
"BUILD OK"
