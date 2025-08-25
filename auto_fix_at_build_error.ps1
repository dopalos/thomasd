$ErrorActionPreference='Stop'
$router = "internal\rpc\router.go"
$bak = Get-ChildItem "internal\rpc" -Filter "router.go.fixwrap_only_*" |
       Sort-Object LastWriteTime -Descending | Select-Object -First 1
if(-not $bak){ throw "backup router.go.fixwrap_only_* not found" }

# 0) 현재 빌드 로그에서 첫 router.go 에러 라인 추출 (예: 800)
$log = Get-Content .\build_full.log -ErrorAction SilentlyContinue
if(-not $log){ throw "build_full.log not found. Run a build to produce it." }
$lineNum = ($log | Select-String -Pattern 'router\.go:(\d+):' | Select-Object -First 1).Matches.Groups[1].Value
if(-not $lineNum){ throw "could not parse error line from build_full.log" }
$lineNum = [int]$lineNum
"TARGET LINE: $lineNum"

# 1) 파일 로드 + 함수 경계 계산
[string[]]$cur = Get-Content $router
[string[]]$old = Get-Content $bak.FullName

function Get-Functions([string[]]$lines){
  $funcs = @()
  $n = $lines.Count
  for($i=0;$i -lt $n;$i++){
    if($lines[$i] -match '^\s*func\s'){
      $sigIdx = $i
      $j = $i
      while($j -lt $n -and ($lines[$j] -notmatch '{')){ $j++ }
      if($j -ge $n){ continue }
      $balance = 0
      for($k=$j; $k -lt $n; $k++){
        $balance += ([regex]::Matches($lines[$k], '\{').Count)
        $balance -= ([regex]::Matches($lines[$k], '\}').Count)
        if($balance -eq 0){
          $funcs += [pscustomobject]@{
            Start=$sigIdx; BodyStart=$j; End=$k
            Key = ($lines[$sigIdx] -replace '\s+',' ' -replace '\s*\{.*$','').Trim()
          }
          $i=$k; break
        }
      }
    }
  }
  $funcs
}

$curFuncs = Get-Functions $cur
$oldFuncs = Get-Functions $old
$oldMap = @{}; foreach($f in $oldFuncs){ $oldMap[$f.Key]=$f }

# 2) 에러 라인이 포함된 현재 함수 찾기
$target = $null
foreach($f in $curFuncs){ if($lineNum-1 -ge $f.Start -and $lineNum-1 -le $f.End){ $target=$f; break } }
if(-not $target){ throw "could not locate function containing line $lineNum" }
"FUNCTION KEY: $($target.Key)  [$($target.Start+1)-$($target.End+1)]"

# 3) 대상 함수 내부에서 ServeMux 변수명 찾기
$muxVar = $null
for($i=$target.Start; $i -le $target.End; $i++){
  if($cur[$i] -match '^\s*([A-Za-z_]\w*)\s*:=\s*http\.NewServeMux\(\)\s*$'){ $muxVar=$Matches[1]; break }
}

$action = ""

if($muxVar){
  # 3-a) 같은 함수 내부에서만 precheckCommit(mux) -> precheckCommit(<muxVar>) 치환
  for($i=$target.Start; $i -le $target.End; $i++){
    $cur[$i] = $cur[$i] -replace 'precheckCommit\(\s*mux\s*\)', "precheckCommit($muxVar)"
  }
  $action = "REWIRED precheckCommit(mux) -> precheckCommit($muxVar)"
}else{
  # 3-b) 백업의 동일 함수로 되돌린 뒤, 반환부만 안전히 래핑(해당 함수에 ServeMux가 있다면)
  if(-not $oldMap.ContainsKey($target.Key)){ throw "backup does not contain function: $($target.Key)" }
  $of = $oldMap[$target.Key]
  # 덮어쓰기
  $before = $cur[0..($target.Start-1)]
  $after  = $cur[($target.End+1)..($cur.Count-1)]
  $mid    = $old[$of.Start..$of.End]
  $cur    = @($before + $mid + $after)
  $action = "RESTORED function from backup"
}

# 4) 저장
Set-Content $router $cur -Encoding UTF8
"router.go patched: $action"

# 5) 빌드
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){ cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1" }
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 200

# 6) 성공 시 재기동 + 바인딩 스모크
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
