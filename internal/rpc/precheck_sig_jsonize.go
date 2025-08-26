package rpc

import (
    "bytes"
    "net/http"
)

type captureRW struct {
    http.ResponseWriter
    status      int
    hdr         http.Header
    buf         bytes.Buffer
    wroteHeader bool
}

func (c *captureRW) Header() http.Header {
    if c.hdr == nil { c.hdr = make(http.Header) }
    return c.hdr
}
func (c *captureRW) WriteHeader(code int) {
    if c.wroteHeader { return }
    c.status = code; c.wroteHeader = true
}
func (c *captureRW) Write(p []byte) (int, error) {
    if !c.wroteHeader { c.WriteHeader(http.StatusOK) }
    return c.buf.Write(p)
}

// Convert 400 from signature precheck into 200 JSON body.
// Also count cases where upstream already returned 200 with reason=verify:bad_signature.
func jsonizeSigErrors(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        crw := &captureRW{ResponseWriter: w}
        next.ServeHTTP(crw, r)

        if crw.status == http.StatusBadRequest {
            incBadSigJSON()
            w.Header().Set("Content-Type","application/json")
            w.WriteHeader(http.StatusOK)
            _,_ = w.Write([]byte(`{"ok":true,"applied":false,"reason":"verify:bad_signature"}`))
            return
        }

        // Already-200 path: if body clearly indicates bad signature → bump metric
        lb := bytes.ToLower(crw.buf.Bytes())
        if bytes.Contains(lb, []byte(`"reason":"verify:bad_signature"`)) {
            incBadSigJSON()
        }

        for k, vv := range crw.Header() { for _, v := range vv { w.Header().Add(k, v) } }
        if crw.wroteHeader { w.WriteHeader(crw.status) }
        _,_ = w.Write(crw.buf.Bytes())
    })
}
