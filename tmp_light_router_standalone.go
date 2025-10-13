//go:build light_client && ignore_standalone
// +build light_client,ignore_standalone

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

func main() {
	var (
		mu       sync.Mutex
		height   uint64
		nonces   = map[string]uint64{}
		headers  = map[uint64][32]byte{}
		bundles  = map[uint64][32]byte{}
	)

	mux := http.NewServeMux()

	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200); w.Write([]byte("pong"))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		j(w, 200, map[string]any{"ok": true, "ts": time.Now().UTC().Format(time.RFC3339)})
	})
	mux.HandleFunc("/policy", func(w http.ResponseWriter, r *http.Request) {
		j(w, 200, map[string]any{"allowed_chain_id": 777})
	})
	mux.HandleFunc("/nonce/", func(w http.ResponseWriter, r *http.Request) {
		addr := strings.TrimPrefix(r.URL.Path, "/nonce/")
		mu.Lock(); n := nonces[addr]; mu.Unlock()
		j(w, 200, map[string]any{"expected_nonce": n})
	})
	mux.HandleFunc("/tx", func(w http.ResponseWriter, r *http.Request) {
		var tx map[string]any
		if err := json.NewDecoder(r.Body).Decode(&tx); err != nil { j(w,400,map[string]any{"error":"bad_json"}); return }
		raw, _ := json.Marshal(tx)
		h := sha256.Sum256(raw); hx := hex.EncodeToString(h[:])
		j(w, 200, map[string]any{"tx_hash": hx, "receipt": map[string]any{"tx_hash": hx}})
	})
	mux.HandleFunc("/round/commit", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); height++; root := sha256.Sum256([]byte(strconv.FormatUint(height,10))); headers[height]=root; bundles[height]=root; mu.Unlock()
		j(w, 200, map[string]any{"to_height": height})
	})
	mux.HandleFunc("/round/latest", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); h:=height; mu.Unlock(); j(w, 200, map[string]any{"from_height": h, "to_height": h})
	})
	mux.HandleFunc("/receipt/get", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("hash"); if q=="" { j(w,400,map[string]any{"error":"bad_hash"}); return }
		j(w,200,map[string]any{"tx_hash": q})
	})
	mux.HandleFunc("/block/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/light-proof") { http.NotFound(w,r); return }
		trim := strings.Trim(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path,"/block/"),"/light-proof"),"/")
		h, err := strconv.ParseUint(trim,10,64); if err!=nil { j(w,400,map[string]any{"error":"bad_height"}); return }
		mu.Lock(); hr, ok1 := headers[h]; br, ok2 := bundles[h]; mu.Unlock()
		if !ok1 { j(w,404,map[string]any{"error":"header_not_found"}); return }
		if !ok2 { j(w,404,map[string]any{"error":"bundle_not_found"}); return }
		okEq := bytes.Equal(hr[:], br[:])
		resp := map[string]any{"ok": okEq, "height": h, "header_commit_root": hex.EncodeToString(hr[:]), "bundle_root": hex.EncodeToString(br[:])}
		if !okEq { j(w,409,resp); return }
		j(w,200,resp)
	})

	addr := "127.0.0.1:8081"
	log.Printf("[standalone-light] http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func j(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type","application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
