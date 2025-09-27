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
	if name == "" {
		return map[string]any{}, true
	}
	if strings.HasPrefix(name, "@") {
		name = strings.TrimPrefix(name, "@")
	}
	m := e.aliasFileMap()
	if m == nil {
		return map[string]any{}, true
	}
	if owner, ok := m[name]; ok && owner != "" {
		return map[string]any{"owner": owner, "version": int64(1)}, true
	}
	return map[string]any{}, true
}

// 역방향 보조 구현
func (e *Engine) ReverseAliasFile(addr string) (string, bool) {
	if addr == "" {
		return "", true
	}
	m := e.aliasFileMap()
	if m == nil {
		return "", true
	}
	for k, v := range m {
		if v == addr {
			return k, true
		}
	}
	return "", true
}
