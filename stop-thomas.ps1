Get-Process thomasd -ErrorAction SilentlyContinue | Stop-Process -Force
Write-Host "thomasd stopped."
