# ===== CONFIG =====
$Owner  = "dopalos"
$Repo   = "thomasd"
$Branch = "main"

$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

# ===== Build strict protection JSON =====
# NOTE: GitHub is yelling that "restrictions" must be supplied.
# For a personal repo (not org), we MUST send "restrictions": null.
$payload = @'
{
  "required_status_checks": {
    "strict": true,
    "checks": [
      { "context": "smoke" }
    ]
  },
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "dismissal_restrictions": {},
    "dismiss_stale_reviews": false,
    "require_code_owner_reviews": false,
    "required_approving_review_count": 1,
    "require_last_push_approval": false
  },
  "restrictions": null
}
'@

# write JSON to temp file (UTF-8 no BOM, gh --input wants a file)
$tmpFile = Join-Path $env:TEMP "branch_protect_main.json"
[System.IO.File]::WriteAllText(
    $tmpFile,
    $payload,
    (New-Object System.Text.UTF8Encoding($false))
)

Write-Host ">> payload written to $tmpFile"
Get-Content $tmpFile

# ===== PUT call (create/replace protection on main) =====
$respRaw = gh api `
  -X PUT "repos/$Owner/$Repo/branches/$Branch/protection" `
  -H "Accept: application/vnd.github+json" `
  --input $tmpFile 2>&1

Write-Host ""
Write-Host "===== RAW RESPONSE FROM GITHUB ====="
$respRaw | Write-Output

# Try to parse if it's valid JSON
$respObj = $null
try {
    $respObj = $respRaw | ConvertFrom-Json
} catch {
    Write-Host "[WARN] Response was not valid JSON (probably error text)."
}

# ===== sanity check / pretty summary =====
# We'll GET the branch protection again. This will throw if protection not applied.
$protJson = gh api "repos/$Owner/$Repo/branches/$Branch/protection" `
  -H "Accept: application/vnd.github+json" 2>&1

Write-Host ""
Write-Host "===== CURRENT PROTECTION RAW (GET) ====="
$protJson | Write-Output

$protObj = $null
try {
    $protObj = $protJson | ConvertFrom-Json
} catch {
    Write-Host "[WARN] Could not ConvertFrom-Json on GET response."
}

if ($protObj) {
    # Safely pull fields (they might be null/missing)
    $admins_enforced    = $protObj.enforce_admins.enabled
    $strict             = $protObj.required_status_checks.strict
    $check_contexts     = $protObj.required_status_checks.checks | ForEach-Object { $_.context }
    if (-not $check_contexts) { $check_contexts = @() }

    $min_reviews        = $protObj.required_pull_request_reviews.required_approving_review_count
    $code_owner_req     = $protObj.required_pull_request_reviews.require_code_owner_reviews
    $dismiss_stale      = $protObj.required_pull_request_reviews.dismiss_stale_reviews
    $last_push_approval = $protObj.required_pull_request_reviews.require_last_push_approval

    Write-Host ""
    Write-Host "===== SUMMARY ====="
    [pscustomobject]@{
        admins_enforced    = $admins_enforced
        strict             = $strict
        required_checks    = ($check_contexts -join ", ")
        min_reviews        = $min_reviews
        code_owner_req     = $code_owner_req
        dismiss_stale      = $dismiss_stale
        last_push_approval = $last_push_approval
    } | Format-List
} else {
    Write-Host ""
    Write-Host "===== SUMMARY ====="
    Write-Host "(No parsed protection object - branch protection may still be partial or failed.)"
}
