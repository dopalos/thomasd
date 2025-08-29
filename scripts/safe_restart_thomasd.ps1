param(
  [switch]$NoBuild = $false,
  [int]$WaitSec = 10
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$ROOT = "C:\thomas-scaffold\thomasd"
$BIN  = Join-Path $ROOT "bin\thomasd.exe"
$LOGD = Join-Path $ROOT "run_logs"
$PIDF = Join-Path $LOGD "thomasd.pid"
New-Item -ItemType Directory -Force -Path $LOGD | Out-Null

function Find-MainImport {
  $lines = & go.exe list -e -f "{{.Dir}}|{{.ImportPath}}|{{.Name}}" ./... 2>$null
  if (-not $lines) { throw "go list ./... produced no output" }
  if (-not ($lines -is [array])) { $lines = @($lines) }
  $pkgs = foreach ($ln in $lines) {
    if ($ln -notmatch '\|') { continue }
    $p = $ln -split '\|'
    if ($p.Count -ge 3) { [pscustomobject]@{ Dir=$p[0]; Import=$p[1]; Name=$p[2] } }
  }
  $mains = $pkgs | Where-Object { $_.Name -eq "main" }
  $c = $mains | Where-Object { $_.Import -match '/cmd/thomasd$' -or $_.Dir -match '\\cmd\\thomasd$' } | Select-Object -First 1
  if (-not $c) { $c = $mains | Where-Object { $_.Import -match '/cmd/' -or $_.Dir -match '\\cmd\\' } | Select-Object -First 1 }
  if (-not $c) { $c = $mains | Select-Object -First 1 }
  if (-not $c) { throw "no main package found" }
  return $c.Import
}

function Stop-Old {
  $selfPid = $PID
  $stopped = $false

  if (Test-Path $PIDF) {
    $pidTxt = (Get-Content $PIDF -ErrorAction SilentlyContinue | Select-Object -First 1).Trim()
    if ($pidTxt -match '^\d+$') {
      $old = Get-Process -Id [int]$pidTxt -ErrorAction SilentlyContinue
      if ($old) {
        $cmd = (Get-CimInstance Win32_Process -Filter "ProcessId=$($old.Id)" -ErrorAction SilentlyContinue).CommandLine
        if ($cmd -and $cmd -like "*\bin\thomasd.exe*" -and $old.Id -ne $selfPid) {
          Stop-Process -Id $old.Id -Force; $stopped = $true
        }
      }
    }
  }

  $procs = Get-CimInstance Win32_Process -Filter "Name='thomasd.exe'" -ErrorAction SilentlyContinue
  foreach ($p in ($procs | Sort-Object ProcessId -Descending)) {
    $cmd = $p.CommandLine
    if ($cmd -and $cmd -like "*\bin\thomasd.exe*" -and $p.ProcessId -ne $selfPid) {
      try { Stop-Process -Id $p.ProcessId -Force; $stopped = $true } catch {}
    }
  }
  return $stopped
}

function Start-New {
  $ts = Get-Date -Format "yyyyMMdd_HHmmss"
  $OUT = Join-Path $LOGD "out_$ts.txt"
  $ERR = Join-Path $LOGD "err_$ts.txt"
  $p = Start-Process -FilePath $BIN -NoNewWindow -PassThru `
       -RedirectStandardOutput $OUT -RedirectStandardError $ERR
  $p.Id | Set-Content $PIDF -Encoding ascii
  Write-Host "started thomasd (pid=$($p.Id))"
  # 확실히 객체만 반환
  return [pscustomobject]@{ P = $p; OUT = $OUT; ERR = $ERR }
}

Write-Host "[safe-restart] stopping old thomasd..."
$st = Stop-Old
if ($st) { Write-Host "[safe-restart] old process stopped." } else { Write-Host "[safe-restart] no matching old process." }

if (-not $NoBuild) {
  Write-Host "[safe-restart] building..."
  $import = Find-MainImport
  $null = & go.exe build -o $BIN $import 2>&1
  if ($LASTEXITCODE -ne 0) { throw "go build failed for $import" }
  Write-Host "[safe-restart] build OK -> $BIN"
} else {
  Write-Host "[safe-restart] skipping build (-NoBuild)."
}

Write-Host "[safe-restart] starting..."
$res = Start-New
if ($res -is [array]) { $res = $res[-1] }  # 방어
$OUT = $res.OUT; $ERR = $res.ERR

# BASE 추출: 방금 만든 타임스탬프 로그에서만 검색
$BASE = $null
for ($i=0; $i -lt $WaitSec*10; $i++) {
  Start-Sleep -Milliseconds 100
  $m = Select-String -Path $OUT -Pattern 'listening on (http://[0-9\.]+:\d+)' -SimpleMatch -AllMatches -ErrorAction SilentlyContinue | Select-Object -Last 1
  if ($m) { $BASE = $m.Matches[0].Groups[1].Value.Trim(); break }
}

if (-not $BASE) {
  Write-Warning "[safe-restart] BASE not found in logs yet. Check:`n  $OUT`n  $ERR"
  return
}

Write-Host "[safe-restart] BASE =" $BASE

try {
  $h = Invoke-RestMethod "$BASE/health" -TimeoutSec 2
  Write-Host "[safe-restart] health =" ($h.status)
} catch { Write-Warning "[safe-restart] health check failed: $($_.Exception.Message)" }

Write-Host "----"
Write-Host "logs:"
Write-Host "  $OUT"
Write-Host "  $ERR"
Write-Host "BASE var:"
Write-Host "  `$BASE = '$BASE'"
