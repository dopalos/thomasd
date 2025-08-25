package rpc

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"
)

func req(body string, pub string) *http.Request {
    r := httptest.NewRequest(http.MethodPost, "/tx", bytes.NewBufferString(body))
    if pub != "" {
        r.Header.Set("X-PubKey", pub)
    }
    return r
}

func TestPrecheckFromBinding_BlocksMismatch(t *testing.T) {
    t.Setenv("THOMAS_PUBKEY_tho1alice", "AAAA")
    h := precheckFromBinding(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        t.Fatalf("next should not be called on mismatch")
    }))
    w := httptest.NewRecorder()
    h.ServeHTTP(w, req(`{"from":"tho1alice"}`, "BBBB"))

    if w.Code != http.StatusOK {
        t.Fatalf("code=%d", w.Code)
    }
    var got map[string]any
    _ = json.Unmarshal(w.Body.Bytes(), &got)
    if got["ok"] != true || got["applied"] != false || got["reason"] != "verify:from_pub_mismatch" {
        t.Fatalf("unexpected body: %s", w.Body.String())
    }
}

func TestPrecheckFromBinding_PassesWhenMatchOrUnset(t *testing.T) {
    t.Setenv("THOMAS_PUBKEY_tho1alice", "PUB")
    called := false
    h := precheckFromBinding(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        called = true
        w.WriteHeader(http.StatusOK)
        w.Write([]byte(`{"ok":true,"applied":true}`))
    }))
    w := httptest.NewRecorder()
    h.ServeHTTP(w, req(`{"from":"tho1alice"}`, "PUB"))
    if !called {
        t.Fatal("next not called on match")
    }

    // ENV unset → 통과
    t.Setenv("THOMAS_PUBKEY_tho1alice", "")
    called = false
    w = httptest.NewRecorder()
    h.ServeHTTP(w, req(`{"from":"tho1alice"}`, "ANY"))
    if !called {
        t.Fatal("next not called when ENV unset")
    }
}

func TestJsonizeSigErrors_Rewrites400(t *testing.T) {
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        http.Error(w, "bad sig", http.StatusBadRequest)
    })
    h := jsonizeSigErrors(next)

    w := httptest.NewRecorder()
    h.ServeHTTP(w, req(`{}`, "PUB"))

    if w.Code != http.StatusOK {
        t.Fatalf("code=%d", w.Code)
    }
    var got map[string]any
    _ = json.Unmarshal(w.Body.Bytes(), &got)
    if got["ok"] != true || got["applied"] != false || got["reason"] != "verify:bad_signature" {
        t.Fatalf("unexpected body: %s", w.Body.String())
    }
}
