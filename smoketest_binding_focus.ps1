$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
[System.Net.ServicePointManager]::Expect100Continue = $false

function Get-BaseUrl([int]$timeoutMs=12000){
  $proc = Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Select-Object -First 1
  if(-not $proc){ throw "thomasd_dbg not running" }
  $procId = $proc.Id
  $sw=[Diagnostics.Stopwatch]::StartNew()
  do{
    $c = Get-NetTCPConnection -State Listen -OwningProcess $procId -ErrorAction SilentlyContinue |
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

# --- 준비: good(바인딩된) 키, alt(다른) 키 ---
$skFile = ".\dev_sk.b64"
if(!(Test-Path $skFile)){ throw "dev_sk.b64 not found" }
$goodSk = (Get-Content $skFile -Raw).Trim()
$goodPub = (& go run .\sig_util.go pub $goodSk).Trim()

# alt key 생성
$gen = & go run .\sig_util.go gen
$altPub = $gen[0].Trim()
$altSk  = $gen[1].Trim()

"GOOD_PUB=$goodPub"
"ALT_PUB =$altPub"

# --- 노드 health ---
$baseUrl = Get-BaseUrl
"[readyz] $baseUrl/readyz"
$rz = Invoke-WebRequest "$baseUrl/readyz" -Proxy $null -TimeoutSec 5
$jb = $rz.Content | ConvertFrom-Json
"[readyz] ok height=$($jb.height)"

# --- 공통 파라미터 ---
$from='tho1alice'    # 바인딩된 계정으로 고정!
$to='tho1bob'
$amt=1; $fee=1; $cid='thomas-dev-1'; $exp=0

# 0) probe: nonce=1으로 expected_nonce 얻기 (good 키/헤더)
$commit0 = Get-TxCommit 1 $from $to $amt $fee 1 $cid $exp
$hdr0 = @{ 'X-PubKey'=$goodPub; 'X-Sig'=(& go run .\sig_util.go sign $goodSk $commit0)[1].Trim(); 'Expect'='' }
$body0 = (@{type=1;from=$from;to=$to;amount_mas=$amt;fee_mas=$fee;nonce=1;chain_id=$cid;expiry_height=$exp;msg_commitment=$commit0} | ConvertTo-Json -Compress)
$r0 = Invoke-WebRequest "$baseUrl/tx" -Method Post -Headers $hdr0 -ContentType 'application/json' -Body $body0 -Proxy $null -TimeoutSec 6
$j0 = $r0.Content | ConvertFrom-Json
$nonceGood = [int]$j0.expected_nonce
"`n=== expected_nonce=$nonceGood ==="

# 1) happy path: good 키/헤더 (정상 적용)
$commit1 = Get-TxCommit 1 $from $to $amt $fee $nonceGood $cid $exp
$sign1 = & go run .\sig_util.go sign $goodSk $commit1
$hdr1  = @{ 'X-PubKey'=$goodPub; 'X-Sig'=$sign1[1].Trim(); 'Expect'='' }
$body1 = (@{type=1;from=$from;to=$to;amount_mas=$amt;fee_mas=$fee;nonce=$nonceGood;chain_id=$cid;expiry_height=$exp;msg_commitment=$commit1} | ConvertTo-Json -Compress)
$r1 = Invoke-WebRequest "$baseUrl/tx" -Method Post -Headers $hdr1 -ContentType 'application/json' -Body $body1 -Proxy $null -TimeoutSec 6
$j1 = $r1.Content | ConvertFrom-Json
"`n[happy] ok=$($j1.ok) applied=$($j1.applied) reason=$($j1.reason)"

# 2) binding 차단 테스트: from=tho1alice 고정, alt 키로 서명 + alt pub 헤더
$nonceBad = $nonceGood + 1
$commit2 = Get-TxCommit 1 $from $to $amt $fee $nonceBad $cid $exp
$sign2 = & go run .\sig_util.go sign $altSk $commit2
$hdr2  = @{ 'X-PubKey'=$altPub; 'X-Sig'=$sign2[1].Trim(); 'Expect'='' }
$body2 = (@{type=1;from=$from;to=$to;amount_mas=$amt;fee_mas=$fee;nonce=$nonceBad;chain_id=$cid;expiry_height=$exp;msg_commitment=$commit2} | ConvertTo-Json -Compress)

try{
  $r2 = Invoke-WebRequest "$baseUrl/tx" -Method Post -Headers $hdr2 -ContentType 'application/json' -Body $body2 -Proxy $null -TimeoutSec 6
  $j2 = $r2.Content | ConvertFrom-Json
  "`n[binding-test A] status=$($r2.StatusCode) ok=$($j2.ok) applied=$($j2.applied) reason=$($j2.reason)"
}catch{
  if($_.Exception.Response){
    $raw = (New-Object IO.StreamReader $_.Exception.Response.GetResponseStream()).ReadToEnd()
    "`n[binding-test A] HTTP error: $raw"
  } else { throw }
}

# 3) 서명 불일치 테스트: good pub 헤더 + alt 키로 서명 (서명 검증 실패 400 예상)
$nonceBad2 = $nonceBad + 1
$commit3 = Get-TxCommit 1 $from $to $amt $fee $nonceBad2 $cid $exp
$sign3 = & go run .\sig_util.go sign $altSk $commit3
$hdr3  = @{ 'X-PubKey'=$goodPub; 'X-Sig'=$sign3[1].Trim(); 'Expect'='' }
$body3 = (@{type=1;from=$from;to=$to;amount_mas=$amt;fee_mas=$fee;nonce=$nonceBad2;chain_id=$cid;expiry_height=$exp;msg_commitment=$commit3} | ConvertTo-Json -Compress)
try{
  $r3 = Invoke-WebRequest "$baseUrl/tx" -Method Post -Headers $hdr3 -ContentType 'application/json' -Body $body3 -Proxy $null -TimeoutSec 6
  $j3 = $r3.Content | ConvertFrom-Json
  "`n[invalid-sig] status=$($r3.StatusCode) ok=$($j3.ok) applied=$($j3.applied) reason=$($j3.reason)"
}catch{
  if($_.Exception.Response){
    $raw = (New-Object IO.StreamReader $_.Exception.Response.GetResponseStream()).ReadToEnd()
    "`n[invalid-sig] HTTP error: $raw"
  } else { throw }
}
