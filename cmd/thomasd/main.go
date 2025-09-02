package main

import (
    "fmt"
    "log"
    "net/http"
    "time"
)

func main() {
    mux := http.NewServeMux()
    mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        now := time.Now().UTC().Format(time.RFC3339)
        fmt.Fprintf(w, `{"status":"ok","time_utc":"%s"}`, now)
    })
    srv := &http.Server{Addr: ":8081", Handler: mux}
    log.Println("thomasd (Thomas Chain) listening on :8081")
    log.Fatal(srv.ListenAndServe())
}
