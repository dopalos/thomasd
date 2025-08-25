$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"

# 0) 프로세스 정지
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force

# 1) 라인 로드
$lines = Get-Content $router

function Find-MuxName {
  param([int]$idx, [string[]]$arr)
  for($k=$idx-1; $k -ge 0 -and $k -ge $idx-300; $k--){
    if($arr[$k] -match '^\s*([A-Za-z_]\w*)\s*:=\s*http\.NewServeMux\(\)\s*$'){
      return $Matches[1]
    }
    if($arr[$k] -match '^\s*func\s+'){ break } # 함수 시작을 넘어가면 중단
  }
  return $null
}

$changed = 0
for($i=0; $i -lt $lines.Count; $i++){
  if($lines[$i] -match 'precheckCommit\(\s*mux\s*\)'){
    $name = Find-MuxName -idx $i -arr $lines
    if($name -and $name -ne 'mux'){
      $lines[$i] = $lines[$i] -replace 'precheckCommit\(\s*mux\s*\)', "precheckCommit($name)"
      $changed++
    }
  }
}

"REPLACED precheckCommit(mux) -> precheckCommit(<var>): $changed"

Set-Content $router $lines -Encoding UTF8
"router.go saved"

# 2) 빌드
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 120

# 3) 성공 시 재기동 + 바인딩 스모크
if(Test-Path .\thomasd_dbg.exe){
  $sk  = (Get-Content .\dev_sk.b64 -Raw).Trim()
  $pub = (& go run .\sig_util.go pub $sk).Trim()
  $env:THOMAS_PUBKEY_tho1alice = $pub
  $env:THOMAS_REQUIRE_COMMIT   = '1'
  $env:THOMAS_VERIFY_SIG       = '1'

  Start-Process .\thomasd_dbg.exe -RedirectStandardOutput thomasd_out.log -RedirectStandardError thomasd_err.log
  Start-Sleep 1

  .\smoketest_binding_focus.ps1
}
