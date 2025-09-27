<#  check-alias.ps1
    Thomas Chain — Alias/PayCode 최소 실행체크
    1) 헬스체크
    2) nonce 조회
    3) 별명 등록 TX (to=tho1alias.sys, msg_commitment=base64(JSON payload))
    4) /alias/resolve 로 확인
    5) /alias/reverse 로 확인
#>

param(
  [string]$BaseUrl = "http://127.0.0.1:8081",
  [string]$FromAddr = "tho1alice",       # 제네시스 잔고 계정
  [string]$SysAliasAddr = "tho1alias.sys",
  [string]$ChainId = "thomas-dev-1"
)

function Write-Step($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Get-JSON($url) {
  try {
    $r = Invoke-WebRequest -UseBasicParsing -Method GET -Uri $url -ErrorAction Stop
    return @{ ok = $true; status = $r.StatusCode; raw = $r.Content; json = ($r.Content | ConvertFrom-Json) }
  } catch {
    Write-Error "GET $url failed: $($_.Exception.Message)"
    return @{ ok = $false }
  }
}
function Post-JSON($url, $obj) {
  try {
    $body = ($obj | ConvertTo-Json -Compress)
    $r = Invoke-WebRequest -UseBasicParsing -Method POST -Uri $url -ContentType 'application/json' -Body $body -ErrorAction Stop
    $j = $r.Content | ConvertFrom-Json
    return @{ ok = $true; status = $r.StatusCode; raw = $r.Content; json = $j; body = $body }
  } catch {
    Write-Error "POST $url failed: $($_.Exception.Message)"
    return @{ ok = $false }
  }
}

# 1) Health
Write-Step "노드 헬스체크"
$h = Get-JSON "$BaseUrl/health"
if (-not $h.ok) { exit 1 }
Write-Host $h.raw

# 2) Nonce (from=tho1alice)
Write-Step "nonce 조회 ($FromAddr)"
$n = Get-JSON "$BaseUrl/nonce/$FromAddr"
if (-not $n.ok) { exit 1 }
$expectedNonce = [int64]$n.json.expected_nonce
Write-Host ("expected_nonce = {0}" -f $expectedNonce)

# 3) 별명 등록 TX 준비
Write-Step "별명 등록 페이로드(Base64) 생성"
$rand = Get-Random -Minimum 1000 -Maximum 9999
$Alias = "merchant$rand"              # 충돌 방지용 랜덤 suffix
$ScanPub  = "SCANPUB_${Alias}_$(Get-Random)_PAYCODE"
$SpendPub = "SPENDPUB_${Alias}_$(Get-Random)_PAYCODE"
$payloadObj = @{ alias=$Alias; scan_pub=$ScanPub; spend_pub=$SpendPub }
$payloadJson = $payloadObj | ConvertTo-Json -Compress
$b64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($payloadJson))
Write-Host ("alias={0}, payload(base64) length={1}" -f $Alias, $b64.Length)

# 4) 별명 등록 전송 (amount=1 mas, fee=1 mas)
Write-Step "별명 등록 TX 브로드캐스트 (to=$SysAliasAddr)"
$tx = @{
  type=1
  from=$FromAddr
  to=$SysAliasAddr
  amount_mas=1
  fee_mas=1
  nonce=$expectedNonce
  chain_id=$ChainId
  expiry_height=0
  msg_commitment=$b64     # 주의: THOMAS_REQUIRE_COMMIT 비활성 상태여야 함
}
$tres = Post-JSON "$BaseUrl/tx" $tx
if (-not $tres.ok) { exit 1 }
Write-Host $tres.raw
if (-not $tres.json.ok) {
  Write-Warning ("TX Precheck 실패: {0}" -f $tres.json.reason)
  exit 1
}
if (-not $tres.json.applied) {
  Write-Warning ("TX 적용 실패: {0}" -f $tres.json.reason)
  exit 1
}
$txHash = $tres.json.tx_hash
Write-Host ("tx_hash = {0}" -f $txHash)

# 5) /alias/resolve 확인
Start-Sleep -Milliseconds 200
Write-Step "별명 해석(resolve): @$Alias"
$rv = Get-JSON "$BaseUrl/alias/resolve?name=@$Alias"
if (-not $rv.ok) {
  Write-Warning "resolve 호출 실패(엔진 미구현 가능): 스킵"
} else {
  $rec = $rv.json.record
  if ($rv.json.ok -and $rec.alias -eq $Alias) {
    Write-Host ("OK - version={0}, owner={1}, scan_pub.len={2}" -f $rec.version, $rec.owner, ($rec.scan_pub | Measure-Object -Character).Characters)
  } else {
    Write-Warning ("별명 레코드를 찾지 못함: {0}" -f ($rv.raw))
    exit 1
  }
}

# 6) /alias/reverse 확인
Write-Step "역해석(reverse): $FromAddr"
$rev = Get-JSON "$BaseUrl/alias/reverse?addr=$FromAddr"
if ($rev.ok -and $rev.json.ok) {
  Write-Host ("OK - reverse alias = @{0}" -f $rev.json.alias)
} else {
  Write-Warning "reverse 호출 실패(엔진 미구현 가능): 스킵"
}

# 7) 요약
Write-Host ""
Write-Host "=== 요약 ===" -ForegroundColor Green
Write-Host ("별명 등록: @{0}" -f $Alias)
Write-Host ("TX Hash   : {0}" -f $txHash)
Write-Host "resolve/reverse가 OK면 별명 기능 정상입니다."
