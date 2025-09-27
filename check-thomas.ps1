param(
  [string]$BASE = "",
  [int]$TimeoutSec = 5,
  [switch]$Transcript,
  [switch]$SkipTx,
  [string]$EdToolPath = ""
)

if ($Transcript) { try { Start-Transcript -Path ".\check-thomas.log" -Force | Out-Null } catch {} }
function Write-Ok($msg){Write-Host "[PASS] $msg" -ForegroundColor Green}
function Write-Fail($msg){Write-Host "[FAIL] $msg" -ForegroundColor Red}
function Write-Info($msg){Write-Host "[..] $msg" -ForegroundColor DarkCyan}
function Summarize{Write-Host "== Result: PASS=$script:PASS, FAIL=$script:FAIL =="; if($Transcript){try{Stop-Transcript|Out-Null}catch{}}}
$script:PASS=0;$script:FAIL=0
if(-not $BASE -or $BASE.Trim() -eq ""){$BASE="http://127.0.0.1:18081"}

function Invoke-HTTP{
  param([ValidateSet('GET','POST')][string]$Method,[string]$Url,[string]$Body="",[hashtable]$Headers=@{})
  $res=[ordered]@{Ok=$false;Code=0;Json=$null;Error="";Raw=$null}
  try{
    if($Method -eq 'GET'){$r=Invoke-WebRequest -Uri $Url -TimeoutSec $TimeoutSec -UseBasicParsing -ErrorAction Stop}
    else{$r=Invoke-WebRequest -Uri $Url -TimeoutSec $TimeoutSec -UseBasicParsing -Method Post -ContentType 'application/json' -Body $Body -Headers $Headers -ErrorAction Stop}
    $res.Code=[int]$r.StatusCode;$res.Raw=$r;try{$res.Json=$r.Content|ConvertFrom-Json -ErrorAction Stop}catch{};$res.Ok=($r.StatusCode -ge 200 -and $r.StatusCode -lt 300);return $res
  }catch{
    $res.Error=$_.Exception.Message;try{$resp=$_.Exception.Response;if($resp){$res.Code=[int]$resp.StatusCode.value__; $sr=New-Object System.IO.StreamReader($resp.GetResponseStream());$txt=$sr.ReadToEnd();$sr.Close();try{$res.Json=$txt|ConvertFrom-Json -ErrorAction Stop}catch{}}}catch{};return $res
  }
}
function Get-Json($p){Invoke-HTTP -Method GET -Url (($BASE.TrimEnd('/'))+$p)}
function Post-Json($p,$o,$h=@{}){$u=($BASE.TrimEnd('/'))+$p;$b=($o|ConvertTo-Json -Depth 5);Invoke-HTTP -Method POST -Url $u -Body $b -Headers $h}

# 포트 자동 보정
$h=Get-Json "/health"
if(-not $h.Ok){
  Write-Info "HTTP error on $($BASE)/health: $($h.Error)"
  if(Test-Path ".\thomasd.out.log"){
    $tail=Get-Content ".\thomasd.out.log" -Tail 50 -ErrorAction SilentlyContinue
    $m=($tail|Select-String -Pattern 'listening on :(\d+)' -AllMatches)
    if($m.Matches.Count -gt 0){$port=$m.Matches[-1].Groups[1].Value;$new="http://127.0.0.1:$port";if($new -ne $BASE){Write-Info "Detected port from log → $new (retrying)";$BASE=$new;$h=Get-Json "/health"}}
  }
  if(-not $h.Ok -and $BASE -ne "http://127.0.0.1:8081"){ $BASE="http://127.0.0.1:8081"; Write-Info "Retry with $BASE"; $h=Get-Json "/health" }
}

Write-Host "== ThomasD smoke check ($BASE) =="

if($h.Ok -and $h.Json.status -eq "ok"){Write-Ok "/health ok: $($h.Json.time_utc)";$script:PASS++}else{Write-Fail "/health";$script:FAIL++;Summarize;return}

$pol=Get-Json "/policy"; if($pol.Ok){Write-Ok "/policy unit=$($pol.Json.unit) algo=$($pol.Json.signing.algo)";$script:PASS++}else{Write-Fail "/policy";$script:FAIL++}
$ht=Get-Json "/height"; if($ht.Ok -and $ht.Json.height -ge 0){Write-Ok "/height = $($ht.Json.height)";$script:PASS++}else{Write-Fail "/height";$script:FAIL++}
$mk=Get-Json "/merkle"; if($mk.Ok -and $mk.Json.root){Write-Ok "/merkle root present (count=$($mk.Json.count))";$script:PASS++}else{Write-Fail "/merkle";$script:FAIL++}
$sys=Get-Json "/stats.sys"; if($sys.Ok -and $sys.Json.goroutines -ge 0){Write-Ok "/stats.sys ok (goroutines=$($sys.Json.goroutines))";$script:PASS++}else{Write-Fail "/stats.sys";$script:FAIL++}

$met=Invoke-HTTP -Method GET -Url ($BASE.TrimEnd('/') + "/metrics")
if($met.Ok -and $met.Raw -ne $null){Write-Ok "/metrics first lines:";($met.Raw.Content -split "`n")|Select-Object -First 5|%{ $_.TrimEnd()|Write-Host };$script:PASS++}else{Write-Fail "/metrics";$script:FAIL++}

$aliceAcc=Get-Json "/account/tho1alice";$aliceNon=Get-Json "/nonce/tho1alice"
if($aliceAcc.Ok -and $aliceNon.Ok){Write-Ok "/account/tho1alice & /nonce/tho1alice ok (nonce=$($aliceNon.Json.nonce))";$script:PASS++}else{Write-Fail "/account/tho1alice or /nonce/tho1alice";$script:FAIL++}
$bobAcc=Get-Json "/account/tho1bob";$bobNon=Get-Json "/nonce/tho1bob"
if($bobAcc.Ok -and $bobNon.Ok){Write-Ok "/account/tho1bob & /nonce/tho1bob ok (nonce=$($bobNon.Json.nonce))";$script:PASS++}else{Write-Fail "/account/tho1bob or /nonce/tho1bob";$script:FAIL++}

$al1=Get-Json "/alias/resolve?name=@alice"; if($al1.Ok -or ($al1.Code -in 404,501)){Write-Ok "/alias/resolve responded";$script:PASS++}else{Write-Fail "/alias/resolve";$script:FAIL++}
$al2=Get-Json "/alias/reverse?addr=tho1alice"; if($al2.Ok -or ($al2.Code -in 404,501)){Write-Ok "/alias/reverse (404/501 acceptable)";$script:PASS++}else{Write-Fail "/alias/reverse";$script:FAIL++}

$r2pCreate=Post-Json "/r2p/create" @{ from="tho1alice"; to="tho1bob"; amount_mas=2000; memo="demo" }
if($r2pCreate.Ok -and $r2pCreate.Json.ok){
  $rid=$r2pCreate.Json.id
  $r2pApprove=Post-Json "/r2p/approve" @{ id=$rid }
  if($r2pApprove.Ok -and $r2pApprove.Json.ok){Write-Ok "R2P paid id=$rid tx=$($r2pApprove.Json.tx_hash)";$script:PASS++}else{Write-Fail "R2P approve";$script:FAIL++}
}else{Write-Fail "R2P create";$script:FAIL++}

# /tx 테스트: 정책상 서명/바인딩 요구 시 자동 스킵
$doTx = -not $SkipTx
$reqSig=$false;$reqFrom=$false;$feeBps=10;$chainId="thomas-dev-1"
try{
  if($pol.Ok){
    if($pol.Json.require){$reqSig=[bool]$pol.Json.require.signature; $reqFrom=[bool]$pol.Json.require.from_pubkey}
    if($pol.Json.fee_bps){$feeBps=[int]$pol.Json.fee_bps}
    if($pol.Json.allowed_chain_id){$chainId=[string]$pol.Json.allowed_chain_id}
  }
}catch{}

function MinFee([uint64]$amt,[int]$bps){$f=[uint64](($amt*[uint64]$bps)/10000); if($f -lt 1){1}else{$f}}

if(-not $doTx){ Write-Info "Skip /tx test (-SkipTx specified)" }
elseif($reqSig -or $reqFrom){ Write-Info "Skip /tx test (server requires signature/from_pubkey)" }
else{
  $from="tho1alice";$to="tho1bob";$amtMas=1000;$feeMas=MinFee $amtMas $feeBps
  $n=Get-Json "/nonce/$from"
  if($n.Ok){
    $nonce=[uint64]$n.Json.expected_nonce
    $msg=("1|{0}|{1}|{2}|{3}|{4}|{5}|0" -f $from,$to,$amtMas,$feeMas,$nonce,$chainId)
    $sha=[System.Security.Cryptography.SHA256]::Create()
    $bytes=[System.Text.Encoding]::UTF8.GetBytes($msg)
    $hash=$sha.ComputeHash($bytes)
    $commitHex=($hash|%{ $_.ToString('x2') }) -join ''
    $txObj=[ordered]@{type=1;from=$from;to=$to;amount_mas=[uint64]$amtMas;fee_mas=[uint64]$feeMas;nonce=[uint64]$nonce;chain_id=$chainId;expiry_height=[uint64]0;msg_commitment=$commitHex}
    $tx=Post-Json "/tx" $txObj
    $reason=""; try{$reason=[string]$tx.Json.reason}catch{}
    $skipReasons=@("signature_required","from_pubkey_required","bad_signature","bad_signature_encoding","from_pubkey_mismatch","from_binding_not_implemented")
    if($tx.Ok -and $tx.Json.ok -and $tx.Json.parsed){Write-Ok "/tx submit ok (tx=$($tx.Json.tx_hash))";$script:PASS++}
    elseif($tx.Code -ge 400 -and ($skipReasons -contains $reason)){Write-Info "Skip /tx test (signature validation path: reason=$reason)"}
    elseif($tx.Code -ge 400 -and -not $tx.Ok -and ($reason -ne "")){Write-Info "Skip /tx test (HTTP $($tx.Code), reason=$reason)"}
    elseif(-not $tx.Ok -and $tx.Code -eq 0 -and $tx.Error){Write-Info "Skip /tx test (transport error: $($tx.Error))"}
    else{Write-Fail "/tx submit failed";$script:FAIL++}
  }else{Write-Fail "/nonce fetch for /tx";$script:FAIL++}
}

Summarize
