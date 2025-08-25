$ErrorActionPreference='Stop'
$ProgressPreference='SilentlyContinue'
[System.Net.ServicePointManager]::Expect100Continue = $false

# 0) 데몬 없으면 엄격 모드로 기동
$proc = Get-Process thomasd_dbg -ErrorAction SilentlyContinue
if(-not $proc){
  $env:THOMAS_REQUIRE_COMMIT='1'
  $env:THOMAS_VERIFY_SIG='1'
  $proc = Start-Process .\thomasd_dbg.exe -RedirectStandardOutput thomasd_out.log -RedirectStandardError thomasd_err.log -PassThru
  Start-Sleep -Milliseconds 800
}

# 1) 리슨 포트 → baseUrl
function Get-BaseUrl([int]$procId,[int]$timeoutMs=12000){
  $sw=[Diagnostics.Stopwatch]::StartNew()
  do{
    $c = Get-NetTCPConnection -State Listen -OwningProcess $procId -ErrorAction SilentlyContinue |
         Where-Object { $_.LocalAddress -in @('127.0.0.1','::1') } | Select-Object -First 1
    if($c){
      $addr = if($c.LocalAddress -eq '::1'){'[::1]'}else{'127.0.0.1'}
      return ("http://{0}:{1}" -f $addr,$c.LocalPort)
    }
    Start-Sleep -Milliseconds 200
  } while ($sw.ElapsedMilliseconds -lt $timeoutMs)
  throw "listen timeout"
}
$baseUrl = Get-BaseUrl -procId $proc.Id
"[readyz] $baseUrl/readyz"
$rz = Invoke-WebRequest "$baseUrl/readyz" -Proxy $null -TimeoutSec 6
$jb = $rz.Content | ConvertFrom-Json
"[readyz] ok height=$($jb.height)"

# 2) 커밋 계산
function Get-TxCommit([int]$t,[string]$f,[string]$to,[long]$amt,[long]$fee,[long]$nonce,[string]$cid,[long]$exp){
  $s = "{0}|{1}|{2}|{3}|{4}|{5}|{6}|{7}" -f $t,$f,$to,$amt,$fee,$nonce,$cid,$exp
  $b=[Text.Encoding]::UTF8.GetBytes($s)
  $sha=[Security.Cryptography.SHA256]::Create()
  ($sha.ComputeHash($b) | ForEach-Object { $_.ToString('x2') }) -join ''
}

# 3) ed25519 1회용 서명기 (여기서 here-string 안 씀!)
$goLines = @(
'package main'
'import ('
'"crypto/ed25519"'
'"crypto/rand"'
'"encoding/base64"'
'"fmt"'
'"os"'
')'
'func main(){'
' m := []byte(os.Args[1])'
' _, sk, _ := ed25519.GenerateKey(rand.Reader)'
' fmt.Println(base64.StdEncoding.EncodeToString(sk[32:]))'
' fmt.Println(base64.StdEncoding.EncodeToString(ed25519.Sign(sk, m)))'
'}'
)
$goLines | Set-Content .\sig_once.go -Encoding UTF8

# 4) 1차 전송 (nonce=1 → 보통 apply:bad_nonce)
$commit1 = Get-TxCommit 1 'tho1alice' 'tho1bob' 1 1 1 'thomas-dev-1' 0
$lines1  = & go run .\sig_once.go $commit1
$hdr1    = @{ 'X-PubKey'=$lines1[0].Trim(); 'X-Sig'=$lines1[1].Trim(); 'Expect'='' }
$body1   = (@{type=1;from='tho1alice';to='tho1bob';amount_mas=1;fee_mas=1;nonce=1;chain_id='thomas-dev-1';expiry_height=0;msg_commitment=$commit1} | ConvertTo-Json -Compress)

"[POST-1] nonce=1"
$r1 = Invoke-WebRequest "$baseUrl/tx" -Method Post -Headers $hdr1 -ContentType 'application/json' -Body $body1 -Proxy $null -TimeoutSec 8
"[POST-1] status=$($r1.StatusCode)"
$j1 = $r1.Content | ConvertFrom-Json
"[info] expected_nonce=$($j1.expected_nonce)"

# 5) expected_nonce로 재전송 (정상 적용 기대)
$nonce2  = [int]$j1.expected_nonce
$commit2 = Get-TxCommit 1 'tho1alice' 'tho1bob' 1 1 $nonce2 'thomas-dev-1' 0
$lines2  = & go run .\sig_once.go $commit2
$hdr2    = @{ 'X-PubKey'=$lines2[0].Trim(); 'X-Sig'=$lines2[1].Trim(); 'Expect'='' }
$body2   = (@{type=1;from='tho1alice';to='tho1bob';amount_mas=1;fee_mas=1;nonce=$nonce2;chain_id='thomas-dev-1';expiry_height=0;msg_commitment=$commit2} | ConvertTo-Json -Compress)

"[POST-2] nonce=$nonce2"
$r2 = Invoke-WebRequest "$baseUrl/tx" -Method Post -Headers $hdr2 -ContentType 'application/json' -Body $body2 -Proxy $null -TimeoutSec 8
"[POST-2] status=$($r2.StatusCode)"
$j2 = $r2.Content | ConvertFrom-Json

# 6) 결과 요약
""
"ok             : $($j2.ok)"
"applied        : $($j2.applied)"
"nonce          : $($j2.nonce)"
"expected_nonce : $($j2.expected_nonce)"
"reason         : $($j2.reason)"
"tx_hash        : $($j2.tx_hash)"
"height         : $($j2.height)"
