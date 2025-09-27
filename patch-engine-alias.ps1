# patch-engine-alias.ps1
param([string]$RootPath = (Get-Location).Path)
$ErrorActionPreference = 'Stop'

$File = Join-Path $RootPath 'internal\app\engine.go'
if (!(Test-Path $File)) { throw "engine.go not found: $File" }

# 백업
$bak = "$File.bak-$(Get-Date -Format 'yyyyMMddHHmmss')"
Copy-Item -LiteralPath $File -Destination $bak -Force
Write-Host "[bak] $bak"

# 원본 로드
[string]$s = Get-Content -LiteralPath $File -Raw -Encoding UTF8

# import 블록에 "strings" 추가
$importRx = [regex]'import\s*\(\s*([\s\S]*?)\s*\)'
$m = $importRx.Match($s)
if (!$m.Success) { throw "import block not found in $File" }
$inner = $m.Groups[1].Value
if ($inner -notmatch [regex]::Escape('"strings"')) {
  $inner = ($inner.TrimEnd() + "`n`t""strings""`n")
}
$newImport = "import (`n$inner`n)"
$s = $s.Substring(0,$m.Index) + $newImport + $s.Substring($m.Index + $m.Length)

# 이미 구현돼 있으면 중복 추가 방지
if ($s -notmatch 'func\s+\(e \*Engine\)\s+ResolveAlias\(') {
$snippet = @'
/* === Minimal alias resolver (file-backed) ==================================
   - 파일: data/alias_map.json (선택)
     예시: { "alice": "tho1alice", "bob": "tho1bob" }
   - 파일이 없으면 기본 매핑: alice->tho1alice, bob->tho1bob
   - Router의 reflective 호출과 호환:
     ResolveAlias(name) (map[string]any, bool)
     ReverseAlias(addr) (string, bool)
=============================================================================*/

func (e *Engine) aliasMap() map[string]string {
    path := filepath.Join("data", "alias_map.json")
    if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
        var m map[string]string
        if json.Unmarshal(b, &m) == nil && len(m) > 0 {
            return m
        }
    }
    return map[string]string{
        "alice": "tho1alice",
        "bob":   "tho1bob",
    }
}

func (e *Engine) ResolveAlias(name string) (map[string]any, bool) {
    if name == "" { return map[string]any{}, false }
    if strings.HasPrefix(name, "@") { name = strings.TrimPrefix(name, "@") }
    m := e.aliasMap()
    if owner, ok := m[name]; ok && owner != "" {
        return map[string]any{ "owner": owner, "version": int64(1) }, true
    }
    return map[string]any{}, true
}

func (e *Engine) ReverseAlias(addr string) (string, bool) {
    if addr == "" { return "", false }
    m := e.aliasMap()
    for k, v := range m {
        if v == addr { return k, true }
    }
    return "", true
}
'@
  $s = $s.TrimEnd() + "`n`n" + $snippet + "`n"
}

Set-Content -LiteralPath $File -Value $s -Encoding UTF8
Write-Host "[ok] Alias resolver patch applied -> $File"
