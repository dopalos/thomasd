$ErrorActionPreference = "Stop"
$ProgressPreference    = "SilentlyContinue"
[System.Net.ServicePointManager]::Expect100Continue = $false

function Resolve-RepoRoot {
  param([string]$StartDir = (Get-Location).Path)
  $d = Get-Item -LiteralPath $StartDir
  while ($null -ne $d) {
    if (Test-Path (Join-Path $d.FullName 'go.mod')) { return $d.FullName }
    $d = $d.Parent
  }
  $fallback = "C:\thomas-scaffold\thomasd"
  if (Test-Path (Join-Path $fallback 'go.mod')) { return $fallback }
  throw "go.mod not found"
}

function Invoke-GoBuild([string]$tags) {
  & go clean -cache | Out-Null
  & go mod tidy        | Out-Null
  $stderr = Join-Path $PWD "go_build.stderr.txt"
  $stdout = Join-Path $PWD "go_build.stdout.txt"
  Remove-Item $stderr,$stdout -ErrorAction SilentlyContinue
  $args = @('build')
  if ($tags) { $args += @('-tags', $tags) }
  $args += @('-o', '.\thomasd_dbg.exe', '.\cmd\thomasd')

  $p = Start-Process -FilePath "go.exe" -ArgumentList $args `
        -NoNewWindow -PassThru -Wait `
        -RedirectStandardError $stderr `
        -RedirectStandardOutput $stdout
  return [pscustomobject]@{ ExitCode = $p.ExitCode; Stderr = $stderr; Stdout = $stdout; Tags=$tags }
}

function Show-BuildTail($result) {
  Write-Host "`n--- build errors (tail) ---" -ForegroundColor Yellow
  if (Test-Path $result.Stderr) { Get-Content $result.Stderr -Tail 80 }
  elseif (Test-Path $result.Stdout) { Get-Content $result.Stdout -Tail 80 }
  else { Write-Host "(no log files?)" -ForegroundColor Yellow }
}

# 콤마로 구분된 return 식을 최상위 레벨에서만 split
function Split-TopComma([string]$expr){
  $items=@(); $buf=""
  $r=0; $c=0; $s=0  # () {} []
  $inDQ=$false; $inRaw=$false; $inLine=$false; $inBlock=$false
  for($i=0;$i -lt $expr.Length;$i++){
    $ch=$expr[$i]; $nx = if($i+1 -lt $expr.Length){ $expr[$i+1] } else { [char]0 }
    if($inLine){ if($ch -eq "`n"){ $inLine=$false; $buf+=$ch }; continue }
    if($inBlock){ if($ch -eq '*' -and $nx -eq '/'){ $inBlock=$false; $i++; $buf+='*/'; continue }; $buf+=$ch; continue }
    if($inDQ){ if($ch -eq '\'){ if($i+1 -lt $expr.Length){ $buf+=$ch+$expr[$i+1]; $i++; continue } }; if($ch -eq '"'){ $inDQ=$false }; $buf+=$ch; continue }
    if($inRaw){ if($ch -eq '`'){ $inRaw=$false }; $buf+=$ch; continue }
    if($ch -eq '/' -and $nx -eq '/'){ $inLine=$true; $i++; $buf+='//'; continue }
    if($ch -eq '/' -and $nx -eq '*'){ $inBlock=$true; $i++; $buf+='/*'; continue }
    if($ch -eq '"'){ $inDQ=$true; $buf+=$ch; continue }
    if($ch -eq '`'){ $inRaw=$true; $buf+=$ch; continue }
    if($ch -eq '('){ $r++; $buf+=$ch; continue }
    if($ch -eq ')'){ if($r -gt 0){ $r-- }; $buf+=$ch; continue }
    if($ch -eq '{'){ $c++; $buf+=$ch; continue }
    if($ch -eq '}'){ if($c -gt 0){ $c-- }; $buf+=$ch; continue }
    if($ch -eq '['){ $s++; $buf+=$ch; continue }
    if($ch -eq ']'){ if($s -gt 0){ $s-- }; $buf+=$ch; continue }
    if($ch -eq ',' -and $r -eq 0 -and $c -eq 0 -and $s -eq 0){
      $items += $buf.Trim()
      $buf=""
      continue
    }
    $buf+=$ch
  }
  if($buf.Trim().Length -gt 0){ $items += $buf.Trim() }
  return $items
}

function Wrap-NewRouter-Smart([string]$code){
  $m = [regex]::Match($code,'(?s)func\s+NewRouter\s*\([^)]*\)\s*[^{]*\{')
  if(-not $m.Success){ throw "NewRouter signature not found" }
  $start = $m.Index
  $rest  = $code.Substring($start + 1)
  $next  = [regex]::Match($rest,'(?m)^\s*func\s+\w+')
  $end   = if($next.Success){ $start + 1 + $next.Index } else { $code.Length }
  $seg   = $code.Substring($start, $end - $start)

  # 이미 감싸짐?
  if($seg -match 'return\s+.+precheckSig\s*\(\s*precheckCommit'){ return $code }

  # router 변수 후보 추정
  $routerVar = $null
  if($seg -match '([A-Za-z_]\w*)\s*:=\s*http\.NewServeMux\(\)'){ $routerVar=$matches[1] }

  # 마지막 return
  $retMatches = [regex]::Matches($seg,'(?m)^\s*(return)\s+(.+?)\s*$')
  if($retMatches.Count -eq 0){ throw "return not found in NewRouter" }
  $last = $retMatches[$retMatches.Count-1]
  $indent = ($last.Groups[1].Index - $last.Index) | ForEach-Object { ' ' * $_ }
  $retExpr = $last.Groups[2].Value.Trim()

  $items = Split-TopComma $retExpr
  $idx = -1

  if($routerVar){
    for($i=0;$i -lt $items.Count;$i++){ if($items[$i].Trim() -eq $routerVar){ $idx=$i; break } }
  }
  if($idx -lt 0){
    for($i=0;$i -lt $items.Count;$i++){ if($items[$i] -match 'http\.NewServeMux\s*\('){ $idx=$i; break } }
  }
  if($idx -lt 0 -and $items.Count -eq 1){ $idx=0 }
  if($idx -lt 0){
    foreach($kw in @('\bmux\b','\brouter\b','\bhandler\b','\bh\b')){
      for($i=0;$i -lt $items.Count -and $idx -lt 0;$i++){
        if($items[$i] -match $kw){ $idx=$i; break }
      }
      if($idx -ge 0){ break }
    }
  }
  if($idx -lt 0){ $idx = $items.Count - 1 } # 막차로 마지막 항목을 핸들러로 가정

  $items[$idx] = "precheckSig(precheckCommit(" + $items[$idx] + "))"
  $newRet = $indent + "return " + ($items -join ", ")

  $seg2 = $seg.Substring(0,$last.Index) + $newRet + $seg.Substring($last.Index + $last.Length)
  return $code.Substring(0,$start) + $seg2 + $code.Substring($end)
}

# ===== main =====
$root = Resolve-RepoRoot
Set-Location $root
"ROOT = $root"
$target = "internal\rpc\router.go"
if (-not (Test-Path $target)) { throw "router.go not found: $target" }

# 백업
$bk = "$target.bak_tuple_$(Get-Date -Format 'yyyyMMdd_HHmmss')"
Copy-Item $target $bk -Force
"BACKUP: $bk"

# 래핑
[string]$code = Get-Content $target -Raw
$code2 = Wrap-NewRouter-Smart $code
if ($code2 -ne $code) { "PATCH: tuple-safe wrap applied" } else { "PATCH: already wrapped (skip)" }
Set-Content $target $code2 -Encoding UTF8
"PATCHED: $target"

# 빌드 (태그 시도 순서 유지)
$result = Invoke-GoBuild 'cbor blake3'
if ($result.ExitCode -ne 0) { $result = Invoke-GoBuild 'cbor' }
if ($result.ExitCode -ne 0) { Show-BuildTail $result; exit 1 }
"BUILD OK"
