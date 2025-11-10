package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/dopalos/thomasd/eternity/api"
	"github.com/dopalos/thomasd/eternity/engine"
)

func main() {
	addrFlag := flag.String("addr", ":8080", "listen address")
	metricsMiniPath := flag.String("metrics-mini", "/metrics-mini", "mini metrics path")
	promPath := flag.String("metrics", "/metrics", "prometheus metrics path")
	healthzPath := flag.String("healthz", "/healthz", "liveness path")
	readyzPath := flag.String("readyz", "/readyz", "readiness path")
	roundsEnv := os.Getenv("ETERNITY_ROUNDS")
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags)

	// in-memory metrics (엔진에서 카운트 올릴 때 사용)
	mm := &engine.MemMetrics{C: map[string]int64{}}

	// HTTP 서버 생성(동일 mux 사용)
	srv := api.NewHTTPServer(*addrFlag, api.NewDummyProofGen())

	// 같은 mux에 보조 핸들러들 마운트
	mux, ok := srv.Handler.(*http.ServeMux)
	if !ok {
		logger.Println("[eternityd] unexpected handler type; cannot mount extra endpoints")
	}

	// /metrics-mini: 단순 텍스트 카운터
	if ok {
		mux.HandleFunc(*metricsMiniPath, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			// 키 순서는 중요치 않음(데모)
			for k, v := range mm.C {
				_, _ = w.Write([]byte(k + " " + strconv.FormatInt(v, 10) + "\n"))
			}
		})
	}

	// /metrics: Prometheus 텍스트 노출 (간단 Counter만)
	if ok {
		mux.HandleFunc(*promPath, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			// HELP/TYPE은 선택 사항이지만 넣어둠
			_, _ = w.Write([]byte("# HELP proposal_total total proposals\n# TYPE proposal_total counter\n"))
			_, _ = w.Write([]byte("proposal_total " + strconv.FormatInt(mm.C["proposal_total"], 10) + "\n"))
			_, _ = w.Write([]byte("# HELP prevote_total total prevotes\n# TYPE prevote_total counter\n"))
			_, _ = w.Write([]byte("prevote_total " + strconv.FormatInt(mm.C["prevote_total"], 10) + "\n"))
			_, _ = w.Write([]byte("# HELP precommit_total total precommits\n# TYPE precommit_total counter\n"))
			_, _ = w.Write([]byte("precommit_total " + strconv.FormatInt(mm.C["precommit_total"], 10) + "\n"))
			_, _ = w.Write([]byte("# HELP commit_total total commits\n# TYPE commit_total counter\n"))
			_, _ = w.Write([]byte("commit_total " + strconv.FormatInt(mm.C["commit_total"], 10) + "\n"))
		})
	}

	// /healthz: 항상 200
	if ok {
		mux.HandleFunc(*healthzPath, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("ok"))
		})
	}

	// /readyz: 부팅 즉시 200 (원하면 실제 준비 조건으로 교체)
	ready := true
	if ok {
		mux.HandleFunc(*readyzPath, func(w http.ResponseWriter, r *http.Request) {
			if !ready {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("ready"))
		})
	}

	// 합의 루프 가동 (5s 주기 데모)
	eng := &engine.Engine{
		Logger:  logger,
		Metrics: mm,
	}
	go eng.StartConsensusLoop()

	// HTTP 서버 시작
	go func() {
		logger.Println("[eternityd] http listen", *addrFlag)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Println("http error:", err)
		}
	}()

	// 그레이스풀 셧다운 (CTRL+C / SIGTERM)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	// 데모 모드: ETERNITY_ROUNDS 설정 시 일정 라운드만 돌고 종료
	if roundsEnv != "" {
		// 간단히 타이머로 유사 구현: 5초 한 라운드 × N
		if n, err := strconv.Atoi(roundsEnv); err == nil && n > 0 {
			time.AfterFunc(time.Duration(n)*5*time.Second+1500*time.Millisecond, func() { // 약간의 여유
				quit <- os.Interrupt
			})
		}
	}
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	logger.Println("[eternityd] shutdown complete")
}
