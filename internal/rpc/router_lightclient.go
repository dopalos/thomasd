//go:build light_client
// +build light_client

package rpc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

/************  in-memory light state  *************/
type lcHeader struct{ Root [32]byte }
type lcBundle struct{ CommitRoot [32]byte }
type lcReceipt struct{ TxHash string `json:"tx_hash"` }

var (
	lcMu         sync.Mutex
	lcChainID    = 777
	lcHeight     uint64
	lcNonces     = map[string]uint64{}
	lcReceipts   = map[string]lcReceipt{}
	lcHeaders    = map[uint64]lcHeader{}
	lcBundles    = map[uint64]lcBundle{}
)

func lcH256(b []byte) [32]byte { sum := sha256.Sum256(b); return sum }

func lcWriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func lcWriteErr(w http.ResponseWriter, status int, key string) {
	lcWriteJSON(w, status, map[string]any{"error": key})
}

/************  router install  *************/
func NewLightMuxForTest() *http.ServeMux {
	mux := http.NewServeMux()
	installLightRoutes(mux)
	return mux
}

func installLightRoutes(mux *http.ServeMux) {
	// ping (quick check)
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200); _, _ = w.Write([]byte("pong"))
	})

	// /health
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet { lcWriteErr(w, http.StatusMethodNotAllowed, "method"); return }
		lcWriteJSON(w, http.StatusOK, map[string]any{"ok": true, "ts": time.Now().UTC().Format(time.RFC3339)})
	})

	// /policy
	mux.HandleFunc("/policy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet { lcWriteErr(w, http.StatusMethodNotAllowed, "method"); return }
		lcWriteJSON(w, http.StatusOK, map[string]any{"allowed_chain_id": lcChainID})
	})

	// /nonce/{addr}
	mux.HandleFunc("/nonce/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet { lcWriteErr(w, http.StatusMethodNotAllowed, "method"); return }
		addr := strings.TrimPrefix(r.URL.Path, "/nonce/")
		if addr == "" { lcWriteErr(w, http.StatusBadRequest, "bad_addr"); return }
		lcMu.Lock(); n := lcNonces[addr]; lcMu.Unlock()
		lcWriteJSON(w, http.StatusOK, map[string]any{"expected_nonce": n})
	})

	// POST /tx
	mux.HandleFunc("/tx", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost { lcWriteErr(w, http.StatusMethodNotAllowed, "method"); return }
		defer r.Body.Close()
		// 라이트 스텁: 들어온 JSON 전체를 해시해 tx_hash 생성
		var tx map[string]any
		if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
			lcWriteErr(w, http.StatusBadRequest, "bad_json"); return
		}
		from, _ := tx["from"].(string)
		nonceF, _ := tx["nonce"].(float64)
		chainIDF, _ := tx["chain_id"].(float64)
		if int(chainIDF) != lcChainID {
			lcWriteErr(w, http.StatusBadRequest, "bad_chain_id"); return
		}
		lcMu.Lock()
		exp := lcNonces[from]
		if uint64(nonceF) != exp { lcMu.Unlock(); lcWriteErr(w, http.StatusConflict, "bad_nonce"); return }
		lcNonces[from] = exp + 1
		raw, _ := json.Marshal(tx)
		th := lcH256(raw)
		hashHex := hex.EncodeToString(th[:])
		lcReceipts[hashHex] = lcReceipt{TxHash: hashHex}
		lcMu.Unlock()

		lcWriteJSON(w, http.StatusOK, map[string]any{"tx_hash": hashHex, "receipt": lcReceipts[hashHex]})
	})

	// POST /round/commit  → height++, header/bundle 생성
	mux.HandleFunc("/round/commit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost { lcWriteErr(w, http.StatusMethodNotAllowed, "method"); return }
		lcMu.Lock()
		lcHeight++
		root := lcH256([]byte("h:" + strconv.FormatUint(lcHeight, 10)))
		lcHeaders[lcHeight] = lcHeader{Root: root}
		lcBundles[lcHeight] = lcBundle{CommitRoot: root}
		to := lcHeight
		lcMu.Unlock()
		lcWriteJSON(w, http.StatusOK, map[string]any{"to_height": to})
	})

	// GET /round/latest
	mux.HandleFunc("/round/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet { lcWriteErr(w, http.StatusMethodNotAllowed, "method"); return }
		lcMu.Lock(); h := lcHeight; lcMu.Unlock()
		lcWriteJSON(w, http.StatusOK, map[string]any{"from_height": h, "to_height": h})
	})

	// GET /receipt/get?hash=
	mux.HandleFunc("/receipt/get", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet { lcWriteErr(w, http.StatusMethodNotAllowed, "method"); return }
		q := r.URL.Query().Get("hash")
		if q == "" { lcWriteErr(w, http.StatusBadRequest, "bad_hash"); return }
		lcMu.Lock(); rc, ok := lcReceipts[q]; lcMu.Unlock()
		if !ok { lcWriteErr(w, http.StatusNotFound, "not_found"); return }
		lcWriteJSON(w, http.StatusOK, rc)
	})

	// GET /block/{height}/light-proof  (중복 등록 금지: 라이트에서 이 한 군데만!)
	mux.HandleFunc("/block/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet { lcWriteErr(w, http.StatusMethodNotAllowed, "method"); return }
		if !strings.HasSuffix(r.URL.Path, "/light-proof") { http.NotFound(w, r); return }
		trim := strings.Trim(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/block/"), "/light-proof"), "/")
		h, err := strconv.ParseUint(trim, 10, 64)
		if err != nil { lcWriteErr(w, http.StatusBadRequest, "bad_height"); return }

		lcMu.Lock(); hdr, okH := lcHeaders[h]; bun, okB := lcBundles[h]; lcMu.Unlock()
		if !okH { lcWriteErr(w, http.StatusNotFound, "header_not_found"); return }
		if !okB { lcWriteErr(w, http.StatusNotFound, "bundle_not_found"); return }

		okEq := bytes.Equal(hdr.Root[:], bun.CommitRoot[:])
		resp := map[string]any{
			"ok": okEq, "height": h,
			"header_commit_root": hex.EncodeToString(hdr.Root[:]),
			"bundle_root":        hex.EncodeToString(bun.CommitRoot[:]),
		}
		if !okEq { lcWriteJSON(w, http.StatusConflict, resp); return }
		lcWriteJSON(w, http.StatusOK, resp)
	})
}
