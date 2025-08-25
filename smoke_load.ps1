param(
  [int]$N = 5,
  [int]$AmtMas = 10,
  [string]$From = "tho1alice",
  [string]$To   = "tho1bob"
)

# 포트
$proc = Get-Process thomasd_dbg
$port = (Get-NetTCPConnection -State Listen -OwningProcess $proc.Id |
         ? LocalAddress -eq '127.0.0.1' |
         Select-Object -First 1 -ExpandProperty LocalPort)

# 헬스
$h = irm "http://127.0.0.1:$port/health"
if ($h.status -ne 'ok') { throw "node unhealthy: $($h | ConvertTo-Json -Compress)" }

function Get-FeeMas([int]$amountMas){ [Math]::Max([int](($amountMas*10)/10000),1) }

function Send-THO([string]$From,[string]$To,[int]$AmountMas){
  $nn    = irm "http://127.0.0.1:$port/nonce/$From"
  $next  = $nn.expected_nonce
  $fee   = Get-FeeMas $AmountMas
  $payload = @{
    type=1; from=$From; to=$To;
    amount_mas=$AmountMas; fee_mas=$fee;
    nonce=$next; chain_id="thomas-dev-1";
    expiry_height=0; msg_commitment=""
  } | ConvertTo-Json -Compress
  irm "http://127.0.0.1:$port/tx" -Method Post -ContentType 'application/json' -Body $payload
}

function Get-Acc($addr){ irm "http://127.0.0.1:$port/account/$addr" }
$alice0 = Get-Acc $From
$bob0   = Get-Acc $To
$fd0    = Get-Acc "tho1foundation"
$mk0    = irm "http://127.0.0.1:$port/merkle"

$sent=0; $applied=0; $hashes=@()
for($i=1; $i -le $N; $i++){
  $res = Send-THO $From $To $AmtMas
  $sent++
  if ($res.applied -eq $true) { $applied++ }
  if ($res.tx_hash) { $hashes += $res.tx_hash }
  Start-Sleep -Milliseconds 120
}

$deadline = [DateTime]::UtcNow.AddSeconds(3)
do{
  $mk = irm "http://127.0.0.1:$port/merkle"
  $okMerkle = ($mk.count -ge ($mk0.count + $applied))
  if (-not $okMerkle) { Start-Sleep -Milliseconds 150 }
} while((-not $okMerkle) -and [DateTime]::UtcNow -lt $deadline)

$alice1 = Get-Acc $From
$bob1   = Get-Acc $To
$fd1    = Get-Acc "tho1foundation"

$feeTotal = (Get-FeeMas $AmtMas) * $applied
$amtTotal = $AmtMas * $applied

$deltaAlice = $alice1.balance_mas - $alice0.balance_mas
$deltaBob   = $bob1.balance_mas   - $bob0.balance_mas
$deltaFD    = $fd1.balance_mas    - $fd0.balance_mas

$expectAlice = -($amtTotal + $feeTotal)
$expectBob   =  ($amtTotal)
$expectFD    =  ($feeTotal)

$okAlice = ($deltaAlice -eq $expectAlice)
$okBob   = ($deltaBob   -eq $expectBob)
$okFD    = ($deltaFD    -eq $expectFD)

[pscustomobject]@{
  port           = $port
  sent           = $sent
  applied        = $applied
  merkle_before  = $mk0.count
  merkle_after   = $mk.count
  merkle_ok      = $okMerkle
  delta_alice    = $deltaAlice
  delta_bob      = $deltaBob
  delta_found    = $deltaFD
  expect_alice   = $expectAlice
  expect_bob     = $expectBob
  expect_found   = $expectFD
  invariant_ok   = ($okAlice -and $okBob -and $okFD)
  hashes         = ($hashes -join ",")
}
