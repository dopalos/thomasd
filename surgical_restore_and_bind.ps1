$ErrorActionPreference='Stop'

$router = "internal\rpc\router.go"
$bak = Get-ChildItem "internal\rpc" -Filter "router.go.fixwrap_only_*" |
       Sort-Object LastWriteTime -Descending | Select-Object -First 1
if(-not $bak){ throw "backup router.go.fixwrap_only_* not found" }

# 0) 노드 중지
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force

# 1) 파일 로딩
[string]$cur = Get-Content $router -Raw
[string]$old = Get-Content $bak.FullName -Raw
$curLines = $cur -split "`r?`n"
$oldLines = $old -split "`r?`n"

function Get-Functions([string[]]$lines){
  $funcs = @()
  $n = $lines.Count
  for($i=0;$i -lt $n;$i++){
    if($lines[$i] -match '^\s*func\s'){
      # 함수 시작 찾기
      $sigIdx = $i
      # 시그니처 줄부터 { 를 찾고 brace 카운트 진행
      $j = $i
      $foundBrace = $false
      while($j -lt $n){
        $line = $lines[$j]
        if($line -match '{'){
          $foundBrace = $true
          break
        }
        $j++
      }
      if(-not $foundBrace){ continue }
      # 본문 끝 찾기 (brace balance)
      $balance = 0
      for($k=$j; $k -lt $n; $k++){
        $balance += ([regex]::Matches($lines[$k], '\{').Count)
        $balance -= ([regex]::Matches($lines[$k], '\}').Count)
        if($balance -eq 0){
          # 함수 종료
          $funcs += [pscustomobject]@{
            SigLine   = $lines[$sigIdx]
            SigIndex  = $sigIdx
            Start     = $sigIdx
            BodyStart = $j
            End       = $k
            Text      = ($lines[$sigIdx..$k] -join "`n")
            Key       = ($lines[$sigIdx] -replace '\s+',' ' -replace '\s*\{.*$', '').Trim()
          }
          $i = $k
          break
        }
      }
    }
  }
  return $funcs
}

$curFuncs = Get-Functions $curLines
$oldFuncs = Get-Functions $oldLines

# 2) 빠른 룩업 맵 (백업 함수 시그니처 매칭)
$oldMap = @{}
foreach($f in $oldFuncs){ $oldMap[$f.Key] = $f }

# 3) “수상한 함수” 찾기:
#    조건: 함수 안에 'return precheckSig(' 있고, 같은 함수 내에 'http.NewServeMux()' 가 없음
$suspects = @()
foreach($f in $curFuncs){
  $t = $f.Text
  if($t -match 'return\s+precheckSig\(' -and $t -notmatch 'http\.NewServeMux\(\)'){
    $suspects += $f
  }
}
"FOUND suspect functions (no mux in scope but returning precheckSig): $($suspects.Count)"

# 4) 수상한 함수는 백업으로 복원
$curArr = [System.Collections.Generic.List[string]]::new()
$curArr.AddRange($curLines)

$replaced=0
foreach($sf in $suspects){
  if($oldMap.ContainsKey($sf.Key)){
    $of = $oldMap[$sf.Key]
    # 현재 블록 대체
    $before = $curArr.GetRange(0, $sf.Start)
    $after  = $curArr.GetRange($sf.End+1, $curArr.Count - ($sf.End+1))
    $mid    = [System.Collections.Generic.List[string]]::new()
    $mid.AddRange(($oldLines[$of.Start..$of.End]))
    $curArr = [System.Collections.Generic.List[string]]::new()
    $curArr.AddRange($before)
    $curArr.AddRange($mid)
    $curArr.AddRange($after)
    $replaced++
  }
}
"REPLACED suspect functions from backup: $replaced"

# 5) 다시 함수 테이블 재구성(라인 인덱스가 바뀌었으니)
$curLines2 = $curArr.ToArray()
$curFuncs2 = Get-Functions $curLines2

# 6) ServeMux 만드는 함수(본문에 http.NewServeMux())만 타겟팅해서 반환부/txPrecheckWith에 binding 삽입
$patched=0
for($idx=0;$idx -lt $curFuncs2.Count;$idx++){
  $f = $curFuncs2[$idx]
  if($f.Text -match 'http\.NewServeMux\(\)'){
    # 변수명 캡처: ^   <name> := http.NewServeMux()
    $name = $null
    foreach($ln in ($curLines2[$f.Start..$f.End])){
      if($ln -match '^\s*([A-Za-z_]\w*)\s*:=\s*http\.NewServeMux\(\)\s*$'){
        $name = $Matches[1]; break
      }
    }
    if(-not $name){ continue }

    # 이 함수 범위에서만 치환
    for($i=$f.Start; $i -le $f.End; $i++){
      # return precheckSig(precheckCommit(name)) -> ...FromBinding...
      if($curLines2[$i] -match ('return\s+precheckSig\s*\(\s*precheckCommit\s*\(\s*'+[regex]::Escape($name)+'\s*\)\s*\)\s*$')){
        $curLines2[$i] = $curLines2[$i] -replace ('precheckSig\s*\(\s*precheckCommit\s*\(\s*'+[regex]::Escape($name)+'\s*\)\s*\)'),
                                               ("precheckSig(precheckFromBinding(precheckCommit($name)))")
        $patched++
      }
      # txPrecheckWith(precheckSig(precheckCommit(name)),
      if($curLines2[$i] -match ('txPrecheckWith\s*\(\s*precheckSig\s*\(\s*precheckCommit\s*\(\s*'+[regex]::Escape($name)+'\s*\)\s*\)\s*,')){
        $curLines2[$i] = $curLines2[$i] -replace ('txPrecheckWith\s*\(\s*precheckSig\s*\(\s*precheckCommit\s*\(\s*'+[regex]::Escape($name)+'\s*\)\s*\)\s*,'),
                                                 ("txPrecheckWith(precheckSig(precheckFromBinding(precheckCommit($name))),")
        $patched++
      }
    }
  }
}
"PATCHED returns/txPrecheckWith in mux-builder functions: $patched"

# 7) 저장
Set-Content $router ($curLines2 -join "`r`n") -Encoding UTF8
"router.go written"

# 8) 빌드
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 200

# 9) 성공 시 기동 + 바인딩 스모크
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
