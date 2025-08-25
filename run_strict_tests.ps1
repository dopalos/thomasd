$ErrorActionPreference = 'Stop'

# 0) 실행 파일 확인 & 기존 프로세스 정리
if (-not (Test-Path .\thomasd_dbg.exe)) { throw "thomasd_dbg.exe 를 찾지 못했습니다. 실행 파일이 있는 폴더에서 실행하세요." }
Get-Process thomasd_dbg -ErrorAction SilentlyContinue | Stop-Process -Force

# 1) 엄격 모드 환경변수
$env:THOMAS_REQUIRE_COMMIT = '1'
$env:THOMAS_VERIFY_SIG    = '1'

# 2) 실행
$p = Start-Process .\thomasd_dbg.exe -RedirectStandardOutput thomasd_out.log -RedirectStandardError thomasd_err.log -PassThru

# 3) 리슨 포트 찾기
function Get-NodePort([int]$ProcId,[int]$TimeoutMs=12000){
  $sw=[Diagnostics.Stopwatch]::StartNew()
  do{
    $c = Get-NetTCPConnection -State Listen -OwningProcess $ProcId -ErrorAction SilentlyContinue |
         Where-Object { $_.LocalAddress -in @('127.0.0.1','::1') }
    if($c){ $c=$c|Select-Object -First 1; return @{ Addr=$c.LocalAddress; Port=$c.LocalPort } }
    if($p.HasExited){
      $tail = (Get-Content .\thomasd_err.log -Tail 80 -ea SilentlyContinue) -join "`n"
      throw "프로세스 종료됨. err.log tail:`n$tail"
    }
    Start-Sleep -Milliseconds 200
  } while ($sw.ElapsedMilliseconds -lt $TimeoutMs)
  throw "listen timeout"
}
$r = Get-NodePort -ProcId $p.Id
$baseUrl = "http://{0}:{1}" -f ($(if($r.Addr -eq '::1'){'[::1]'}else{'127.0.0.1'}), $r.Port)
"BASE = $baseUrl"

# 4) 4xx 본문까지 회수하는 HTTP 도우미
function Invoke-Json($Method,$Url,$Headers,$BodyJson){
  try{
    $resp = Invoke-WebRequest $Url -Method $Method -Headers $Headers -ContentType 'application/json' -Body $BodyJson -Proxy $null -TimeoutSec 6
    return @{ ok=$true; status=[int]$resp.StatusCode; body=$resp.Content }
  } catch {
    $resp = $_.Exception.Response
    $body = $null; $code = 0
    if ($resp) {
      $sr = New-Object IO.StreamReader($resp.GetResponseStream()); $body = $sr.ReadToEnd(); $sr.Dispose()
      $code = try { [int]$resp.StatusCode } catch { 0 }
    } else {
      $body = $_.ErrorDetails.Message
    }
    return @{ ok=$false; status=$code; body=$body; msg=$_.Exception.Message }
  }
}

# 5) readyz
"== readyz =="
$rz = Invoke-Json 'GET' ("{0}/readyz" -f $baseUrl) @{} $null
$rz
if (-not $rz.ok) {
  "== thomasd_err.log (tail) =="; Get-Content .\thomasd_err.log -Tail 80 -ea SilentlyContinue
  "== sockets (PID $($p.Id)) =="; Get-NetTCPConnection -OwningProcess $p.Id -ea SilentlyContinue | ft -AutoSize
  throw "readyz failed"
}

# 6) 커밋 계산 (SHA-256 of 'type|from|to|amount|fee|nonce|chainId|expiry')
function Get-Commit([int]$t,[string]$f,[string]$to,[long]$amt,[long]$fee,[long]$nonce,[string]$cid,[long]$exp){
  $s = "{0}|{1}|{2}|{3}|{4}|{5}|{6}|{7}" -f $t,$f,$to,$amt,$fee,$nonce,$cid,$exp
  $b=[Text.Encoding]::UTF8.GetBytes($s)
  $sha=[Security.Cryptography.SHA256]::Create()
  ($sha.ComputeHash($b) | ForEach-Object { $_.ToString('x2') }) -join ''
}
$commit = Get-Commit 1 "tho1alice" "tho1bob" 1 1 1 "thomas-dev-1" 0
"COMMIT=$commit"

# 7) B) 커밋만 (signature_required 기대)
"== B) commit only =="
$bodyB = (@{type=1;from="tho1alice";to="tho1bob";amount_mas=1;fee_mas=1;nonce=1;chain_id="thomas-dev-1";expiry_height=0;msg_commitment=$commit} | ConvertTo-Json -Compress)
$resB = Invoke-Json 'POST' ("{0}/tx" -f $baseUrl) @{} $bodyB
$resB

# 8) C) 서명 포함 (1회용 ed25519; 헤더 X-PubKey/X-Sig)
$go=@"
package main
import("crypto/ed25519";"crypto/rand";"encoding/base64";"fmt";"os")
func main(){ m:=[]byte(os.Args[1]); _,sk,_:=ed25519.GenerateKey(rand.Reader)
fmt.Println(base64.StdEncoding.EncodeToString(sk[32:]))
fmt.Println(base64.StdEncoding.EncodeToString(ed25519.Sign(sk,m))) }
"@
Set-Content .\sig_once.go $go -Encoding UTF8
$lines = & go run .\sig_once.go $commit
$pub=$lines[0]; $sig=$lines[1]
$hdr=@{'X-PubKey'=$pub; 'X-Sig'=$sig}

"== C) signed =="
$bodyC = $bodyB
$resC = Invoke-Json 'POST' ("{0}/tx" -f $baseUrl) $hdr $bodyC
$resC

# 9) 네트워크 예외일 때 진단
if (-not $resC.ok -and $resC.status -eq 0) {
  "== thomasd_err.log (tail) =="; Get-Content .\thomasd_err.log -Tail 120 -ea SilentlyContinue
  "== sockets (PID $($p.Id)) =="; Get-NetTCPConnection -OwningProcess $p.Id -ea SilentlyContinue | ft -AutoSize
}
