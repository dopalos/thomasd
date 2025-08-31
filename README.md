# THO Node (thomasd)

- **Docs(UI)**: `/docs` — `THOMAS_ENABLE_DOCS=1`일 때만 노출
- **OpenAPI**: `/openapi.json` (코어), `/openapi/merged.json` (조각 병합판)
- **Metrics**: `/metrics` — `THOMAS_ENABLE_METRICS=1`일 때만 노출
- **Signed headers**: `/round/latest/signed`, `/round/{round}/signed`
- **Tx submit**: `/tx`
- **Health/Policy**: `/health`, `/policy`

---

## 1) 빠른 시작

```powershell
# 빌드
cd C:\thomas-scaffold\thomasd
go build -o .\bin\thomasd.exe .\cmd\thomasd

# (개발용) 문서/메트릭 노출
$env:THOMAS_ENABLE_DOCS    = "1"
$env:THOMAS_ENABLE_METRICS = "1"

# 실행
Start-Process -FilePath .\bin\thomasd.exe -WorkingDirectory . -WindowStyle Hidden
Start-Sleep 1

# BASE 탐색
$pid  = (Get-Process thomasd | Select-Object -First 1).Id
$port = (Get-NetTCPConnection -State Listen | ? OwningProcess -eq $pid | Select -First 1 -Expand LocalPort)
$BASE = "http://127.0.0.1:$port"
"BASE = $BASE"

## 운영 체크리스트
- 문서 노출: THOMAS_ENABLE_DOCS=1 일 때만 /docs, /openapi/* 노출
- 메트릭 노출: THOMAS_ENABLE_METRICS=1 일 때 /metrics 노출
- 프록시: tools\signedmsg_proxy\run-proxy-task.ps1 를 시작프로그램에 등록(install-proxy-startup.ps1)

## 빠른 점검
$BASE=(Select-String -Path .\run_logs\out_*.txt -Pattern 'listening on (http://[0-9\.]+:\d+)'|Select-Object -Last 1).Matches[0].Groups[1].Value.Trim()
(Invoke-RestMethod "$BASE/health").status
(Invoke-RestMethod "$BASE/openapi/merged.json").openapi
curl.exe -sI "$BASE/docs/" | findstr /C:"200"
(Invoke-RestMethod "$BASE/metrics") -as [string] | findstr thomas_health_ok
