$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"
if(-not (Test-Path $router)){ throw "router.go not found" }
[string]$code = Get-Content $router -Raw

# mux 라인의 들여쓰기 추출
$vm = [regex]::Match($code,'(?m)^(?<indent>\s*)mux\s*:=\s*http\.NewServeMux\(\)\s*$')
if(-not $vm.Success){ throw "mux := http.NewServeMux() not found" }
$indent = $vm.Groups['indent'].Value

$fixed = $false

# 1) 명백한 오타: "muxWrapped := muxWrapped" → 올바른 체인으로 교체
$code2 = [regex]::Replace(
  $code,
  '(?m)^\s*muxWrapped\s*:=\s*muxWrapped\s*$',
  { param($m) $script:fixed=$true; $indent + 'muxWrapped := precheckSig(precheckFromBinding(precheckCommit(mux)))' }
)

# 2) 만약 1)이 없었고, 올바른 할당도 없다면 mux 바로 아래에 삽입
if(-not $fixed){
  if($code2 -notmatch '(?m)^\s*muxWrapped\s*:=\s*precheckSig\('){
    $insertAt = $vm.Index + $vm.Length
    $ins = "`r`n" + $indent + 'muxWrapped := precheckSig(precheckFromBinding(precheckCommit(mux)))'
    $code2 = $code2.Insert($insertAt, $ins)
    "INSERTED muxWrapped after mux"
  } else {
    "muxWrapped assignment already correct; no insert."
  }
} else {
  "REPLACED bad muxWrapped assignment"
}

Set-Content $router $code2 -Encoding UTF8
"router.go patched"

# 빌드 로그 캡처 (cmd.exe 사용해 stdout/stderr 모두 파일로)
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
Write-Host "`n--- build_full.log (tail) ---" -ForegroundColor Yellow
Get-Content .\build_full.log -Tail 200
