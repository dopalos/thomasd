package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	app "thomasd/internal/app"
	rpc "thomasd/internal/rpc"
	server "thomasd/server"
	"time"
)

var releaseBuild = "dev" // ldflags로 prod로 바꿀 수 있음

func withCORS(h http.Handler) http.Handler {
	// 허용 Origin만 여기에 등록
	allowed := map[string]bool{
		"http://localhost:5174": true, // 개발
		// "https://app.example.com": true, // 운영 도메인 추가
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o := r.Header.Get("Origin")
		if o != "" && allowed[o] {
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-PubKey, X-Sig")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		} else if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// ---- Rate limit for /wallet/restore ----
type rateBucket struct {
	tokens int
	last   time.Time
}

var (
	rlMu sync.Mutex
	rl   = map[string]*rateBucket{}
)

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func withRateLimitRestore(h http.Handler, rpm int) http.Handler {
	refill := func(b *rateBucket) {
		now := time.Now()
		if b.last.IsZero() {
			b.last = now
			return
		}
		elapsed := now.Sub(b.last)
		add := int(elapsed.Minutes() * float64(rpm))
		if add > 0 {
			if b.tokens+add > rpm {
				b.tokens = rpm
			} else {
				b.tokens += add
			}
			b.last = now
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/wallet/restore") {
			ip := clientIP(r)
			rlMu.Lock()
			b := rl[ip]
			if b == nil {
				b = &rateBucket{tokens: rpm, last: time.Now()}
				rl[ip] = b
			}
			refill(b)
			if b.tokens <= 0 {
				rlMu.Unlock()
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"ok":false,"error":"rate_limited"}`))
				return
			}
			b.tokens--
			rlMu.Unlock()
		}
		h.ServeHTTP(w, r)
	})
}

// ---- main ----
func main() {
	// 🚨 운영 보호
	if os.Getenv("THOMAS_ENV") == "prod" && os.Getenv("THOMAS_DEV_FREE_R2P") == "1" {
		log.Fatal("REFUSING TO START: THOMAS_DEV_FREE_R2P=1 in production (THOMAS_ENV=prod)")
	}
	if releaseBuild == "prod" && os.Getenv("THOMAS_DEV_FREE_R2P") == "1" {
		log.Fatal("REFUSING TO START: THOMAS_DEV_FREE_R2P=1 in release build")
	}
	if os.Getenv("THOMAS_DEV_FREE_R2P") == "1" {
		log.Println("!!! DANGER: THOMAS_DEV_FREE_R2P is ENABLED (dev only). This bypasses balance checks.")
	}

	// 1) mux 생성
	mux := http.NewServeMux()

	engine := app.NewEngine()
	rpc.SetEngine(engine)

	// 2) R2P 파일 경로
	exe, _ := os.Executable()
	base := filepath.Dir(exe)
	rpc.SetR2PStorage(filepath.Join(base, "data", "r2p.json"))
	rpc.SetR2PStorage("data/r2p.json")

	// 4) 문서 라우트 ( /openapi.json , /openapi/* , /docs )
	server.RegisterDocsRoutes(mux)

	// 5) 기존 RPC 라우터만 연결
	h := rpc.Handler()
	h = withRateLimitRestore(h, 5)
	h = withCORS(h)
	mux.Handle("/", h)

	// 6) 공통 미들웨어(CORS, /wallet/restore rate limit)
	root := withCORS(withRateLimitRestore(mux, 5))

	// 7) 서버 시작
	addr := ":8081"
	log.Printf("thomasd (Thomas Chain) listening on %s", addr)
	if err := http.ListenAndServe(addr, root); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
