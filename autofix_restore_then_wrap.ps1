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

# ===== main =====
$root = Resolve-RepoRoot
Set-Location $root
"ROOT = $root"
$target = "internal\rpc\router.go"
if (-not (Test-Path $target)) { throw "router.go not found: $target" }

# 0) 먼저 현재 상태로 빌드 시도 (cbor+blake3 → 실패 시 cbor)
$result = Invoke-GoBuild 'cbor blake3'
if ($result.ExitCode -ne 0) {
  Write-Host "RPC build with -tags 'cbor blake3' failed. Retrying with 'cbor' only..." -ForegroundColor Yellow
  $result = Invoke-GoBuild 'cbor'
}
if ($result.ExitCode -eq 0) {
  Write-Host "BUILD OK (no restore needed)"
  exit 0
}

# 1) 실패 → 가장 최신 백업부터 되돌리며 성공할 때까지 시도
Write-Host "BUILD FAIL -> trying backups restore..." -ForegroundColor Yellow
$backups = Get-ChildItem "internal\rpc" -Filter "router.go.bak_*" -ErrorAction SilentlyContinue |
           Sort-Object LastWriteTime -Descending
if (-not $backups) { Write-Host "No backups found."; Show-BuildTail $result; exit 1 }

$restored = $false
foreach($bk in $backups){
  Write-Host "TRY RESTORE: $($bk.Name)"
  Copy-Item $bk.FullName $target -Force
  $r2 = Invoke-GoBuild 'cbor blake3'
  if ($r2.ExitCode -ne 0) { $r2 = Invoke-GoBuild 'cbor' }
  if ($r2.ExitCode -eq 0) { $restored=$true; break }
}

if (-not $restored) {
  Write-Host "All backups failed to build." -ForegroundColor Red
  Show-BuildTail $result
  exit 1
}

# 2) 복구 성공 시, NewRouter 래핑만 (이미 감싸졌으면 그대로)
[string]$code = Get-Content $target -Raw
try {
  $code2 = Wrap-NewRouter $code
  if ($code2 -ne $code) { "PATCH: NewRouter wrapped" } else { "PATCH: NewRouter already wrapped (skip)" }
  Set-Content $target $code2 -Encoding UTF8
} catch { Write-Host "WARN: wrap step: $($_.Exception.Message)" -ForegroundColor Yellow }

# 3) 최종 빌드
$result = Invoke-GoBuild 'cbor blake3'
if ($result.ExitCode -ne 0) { $result = Invoke-GoBuild 'cbor' }
if ($result.ExitCode -ne 0) {
  Show-BuildTail $result
  exit 1
}
"BUILD OK"
