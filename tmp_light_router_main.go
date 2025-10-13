//go:build light_client && ignore_standalone
// +build light_client,ignore_standalone


package main

import (
	"log"
	"net/http"

	// 모듈명이 thomasd 라고 가정
	"thomasd/internal/rpc"
)

func main() {
	// 라이트 전용 mux 생성 (router_lightclient.go에서 제공)
	mux := rpc.NewLightMuxForTest()

	addr := "127.0.0.1:8081"
	log.Printf("[light-router] listening on http://%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
