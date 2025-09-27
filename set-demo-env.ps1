param([string]$Base = 'http://127.0.0.1:8081')
$ErrorActionPreference = 'Stop'
$here = Get-Location

# 1) 기능 플래그
$env:THOMAS_REQUIRE_COMMIT      = "1"
$env:THOMAS_VERIFY_SIG          = "1"
$env:THOMAS_REQUIRE_FROM_PUBKEY = "1"
$env:THOMAS_FEAT_ALIAS          = "1"
$env:THOMAS_FEAT_R2P            = "1"

# 2) 키/바인딩 파일 준비
$ed = Join-Path $here 'bin\ed25519tool.exe'
if (!(Test-Path $ed)) { throw "ed25519tool not found: $ed" }
New-Item -ItemType Directory -Force -Path (Join-Path $here 'data') | Out-Null

$keyFile = Join-Path $here 'data\client_key.txt'
if (!(Test-Path $keyFile)) {
  $out = & $ed -gen | Out-String
  $sk = ([regex]::Match($out,'^SK:(.+)$','Multiline')).Groups[1].Value.Trim()
  $pk = ([regex]::Match($out,'^PK:(.+)$','Multiline')).Groups[1].Value.Trim()
  "SK=$sk`nPK=$pk" | Set-Content -Encoding ASCII $keyFile
}

$pkB64 = (Get-Content $keyFile | ? { $_ -match '^PK=' }) -replace '^PK=',''
$pkHex = -join ([Convert]::FromBase64String($pkB64) | % { $_.ToString('x2') })
@{ tho1alice = $pkHex } | ConvertTo-Json | Set-Content -Encoding ASCII (Join-Path $here 'data\from_pubkeys.json')

# 3) 별칭 맵 (있으면 보존)
$aliasPath = Join-Path $here 'data\alias_map.json'
if (!(Test-Path $aliasPath)) {
  @{ alice = 'tho1alice'; bob = 'tho1bob' } | ConvertTo-Json | Set-Content -Encoding ASCII $aliasPath
}

# 4) 재기동
Get-Process thomasd -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Process -NoNewWindow (Join-Path $here 'bin\thomasd.exe')
Start-Sleep 1

# 5) 헬스체크
for ($i=0; $i -lt 20; $i++) {
  try { $h = Invoke-RestMethod "$Base/health"; if ($h.status -eq 'ok') { "OK: $($h.time_utc)"; break } } catch {}
  Start-Sleep 0.3
}
if (-not $h) { throw "health check failed on $Base" }

$pol = Invoke-RestMethod "$Base/policy"
"policy flags => alias=$($pol.alias_enabled) r2p=$($pol.r2p_enabled) require.commit=$($pol.require.commit) sig=$($pol.require.signature) from_pubkey=$($pol.require.from_pubkey)"
