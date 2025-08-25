$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"
if(-not (Test-Path $router)){ throw "router.go not found" }

# 패치 전 /tx 등록 라인 확인
"--- BEFORE (/tx registrations) ---"
Select-String $router -Pattern '"/tx"' -Context 2,2 | ForEach-Object { "{0,5}: {1}" -f $_.LineNumber, $_.Line }

[string]$code = Get-Content $router -Raw
$wrapped = 0

# 1) v.HandleFunc("/tx", handler)  -> v.Handle("/tx", precheckFromBinding(precheckSig(precheckCommit(http.HandlerFunc(handler)))))
$code = [regex]::Replace(
  $code,
  '(?m)^(?<indent>\s*)(?<v>[A-Za-z_]\w*)\.HandleFunc\(\s*"/tx"\s*,\s*(?<h>[^)]+)\)\s*$',
  {
    param($m)
    $indent = $m.Groups['indent'].Value
    $v  = $m.Groups['v'].Value
    $h  = $m.Groups['h'].Value.Trim()
    if($h -match 'precheckFromBinding\s*\('){
      return $m.Value # 이미 감싸져 있음
    }
    $script:wrapped++
    return $indent + "$v.Handle(""/tx"", precheckFromBinding(precheckSig(precheckCommit(http.HandlerFunc($h)))))"
  }
)

# 2) v.Handle("/tx", handler) -> v.Handle("/tx", precheckFromBinding(precheckSig(precheckCommit(handler))))
$code = [regex]::Replace(
  $code,
  '(?m)^(?<indent>\s*)(?<v>[A-Za-z_]\w*)\.Handle\(\s*"/tx"\s*,\s*(?<h>[^)]+)\)\s*$',
  {
    param($m)
    $indent = $m.Groups['indent'].Value
    $v  = $m.Groups['v'].Value
    $h  = $m.Groups['h'].Value.Trim()
    if($h -match 'precheckFromBinding\s*\('){
      return $m.Value # 이미 감싸져 있음
    }
    $script:wrapped++
    return $indent + "$v.Handle(""/tx"", precheckFromBinding(precheckSig(precheckCommit($h))))"
  }
)

Set-Content $router $code -Encoding UTF8
"WRAPPED /tx handlers: $wrapped"

"--- AFTER (/tx registrations) ---"
Select-String $router -Pattern '"/tx"' -Context 0,0 | ForEach-Object { "{0,5}: {1}" -f $_.LineNumber, $_.Line }

# 빌드 로그 캡처
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 120
