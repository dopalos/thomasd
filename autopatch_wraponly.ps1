$ErrorActionPreference = "Stop"

# --- repo root check ---
if (-not (Test-Path ".\go.mod")) {
  if (Test-Path "C:\thomas-scaffold\thomasd\go.mod") { Set-Location "C:\thomas-scaffold\thomasd" }
  else { throw "go.mod not found. Run at repo root." }
}
$target = "internal\rpc\router.go"
if (-not (Test-Path $target)) { throw "router.go not found: $target" }

function Try-Build {
  go clean -cache | Out-Null
  & go build -tags "cbor blake3" -o .\thomasd_dbg.exe .\cmd\thomasd 2>&1 |
    Tee-Object -Variable buildOut | Out-Null
  if ($LASTEXITCODE -ne 0) {
    "`n--- go build failed (tail) ---`n" + ($buildOut | Select-Object -Last 80) -join "`n" | Write-Host -ForegroundColor Yellow
    return $false
  }
  return $true
}

# --- Fallback: no brace scan. Use next 'func' boundary to slice the segment ---
function Patch-NewRouter-WrapOnly([string]$code){
  $m = [regex]::Match($code,'(?s)func\s+NewRouter\s*\([^)]*\)\s*[^{]*\{?')
  if(-not $m.Success){ throw "NewRouter signature not found (fallback)" }
  $start = $m.Index

  # find next 'func ' after start using .NET overload Match(input, startAt)
  $rxNext = [regex]'(?m)^\s*func\s+\w+\s*\('
  $mNext  = $rxNext.Match($code, $start + 1)
  $end    = if($mNext.Success){ $mNext.Index } else { $code.Length }

  $segment = $code.Substring($start, $end - $start)

  # 이미 래핑돼 있으면 그대로 반환
  if ($segment -match 'precheckCommit\(|precheckSig\(') { return $code }

  # 마지막 return 찾아서 래핑
  $rets = [regex]::Matches($segment,'(?m)^\s*return\s+(.+?)\s*$')
  if($rets.Count -eq 0){ throw "return not found in NewRouter segment (fallback)" }
  $last  = $rets[$rets.Count-1]
  $expr  = $last.Groups[1].Value.Trim()
  $indent = ($last.Value -replace '^( *).*','$1')
  $wrapped = $indent + 'return precheckSig(precheckCommit(' + $expr + '))'

  $segment2 = $segment.Substring(0,$last.Index) + $wrapped + $segment.Substring($last.Index + $last.Length)
  return $code.Substring(0,$start) + $segment2 + $code.Substring($end)
}

# --- load & backup ---
[string]$src = Get-Content $target -Raw
$bk = "$target.bak_$(Get-Date -Format 'yyyyMMdd_HHmmss')"
Copy-Item $target $bk -Force
"BACKUP: $bk"

# --- patch (fallback only) ---
try {
  $srcNew = Patch-NewRouter-WrapOnly $src
  if($srcNew -eq $src){
    "PATCH: NewRouter already wrapped (skip)" | Write-Host
  } else {
    $src = $srcNew
    "PATCH: NewRouter wrapped (fallback)" | Write-Host
  }
} catch {
  $_.Exception.Message | Write-Host -ForegroundColor Yellow
  throw
}

# --- save & build ---
Set-Content $target $src -Encoding UTF8
"PATCHED: $target"
if (-not (Try-Build)) { throw "go build failed" }
"BUILD OK"
