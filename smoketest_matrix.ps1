$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
[System.Net.ServicePointManager]::Expect100Continue = $false

function Get-BaseUrl([int]$ownerPid,[int]$timeoutMs=15000){
  $sw=[Diagnostics.Stopwatch]::StartNew()
  do{
    $c = Get-NetTCPConnection -State Listen -OwningProcess $ownerPid -ErrorAction SilentlyContinue |
         Where-Object { $_.LocalAddress -in @('127.0.0.1','::1') } | Select-Object -First 1
    if($c){ $h = if($c.LocalAddress -eq '::1'){'[::1]'}else{'127.0.0.1'}; return ("http://{0}:{1}" -f $h,$c.LocalPort) }
    Start-Sleep -Milliseconds 200
  } while ($sw.ElapsedMilliseconds -lt $timeoutMs)
  throw "listen timeout"
}
function Get-TxCommit([int]$t,[string]$f,[string]$to,[long]$amt,[long]$fee,[long]$nonce,[string]$cid,[long]$exp){
  $s = "{0}|{1}|{2}|{3}|{4}|{5}|{6}|{7}" -f $t,$f,$to,$amt,$fee,$nonce,$cid,$exp
  $b=[Text.Encoding]::UTF8.GetBytes($s)
  $sha=[Security.Cryptography.SHA256]::Create()
  ($sha.ComputeHash($b) | ForEach-Object { $_.ToString('x2') }) -join ''
}
function TryParseJson($raw){ try { return $raw | ConvertFrom-Json } catch { return $null } }
function SignPair([string]$sk,[string]$msg){
  $o = & go run .\sig_util.go sign $sk $msg
  $a = @($o)
  if($a.Count -lt 2){ throw "sign: expected 2 lines (pub,sig), got $($a.Count)" }
  return [pscustomobject]@{ pub=$a[0].Trim(); sig=$a[1].Trim() }
}
function HttpPostTx($base,$hdr,$body){
  try{
    $r = Invoke-WebRequest "$base/tx" -Method Post -Headers $hdr -ContentType 'application/json' -Body $body -Proxy $null -TimeoutSec 8
    $j = TryParseJson $r.Content
    if($j){ $j | Add-Member NoteProperty status $r.StatusCode -Force; return $j }
    return [pscustomobject]@{ status=$r.StatusCode; ok=$true; applied=$null; reason='non-json-success'; raw=$r.Content }
  }catch{
    $status=$null; $raw=$null
    if($_.Exception.Response){
      try { $status = $_.Exception.Response.StatusCode.value__ } catch {}
      try { $raw = (New-Object IO.StreamReader $_.Exception.Response.GetResponseStream()).ReadToEnd() } catch {}
    }
    $j = $null
    if($raw){ $j = TryParseJson $raw }
    if($j){ $j | Add-Member NoteProperty status $status -Force; return $j }
    return [pscustomobject]@{ status=$status; ok=$false; applied=$false; reason='http_error'; raw=$raw }
  }
}
function PrintResult($title,$j){
  ""
  "=== $title ==="
  "status         : $($j.status)"
  "ok             : $($j.ok)"
  "applied        : $($j.applied)"
  "nonce          : $($j.nonce)"
  "expected_nonce : $($j.expected_nonce)"
  "reason         : $($j.reason)"
  "tx_hash        : $($j.tx_hash)"
  "height         : $($j.height)"
  if($j.raw){ "raw            : $($j.raw)" }
}

# ── node ready ──
$proc = Get-Process thomasd_dbg -ErrorAction SilentlyContinue
if(-not $proc){
  $env:THOMAS_REQUIRE_COMMIT='1'
  $env:THOMAS_VERIFY_SIG='1'
  $proc = Start-Process .\thomasd_dbg.exe -RedirectStandardOutput thomasd_out.log -RedirectStandardError thomasd_err.log -PassThru
  Start-Sleep 1
}
$base = Get-BaseUrl -ownerPid ((@($proc.Id))[0])
"[readyz] $base/readyz"
$ok=$false; for($i=0;$i -lt 50;$i++){
  try{ $rz = Invoke-WebRequest "$base/readyz" -Proxy $null -TimeoutSec 2; $jb = $rz.Content | ConvertFrom-Json; "[readyz] ok height=$($jb.height)"; $ok=$true; break }catch{}
  Start-Sleep -Milliseconds 200
}
if(-not $ok){ throw "readyz timeout" }

# ── keys ──
$skFile = ".\dev_sk.b64"
if(Test-Path $skFile){ $sk = (Get-Content $skFile -Raw).Trim() } else {
  $gen = go run .\sig_util.go gen
  $sk  = (@($gen))[1].Trim()
  Set-Content $skFile $sk -Encoding ASCII
}
$sk_bad = (@(go run .\sig_util.go gen))[1].Trim()

# ── 0) probe for expected nonce ──
$probeCommit = Get-TxCommit 1 'tho1alice' 'tho1bob' 1 1 1 'thomas-dev-1' 0
$pair0 = SignPair $sk $probeCommit
$hdrProbe = @{ 'X-PubKey'=$pair0.pub; 'X-Sig'=$pair0.sig }
$bodyProbe = (@{type=1;from='tho1alice';to='tho1bob';amount_mas=1;fee_mas=1;nonce=1;chain_id='thomas-dev-1';expiry_height=0;msg_commitment=$probeCommit} | ConvertTo-Json -Compress)
$j0 = HttpPostTx $base $hdrProbe $bodyProbe
[int]$nonce = $j0.expected_nonce
PrintResult "0) probe (expect nonce)" $j0

# 1) happy path
$commit1 = Get-TxCommit 1 'tho1alice' 'tho1bob' 1 1 $nonce 'thomas-dev-1' 0
$pair1 = SignPair $sk $commit1
$hdr1 = @{ 'X-PubKey'=$pair1.pub; 'X-Sig'=$pair1.sig }
$body1 = (@{type=1;from='tho1alice';to='tho1bob';amount_mas=1;fee_mas=1;nonce=$nonce;chain_id='thomas-dev-1';expiry_height=0;msg_commitment=$commit1} | ConvertTo-Json -Compress)
$j1 = HttpPostTx $base $hdr1 $body1
PrintResult "1) happy path" $j1

# 2) replay (same nonce)
$pairR = SignPair $sk $commit1
$hdrR = @{ 'X-PubKey'=$pairR.pub; 'X-Sig'=$pairR.sig }
$j2 = HttpPostTx $base $hdrR $body1
PrintResult "2) replay (same nonce)" $j2

# 3) wrong-key but consistent pair
$commit3 = Get-TxCommit 1 'tho1alice' 'tho1bob' 1 1 ($nonce+1) 'thomas-dev-1' 0
$pair3 = SignPair $sk_bad $commit3
$hdr3 = @{ 'X-PubKey'=$pair3.pub; 'X-Sig'=$pair3.sig }
$body3 = (@{type=1;from='tho1alice';to='tho1bob';amount_mas=1;fee_mas=1;nonce=($nonce+1);chain_id='thomas-dev-1';expiry_height=0;msg_commitment=$commit3} | ConvertTo-Json -Compress)
$j3 = HttpPostTx $base $hdr3 $body3
PrintResult "3) bad signature scenario A (other key, no binding)" $j3

# 3a) invalid signature: pub=ok, sig=bad sk
$commit3a = Get-TxCommit 1 'tho1alice' 'tho1bob' 1 1 ($nonce+2) 'thomas-dev-1' 0
$pair3a_bad = SignPair $sk_bad $commit3a
$hdr3a = @{ 'X-PubKey'=($pair1.pub); 'X-Sig'=$pair3a_bad.sig }  # pub은 ok, 서명은 bad
$body3a = (@{type=1;from='tho1alice';to='tho1bob';amount_mas=1;fee_mas=1;nonce=($nonce+2);chain_id='thomas-dev-1';expiry_height=0;msg_commitment=$commit3a} | ConvertTo-Json -Compress)
$j3a = HttpPostTx $base $hdr3a $body3a
PrintResult "3a) invalid signature (pub != signer)" $j3a

# 4) expired height
$rz = Invoke-WebRequest "$base/readyz" -Proxy $null -TimeoutSec 2; $jb = $rz.Content | ConvertFrom-Json
$expired = [int]$jb.height - 1
$commit4 = Get-TxCommit 1 'tho1alice' 'tho1bob' 1 1 ($nonce+3) 'thomas-dev-1' $expired
$pair4 = SignPair $sk $commit4
$hdr4 = @{ 'X-PubKey'=$pair4.pub; 'X-Sig'=$pair4.sig }
$body4 = (@{type=1;from='tho1alice';to='tho1bob';amount_mas=1;fee_mas=1;nonce=($nonce+3);chain_id='thomas-dev-1';expiry_height=$expired;msg_commitment=$commit4} | ConvertTo-Json -Compress)
$j4 = HttpPostTx $base $hdr4 $body4
PrintResult "4) expired height" $j4

# 5) wrong chain_id
$commit5 = Get-TxCommit 1 'tho1alice' 'tho1bob' 1 1 ($nonce+4) 'wrong-chain' 0
$pair5 = SignPair $sk $commit5
$hdr5 = @{ 'X-PubKey'=$pair5.pub; 'X-Sig'=$pair5.sig }
$body5 = (@{type=1;from='tho1alice';to='tho1bob';amount_mas=1;fee_mas=1;nonce=($nonce+4);chain_id='wrong-chain';expiry_height=0;msg_commitment=$commit5} | ConvertTo-Json -Compress)
$j5 = HttpPostTx $base $hdr5 $body5
PrintResult "5) wrong chain_id" $j5

""
"=== done ==="

