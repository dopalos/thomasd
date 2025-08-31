param(
  [int]$MaxStaleSeconds = 600,
  [switch]$Strict
)

$ErrorActionPreference = "Stop"

function Get-Base {
  $p = Get-Process thomasd -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($p) {
    try {
      $c = Get-NetTCPConnection -State Listen -ErrorAction Stop |
        Where-Object OwningProcess -eq $p.Id |
        Select-Object -First 1
      if ($c) { return "http://127.0.0.1:$($c.LocalPort)" }
    } catch {}
    $exeDir = Split-Path $p.Path -Parent
    $root   = Split-Path $exeDir -Parent
    $logDir = Join-Path $root 'run_logs'
    if (Test-Path $logDir) {
      $log = Get-ChildItem $logDir -Filter 'out_*.txt' | Sort-Object LastWriteTime | Select-Object -Last 1
      if ($log) {
        $m = Select-String -Path $log.FullName -Pattern 'listening on (http://[0-9\.]+:\d+)'
        if ($m -and $m.Matches.Count -gt 0) { return $m.Matches[0].Groups[1].Value.Trim() }
      }
    }
  }
  throw "BASE 탐색 실패"
}

function Get-MetricsText {
  param([string]$Url)
  $r = Invoke-WebRequest $Url -TimeoutSec 5
  return $r.Content
}

function Get-MetricValue {
  param([string]$Text, [string]$Name)
  $pattern = "^{0}\s+([0-9]+(?:\.[0-9]+)?)$" -f [regex]::Escape($Name)
  $m = Select-String -InputObject $Text -Pattern $pattern
  if ($m) {
    $val = [double]$m.Matches[0].Groups[1].Value
    return $val
  }
  return $null
}

$BASE = Get-Base
"BASE = $BASE" | Write-Host

$metrics = Get-MetricsText "$BASE/metrics"

$health = Get-MetricValue -Text $metrics -Name 'thomas_health_ok'
if ($null -eq $health) {
  Write-Warning "thomas_health_ok 미존재"
  if ($Strict) { throw "health 지표 없음" }
} elseif ($health -ne 1) {
  Write-Warning "thomas_health_ok=$health"
  if ($Strict) { throw "health not ok" }
} else {
  "thomas_health_ok=1" | Write-Host
}

$roundTs = Get-MetricValue -Text $metrics -Name 'thomas_round_time_utc'
if ($null -eq $roundTs) {
  Write-Warning "thomas_round_time_utc 미존재"
  if ($Strict) { throw "round_time 지표 없음" }
} else {
  $now = [double][int][DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
  $age = $now - $roundTs
  "round age(s) = $age" | Write-Host
  if ($age -gt $MaxStaleSeconds) {
    Write-Warning "라운드 지연: ${age}s > ${MaxStaleSeconds}s"
    if ($Strict) { throw "round stale" }
  }
}

$txCnt = Get-MetricValue -Text $metrics -Name 'thomas_tx_count_latest'
if ($null -ne $txCnt) { "tx_count_latest = $txCnt" | Write-Host }

"`n[CI] metrics_check OK" | Write-Host
exit 0
