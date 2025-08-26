package rpc

import (
    "bytes"
    "encoding/json"
    "io"
    "net/http"
    "strings"
)

type txFromOnly struct {
    From string `json:"from"`
}

func precheckFromBinding(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost || r.URL.Path != "/tx" {
            next.ServeHTTP(w, r); return
        }
        pub := strings.TrimSpace(r.Header.Get("X-PubKey"))
        if pub == "" { next.ServeHTTP(w, r); return }

        var body []byte
        if r.Body != nil {
            b,_ := io.ReadAll(r.Body); body=b; r.Body = io.NopCloser(bytes.NewReader(b))
        }
        if len(body) > 0 {
            var tx txFromOnly
            _ = json.Unmarshal(body, &tx)
            if tx.From != "" {
                if want, ok := lookupPubForFrom(tx.From); ok && strings.TrimSpace(want) != pub {
                    incBindMismatch()
                    w.Header().Set("Content-Type","application/json")
                    w.WriteHeader(http.StatusOK)
                    _,_ = w.Write([]byte(`{"ok":true,"applied":false,"reason":"verify:from_pub_mismatch"}`))
                    return
                }
            }
        }
        next.ServeHTTP(w, r)
    })
}
