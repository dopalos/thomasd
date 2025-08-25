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

# 3) mux 선언 찾기
$vm = [regex]::Match($code,'(?m)^(?<indent>\s*)(?<name>[A-Za-z_]\w*)\s*:=\s*http\.NewServeMux\(\)\s*$')
if(-not $vm.Success){ throw "ServeMux variable not found (http.NewServeMux)" }
$indent = $vm.Groups['indent'].Value
$name   = $vm.Groups['name'].Value
$wrap   = "${name}Wrapped"
$declIdx = $vm.Index

# 4) 이 선언이 포함된 '최상위 func' 텍스트 범위 구하기
#    - 선언 이전에서 마지막 '^func ' 시작점
$topBefore = [regex]::Matches($code.Substring(0, $declIdx), '(?m)^func\s')
if($topBefore.Count -eq 0){ throw "Top-level func header not found before mux declaration" }
$funcStartIdx = $topBefore[$topBefore.Count-1].Index

#    - 선언 이후에서 다음 '^func ' 시작점 (없으면 파일 끝까지)
$afterText = $code.Substring($declIdx)
$nextFuncs = [regex]::Matches($afterText, '(?m)^func\s')
if($nextFuncs.Count -gt 0){
  $funcEndIdx = $declIdx + $nextFuncs[0].Index
}else{
  $funcEndIdx = $code.Length
}

# 5) 세 부분 분리
$prefix = $code.Substring(0, $funcStartIdx)
$fnText = $code.Substring($funcStartIdx, $funcEndIdx - $funcStartIdx)
$suffix = $code.Substring($funcEndIdx)

# 6) 함수 내부 패치
$changed = $false

# 6-1) 선언 직후 래핑 변수 삽입 (이미 있으면 스킵)
if($fnText -notmatch "(?m)^\s*$([Regex]::Escape($wrap))\s*:="){
  $declLocal = [regex]::Match($fnText,'(?m)^(?<i>\s*)'+[Regex]::Escape($name)+'\s*:=\s*http\.NewServeMux\(\)\s*$')
  if(-not $declLocal.Success){ throw "ServeMux declaration not found inside function slice" }
  $iIndent = $declLocal.Groups['i'].Value
  $insert = "`r`n$($iIndent)$wrap := precheckSig(precheckFromBinding(precheckCommit($name)))"
  $fnText = $fnText.Insert($declLocal.Index + $declLocal.Length, $insert)
  "INSERT: $wrap := precheckSig(precheckFromBinding(precheckCommit($name)))"
  $changed = $true
}else{
  "SKIP INSERT: $wrap already present"
}

# 6-2) return 라인에서 첫 번째 '$name' 토큰만 '$wrap'으로 교체 (함수 내부에 한정)
$repAny = 0
$fnText = [regex]::Replace($fnText,
  "(?m)^(?<lead>\s*return\s+)(?<rest>.*)$",
  {
    param($m)
    $rest = $m.Groups['rest'].Value
    # 단어 경계로 첫 1회만 치환
    $newRest = [regex]::Replace($rest, "\b"+[Regex]::Escape($name)+ "\b", $wrap, 1)
    if($newRest -ne $rest){
      $script:repAny++
      return $m.Groups['lead'].Value + $newRest
    } else {
      return $m.Value
    }
  })

"RETURN REPLACED (any position): $repAny"

# 6-3) 치환이 하나도 안 됐으면, 사용 안하는 $wrap 제거
if($repAny -eq 0){
  $fnText = $fnText -replace "(?m)^\s*"+[Regex]::Escape($wrap)+"\s*:.*\r?\n",""
  "NO RETURN changed; removed $wrap to avoid 'declared and not used'."
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
