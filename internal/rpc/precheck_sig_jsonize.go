package rpc

import (
    "net/http"
)

type sigCapWriter struct {
    http.ResponseWriter
    forced bool
}

func (w *sigCapWriter) WriteHeader(code int) {
    // precheckSig가 400으로 쓰려 하면 200 JSON으로 강제 변환
    if code == http.StatusBadRequest && !w.forced {
        w.Header().Set("Content-Type", "application/json")
        w.ResponseWriter.WriteHeader(http.StatusOK)
        w.forced = true
        _, _ = w.ResponseWriter.Write([]byte(`{"ok":true,"applied":false,"reason":"verify:bad_signature"}`))
        return
    }
    w.ResponseWriter.WriteHeader(code)
}

func (w *sigCapWriter) Write(b []byte) (int, error) {
    // 이미 강제 응답 썼다면 이후 바디 쓰기는 무시
    if w.forced {
        return len(b), nil
    }
    return w.ResponseWriter.Write(b)
}

// 체인 외곽에서 400을 200/JSON으로 바꿔주는 래퍼
func jsonizeSigErrors(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        cw := &sigCapWriter{ResponseWriter: w}
        next.ServeHTTP(cw, r)
    })
}
