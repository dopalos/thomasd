// cmd/thomasd/main.go
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"
)

// NOTE: GitHub Actions에서 아래 변수에 ldflags로 주입합니다.
// go build -ldflags "-s -w -X main.version=${TAG}"
var version = "dev"

var (
	showVersion bool
	addr        string
	healthPath  string
)

func init() {
	// Go의 flag 패키지는 -flag 와 --flag 둘 다 인식합니다.
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.BoolVar(&showVersion, "v", false, "print version and exit (shorthand)")
	flag.StringVar(&addr, "addr", ":8081", "listen address (e.g. :8081 or 127.0.0.1:8081)")
	flag.StringVar(&healthPath, "health-path", "/health", "health check HTTP path")
}

func main() {
	flag.Parse()

	if showVersion {
		fmt.Println(version)
		return
	}

	mux := http.NewServeMux()

	// /health 응답: {"status":"ok","version":"vX.Y.Z","time":"..."}
	mux.HandleFunc(healthPath, func(w http.ResponseWriter, r *http.Request) {
		type Health struct {
			Status  string `json:"status"`
			Version string `json:"version"`
			Time    string `json:"time"`
		}
		resp := Health{
			Status:  "ok",
			Version: version,
			Time:    time.Now().UTC().Format(time.RFC3339),
		}
		writeJSON(w, http.StatusOK, resp)
	})

	// 루트 페이지(선택)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		type Info struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Health  string `json:"health"`
		}
		writeJSON(w, http.StatusOK, Info{
			Name:    "thomasd (Thomas Chain)",
			Version: version,
			Health:  healthPath,
		})
	})

	srv := &http.Server{
		Addr:         addr,
		Handler:      loggingMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 서버 시작
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// 기존 로그 스타일 유지 (예: "2025/09/02 20:34:34 thomasd (Thomas Chain) listening on :8081")
	log.Printf("thomasd (Thomas Chain) listening on %s", addr)

	// SIGINT/SIGTERM 그레이스풀 셧다운
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Printf("shutting down ...")
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
		_ = srv.Close()
	}
	log.Printf("bye")
}

// ------------ helpers ------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		start := time.Now()

		remote := r.RemoteAddr
		if host, _, err := net.SplitHostPort(remote); err == nil {
			remote = host
		}

		next.ServeHTTP(sw, r)

		dur := time.Since(start)
		log.Printf("%s %s %d %dB %s from %s",
			r.Method, r.URL.Path, sw.status, sw.bytes, dur.Truncate(time.Millisecond), remote)
	})
}
