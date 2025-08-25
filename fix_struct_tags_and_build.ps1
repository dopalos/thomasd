$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"
if(-not (Test-Path $router)){ throw "router.go not found" }

# 0) 소스 로드
[string]$code = Get-Content $router -Raw

# 1) 잘못 주입된 struct 태그 복구:
#    var tx struct{ From string json:"from" }  -->  var tx struct{ From string `json:"from"` }
#    (공백/개행 변형까지 모두 커버)
$pat = 'var\s+tx\s+struct\{\s*From\s+string\s+json:"from"\s*\}'
$fixed = 'var tx struct{ From string `json:"from"` }'
$before = [regex]::Matches($code, $pat).Count
if($before -gt 0){
  $code = [regex]::Replace($code, $pat, $fixed)
  "FIXED struct tags (count=$before)"
}

# 2) 혹시 다른 자리에도 동일 패턴이 있을 수 있어 한 번 더(멀티 라인 방어)
$pat2 = 'var\s+tx\s+struct\{\s*From\s+string\s+json:"from"\s*\}'
$before2 = [regex]::Matches($code, $pat2,[System.Text.RegularExpressions.RegexOptions]::Singleline).Count
if($before2 -gt 0){
  $code = [regex]::Replace($code, $pat2, $fixed,[System.Text.RegularExpressions.RegexOptions]::Singleline)
  "FIXED struct tags (singleline count=$before2)"
}

# 3) import에 필요한 패키지 확인 (log/bytes/encoding/json/io/os/strings)
$code = [regex]::Replace($code, '(?s)(\bimport\s*\(\s*)(.*?)\)', {
  param($m)
  $head=$m.Groups[1].Value; $body=$m.Groups[2].Value
  foreach($pkg in @('log','bytes','encoding/json','io','os','strings')){
    if($body -notmatch "(?m)^\s*`"$([regex]::Escape($pkg))`"\s*$"){ $body += "`r`n`t`"$pkg`"" }
  }
  $head + $body + ")"
}, 1)

# 4) 저장
Set-Content $router $code -Encoding UTF8
"router.go saved"

# 5) 빌드 (풀 로그 캡처)
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 120
