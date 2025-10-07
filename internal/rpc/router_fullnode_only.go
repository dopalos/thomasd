//go:build !light_client

package rpc

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"thomasd/internal/app"
)

func installFullnodeOnlyRoutes(mux *http.ServeMux, eng *app.Engine) {
	mux.HandleFunc("/health/chain", func(w http.ResponseWriter, r *http.Request) {
		h := eng.CurrentHeight()
		receiptCount := uint64(eng.ReceiptCount())
		ok := h == receiptCount
		status := "ok"
		reason := ""
		if !ok {
			status = "degraded"
			reason = "height_mismatch"
		}
		resp := map[string]any{
			"status":         status,
			"ok":             ok,
			"reason":         reason,
			"height":         h,
			"receipts_count": receiptCount,
			"time_utc":       time.Now().UTC().Format(time.RFC3339),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/supply/current", func(w http.ResponseWriter, r *http.Request) {
		aF := eng.GetAccount("tho1foundation")
		aX := eng.GetAccount("tho1exchange")
		aA := eng.GetAccount("tho1alice")
		aB := eng.GetAccount("tho1bob")
		totalMas := aA.Balance + aB.Balance + aF.Balance + aX.Balance
		format := func(m uint64) map[string]any {
			return map[string]any{"mas_total": m}
		}
		network := totalMas - aF.Balance - aX.Balance
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"unit":       "mas",
			"foundation": format(aF.Balance),
			"exchange":   format(aX.Balance),
			"network":    format(network),
			"total":      format(totalMas),
		})
	})

	mux.HandleFunc("/supply/at/", func(w http.ResponseWriter, r *http.Request) {
		heightStr := strings.TrimPrefix(r.URL.Path, "/supply/at/")
		if _, err := strconv.ParseUint(heightStr, 10, 64); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_height"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_supported"})
	})
}
