package rpc

import "net/http"

// Eternity + Round 엔드포인트를 한 데 모은 http.Handler 반환
func NewEternityHTTPHandler(p EternityProvider) http.Handler {
	mux := http.NewServeMux()
	// 이미 만들었던 등록 함수 두 개 재사용
	RegisterRoundEndpoints(mux, p)    // /round/*
	RegisterEternityEndpoints(mux, p) // /eternity/round/*
	return mux
}
