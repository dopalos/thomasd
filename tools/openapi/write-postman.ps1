param(
  [string]$Merged = ".\server\static\openapi\merged.json",
  [string]$Out = ".\dist\thomasd-postman.json",
  [string]$BaseForVar = "http://127.0.0.1:62533"
)

$ErrorActionPreference = 'Stop'
$root = Get-Content $Merged -Raw -Encoding UTF8 | ConvertFrom-Json

function New-PmItem {
  param([string]$method,[string]$path,[object]$bodyJson)
  $item = [ordered]@{
    name    = "$method $path"
    request = [ordered]@{
      method = $method
      header = @(@{ key="Content-Type"; value="application/json"; disabled=$false })
      url    = @{
        raw  = "{{base}}$path"
        host = @("{{base}}")
        path = ($path.Trim('/') -split '/')
      }
    }
  }
  if ($method -eq 'POST' -and $bodyJson) {
    $item.request.body = @{
      mode    = "raw"
      raw     = ($bodyJson | ConvertTo-Json -Depth 16)
      options = @{ raw = @{ language = "json" } }
    }
  }
  return $item
}

$items = @()
$httpVerbs = @('get','post','put','patch','delete','head','options')

$paths = $root.paths.PSObject.Properties | ForEach-Object { $_.Name }
foreach ($p in $paths) {
  $node = $root.paths.$p
  foreach ($prop in $node.PSObject.Properties) {
    if ($httpVerbs -notcontains $prop.Name.ToLower()) { continue }
    $m = $prop.Name.ToUpperInvariant()
    $body = $null
    if ($p -eq '/tx' -and $m -eq 'POST') {
      $body = @{
        type=1; from="tho1alice"; to="tho1bob";
        amount_mas=100000; fee_mas=100; nonce=4935; expiry_height=0; chain_id="thomas-dev-1"
      }
    }
    $items += (New-PmItem -method $m -path $p -bodyJson $body)
  }
}

$collection = [ordered]@{
  info = @{
    name   = "thomasd API"
    schema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
  }
  item     = $items
  variable = @(@{ key="base"; value=$BaseForVar })
}

New-Item -ItemType Directory -Force (Split-Path -Parent $Out) | Out-Null
($collection | ConvertTo-Json -Depth 64) | Out-File -FilePath $Out -Encoding UTF8
Write-Host "Postman collection -> $Out"
