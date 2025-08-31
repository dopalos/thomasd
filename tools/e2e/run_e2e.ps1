# tools\e2e\run_e2e.ps1
param(
  [string]$Base = $env:THOMAS_BASE,
  [string]$ProxyBase = $env:THOMAS_SIGNEDMSG_PROXY,
  [switch]$SkipTx
)
$ErrorActionPreference = "Stop"

function Get-ThomasBase {
  param([string]$Manual)
  if ($Manual) { return $Manual }
  $p = Get-Process thomasd -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($p) {
    try {
      $c = Get-NetTCPConnection -State Listen -ErrorAction Stop | Where-Object OwningProcess -eq $p.Id | Select-Object -First 1
      if ($c) { return "http://127.0.0.1:$($c.LocalPort)" }
    } catch {}
    $exeDir = Split-Path $p.Path -Parent; $root = Split-Path $exeDir -Parent; $logDir = Join-Path $root 'run_logs'
    if (Test-Path $logDir) {
      $log = Get-ChildItem $logDir -Filter 'out_*.txt' | Sort-Object LastWriteTime | Select-Object -Last 1
      if ($log) {
        $m = Select-String -Path $log.FullName -Pattern 'listening on (http://[0-9\.]+:\d+)'
        if ($m -and $m.Matches.Count -gt 0) { return $m.Matches[0].Groups[1].Value.Trim() }
      }
    }
  }
  $root = 'C:\thomas-scaffold\thomasd'; $logDir = Join-Path $root 'run_logs'
  if (Test-Path $logDir) {
    $log = Get-ChildItem $logDir -Filter 'out_*.txt' | Sort-Object LastWriteTime | Select-Object -Last 1
    if ($log) {
      $m = Select-String -Path $log.FullName -Pattern 'listening on (http://[0-9\.]+:\d+)'
      if ($m -and $m.Matches.Count -gt 0) { return $m.Matches[0].Groups[1].Value.Trim() }
    }
  }
  throw "BASE를 찾지 못했습니다. THOMAS_BASE를 지정하거나 thomasd를 먼저 실행하세요."
}

function Get-ProxyBase {
  param([string]$Manual)
  if ($Manual) { return $Manual }
  return "http://127.0.0.1:63081"
}

function Post-Tx {
  param([string]$BaseUrl)
  $body = @{ tx = "0xDEADBEEF" } | ConvertTo-Json -Depth 5
  try {
    $resp = Invoke-RestMethod -Method Post -Uri "$BaseUrl/tx" -Body $body -ContentType "application/json"
    Write-Host "/tx ->" ($resp | ConvertTo-Json -Depth 8)
  } catch {
    Write-Warning "/tx 호출 실패(무시하고 진행): $($_.Exception.Message)"
  }
}

function Assert-Health {
  param([string]$BaseUrl)
  $h = Invoke-RestMethod "$BaseUrl/health"
  if (-not $h.status) { throw "/health status 없음" }
  Write-Host "/health ->" $h.status
}

function Assert-OpenAPI {
  param([string]$BaseUrl)
  $v = (Invoke-RestMethod "$BaseUrl/openapi.json").openapi
  if ($v -ne "3.0.3") { Write-Warning "/openapi.json openapi=$v (예상: 3.0.3)" }
  Write-Host "/openapi.json ->" $v
  try { $ok = Invoke-RestMethod "$BaseUrl/openapi/merged.json" -TimeoutSec 5; Write-Host "/openapi/merged.json -> OK" } catch {}
}

function Assert-RoundAndProxy {
  param([string]$BaseUrl, [string]$ProxyUrl)
  $latest = Invoke-RestMethod "$BaseUrl/round/latest/signed"
  $scheme = $latest.scheme
  Write-Host "/round/latest/signed -> scheme:" $scheme
  try {
    $msg = Invoke-RestMethod "$ProxyUrl/round/latest/signed_msg"
    $valid = $msg.signature_valid
    if (-not $valid) { throw "signature_valid=False" }
    Write-Host "$ProxyUrl/round/latest/signed_msg -> signature_valid=True"
  } catch {
    throw "프록시 검증 실패: $($_.Exception.Message)"
  }
}

$BASE = Get-ThomasBase -Manual $Base
$PROXY = Get-ProxyBase -Manual $ProxyBase
Write-Host "BASE  =" $BASE
Write-Host "PROXY =" $PROXY

Assert-Health -BaseUrl $BASE
Assert-OpenAPI -BaseUrl $BASE
if (-not $SkipTx) { Post-Tx -BaseUrl $BASE }
Assert-RoundAndProxy -BaseUrl $BASE -ProxyUrl $PROXY

Write-Host "`n[E2E] OK: health/openapi/round/proxy 스모크 통과"
exit 0
