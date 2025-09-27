# patch-alias-fileback.ps1
param([string]$Root = (Get-Location).Path)
$ErrorActionPreference = 'Stop'

# 1) 새 파일 추가: internal/app/engine_alias_file.go
$aliasGo = Join-Path $Root 'internal\app\engine_alias_file.go'
$dir = Split-Path $aliasGo
New-Item -ItemType Directory -Force -Path $dir | Out-Null

$goSrc = @"
package app

import (
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
)

// 파일: data/alias_map.json
// 예시: { "alice": "tho1alice", "bob": "tho1bob" }
func (e *Engine) aliasFileMap() map[string]string {
    path := filepath.Join("data", "alias_map.json")
    b, err := os.ReadFile(path)
    if err != nil || len(b) == 0 {
        return nil
    }
    var m map[string]string
    if json.Unmarshal(b, &m) != nil || len(m) == 0 {
        return nil
    }
    return m
}

// 기존 ResolveAlias 가 비어있을 경우를 위한 '보조' 구현
func (e *Engine) ResolveAliasFile(name string) (map[string]any, bool) {
    if name == "" { return map[string]any{}, true }
    if strings.HasPrefix(name, "@") { name = strings.TrimPrefix(name, "@") }
    m := e.aliasFileMap()
    if m == nil { return map[string]any{}, true }
    if owner, ok := m[name]; ok && owner != "" {
        return map[string]any{"owner": owner, "version": int64(1)}, true
    }
    return map[string]any{}, true
}

// 역방향 보조 구현
func (e *Engine) ReverseAliasFile(addr string) (string, bool) {
    if addr == "" { return "", true }
    m := e.aliasFileMap()
    if m == nil { return "", true }
    for k, v := range m {
        if v == addr { return k, true }
    }
    return "", true
}
"@
Set-Content -LiteralPath $aliasGo -Value $goSrc -Encoding UTF8
Write-Host "[ok] wrote $aliasGo"

# 2) 라우터 후보군에 보조 메서드 추가
$router = Join-Path $Root 'internal\rpc\router.go'
if (!(Test-Path $router)) { throw "router.go not found: $router" }
$bak = "$router.bak-$(Get-Date -Format 'yyyyMMddHHmmss')"
Copy-Item $router $bak -Force
Write-Host "[bak] $bak"

$s = Get-Content $router -Raw -Encoding UTF8

# callEngResolveAlias 후보군 앞에 ResolveAliasFile 추가
$s = $s -replace 'candidates := \[\]string\{("ResolveAlias"[^}]+)\}',
                'candidates := []string{"ResolveAliasFile", ${1}}'

# callEngReverseAlias 후보군 앞에 ReverseAliasFile 추가
$s = $s -replace 'candidates := \[\]string\{("ReverseAlias"[^}]+)\}',
                'candidates := []string{"ReverseAliasFile", ${1}}'

Set-Content $router -Value $s -Encoding UTF8
Write-Host "[ok] router.go candidates patched"

# 3) 빌드 & 재기동(포트 :8081 로그 확인)
Push-Location $Root
go build -o .\bin\thomasd.exe .\cmd\thomasd
Get-Process thomasd -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Process -NoNewWindow .\bin\thomasd.exe
Start-Sleep 1
Pop-Location
