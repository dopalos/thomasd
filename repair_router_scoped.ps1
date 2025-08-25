$ErrorActionPreference='Stop'

$target = "internal\rpc\router.go"
if(-not (Test-Path $target)){ throw "router.go not found" }

# 1) 최신 백업으로 롤백 (있을 때만)
$bk = Get-ChildItem "$target.fixwrap_only_*" -ErrorAction SilentlyContinue | Sort-Object LastWriteTime -Descending | Select-Object -First 1
if($bk){
  Copy-Item $bk.FullName $target -Force
  "ROLLED BACK: $($bk.Name)"
}else{
  "No backup found; proceeding without rollback."
}

# 2) 파일 읽기
[string]$code = Get-Content $target -Raw

# 3) mux 선언 찾기 (가장 첫 선언 기준)
$vm = [regex]::Match($code,'(?m)^(?<indent>\s*)(?<name>[A-Za-z_]\w*)\s*:=\s*http\.NewServeMux\(\)\s*$')
if(-not $vm.Success){ throw "ServeMux variable not found (http.NewServeMux)" }
$indent = $vm.Groups['indent'].Value
$name   = $vm.Groups['name'].Value
$wrap   = "${name}Wrapped"

# 4) mux 선언이 속한 함수 블록 경계 찾기
#    - 선언 위치 이전에서 마지막 'func ... {'의 '{' 위치를 찾고, 거기서 중괄호 밸런싱으로 끝 '}'를 찾음
$declIdx = $vm.Index
# func 헤더 시작 후보들
$funcHdrMatches = [regex]::Matches($code.Substring(0, $declIdx), '(?ms)func\s+[^\{]*\{')
if($funcHdrMatches.Count -eq 0){ throw "Enclosing func not found for mux declaration" }
$funcHdr = $funcHdrMatches[$funcHdrMatches.Count-1]
$funcStart = $funcHdr.Index + $funcHdr.Length - 1  # '{' 위치
# 중괄호 밸런싱
$depth = 0
$endIdx = -1
for($i=$funcStart; $i -lt $code.Length; $i++){
  if($code[$i] -eq '{'){ $depth++ }
  elseif($code[$i] -eq '}'){
    $depth--
    if($depth -eq 0){ $endIdx = $i; break }
  }
}
if($endIdx -lt 0){ throw "Function end not found" }

# 5) 함수 서브스트링 추출
$prefix = $code.Substring(0, $funcHdr.Index)
$fnText = $code.Substring($funcHdr.Index, $endIdx - $funcHdr.Index + 1)
$suffix = $code.Substring($endIdx + 1)

# 6) 함수 내부 패치
$changed = $false

# 6-1) 선언 직후 래핑 변수 삽입 (이미 있으면 건너뜀)
if($fnText -notmatch "(?m)^\s*$([Regex]::Escape($wrap))\s*:="){
  # 함수 내부에서 해당 선언 라인의 로컬 인덱스
  $declLocalLine = [regex]::Match($fnText,'(?m)^(?<i>\s*)'+[Regex]::Escape($name)+'\s*:=\s*http\.NewServeMux\(\)\s*$')
  if(-not $declLocalLine.Success){ throw "ServeMux declaration not found inside function slice" }
  $iIndent = $declLocalLine.Groups['i'].Value
  $insert = "`r`n$($iIndent)$wrap := precheckSig(precheckFromBinding(precheckCommit($name)))"
  $fnText = $fnText.Insert($declLocalLine.Index + $declLocalLine.Length, $insert)
  "INSERT: $wrap := precheckSig(precheckFromBinding(precheckCommit($name)))"
  $changed = $true
}

# 6-2) return 교체 (함수 내부에 한정)
$rep1 = 0
$fnText = [regex]::Replace($fnText,
  "(?m)^(?<p>\s*return\s+)$([Regex]::Escape($name))(?<t>\s*)$",
  { param($m) $script:rep1++; $m.Groups['p'].Value + $wrap + $m.Groups['t'].Value })

$rep2 = 0
$fnText = [regex]::Replace($fnText,
  "(?m)^(?<p>\s*return\s+.*?,\s*)$([Regex]::Escape($name))(?<t>\s*)$",
  { param($m) $script:rep2++; $m.Groups['p'].Value + $wrap + $m.Groups['t'].Value })

"RETURN REPLACED (single) : $rep1"
"RETURN REPLACED (tuple)  : $rep2"

# 6-3) 만약 return이 하나도 안 바뀌었다면, 사용되지 않는 래핑 변수 제거
if(($rep1 + $rep2) -eq 0){
  $fnText = $fnText -replace "(?m)^\s*"+[Regex]::Escape($wrap)+"\s*:.*\r?\n",""
  "NO RETURN touched; removed $wrap to avoid 'declared and not used'."
}else{
  $changed = $true
}

# 7) 재조립 & 저장
if($changed){
  $newCode = $prefix + $fnText + $suffix
  Set-Content $target $newCode -Encoding UTF8
  "PATCHED (scoped): $target"
}else{
  "No changes applied."
}

# 8) 빌드
& go clean -cache | Out-Null
$build = & go build -tags "cbor blake3" -o .\thomasd_dbg.exe .\cmd\thomasd 2>&1
if($LASTEXITCODE -ne 0){
  "BUILD FAILED (cbor blake3). Retrying with 'cbor' only..." | Write-Host -ForegroundColor Yellow
  & go clean -cache | Out-Null
  $build = & go build -tags "cbor" -o .\thomasd_dbg.exe .\cmd\thomasd 2>&1
}
if($LASTEXITCODE -ne 0){
  Write-Host "`n--- build errors (tail) ---" -ForegroundColor Yellow
  $build | Select-Object -Last 120
  exit 1
}
"BUILD OK"
