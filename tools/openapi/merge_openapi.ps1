# tools\openapi\merge_openapi.ps1
param(
  [string]$Out = "server\static\openapi\merged.json",
  [string[]]$Fragments = @(
    "server\static\openapi\tx.json",
    "server\static\openapi\rounds.json",
    "server\static\openapi\policy.json"
  )
)
$ErrorActionPreference = "Stop"

function To-HashDeep {
  param($o)
  if ($null -eq $o) { return $null }
  if ($o -is [System.Collections.IDictionary]) {
    $h=@{}; foreach($k in $o.Keys){ $h[$k]=To-HashDeep $o[$k] }; return $h
  }
  if ($o -is [System.Collections.IEnumerable] -and -not ($o -is [string])) {
    $arr=@(); foreach($v in $o){ $arr+=,(To-HashDeep $v) }; return $arr
  }
  if ($o -is [psobject]) {
    $h=@{}; $o.PSObject.Properties | ForEach-Object { $h[$_.Name]=To-HashDeep $_.Value }; return $h
  }
  return $o
}

function Merge-Map {
  param([hashtable]$dst,[hashtable]$src)
  foreach($k in $src.Keys){
    if ($dst.ContainsKey($k)) {
      if ($dst[$k] -is [hashtable] -and $src[$k] -is [hashtable]) {
        Merge-Map -dst $dst[$k] -src $src[$k] | Out-Null
      } else { $dst[$k] = $src[$k] }
    } else { $dst[$k] = $src[$k] }
  }
}

$root = @{
  openapi = "3.0.3"
  info    = @{ title = "THO Node API"; version = "0.1.0" }
  servers = @(@{ url = "/" })
  paths   = @{}
  components = @{ schemas = @{} }
}

foreach($f in $Fragments){
  if (-not (Test-Path $f)) { Write-Warning ("skip (not found): {0}" -f $f); continue }
  $obj = ConvertFrom-Json -InputObject (Get-Content $f -Raw -Encoding UTF8)
  $h = To-HashDeep $obj
  if ($h.ContainsKey('paths'))      { Merge-Map -dst $root.paths -src $h.paths | Out-Null }
  if ($h.ContainsKey('components')) {
    if ($h.components.ContainsKey('schemas')) { Merge-Map -dst $root.components.schemas -src $h.components.schemas | Out-Null }
  }
}

$raw = ($root | ConvertTo-Json -Depth 50)
New-Item -ItemType Directory -Force -Path (Split-Path $Out -Parent) | Out-Null
Set-Content -Path $Out -Value $raw -Encoding UTF8
Write-Host ("merged -> {0}" -f (Resolve-Path $Out))
