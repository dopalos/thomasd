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

$root = Resolve-RepoRoot
Set-Location $root
"ROOT = $root" | Write-Host

$target = "internal\rpc\router.go"
if (-not (Test-Path $target)) { throw "router.go not found: $target" }

# NewRouter의 마지막 return을 precheckSig(precheckCommit(...))로 감싸기
function Wrap-NewRouter([string]$code){
  $m = [regex]::Match($code,'(?s)func\s+NewRouter\s*\([^)]*\)\s*[^{]*\{')
  if(-not $m.Success){ throw "NewRouter signature not found" }
  $start = $m.Index
  $rest  = $code.Substring($start + 1)
  $next  = [regex]::Match($rest,'(?m)^\s*func\s+\w+')
  $end   = if($next.Success){ $start + 1 + $next.Index } else { $code.Length }
  $seg   = $code.Substring($start, $end - $start)

  if($seg -match 'return\s+precheckSig\s*\(\s*precheckCommit'){ return $code } # 이미 감쌈

  $routerVar = $null
  if($seg -match '([A-Za-z_]\w*)\s*:=\s*http\.NewServeMux\(\)'){ $routerVar=$matches[1] }

  $retMatches = [regex]::Matches($seg,'(?m)^\s*return\s+(.+?)\s*$')
  if($retMatches.Count -eq 0){ throw "return not found in NewRouter" }
  $last    = $retMatches[$retMatches.Count-1]
  $retExpr = $last.Groups[1].Value.Trim()

  $wrapExpr = if($routerVar){ "precheckSig(precheckCommit($routerVar))" } else { "precheckSig(precheckCommit($retExpr))" }
  $wrapped  = "return $wrapExpr"

  $seg2 = $seg.Substring(0,$last.Index) + $wrapped + $seg.Substring($last.Index + $last.Length)
  return $code.Substring(0,$start) + $seg2 + $code.Substring($end)
}

# 로드 & 백업
[string]$src = Get-Content $target -Raw
$bk = "$target.bak_$(Get-Date -Format 'yyyyMMdd_HHmmss')"
Copy-Item $target $bk -Force
"BACKUP: $bk" | Write-Host

# 래핑 적용
$src2 = Wrap-NewRouter $src
if ($src2 -ne $src) { "PATCH: NewRouter wrapped" | Write-Host } else { "PATCH: already wrapped (skip)" | Write-Host }

# 저장
Set-Content $target $src2 -Encoding UTF8
"PATCHED: $target" | Write-Host

# 빌드 (우선 cbor+blake3, 실패 시 cbor만)
& go clean -cache | Out-Null
$build = & go build -tags "cbor blake3" -o .\thomasd_dbg.exe .\cmd\thomasd 2>&1
if ($LASTEXITCODE -ne 0) {
  Write-Host "BUILD FAILED (cbor blake3). Retrying with 'cbor' only..." -ForegroundColor Yellow
  & go clean -cache | Out-Null
  $build = & go build -tags "cbor" -o .\thomasd_dbg.exe .\cmd\thomasd 2>&1
}
if ($LASTEXITCODE -ne 0) {
  $tmp = Join-Path $PWD "go_build.stderr.txt"
  $build | Out-File -FilePath $tmp -Encoding UTF8
  Write-Host "`n--- build errors (tail) ---" -ForegroundColor Yellow
  Get-Content $tmp -Tail 80
  exit 1
}
"BUILD OK" | Write-Host
