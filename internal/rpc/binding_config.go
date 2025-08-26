package rpc

import (
    "encoding/json"
    "io"
    "os"
    "strings"
    "sync"
)

var (
    cfgOnce sync.Once
    cfgMap  map[string]string
)

func loadBindingConfig() {
    path := strings.TrimSpace(os.Getenv("THOMAS_BIND_CONFIG"))
    cfgMap = map[string]string{}
    if path == "" { return }
    f, err := os.Open(path); if err != nil { return }
    defer f.Close()
    b, err := io.ReadAll(f); if err != nil { return }

    var generic map[string]any
    if err := json.Unmarshal(b, &generic); err == nil {
        if inner, ok := generic["pubkey_by_from"].(map[string]any); ok {
            for k, v := range inner {
                if s, ok := v.(string); ok { cfgMap[k] = strings.TrimSpace(s) }
            }
            return
        }
    }
    var m map[string]string
    if err := json.Unmarshal(b, &m); err == nil {
        for k, v := range m { cfgMap[k] = strings.TrimSpace(v) }
    }
}

func lookupPubForFrom(from string) (string, bool) {
    cfgOnce.Do(loadBindingConfig)
    if from == "" { return "", false }
    if v, ok := cfgMap[from]; ok && strings.TrimSpace(v) != "" { return strings.TrimSpace(v), true }
    if v, ok := os.LookupEnv("THOMAS_PUBKEY_" + from); ok && strings.TrimSpace(v) != "" { return strings.TrimSpace(v), true }
    return "", false
}
