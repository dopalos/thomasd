$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
[System.Net.ServicePointManager]::Expect100Continue = $false

# --- 0) 서버 보장 + 최신 baseUrl 찾기 ---
# 노드가 없으면 환경 세팅하고 기동
$proc = Get-Process thomasd_dbg -ErrorAction SilentlyContinue
if(-not $proc){
  $sk = (Get-Content .\dev_sk.b64 -Raw).Trim()
  $pub = (& go run .\sig_util.go pub $sk).Trim()
  $env:THOMAS_PUBKEY_tho1alice = $pub
  $env:THOMAS_REQUIRE_COMMIT   = '1'
  $env:THOMAS_VERIFY_SIG       = '1'
  $proc = Start-Process .\thomasd_dbg.exe -RedirectStandardOutput thomasd_out.log -RedirectStandardError thomasd_err.log -PassThru
  Start-Sleep 1
}

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

$baseUrl = Get-BaseUrl -ownerPid $proc.Id
"[readyz] $baseUrl/readyz"

# readyz 대기
$ok=$false; for($i=0;$i -lt 25;$i++){
  try {
    $rz = Invoke-WebRequest "$baseUrl/readyz" -Proxy $null -TimeoutSec 2
    $jb = $rz.Content | ConvertFrom-Json
    "[readyz] ok height=$($jb.height)"
    $ok=$true; break
  } catch { Start-Sleep -Milliseconds 200 }
}
if(-not $ok){ throw "readyz timeout" }

# --- 1) 헬퍼들 ---
function Get-TxCommit([int]$t,[string]$f,[string]$to,[long]$amt,[long]$fee,[long]$nonce,[string]$cid,[long]$exp){
  $s = "{0}|{1}|{2}|{3}|{4}|{5}|{6}|{7}" -f $t,$f,$to,$amt,$fee,$nonce,$cid,$exp
  $b=[Text.Encoding]::UTF8.GetBytes($s)
  $sha=[Security.Cryptography.SHA256]::Create()
  ($sha.ComputeHash($b) | ForEach-Object { $_.ToString('x2') }) -join ''
}

# 키: dev_sk.b64가 없으면 생성
$skFile = ".\dev_sk.b64"
if(Test-Path $skFile){
  $SK_GOOD = (Get-Content $skFile -Raw).Trim()
} else {
  $gen = & go run .\sig_util.go gen
  $PK_TMP = $gen[0].Trim(); $SK_GOOD = $gen[1].Trim()
  Set-Content $skFile $SK_GOOD -Encoding ASCII
}
$PK_GOOD = (& go run .\sig_util.go pub $SK_GOOD).Trim()

# ALT 키는 항상 새로 생성(서명 불일치 유도)
$gen2 = & go run .\sig_util.go gen
$PK_ALT = $gen2[0].Trim(); $SK_ALT = $gen2[1].Trim()

# 바인딩: from=tho1alice는 PK_GOOD이어야 통과
$env:THOMAS_PUBKEY_tho1alice = $PK_GOOD
$env:THOMAS_REQUIRE_COMMIT   = '1'
$env:THOMAS_VERIFY_SIG       = '1'

# --- 2) PROBE (nonce=1로 expected_nonce 받기) ---
"[probe]"
$commit1 = Get-TxCommit 1 'tho1alice' 'tho1bob' 1 1 1 'thomas-dev-1' 0
$sign1   = & go run .\sig_util.go sign $SK_GOOD $commit1
$hdr1    = @{ 'X-PubKey'=$sign1[0].Trim(); 'X-Sig'=$sign1[1].Trim(); 'Expect'='' }
$body1   = (@{type=1;from='tho1alice';to='tho1bob';amount_mas=1;fee_mas=1;nonce=1;chain_id='thomas-dev-1';expiry_height=0;msg_commitment=$commit1} | ConvertTo-Json -Compress)

$r1 = Invoke-WebRequest "$baseUrl/tx" -Method Post -Headers $hdr1 -ContentType 'application/json' -Body $body1 -Proxy $null -TimeoutSec 6
$j1 = $r1.Content | ConvertFrom-Json
"status=$($r1.StatusCode) ok=$($j1.ok) applied=$($j1.applied) expected_nonce=$($j1.expected_nonce)"

$n2 = [int]$j1.expected_nonce

# --- 3) HAPPY (정상 서명) ---
"[happy]"
$commit2 = Get-TxCommit 1 'tho1alice' 'tho1bob' 1 1 $n2 'thomas-dev-1' 0
$sign2   = & go run .\sig_util.go sign $SK_GOOD $commit2
$hdr2    = @{ 'X-PubKey'=$sign2[0].Trim(); 'X-Sig'=$sign2[1].Trim(); 'Expect'='' }
$body2   = (@{type=1;from='tho1alice';to='tho1bob';amount_mas=1;fee_mas=1;nonce=$n2;chain_id='thomas-dev-1';expiry_height=0;msg_commitment=$commit2} | ConvertTo-Json -Compress)

$r2 = Invoke-WebRequest "$baseUrl/tx" -Method Post -Headers $hdr2 -ContentType 'application/json' -Body $body2 -Proxy $null -TimeoutSec 8
$j2 = $r2.Content | ConvertFrom-Json
"status=$($r2.StatusCode) ok=$($j2.ok) applied=$($j2.applied) reason=$($j2.reason)"

# --- 4) INVALID-SIG (헤더 pub=GOOD, 서명키=ALT로 고의 불일치) ---
"[invalid-sig]"
$n3 = $n2 + 1
$commit3 = Get-TxCommit 1 'tho1alice' 'tho1bob' 1 1 $n3 'thomas-dev-1' 0
$sign3   = & go run .\sig_util.go sign $SK_ALT $commit3   # <-- 다른 키로 서명!
$hdr3    = @{ 'X-PubKey'=$PK_GOOD; 'X-Sig'=$sign3[1].Trim(); 'Expect'='' }  # pub은 GOOD으로 유지 (바인딩 통과)
$body3   = (@{type=1;from='tho1alice';to='tho1bob';amount_mas=1;fee_mas=1;nonce=$n3;chain_id='thomas-dev-1';expiry_height=0;msg_commitment=$commit3} | ConvertTo-Json -Compress)

try {
  $r3 = Invoke-WebRequest "$baseUrl/tx" -Method Post -Headers $hdr3 -ContentType 'application/json' -Body $body3 -Proxy $null -TimeoutSec 8
  $j3 = $r3.Content | ConvertFrom-Json
  "status=$($r3.StatusCode) ok=$($j3.ok) applied=$($j3.applied) reason=$($j3.reason)"
} catch {
  $resp = $_.Exception.Response
  if ($resp) {
    $status = [int]$resp.StatusCode
    $raw = (New-Object IO.StreamReader $resp.GetResponseStream()).ReadToEnd()
    try { $j = $raw | ConvertFrom-Json } catch { $j = $null }
    if ($j) {
      "status=$status ok=$($j.ok) applied=$($j.applied) reason=$($j.reason)"
    } else {
      "status=$status raw=$raw"
    }
  } else {
    "HTTP error (connect): $($_.Exception.Message)"
  }
}
