# thomasd

경량 합의/서명 데모 노드. Windows PowerShell 기준으로 설명합니다.

## TL;DR (5분 셋업)

```powershell
# 0) 문서/메트릭 노출 (옵션)
$env:THOMAS_ENABLE_DOCS="1"
$env:THOMAS_ENABLE_METRICS="1"

# 1) thomasd 실행 (포그라운드)
.\bin\thomasd.exe
# 로그에 listening 주소가 뜸: e.g. http://127.0.0.1:62533

# 2) BASE 자동 탐색(다른 콘솔에서)
$p = Get-Process thomasd -ErrorAction Stop
$c = Get-NetTCPConnection -State Listen | ? OwningProcess -eq $p.Id | Select -First 1
$addr = if($c.LocalAddress -eq '::1'){'[::1]'} else {'127.0.0.1'}
$BASE = "http://{0}:{1}" -f $addr,$c.LocalPort
"BASE = $BASE"

# 3) OpenAPI 병합 + Swagger 확인
powershell -NoProfile -ExecutionPolicy Bypass -File .\tools\openapi\merge_openapi.ps1
(Invoke-RestMethod "$BASE/openapi/merged.json").openapi
curl.exe -sI "$BASE/docs/" | findstr /C:"200"

# 4) 프록시 기동(서명 메시지 호환 레이어)
Get-Process signedmsg-proxy -ErrorAction SilentlyContinue | Stop-Process -Force
.\tools\signedmsg_proxy\signedmsg-proxy.exe -listen :63081 -base $BASE
curl.exe -sI "http://127.0.0.1:63081/round/latest/signed_msg" | findstr /C:"200"
(Invoke-RestMethod "http://127.0.0.1:63081/round/latest/signed_msg").signature_valid

## API Assets
- OpenAPI (merged): [merged.json](https://github.com/dopalos/thomasd/releases/latest/download/merged.json)
- Postman Collection: [thomasd-postman.json](https://github.com/dopalos/thomasd/releases/latest/download/thomasd-postman.json)

### Postman 사용법
1) 위 컬렉션(JSON) 다운로드 → Postman `Import`.
2) `baseUrl` 변수를 원하는 엔드포인트로 설정 (기본값: `http://127.0.0.1:62533`).
3) 필요한 경우 `Authorization` 전역 변수를 환경에 추가.
