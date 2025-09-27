package rpc

import (
	"net/http"
)

func init() {
	if mux != nil {
		mux.HandleFunc("/diag/router-alive", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("router ok"))
		})
	}
}
