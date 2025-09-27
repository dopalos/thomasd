# demo.ps1
$ErrorActionPreference='Stop'
powershell -NoProfile -ExecutionPolicy Bypass -File .\set-demo-env.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\smoke-all.ps1
