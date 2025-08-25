package rpc

import (
    "bytes"
    "encoding/json"
    "io"
    "log"
    "net/http"
    "os"
    "strings"
)

type txMinimal struct {
    From string `json:"from"`
}

// X-PubKey ↔ body.from 바인딩 검사
func precheckFromBinding(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        log.Printf("[bindchk] start method=%s path=%s", r.Method, r.URL.Path)

        pubB64 := strings.TrimSpace(r.Header.Get("X-PubKey"))
        log.Printf("[bindchk] X-PubKey=%q", pubB64)
        if pubB64 == "" {
            next.ServeHTTP(w, r)
            return
        }

        // body 백업/복구
        var body []byte
        if r.Body != nil {
            b, _ := io.ReadAll(r.Body)
            body = b
            r.Body = io.NopCloser(bytes.NewReader(b))
        }

        var tx txMinimal
        if len(body) > 0 {
            _ = json.Unmarshal(body, &tx)
        }
        log.Printf("[bindchk] body.from=%q", tx.From)
        if tx.From == "" {
            next.ServeHTTP(w, r)
            return
        }

        envKey := "THOMAS_PUBKEY_" + tx.From
        if want, ok := os.LookupEnv(envKey); ok {
            want = strings.TrimSpace(want)
            if want == "" {
                log.Printf("[bindchk] want empty for from=%s (envKey=%s) → pass", tx.From, envKey)
                next.ServeHTTP(w, r)
                return
            }
            if want != pubB64 {
                log.Printf("[bindchk] mismatch from=%s envKey=%s want=%q pub=%q", tx.From, envKey, want, pubB64)
                w.Header().Set("Content-Type", "application/json")
                w.WriteHeader(http.StatusOK)
                io.WriteString(w, `{"ok":true,"applied":false,"reason":"verify:from_pub_mismatch"}`)
                return
            }
        } else {
            log.Printf("[bindchk] no env for from=%s (envKey=%s) → pass", tx.From, envKey)
        }

        next.ServeHTTP(w, r)
    })
}

