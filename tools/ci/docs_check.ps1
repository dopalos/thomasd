# tools\ci\docs_check.ps1
# 목적: /docs, /openapi.json, /openapi/*.json, /round/latest/signed 스모크 (PS 5.1 호환)
param(
  [string]$Base,
  [int]$TimeoutSec = 5
)
$ErrorActionPreference = "Stop"

function Get-ThomasBase {
  param([string]$Manual)
  if ($Manual) { return $Manual }
  $p = Get-Process thomasd -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($p) {
    try {
      $c = Get-NetTCPConnection -State Listen -ErrorAction Stop |
           Where-Object OwningProcess -eq $p.Id | Select-Object -First 1
      if ($c) { return ("http://127.0.0.1:{0}" -f $c.LocalPort) }
    } catch {}
  }
  throw "BASE 미지정: -Base 지정하거나 thomasd 실행 필요"
}

function Test-Docs {
  param([string]$BaseUrl, [int]$T = 5)
  $ok = $false
  foreach ($path in @("$BaseUrl/docs", "$BaseUrl/docs/")) {
    # GET만 사용 (일부 서버가 HEAD 405)
    try {
      $r = Invoke-WebRequest -Uri $path -Method Get -MaximumRedirection 5 -TimeoutSec $T
      if ($r.StatusCode -eq 200 -or $r.StatusCode -eq 204) { $ok = $true; break }
    } catch {}
  }
  return $ok
}

$BASE = Get-ThomasBase -Manual $Base
Write-Host ("BASE = {0}" -f $BASE)

# /openapi.json
$openapi = (Invoke-RestMethod "$BASE/openapi.json" -TimeoutSec $TimeoutSec).openapi
if ($openapi -ne "3.0.3") { throw ("/openapi.json openapi={0} (expected 3.0.3)" -f $openapi) }
Write-Host "/openapi.json OK ($openapi)"

# /docs
if (-not (Test-Docs -BaseUrl $BASE -T $TimeoutSec)) { throw "/docs not 200" }
Write-Host "/docs -> 200"

# 조각/머지
Invoke-RestMethod "$BASE/openapi/merged.json" -TimeoutSec $TimeoutSec | Out-Null
Invoke-RestMethod "$BASE/openapi/tx.json"      -TimeoutSec $TimeoutSec | Out-Null
Invoke-RestMethod "$BASE/openapi/rounds.json"  -TimeoutSec $TimeoutSec | Out-Null
Invoke-RestMethod "$BASE/openapi/policy.json"  -TimeoutSec $TimeoutSec | Out-Null
Write-Host "merged/tx/rounds/policy -> 200"

# 실제 엔드포인트 최소 확인
$h = Invoke-RestMethod "$BASE/round/latest/signed" -TimeoutSec $TimeoutSec
if (-not $h.scheme) { throw "/round/latest/signed missing scheme" }
Write-Host "/round/latest/signed scheme=" $h.scheme

Write-Host "`n[CI] docs_check OK"
exit 0
