$ErrorActionPreference = "Stop"

function Resolve-RepoRoot {
  param([string]$StartDir = (Get-Location).Path)
  $dir = Get-Item -LiteralPath $StartDir
  while ($null -ne $dir) {
    if (Test-Path (Join-Path $dir.FullName 'go.mod')) { return $dir.FullName }
    $dir = $dir.Parent
  }
  $fallback = "C:\thomas-scaffold\thomasd"
  if (Test-Path (Join-Path $fallback 'go.mod')) { return $fallback }
  throw "go.mod not found."
}

$root = Resolve-RepoRoot
Set-Location $root
"ROOT = $root"

"`n== go version =="; & go version
"`n== head go.mod =="; Get-Content .\go.mod -TotalCount 12

# --- 1) blake3 import 자동 교정 (이미 했다면 변경 0으로 나옵니다) ---
$files = Get-ChildItem -Recurse -Include *.go -File | Where-Object { $_.FullName -notmatch '\\vendor\\' }
$changed = 0
foreach ($f in $files) {
  $t = Get-Content $f -Raw
  $orig = $t

  # 단일 import 라인
  $t = [regex]::Replace($t, '(?m)^\s*import\s+"blake3"\s*$', 'import blake3 "github.com/zeebo/blake3"')

  # import 블록 내부
  $t = [regex]::Replace($t, '(?s)import\s*\((.*?)\)', {
    param($m)
    $body = $m.Groups[1].Value
    $body2 = [regex]::Replace($body, '(?m)^\s*"blake3"\s*$', "`tblake3 `"github.com/zeebo/blake3`"")
    "import (`r`n$($body2.Trim())`r`n)"
  })

  if ($t -ne $orig) {
    Copy-Item $f "$($f.FullName).bak_$(Get-Date -Format 'yyyyMMdd_HHmmss')" -Force
    Set-Content $f $t -Encoding UTF8
    $changed++
  }
}
"FIXED imports: $changed file(s) changed)"

# --- 2) tidy ---
& go env -w GOPROXY="https://proxy.golang.org,direct" | Out-Null
& go mod tidy

# --- 3) 빌드 유틸 (ArgumentList 안전 처리) ---
function Build-WithArgs {
  param(
    [string[]]$ArgList,
    [string]$OutStd,
    [string]$OutErr
  )
  $safe = @($ArgList | Where-Object { $_ -ne $null -and $_ -ne '' })
  if ($safe.Count -eq 0) { throw "Build-WithArgs: empty ArgList" }
  if (Test-Path $OutStd) { Remove-Item $OutStd -Force }
  if (Test-Path $OutErr) { Remove-Item $OutErr -Force }
  $p = Start-Process -FilePath "go.exe" -ArgumentList $safe `
        -RedirectStandardOutput $OutStd -RedirectStandardError $OutErr `
        -NoNewWindow -PassThru -Wait
  return $p.ExitCode
}

$rpcStd = Join-Path $PWD "go_build_rpc.stdout.txt"
$rpcErr = Join-Path $PWD "go_build_rpc.stderr.txt"
$allStd = Join-Path $PWD "go_build.stdout.txt"
$allErr = Join-Path $PWD "go_build.stderr.txt"

# --- 4) RPC 먼저: 'cbor blake3' → 실패 시 'cbor' ---
& go clean -cache | Out-Null
$exitRpc = Build-WithArgs -ArgList @('build','-v','-tags','cbor blake3','./internal/rpc') -OutStd $rpcStd -OutErr $rpcErr
if ($exitRpc -ne 0) {
  "RPC build with -tags 'cbor blake3' failed. Retrying with 'cbor' only..." | Write-Host -ForegroundColor Yellow
  & go clean -cache | Out-Null
  $exitRpc = Build-WithArgs -ArgList @('build','-v','-tags','cbor','./internal/rpc') -OutStd $rpcStd -OutErr $rpcErr
}
"RPC_EXIT=$exitRpc"

# --- 5) 전체 빌드: 동일 전략 ---
& go clean -cache | Out-Null
$exitAll = Build-WithArgs -ArgList @('build','-v','-tags','cbor blake3','-o','.\thomasd_dbg.exe','./cmd/thomasd') -OutStd $allStd -OutErr $allErr
if ($exitAll -ne 0) {
  "Full build with -tags 'cbor blake3' failed. Retrying with 'cbor' only..." | Write-Host -ForegroundColor Yellow
  & go clean -cache | Out-Null
  $exitAll = Build-WithArgs -ArgList @('build','-v','-tags','cbor','-o','.\thomasd_dbg.exe','./cmd/thomasd') -OutStd $allStd -OutErr $allErr
}
"ALL_EXIT=$exitAll"

# --- 6) 로그 tail ---
"`n--- ./internal/rpc errors (tail) ---"
Get-Content $rpcErr -Tail 80 -ErrorAction SilentlyContinue
"`n--- full build errors (tail) ---"
Get-Content $allErr -Tail 120 -ErrorAction SilentlyContinue
"`n(logs saved as: $rpcErr, $rpcStd, $allErr, $allStd)"
