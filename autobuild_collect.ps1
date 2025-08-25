$ErrorActionPreference = "Stop"

function Resolve-RepoRoot {
  param([string]$StartDir = (Get-Location).Path)
  $dir = Get-Item -LiteralPath $StartDir
  while ($null -ne $dir) {
    if (Test-Path (Join-Path $dir.FullName 'go.mod')) { return $dir.FullName }
    $dir = $dir.Parent
  }
  $fallback = 'C:\thomas-scaffold\thomasd'
  if (Test-Path (Join-Path $fallback 'go.mod')) { return $fallback }
  throw "go.mod not found from '$StartDir'."
}

$root = Resolve-RepoRoot
Set-Location $root
"ROOT = $root"

"`n== ls go.mod =="; Get-ChildItem -Force .\go.mod | Format-List Name,Length,LastWriteTime
"`n== head go.mod =="; Get-Content .\go.mod -TotalCount 8
"`n== go version =="; & go version

# 로그 경로
$errRpc = Join-Path $PWD "go_build_rpc.stderr.txt"
$outRpc = Join-Path $PWD "go_build_rpc.stdout.txt"
$errAll = Join-Path $PWD "go_build.stderr.txt"
$outAll = Join-Path $PWD "go_build.stdout.txt"
Remove-Item $errRpc,$outRpc,$errAll,$outAll -ErrorAction SilentlyContinue

# ---- RPC만 빌드 (플래그는 항상 패키지 '앞') ----
& go clean -cache | Out-Null
$p1 = Start-Process -FilePath "go.exe" `
  -ArgumentList @('build','-v','-tags','cbor blake3','./internal/rpc') `
  -RedirectStandardOutput $outRpc -RedirectStandardError $errRpc `
  -NoNewWindow -PassThru -Wait
"RPC_EXIT=$($p1.ExitCode)"

# ---- 전체 빌드 ----
& go clean -cache | Out-Null
$p2 = Start-Process -FilePath "go.exe" `
  -ArgumentList @('build','-v','-x','-tags','cbor blake3','-o','.\thomasd_dbg.exe','.\cmd\thomasd') `
  -RedirectStandardOutput $outAll -RedirectStandardError $errAll `
  -NoNewWindow -PassThru -Wait
"ALL_EXIT=$($p2.ExitCode)"

"`n--- ./internal/rpc errors (tail) ---"
Get-Content $errRpc -Tail 120 -ErrorAction SilentlyContinue
"`n--- full build errors (tail) ---"
Get-Content $errAll -Tail 200 -ErrorAction SilentlyContinue
"`n(logs saved as: $errRpc, $outRpc, $errAll, $outAll)"
