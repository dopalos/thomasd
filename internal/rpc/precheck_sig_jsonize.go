package rpc

import (
    "bytes"
    "encoding/json"
    "io"
    "net/http"
    "strings"
)

// captureWriter buffers downstream response so we can inspect/normalize errors once.
type captureWriter struct {
    http.ResponseWriter
    status int
    buf    bytes.Buffer
}

func (w *captureWriter) WriteHeader(code int) {
    // defer writing to the real writer; just record the status
    if w.status == 0 {
        w.status = code
    }
}

func (w *captureWriter) Write(b []byte) (int, error) {
    return w.buf.Write(b)
}

// jsonizeSigErrors wraps next and ensures a normalized JSON error shape:
//
// {"ok":false,"applied":false,"error":{"code":"...", "message":"...", "meta":{...}}}
//
// Rules:
// 1) If downstream succeeded (status < 400), just pass-through.
// 2) If downstream already wrote a normalized JSON (has "error" object), pass-through.
// 3) If downstream wrote legacy JSON (e.g. {"ok":false,"reason":"...","expected_message":"..."}), upgrade to normalized.
// 4) If downstream wrote plain text, wrap into normalized with code "bad_request".
func jsonizeSigErrors(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        cw := &captureWriter{ResponseWriter: w, status: 0}
        next.ServeHTTP(cw, r)

        status := cw.status
        if status == 0 {
            status = http.StatusOK
        }

        // Success path → flush as-is.
        if status < 400 {
            for k, vv := range cw.Header() {
                for _, v := range vv {
                    w.Header().Add(k, v)
                }
            }
            w.WriteHeader(status)
            if cw.buf.Len() > 0 {
                _, _ = io.Copy(w, &cw.buf)
            }
            return
        }

        body := bytes.TrimSpace(cw.buf.Bytes())

        // If body is already a normalized JSON that has "error" object → pass-through.
        var m map[string]any
        if len(body) > 0 && body[0] == '{' && json.Unmarshal(body, &m) == nil {
            if _, ok := m["error"]; ok {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
                w.WriteHeader(status)
                _, _ = w.Write(body)
                return
            }
            // Legacy form → upgrade.
            if reason, ok := m["reason"].(string); ok {
                normErr := map[string]any{
                    "code": reason,
                }
                // carry common metadata keys if present
                meta := map[string]any{}
                if em, ok := m["expected_message"].(string); ok {
                    meta["expected_message"] = em
                }
                if ec, ok := m["expected_chain_id"].(string); ok {
                    meta["expected_chain_id"] = ec
                }
                if en, ok := m["expected_nonce"].(float64); ok {
                    // json numbers decode to float64
                    meta["expected_nonce"] = en
                }
                if ef, ok := m["expected_fee_mas"].(float64); ok {
                    meta["expected_fee_mas"] = ef
                }
                if len(meta) > 0 {
                    normErr["meta"] = meta
                }
                norm := map[string]any{
                    "ok":      false,
                    "applied": false,
                    "error":   normErr,
                }
                out, _ := json.Marshal(norm)
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
                w.WriteHeader(status)
                _, _ = w.Write(out)
                return
            }
            // Some other JSON → wrap with generic code, keep message as string.
            // (Avoid double JSON-in-string)
            raw := string(body)
            norm := map[string]any{
                "ok":      false,
                "applied": false,
                "error": map[string]any{
                    "code":    "bad_request",
                    "message": strings.TrimSpace(raw),
                },
            }
            out, _ := json.Marshal(norm)
            w.Header().Set("Content-Type", "application/json; charset=utf-8")
            w.WriteHeader(status)
            _, _ = w.Write(out)
            return
        }

        // Plain text → generic wrap.
        msg := strings.TrimSpace(string(body))
        norm := map[string]any{
            "ok":      false,
            "applied": false,
            "error": map[string]any{
                "code":    "bad_request",
                "message": msg,
            },
        }
        out, _ := json.Marshal(norm)
        w.Header().Set("Content-Type", "application/json; charset=utf-8")
        w.WriteHeader(status)
        _, _ = w.Write(out)
    })
}
