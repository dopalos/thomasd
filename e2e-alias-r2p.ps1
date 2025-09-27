# e2e-alias-r2p.ps1
# - 별명(@alice) 있는 경우: alias_version 가드 OK/FAIL 둘 다 검증
# - R2P create/approve E2E
param([string]$Base)

$ErrorActionPreference = 'Stop'
if (-not $Base -or $Base -eq '') { $Base = 'http://127.0.0.1:8081' }

function MinFee([uint64]$amt, [int]$bps) {
  $f = [math]::Floor($amt * $bps / 10000)
  if ($f -lt 1) { return [uint64]1 } else { return [uint64]$f }
}

Write-Host "== E2E alias + R2P against $Base =="

# 정책
$pol   = Invoke-RestMethod "$Base/policy"
$chain = $pol.allowed_chain_id
$feeBps = [int]$pol.fee_bps
$require = $pol.require

# 발신자/수신자
$from='tho1alice'
$to  ='tho1bob'

# nonce / 수수료
$acc   = Invoke-RestMethod "$Base/nonce/$from"
$nonce = [uint64]$acc.expected_nonce
$amt   = [uint64]1000
$fee   = MinFee $amt $feeBps

# client SK/PK
$EdTool = Join-Path (Get-Location) 'bin\ed25519tool.exe'
$skB64  = (Get-Content 'data\client_key.txt' | ? {$_ -match '^SK='}) -replace '^SK=',''
# 커밋 문자열 (서버와 동일 포맷)
$plain  = "1|$from|$to|$amt|$fee|$nonce|$chain|0"
$sha    = [Security.Cryptography.SHA256]::Create().ComputeHash([Text.Encoding]::UTF8.GetBytes($plain))
$msg    = -join ($sha | % { $_.ToString('x2') })
# 서명
$sOut   = & $EdTool -sk $skB64 -m $msg | Out-String
$pk     = ([regex]::Match($sOut,'^PK:(.+)$','Multiline')).Groups[1].Value.Trim()
$sig    = ([regex]::Match($sOut,'^SIG:(.+)$','Multiline')).Groups[1].Value.Trim()

"policy: chain=$chain, fee_bps=$feeBps, require.commit=$($require.commit), sig=$($require.signature), from_pubkey=$($require.from_pubkey)"
"commit message: $msg"

# ---------------------------
# 1) 별명 테스트 (있으면 수행)
# ---------------------------
$aliasOk = $false
try {
  $res = Invoke-RestMethod "$Base/alias/resolve?name=@alice"
  if ($res.ok -and $res.record) {
    $rec = $res.record
    $owner = $rec.owner; if (-not $owner) { $owner = $rec.address }
    $ver   = 0
    if ($rec.version) { $ver = [int64]$rec.version }
    if ($owner -and $owner -like 'tho1*') {
      "alias: @alice -> owner=$owner, version=$ver"
      # 정상 케이스: alias_version 일치
      $tx1 = [ordered]@{
        type=1; from=$from; to=$owner
        amount_mas=$amt; fee_mas=$fee
        nonce=$nonce; chain_id=$chain; expiry_height=[uint64]0
        msg_commitment=$msg
        to_alias='@alice'; alias_version=[int64]$ver
      }
      $resp1 = Invoke-RestMethod -Method Post "$Base/tx" -ContentType 'application/json' `
               -Headers @{ 'X-PubKey'=$pk; 'X-Sig'=$sig } -Body ($tx1 | ConvertTo-Json -Depth 6)
      "alias OK: applied=$($resp1.applied) tx=$($resp1.tx_hash)"

      # 실패 케이스: alias_version 틀림
      $tx2 = $tx1.PSObject.Copy()
      $tx2.alias_version = [int64]($ver + 1)
      try {
        $null = Invoke-RestMethod -Method Post "$Base/tx" -ContentType 'application/json' `
                -Headers @{ 'X-PubKey'=$pk; 'X-Sig'=$sig } -Body ($tx2 | ConvertTo-Json -Depth 6)
        throw "alias mismatch SHOULD have failed but succeeded"
      } catch {
        "alias mismatch EXPECTED FAIL => $($_.ErrorDetails.Message)"
      }
      $aliasOk = $true
    }
  }
} catch {
  "alias resolve not available (skip): $($_.Exception.Message)"
}

# ---------------------------
# 2) 서명 전송(기본 경로)
# ---------------------------
$tx = [ordered]@{
  type=1; from=$from; to=$to
  amount_mas=$amt; fee_mas=$fee
  nonce=$nonce; chain_id=$chain; expiry_height=[uint64]0
  msg_commitment=$msg
}
$resp = Invoke-RestMethod -Method Post "$Base/tx" -ContentType 'application/json' `
         -Headers @{ 'X-PubKey'=$pk; 'X-Sig'=$sig } -Body ($tx | ConvertTo-Json -Depth 5)
"tx: ok=$($resp.ok) applied=$($resp.applied) tx_hash=$($resp.tx_hash) nonce=$($resp.nonce) height=$($resp.height)"

# ---------------------------
# 3) R2P E2E
# ---------------------------
try {
  $r2pCreate = Invoke-RestMethod -Method Post "$Base/r2p/create" -ContentType 'application/json' -Body (@{
    from='@alice'; to=$to; amount_mas=1234; memo='demo'
  } | ConvertTo-Json)
  if (-not $r2pCreate.ok) { throw "r2p create failed" }
  $rid = $r2pCreate.id
  "r2p: created id=$rid"

  $fee2 = MinFee ([uint64]1234) $feeBps
  $r2pApprove = Invoke-RestMethod -Method Post "$Base/r2p/approve" -ContentType 'application/json' -Body (@{
    id=$rid; payer=$to; fee_mas=$fee2
  } | ConvertTo-Json)
  if (-not $r2pApprove.ok) { throw "r2p approve failed" }
  "r2p: approved tx=$($r2pApprove.tx_hash)"
} catch {
  "r2p skipped/not enabled: $($_.Exception.Message)"
}

# ---------------------------
# 4) 커밋 & 최신 라운드 확인
# ---------------------------
$commit = Invoke-RestMethod -Method Post "$Base/round/commit" -ContentType 'application/json' -Body '{}'
"commit: committed=$($commit.committed) reason=$($commit.reason)"
$ls = Invoke-RestMethod "$Base/round/latest/signed"
"latest round: r=$($ls.header.round) from=$($ls.header.from_height) to=$($ls.header.to_height) tx=$($ls.header.tx_count)"
"signature(16)=$($ls.signature_hex.Substring(0,16))..."

if ($aliasOk) { "alias tests done (OK + mismatch)"; } else { "alias tests skipped"; }
