//go:build light_client
// +build light_client

package main

import (
  "log"
  "net/http"
  "thomasd/internal/rpc"
)

func main() {
  mux := rpc.NewLightMuxForTest()
  addr := "127.0.0.1:8081"
  log.Printf("[light-router] listening on http://%s", addr)
  log.Fatal(http.ListenAndServe(addr, mux))
}
