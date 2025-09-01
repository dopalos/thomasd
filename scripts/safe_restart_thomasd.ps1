param([switch]$NoBuild)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$ROOT = "C:\thomas-scaffold\thomasd"
Set-Location $ROOT

function Stop-Old {
  $procs = Get-Process thomasd -ErrorAction SilentlyContinue
  if ($procs) {
    Write-Host "[safe-restart] stopping old thomasd..."
    $procs | Stop-Process -Force
    Start-Sleep -Milliseconds 300
  } else {
    Write-Host "[safe-restart] no matching old process."
  }
}

function Build-Binary {
  Write-Host "[safe-restart] building..."
  & go.exe build -o .\bin\thomasd.exe thomasd/cmd/thomasd
  if ($LASTEXITCODE -ne 0) { throw "go build failed" }
  Write-Host "[safe-restart] build OK -> $(Resolve-Path .\bin\thomasd.exe)"
}

function Start-New {
  Write-Host "[safe-restart] starting..."
  New-Item -ItemType Directory -Force -Path .\run_logs | Out-Null
  $ts  = Get-Date -Format "yyyyMMdd_HHmmss"
  $OUT = ".\run_logs\out_$ts.txt"
  $ERR = ".\run_logs\err_$ts.txt"
  $p = Start-Process -FilePath ".\bin\thomasd.exe" -NoNewWindow -PassThru `
       -RedirectStandardOutput $OUT -RedirectStandardError $ERR
  Start-Sleep -Milliseconds 800
  if ($p.HasExited) {
    "== OUT (tail) =="; if (Test-Path $OUT) { Get-Content $OUT -Tail 80 } else { "no out log" }
    "== ERR (tail) =="; if (Test-Path $ERR) { Get-Content $ERR -Tail 80 } else { "no err log" }
    throw "thomasd exited immediately."
  }
  return [pscustomobject]@{ PID = $p.Id; OUT = $OUT; ERR = $ERR }
}

Stop-Old
if (-not $NoBuild) { Build-Binary } else { Write-Host "[safe-restart] skipping build (-NoBuild)." }

$res = Start-New
Write-Host ("started thomasd (pid={0})" -f $res.PID)

# BASE from logs
$BASE = $null
for ($i=0; $i -lt 30; $i++) {
  Start-Sleep -Milliseconds 100
  $m = Select-String -Path $res.OUT -Pattern 'listening on (http://[0-9\.]+:\d+)' -ErrorAction SilentlyContinue | Select-Object -Last 1
  if ($m) { $BASE = $m.Matches[0].Groups[1].Value.Trim(); break }
}
if ($BASE) { Write-Host ("BASE = {0}" -f $BASE) } else { Write-Warning "[safe-restart] BASE not found yet. Check logs:`n  $($res.OUT)`n  $($res.ERR)" }
