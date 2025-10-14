package rpc

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"thomasd/internal/types"
)

type HeaderProvider interface {
	LatestSignedHeader() (types.SignedHeader, error)
	SignedHeaderByHeight(h uint64) (types.SignedHeader, error)
}

func RegisterRoundEndpoints(mux *http.ServeMux, p HeaderProvider) {
	mux.HandleFunc("/round/latest/signed", func(w http.ResponseWriter, r *http.Request) {
		sh, err := p.LatestSignedHeader()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sh)
	})

	// /round/{height}/signed
	mux.HandleFunc("/round/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/round/")
		if !strings.HasSuffix(path, "/signed") {
			http.NotFound(w, r)
			return
		}
		hStr := strings.TrimSuffix(path, "/signed")
		hStr = strings.Trim(hStr, "/")
		h, err := strconv.ParseUint(hStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid height", http.StatusBadRequest)
			return
		}
		sh, err := p.SignedHeaderByHeight(h)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sh)
	})
}
