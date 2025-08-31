param(
  [string]$Base = $env:THOMAS_BASE,   # 미지정 시 자동탐색
  [int]$WarnStaleSeconds = 600,       # 0 이하면 스테일 체크 비활성화
  [int]$MinRound = 0,
  [int]$MinTxCount = -1,
  [switch]$FailOnWarn
)

$ErrorActionPreference = 'Stop'

function Get-Base {
  param([string]$b)
  if ($b -and $b -match '^http'){ return $b }
  $p = Get-Process thomasd -ErrorAction SilentlyContinue
  if (-not $p){ throw "thomasd not running; provide -Base" }
  $c = Get-NetTCPConnection -State Listen | ? OwningProcess -eq $p.Id | Select-Object -First 1
  if (-not $c){ throw "cannot detect thomasd port" }
  $addr = if ($c.LocalAddress -eq '::1'){'[::1]'} else {'127.0.0.1'}
  "http://{0}:{1}" -f $addr, $c.LocalPort
}

$Base = Get-Base -b $Base
Write-Host "BASE = $Base"

# /metrics 가져오기
$resp = Invoke-WebRequest "$Base/metrics" -UseBasicParsing -TimeoutSec 4
$raw  = $resp.Content

# Prometheus 텍스트 파싱 (단일 값 지표)
$metrics = @{}
$raw -split "`n" | ForEach-Object {
  $line = $_.Trim()
  if ($line -like '#*' -or [string]::IsNullOrWhiteSpace($line)) { return }
  # metric{labels} 123  →  metric 123
  if ($line -match '^(?<name>[^ {]+)(?:\{[^}]*\})?\s+(?<val>[-+]?\d+(\.\d+)?)') {
    $name = $Matches['name']
    $val  = [double]::Parse($Matches['val'], [Globalization.CultureInfo]::InvariantCulture)
    $metrics[$name] = $val
  }
}

function M($k){ if ($metrics.ContainsKey($k)) { $metrics[$k] } else { $null } }

$health = M 'thomas_health_ok'
$round  = M 'thomas_round_latest'
$rtu    = M 'thomas_round_time_utc'
$txc    = M 'thomas_tx_count_latest'

Write-Host ("thomas_health_ok      = {0}" -f $health)
Write-Host ("thomas_round_latest   = {0}" -f $round)
Write-Host ("thomas_round_time_utc = {0}" -f $rtu)
Write-Host ("thomas_tx_count_latest= {0}" -f $txc)

$warns = @()
$errs  = @()

if ($health -eq $null) { $warns += "thomas_health_ok missing" }
elseif ($health -ne 1) { $errs  += "health_ok != 1" }

if ($MinRound -gt 0 -and ($round -eq $null -or $round -lt $MinRound)) {
  $warns += "round_latest($round) < MinRound($MinRound)"
}

if ($MinTxCount -ge 0 -and ($txc -eq $null -or $txc -lt $MinTxCount)) {
  $warns += "tx_count_latest($txc) < MinTxCount($MinTxCount)"
}

if ($WarnStaleSeconds -gt 0 -and $rtu -ne $null) {
  $now = [int][DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
  $age = $now - [int]$rtu
  Write-Host ("latest round age(sec) = {0}" -f $age)
  if ($age -ge $WarnStaleSeconds) { $warns += "stale round: age=$age >= $WarnStaleSeconds" }
}

if ($errs.Count -gt 0) {
  Write-Error "[metrics_check] FAIL: $($errs -join '; ')"
  exit 2
}
elseif ($warns.Count -gt 0) {
  if ($FailOnWarn) { Write-Error "[metrics_check] WARN→FAIL: $($warns -join '; ')"; exit 1 }
  else { Write-Warning "[metrics_check] WARN: $($warns -join '; ')" }
}
else {
  Write-Host "[metrics_check] OK"
}
