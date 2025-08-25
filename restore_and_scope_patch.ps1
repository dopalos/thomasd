$ErrorActionPreference='Stop'

$router = "internal\rpc\router.go"

# 0) 가장 최신 백업 찾기
$bk = Get-ChildItem "internal\rpc\router.go.fixwrap_only_*" -ErrorAction SilentlyContinue |
      Sort-Object LastWriteTime -Descending | Select-Object -First 1
if(-not $bk){
  throw "backup (router.go.fixwrap_only_*) not found. If you have a clean copy in VCS, restore it first."
}
Copy-Item $bk.FullName $router -Force
"ROLLED BACK to: $($bk.Name)"

# 1) 원본 읽기
[string]$code = Get-Content $router -Raw

# 2) 라우터 구성 함수(= 'mux := http.NewServeMux()'를 포함한 함수)의 범위를 찾아 한 곳만 패치
#    2-1) mux 선언 위치
$vm = [regex]::Match($code,'(?m)^(?<indent>\s*)mux\s*:=\s*http\.NewServeMux\(\)\s*$')
if(-not $vm.Success){ throw "mux := http.NewServeMux() not found (cannot continue)" }
$pos = $vm.Index

#    2-2) pos 기준, 직전의 func 헤더 탐색
$funcHdr = [regex]::Matches($code, '(?m)^func\s+[^\{]+\{')
$owner = $null
foreach($m in $funcHdr){ if($m.Index -le $pos){ $owner = $m } else { break } }
if(-not $owner){ throw "owning func header not found" }

#    2-3) { } 균형으로 함수 끝 위치 찾기
$start = $owner.Index
$brace = 0
$end = $null
for($i=$start; $i -lt $code.Length; $i++){
  $ch = $code[$i]
  if($ch -eq '{'){ $brace++ }
  elseif($ch -eq '}'){
    $brace--
    if($brace -eq 0){ $end = $i; break }
  }
}
if($end -eq $null){ throw "owning function end not found" }

$before = $code.Substring(0,$start)
$body   = $code.Substring($start,$end-$start+1)
$after  = $code.Substring($end+1)

# 3) 함수 본문(body) 안만 정밀 패치
#    3-1) muxWrapped 선언 삽입(없을 때만) — mux 라인 바로 아래
if($body -notmatch '(?m)^\s*muxWrapped\s*:=\s*precheckFromBinding\('){
  $indent = $vm.Groups['indent'].Value
  $body = [regex]::Replace(
    $body,
    '(?m)^(?<i>\s*)mux\s*:=\s*http\.NewServeMux\(\)\s*$',
    { param($m) ($m.Groups['i'].Value) + "mux := http.NewServeMux()`r`n" +
                ($m.Groups['i'].Value) + "muxWrapped := precheckFromBinding(precheckSig(precheckCommit(mux)))" },
    1
  )
  "INSERT muxWrapped in router function"
}else{
  "muxWrapped already present"
}

#    3-2) 함수 내부의 반환/호출만 안전 치환
#   a) return precheckSig(precheckCommit(mux)) → return muxWrapped
$body = [regex]::Replace(
  $body,
  '(?m)^(?<p>\s*return\s*)precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)(?<t>\s*)$',
  { param($m) $m.Groups['p'].Value + 'muxWrapped' + $m.Groups['t'].Value }
)

#   b) return ..., precheckSig(precheckCommit(mux)) → ..., muxWrapped
$body = [regex]::Replace(
  $body,
  '(?m)^(?<p>\s*return\s+.*?,\s*)precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)(?<t>\s*)$',
  { param($m) $m.Groups['p'].Value + 'muxWrapped' + $m.Groups['t'].Value }
)

#   c) txPrecheckWith(precheckSig(precheckCommit(mux)), → txPrecheckWith(muxWrapped,
$body = [regex]::Replace(
  $body,
  'txPrecheckWith\(\s*precheckSig\(\s*precheckCommit\(\s*mux\s*\)\s*\)\s*,',
  { param($m) 'txPrecheckWith(muxWrapped,' }
)

#    3-3) body 바깥은 손대지 않음 (다른 함수 반환 타입 보호)
$code2 = $before + $body + $after
Set-Content $router $code2 -Encoding UTF8
"router.go patched (scoped to router function only)"

# 4) 빌드 로그 캡처
Remove-Item .\build_full.log -ErrorAction SilentlyContinue
cmd /c "go build -tags ""cbor blake3"" -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
if($LASTEXITCODE -ne 0){
  cmd /c "go build -tags cbor -o .\thomasd_dbg.exe .\cmd\thomasd 1>build_full.log 2>&1"
}
"`n--- build_full.log (tail) ---"
Get-Content .\build_full.log -Tail 200
