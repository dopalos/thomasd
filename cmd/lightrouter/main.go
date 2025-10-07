//go:build light_client
// +build light_client

package main

import (
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"thomasd/internal/app"
)

type jsonMap map[string]any

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, jsonMap{
			"status":    "ok",
			"time_utc":  time.Now().UTC().Format(time.RFC3339),
			"node_type": "light",
			"chain_ok":  app.ChainHealthy(),
		})
	})

	mux.HandleFunc("/policy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		p := app.GetPolicy()
		writeJSON(w, http.StatusOK, jsonMap{
			"unit":               "mas",
			"allowed_chain_id":   p.AllowedChainID,
			"fee_bps":            10,
			"min_fee_mas":        1,
			"max_msg_commit_len": 64,
			"expiry_rule":        "height < expiry_height (0 = unlimited)",
		})
	})

	mux.HandleFunc("/nonce/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		addr := strings.TrimPrefix(r.URL.Path, "/nonce/")
		if addr == "" {
			writeError(w, http.StatusBadRequest, "missing_address")
			return
		}
		expected := app.ExpectedNonce(addr)
		var current uint64
		if expected > 0 {
			current = expected - 1
		}
		writeJSON(w, http.StatusOK, jsonMap{
			"address":        addr,
			"nonce":          current,
			"expected_nonce": expected,
		})
	})

	mux.HandleFunc("/tx", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		defer r.Body.Close()

		var tx app.Tx
		if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request")
			return
		}

		receipt, err := app.ApplyTx(tx)
		if err != nil {
			writeJSON(w, http.StatusOK, jsonMap{
				"ok":      false,
				"applied": false,
				"error":   err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, jsonMap{
			"ok":             true,
			"applied":        true,
			"tx_hash":        receipt.TxHash,
			"from":           receipt.From,
			"to":             receipt.To,
			"amount_mas":     receipt.Amount,
			"fee_mas":        receipt.Fee,
			"nonce":          receipt.Nonce,
			"height":         receipt.Height,
			"time_utc":       receipt.TimeUTC,
			"expected_nonce": app.ExpectedNonce(tx.From),
		})
	})

	mux.HandleFunc("/round/commit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		toHeight, err := app.RoundCommit()
		if err != nil || toHeight == 0 {
			writeJSON(w, http.StatusOK, jsonMap{
				"committed": false,
				"error":     "nothing_to_commit",
			})
			return
		}
		header, ok := app.EternityHeaderAt(toHeight)
		if !ok {
			writeError(w, http.StatusInternalServerError, "header_missing")
			return
		}
		writeJSON(w, http.StatusOK, jsonMap{
			"committed": true,
			"to_height": toHeight,
			"header": jsonMap{
				"height":   header.Height,
				"root":     hex.EncodeToString(header.Root[:]),
				"time_utc": header.TimeUTC,
			},
		})
	})

	mux.HandleFunc("/round/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		from, to := app.RoundLatest()
		if to == 0 {
			writeError(w, http.StatusNotFound, "no_round")
			return
		}
		header, ok := app.EternityHeaderAt(to)
		if !ok {
			writeError(w, http.StatusNotFound, "header_not_found")
			return
		}
		writeJSON(w, http.StatusOK, jsonMap{
			"from_height": from,
			"to_height":   to,
			"header": jsonMap{
				"height":   header.Height,
				"root":     hex.EncodeToString(header.Root[:]),
				"time_utc": header.TimeUTC,
			},
		})
	})

	mux.HandleFunc("/block/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/light-proof") {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/block/"), "/light-proof")
		height, err := strconv.ParseUint(strings.Trim(raw, "/"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_height")
			return
		}
		header, ok := app.EternityHeaderAt(height)
		if !ok {
			writeError(w, http.StatusNotFound, "header_not_found")
			return
		}
		resp := jsonMap{
			"ok":                 true,
			"height":             height,
			"header_commit_root": hex.EncodeToString(header.Root[:]),
			"bundle_found":       false,
		}
		if bundle, ok := app.CommitBundleAt(height); ok {
			resp["bundle_found"] = true
			resp["bundle_root"] = hex.EncodeToString(bundle.CommitRoot[:])
		}
		writeJSON(w, http.StatusOK, resp)
	})

	mux.HandleFunc("/receipt/get", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		hash := r.URL.Query().Get("hash")
		if hash == "" {
			writeError(w, http.StatusBadRequest, "missing_hash")
			return
		}
		if receipt, ok := app.GetReceipt(hash); ok {
			writeJSON(w, http.StatusOK, receipt)
			return
		}
		writeError(w, http.StatusNotFound, "receipt_not_found")
	})

	addr := ":8081"
	log.Printf("light router listening on http://127.0.0.1%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, jsonMap{
		"ok":    false,
		"error": code,
	})
}
