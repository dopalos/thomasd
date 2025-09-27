param([string]$Base = 'http://127.0.0.1:8081')
$ErrorActionPreference='Stop'

$pol = Invoke-RestMethod "$Base/policy"
"policy: r2p_enabled=$($pol.r2p_enabled) alias_enabled=$($pol.alias_enabled)"

# 별칭 점검
$ra = Invoke-RestMethod "$Base/alias/resolve?name=@alice"
$rb = Invoke-RestMethod "$Base/alias/reverse?addr=tho1alice"
"alias.resolve.owner=$($ra.record.owner)  alias.reverse=$($rb.alias)"

# 1) create
$createBody = @{ from='@alice'; to='@bob'; amount_mas=1000; memo='smoke' } | ConvertTo-Json
$cr = Invoke-RestMethod -Method Post "$Base/r2p/create" -ContentType 'application/json' -Body $createBody
$id = $cr.id
"created id=$id"

# 2) get
$gr = Invoke-RestMethod "$Base/r2p/get?id=$id"
"get.status=$($gr.record.status) amount=$($gr.record.amount_mas)"

# 3) approve
$approveBody = @{ id=$id } | ConvertTo-Json
$ap = Invoke-RestMethod -Method Post "$Base/r2p/approve" -ContentType 'application/json' -Body $approveBody
"approve.ok=$($ap.ok) tx_hash=$($ap.tx_hash)"

# 4) commit & latest
$c = Invoke-RestMethod -Method Post "$Base/round/commit" -ContentType 'application/json' -Body '{}'
"commit.committed=$($c.committed)"
$ls = Invoke-RestMethod "$Base/round/latest/signed"
"round.r=$($ls.header.round) tx=$($ls.header.tx_count) sig16=$($ls.signature_hex.Substring(0,16))..."
