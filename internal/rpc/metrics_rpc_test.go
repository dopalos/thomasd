package rpc

import (
    "expvar"
    "net/http"
    "net/http/httptest"
    "testing"
)

func expInt(t *testing.T, name string) int64 {
    t.Helper()
    if v := expvar.Get(name); v != nil {
        // *expvar.Int has Value()
        if iv, ok := v.(*expvar.Int); ok { return iv.Value() }
    }
    return -1
}

func TestMetrics_BindingMismatch_Increments(t *testing.T) {
    t.Setenv("THOMAS_PUBKEY_tho1alice", "AAAA")
    before := expInt(t, "rpc_bind_mismatch_total")

    h := precheckFromBinding(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        t.Fatalf("next should not run on mismatch")
    }))
    r := req(`{"from":"tho1alice"}`, "BBBB")
    w := httptest.NewRecorder()
    h.ServeHTTP(w, r)

    after := expInt(t, "rpc_bind_mismatch_total")
    if !(after == before+1) {
        t.Fatalf("bind mismatch metric not incremented: before=%d after=%d", before, after)
    }
}

func TestMetrics_BadSig_Increments_On200Reason(t *testing.T) {
    before := expInt(t, "rpc_bad_signature_total")

    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Upstream already emits 200 with reason (no 400)
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"ok":true,"applied":false,"reason":"verify:bad_signature"}`))
    })
    h := jsonizeSigErrors(next)
    w := httptest.NewRecorder()
    h.ServeHTTP(w, req(`{}`, "PUB"))

    after := expInt(t, "rpc_bad_signature_total")
    if !(after == before+1) {
        t.Fatalf("bad-signature metric not incremented on 200-reason path: before=%d after=%d", before, after)
    }
}


