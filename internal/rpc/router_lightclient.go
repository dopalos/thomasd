//go:build light_client

package rpc

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"thomasd/internal/app"
)

type lightJSON map[string]any

func NewRouter(eng *app.Engine) http.Handler {
	mux := http.NewServeMux()
	setLightClientEngine(eng)

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, lightJSON{
			"status":   "ok",
			"time_utc": time.Now().UTC().Format(time.RFC3339),
			"node":     "light",
		})
	})

	mux.HandleFunc("/policy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		p := app.GetPolicy()
		writeJSON(w, http.StatusOK, lightJSON{
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
		current := uint64(0)
		if expected > 0 {
			current = expected - 1
		}
		writeJSON(w, http.StatusOK, lightJSON{
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
			writeJSON(w, http.StatusOK, lightJSON{
				"ok":      false,
				"applied": false,
				"error":   err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, lightJSON{
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
			writeJSON(w, http.StatusOK, lightJSON{
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
		writeJSON(w, http.StatusOK, lightJSON{
			"committed": true,
			"to_height": toHeight,
			"header": lightJSON{
				"height":   header.Height,
				"root":     hex.EncodeToString(header.CommitRoot[:]),
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
		writeJSON(w, http.StatusOK, lightJSON{
			"from_height": from,
			"to_height":   to,
			"header": lightJSON{
				"height":   header.Height,
				"root":     hex.EncodeToString(header.CommitRoot[:]),
				"time_utc": header.TimeUTC,
			},
		})
	})

	mux.HandleFunc("/block/", lightProofHandler)

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

	return mux
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, lightJSON{
		"ok":    false,
		"error": code,
	})
}
