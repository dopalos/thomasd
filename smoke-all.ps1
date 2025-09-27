param([string]$Base = 'http://127.0.0.1:8081')
$ErrorActionPreference = 'Stop'

function Sign-And-SendTx {
  param($From='tho1alice', $To='tho1bob', [UInt64]$AmountMas=1000)
  $pol   = Invoke-RestMethod "$Base/policy"
  $acc   = Invoke-RestMethod "$Base/nonce/$From"
  $nonce = [uint64]$acc.expected_nonce
  $fee   = [uint64]([Math]::Max(1,[Math]::Floor($AmountMas * [int]$pol.fee_bps / 10000)))
  $plain = "1|$From|$To|$AmountMas|$fee|$nonce|$($pol.allowed_chain_id)|0"

  $sha = [Security.Cryptography.SHA256]::Create().ComputeHash([Text.Encoding]::UTF8.GetBytes($plain))
  $msg = -join ($sha | % { $_.ToString('x2') })

  $ed = Join-Path (Get-Location) 'bin\ed25519tool.exe'
  if (!(Test-Path $ed)) { throw "ed25519tool not found: $ed" }
  $skB64 = (Get-Content 'data\client_key.txt' | ? { $_ -match '^SK=' }) -replace '^SK=',''
  $sOut  = & $ed -sk $skB64 -m $msg | Out-String
  $pk    = ([regex]::Match($sOut,'^PK:(.+)$','Multiline')).Groups[1].Value.Trim()
  $sig   = ([regex]::Match($sOut,'^SIG:(.+)$','Multiline')).Groups[1].Value.Trim()

  $tx=[ordered]@{type=1;from=$From;to=$To;amount_mas=$AmountMas;fee_mas=$fee;nonce=$nonce;chain_id=$pol.allowed_chain_id;expiry_height=[uint64]0;msg_commitment=$msg}
  $resp=Invoke-RestMethod -Method Post "$Base/tx" -ContentType 'application/json' -Headers @{ 'X-PubKey'=$pk; 'X-Sig'=$sig } -Body ($tx|ConvertTo-Json -Depth 5)
  if (-not $resp.applied) { throw "tx failed: $($resp | ConvertTo-Json -Compress)" }
  "tx ok: hash=$($resp.tx_hash) nonce=$($resp.nonce) height=$($resp.height)"
}

function R2P-Flow {
  $createBody = @{ from='@alice'; to='@bob'; amount_mas=1000; memo='smoke' } | ConvertTo-Json
  $cr = Invoke-RestMethod -Method Post "$Base/r2p/create" -ContentType 'application/json' -Body $createBody
  $id = $cr.id
  "r2p created: id=$id"
  $st = Invoke-RestMethod "$Base/r2p/get?id=$id"; "r2p status: $($st.record.status)"
  $ap = Invoke-RestMethod -Method Post "$Base/r2p/approve" -ContentType 'application/json' -Body (@{ id=$id }|ConvertTo-Json)
  "r2p approved: tx=$($ap.tx_hash)"
}

# 1) 정책 확인
$pol = Invoke-RestMethod "$Base/policy"
"policy => alias=$($pol.alias_enabled) r2p=$($pol.r2p_enabled) require.commit=$($pol.require.commit) sig=$($pol.require.signature) from_pubkey=$($pol.require.from_pubkey)"

# 2) 별칭 확인
$ra=Invoke-RestMethod "$Base/alias/resolve?name=@alice"
$rb=Invoke-RestMethod "$Base/alias/reverse?addr=tho1alice"
"alias => owner=$($ra.record.owner) reverse=$($rb.alias)"

# 3) 서명 TX
Sign-And-SendTx | Write-Host

# 4) R2P (기능 켜져 있으면)
if ($pol.r2p_enabled) { R2P-Flow | Write-Host } else { "r2p skipped (disabled)" }

# 5) 커밋 & 라운드
$c = Invoke-RestMethod -Method Post "$Base/round/commit" -ContentType 'application/json' -Body '{}'
"commit => committed=$($c.committed)"
$ls = Invoke-RestMethod "$Base/round/latest/signed"
"latest => r=$($ls.header.round) from=$($ls.header.from_height) to=$($ls.header.to_height) tx=$($ls.header.tx_count) sig16=$($ls.signature_hex.Substring(0,16))..."
