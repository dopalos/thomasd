package main

import (
"errors"
    "log"
    "net"
    "net/http"
    "os"
    "time"

    "thomasd/internal/app"
    "thomasd/internal/rpc"
)

const buildTag = "debug-sticky"

func main() {
    // 로그를 stdout으로
    log.SetOutput(os.Stdout)

    // 엔진 준비
    eng := app.NewEngine()

    // 1) 빈 포트 자동 배정 (127.0.0.1:0)
    ln, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        log.Printf("[STARTUP] listen error: %v", err)
        // 어떤 에러든 창이 닫히지 않게 대기
        for { time.Sleep(10 * time.Second) }
    }

    addr := ln.Addr().String()
    srv := &http.Server{
        Handler: rpc.NewRouter(eng),
    }

    log.Printf("[OK] thomasd %s listening on http://%s", buildTag, addr)

    // 2) 여기서 블록됨. 실패하면 에러 찍고 절대 종료하지 않음
    if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
        log.Printf("[RUNTIME] serve error: %v", err)
    }

    // 3) 어떤 경우든 창이 닫히지 않게 영구 대기
    for { time.Sleep(10 * time.Second) }
}

