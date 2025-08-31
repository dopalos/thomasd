# THO Node (thomasd)

- /docs (Swagger UI) — THOMAS_ENABLE_DOCS=1 일 때만 노출
- /openapi.json, /openapi/merged.json
- /metrics — THOMAS_ENABLE_METRICS=1 일 때만 노출

## Quick start (Windows, PS 5.1)
go build -o .\bin\thomasd.exe .\cmd\thomasd
$env:THOMAS_ENABLE_DOCS="1"; $env:THOMAS_ENABLE_METRICS="1"
Start-Process .\bin\thomasd.exe -WorkingDirectory .
