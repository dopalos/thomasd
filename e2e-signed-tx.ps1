# e2e-signed-tx.ps1
param([string]$Base = $env:THOMAS_BASE_URL)
if (-not $Base -or $Base -eq '') { $Base = 'http://127.0.0.1:8081' }

$ErrorActionPreference = 'Stop'
$EdTool = Join-Path (Get-Location) 'bin\ed25519tool.exe'
if (!(Test-Path $EdTool)) { throw "ed25519tool not found: $EdTool" }

$pol = Invoke-RestMethod "$Base/policy"
$chain = $pol.allowed_chain_id
$feeBps = [int]$pol.fee_bps

$from='tho1alice'; $to='tho1bob'
$acc  = Invoke-RestMethod "$Base/nonce/$from"
$nonce= [uint64]$acc.expected_nonce
$amountMas = [uint64]1000
$minFee = [uint64]([Math]::Max(1, [Math]::Floor($amountMas * $feeBps / 10000)))
$feeMas   = $minFee
$expiry   = [uint64]0
$type     = 1

$plain = "$type|$from|$to|$amountMas|$feeMas|$nonce|$chain|$expiry"
$sha = [System.Security.Cryptography.SHA256]::Create().ComputeHash([Text.Encoding]::UTF8.GetBytes($plain))
$commitHex = -join ($sha | ForEach-Object { $_.ToString('x2') })

$client = Join-Path (Get-Location) 'data\client_key.txt'
if (Test-Path $client) {
  $skB64 = (Get-Content $client | Where-Object { $_ -match '^SK=' }) -replace '^SK=',''
  $pkB64 = (Get-Content $client | Where-Object { $_ -match '^PK=' }) -replace '^PK=',''
} else {
  $out = & $EdTool -gen | Out-String
  $skB64 = ([regex]::Match($out,'^SK:(.+)$','Multiline')).Groups[1].Value.Trim()
  $pkB64 = ([regex]::Match($out,'^PK:(.+)$','Multiline')).Groups[1].Value.Trim()
  "SK=$skB64`nPK=$pkB64" | Set-Content -Encoding ASCII $client
}

$signed = & $EdTool -sk $skB64 -m $commitHex | Out-String
$pk  = ([regex]::Match($signed,'^PK:(.+)$','Multiline')).Groups[1].Value.Trim()
$sig = ([regex]::Match($signed,'^SIG:(.+)$','Multiline')).Groups[1].Value.Trim()

"== E2E signed tx against $Base =="
"policy: chain_id=$chain, fee_bps=$feeBps, require.commit=$($pol.require.commit), require.sig=$($pol.require.signature), require.from_pubkey=$($pol.require.from_pubkey)"
"commit message: $commitHex"

$txObj = [ordered]@{
  type=1; from=$from; to=$to
  amount_mas=[uint64]$amountMas; fee_mas=[uint64]$feeMas
  nonce=[uint64]$nonce; chain_id=$chain; expiry_height=[uint64]$expiry
  msg_commitment=$commitHex
}
$resp = Invoke-RestMethod -Method Post -Uri "$Base/tx" -ContentType 'application/json' `
        -Headers @{ 'X-PubKey'=$pk; 'X-Sig'=$sig } -Body ($txObj | ConvertTo-Json -Depth 5)
"tx: ok=$($resp.ok) applied=$($resp.applied) tx_hash=$($resp.tx_hash) nonce=$($resp.nonce) height=$($resp.height)"

$commit = Invoke-RestMethod -Method Post "$Base/round/commit" -ContentType 'application/json' -Body '{}'
"commit: committed=$($commit.committed)"

$ls = Invoke-RestMethod "$Base/round/latest/signed"
"latest round: r=$($ls.header.round) from=$($ls.header.from_height) to=$($ls.header.to_height) tx=$($ls.header.tx_count)"
"signature(16)=$($ls.signature_hex.Substring(0,16))..."
