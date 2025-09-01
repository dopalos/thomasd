package rpc

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"thomasd/internal/app"
	"thomasd/internal/codec"
	"thomasd/internal/tx"
)

const (
	feeBPS          = 10 // 0.1%
	allowedChainID  = "thomas-dev-1"
	masPerTHO       = 10_000_000
	masPerMicro     = 10
	maxMsgCommitLen = 64
)

func NewRouter(eng *app.Engine) http.Handler {
	mux := http.NewServeMux()

	// Debug: echo request body
	mux.HandleFunc("/debug/echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(b)
	}) // health
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok", "time_utc": time.Now().UTC().Format(time.RFC3339),
		})
	})

	// SSE
	mux.HandleFunc("/events/stream", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		fl, ok := w.(http.Flusher)
		if !ok {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch := eng.SubscribeForSSE()
		defer eng.UnsubscribeForSSE(ch)

		notify := r.Context().Done()
		for {
			select {
			case <-notify:
				return
			case msg := <-ch:
				w.Write([]byte("event: push\n"))
				w.Write([]byte("data: "))
				w.Write(msg)
				w.Write([]byte("\n\n"))
				fl.Flush()
			}
		}
	})

	// node info
	mux.HandleFunc("/node/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"algo":       "ed25519",
			"pubkey_hex": eng.PubKeyHex(),
		})
	})

	// height
	mux.HandleFunc("/height", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		h := eng.CurrentHeight()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"height": h})
	})

	// policy
	mux.HandleFunc("/policy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"unit":               "mas",
			"fee_bps":            feeBPS,
			"min_fee_mas":        1,
			"max_msg_commit_len": maxMsgCommitLen,
			"allowed_chain_id":   allowedChainID,
			"expiry_rule":        "valid if expiry_height==0 OR current_height < expiry_height",
			"signing": map[string]any{
				"algo":             "ed25519",
				"pubkey_hex":       eng.PubKeyHex(),
				"header_canonical": []string{"round", "from_height", "to_height", "tx_count", "root", "time_utc"},
			},
		})
	})

	// account
	mux.HandleFunc("/account/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		addr := strings.TrimPrefix(r.URL.Path, "/account/")
		a := eng.GetAccount(addr)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"balance_mas": a.Balance,
			"balance":     a.Balance / masPerMicro,
			"nonce":       a.Nonce,
			"unit":        "mas",
		})
	})

	// nonce
	mux.HandleFunc("/nonce/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		addr := strings.TrimPrefix(r.URL.Path, "/nonce/")
		acc := eng.GetAccount(addr)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"nonce": acc.Nonce, "expected_nonce": acc.Nonce + 1,
		})
	})

	// merkle
	mux.HandleFunc("/merkle", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		root := eng.MerkleRoot()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"root":  hex.EncodeToString(root),
			"count": eng.ReceiptCount(),
		})
	})

	// tx 제출 (비동기 적용 + 2s 타임아웃, 디버그 스킵, 상세 로그)
	mux.HandleFunc("/tx_old", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		b, _ := io.ReadAll(r.Body)
		ct := strings.ToLower(r.Header.Get("Content-Type"))
		log.Printf("/tx recv ct=%q len=%d", ct, len(b))

		// 파싱 전에 즉시 빠져나오는 디버그 경로 (절대 블로킹 방지)
		if r.URL.Query().Get("debug") == "skipapply" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":  "queued",
				"parsed":  false, // 파싱 안 함
				"ok":      true,
				"applied": false,
				"reason":  "skipapply",
				"len":     len(b),
				"ct":      ct,
			})
			return
		}

		var t tx.Transfer
		var parseErr error

		switch {
		case strings.HasPrefix(ct, "application/json") || (len(b) > 0 && (b[0] == '{' || b[0] == '[')):
			parseErr = codec.DecodeJSON(b, &t)
		case strings.HasPrefix(ct, "application/cbor"):
			parseErr = codec.DecodeCBOR(b, &t)
		default:
			// content-type 모호할 때 JSON→CBOR 순으로 시도
			if err := codec.DecodeJSON(b, &t); err != nil {
				parseErr = codec.DecodeCBOR(b, &t)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if parseErr != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "queued", "parsed": false, "error": parseErr.Error(),
			})
			return
		}

		log.Printf("/tx parsed type=%d from=%s to=%s nonce=%d amount_mas=%d fee_mas=%d",
			t.Type, t.From, t.To, t.Nonce, t.AmountMas, t.FeeMas)

		// --- 정책 검사 (기존 로직 그대로 유지) ---
		ok := t.Type == 1 && t.AmountMas > 0
		reason := ""

		// 체인ID
		if t.ChainID != allowedChainID {
			ok = false
			reason = "bad_chain_id"
		}

		// 수수료(0.1%, 최소 1 mas)
		expFeeMas := (t.AmountMas * feeBPS) / 10000
		if expFeeMas < 1 {
			expFeeMas = 1
		}
		if t.FeeMas != expFeeMas {
			ok = false
			if reason == "" {
				reason = "bad_fee"
			}
		}

		// msg_commitment 길이
		if len(t.MsgCommit) > maxMsgCommitLen {
			ok = false
			if reason == "" {
				reason = "msg_commitment_too_large"
			}
		}

		// 만료
		curH := eng.CurrentHeight()
		if t.ExpiryHeight > 0 && curH >= t.ExpiryHeight {
			ok = false
			if reason == "" {
				reason = "expired"
			}
		}

		// 타입/금액 0 체크
		if t.Type != 1 {
			ok = false
			if reason == "" {
				reason = "bad_type"
			}
		}
		if t.AmountMas == 0 {
			ok = false
			if reason == "" {
				reason = "zero_amount"
			}
		}

		// 논스 힌트
		fromAcc := eng.GetAccount(t.From)
		currentNonce := fromAcc.Nonce
		expectedNonce := currentNonce + 1

		// 디버그: 적용 생략하고 즉시 응답 (?debug=skipapply)
		if r.URL.Query().Get("debug") == "skipapply" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "queued", "parsed": true, "ok": true, "applied": false, "reason": "skip_apply_debug",
				"tx_hash": "", "from": t.From, "to": t.To,
				"amount_mas": t.AmountMas, "fee_mas": t.FeeMas,
				"amount": t.AmountMas / masPerMicro, "fee": t.FeeMas / masPerMicro,
				"nonce": t.Nonce, "current_nonce": currentNonce, "expected_nonce": expectedNonce,
				"expected_fee_mas": expFeeMas,
			})
			return
		}

		// 정책 불일치면 즉시 영수증 저장 후 응답
		if !ok {
			rec := eng.StoreReceipt(t, false, reason)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "queued", "parsed": true, "ok": false, "applied": false, "reason": reason,
				"tx_hash": rec.TxHash, "from": rec.From, "to": rec.To,
				"amount_mas": rec.Amount, "fee_mas": rec.Fee,
				"amount": rec.Amount / masPerMicro, "fee": rec.Fee / masPerMicro,
				"nonce": rec.Nonce, "height": rec.Height, "time_utc": rec.TimeUTC,
				"current_nonce": currentNonce, "expected_nonce": expectedNonce,
				"expected_fee_mas": expFeeMas,
				"merkle_root":      hex.EncodeToString(eng.MerkleRoot()),
				"receipts_count":   eng.ReceiptCount(),
			})
			return
		}

		// --- 여기서부터 핵심: Apply 비동기 + 2초 타임아웃으로 절대 블로킹 금지 ---
		type applyRes struct{ err error }
		resCh := make(chan applyRes, 1)

		go func(tt tx.Transfer) {
			resCh <- applyRes{err: eng.ApplyTransfer(tt)}
		}(t)

		select {
		case rr := <-resCh:
			applied := rr.err == nil
			if !applied {
				reason = "apply:" + rr.err.Error()
			}
			rec := eng.StoreReceipt(t, applied, reason)
			log.Printf("/tx apply done nonce=%d applied=%v reason=%q", t.Nonce, applied, reason)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "queued", "parsed": true, "ok": applied, "applied": applied, "reason": reason,
				"tx_hash": rec.TxHash, "from": rec.From, "to": rec.To,
				"amount_mas": rec.Amount, "fee_mas": rec.Fee,
				"amount": rec.Amount / masPerMicro, "fee": rec.Fee / masPerMicro,
				"nonce": rec.Nonce, "height": rec.Height, "time_utc": rec.TimeUTC,
				"current_nonce": currentNonce, "expected_nonce": expectedNonce,
				"expected_fee_mas": expFeeMas,
				"merkle_root":      hex.EncodeToString(eng.MerkleRoot()),
				"receipts_count":   eng.ReceiptCount(),
			})
			return

		case <-time.After(2 * time.Second):
			// 타임아웃: 즉시 accepted 응답, 백그라운드에서 계속 처리 & 영수증 저장
			log.Printf("/tx apply pending nonce=%d (timeout -> accepted)", t.Nonce)
			go func(tt tx.Transfer) {
				if err := eng.ApplyTransfer(tt); err != nil {
					eng.StoreReceipt(tt, false, "apply:"+err.Error())
				} else {
					eng.StoreReceipt(tt, true, "")
				}
			}(t)

			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "queued", "parsed": true, "ok": true, "applied": false, "reason": "apply_pending",
				"tx_hash": "", "from": t.From, "to": t.To,
				"amount_mas": t.AmountMas, "fee_mas": t.FeeMas,
				"amount": (t.AmountMas / masPerMicro) / masPerMicro, "fee": (t.FeeMas / masPerMicro) / masPerMicro,
				"nonce": t.Nonce, "current_nonce": currentNonce, "expected_nonce": expectedNonce,
				"expected_fee_mas": expFeeMas,
				"merkle_root":      hex.EncodeToString(eng.MerkleRoot()),
				"receipts_count":   eng.ReceiptCount(),
			})
			return
		}
	})

	// supply (확장)
	mux.HandleFunc("/supply/current", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		aF := eng.GetAccount("tho1foundation")
		aX := eng.GetAccount("tho1exchange")
		aA := eng.GetAccount("tho1alice")
		aB := eng.GetAccount("tho1bob")
		totalMas := aA.Balance + aB.Balance + aF.Balance + aX.Balance

		format := func(m uint64) map[string]any {
			return map[string]any{
				"tho":       m / masPerTHO,
				"mas":       m % masPerTHO,
				"mas_total": m,
				"display":   strconv.FormatUint(m/masPerTHO, 10) + " THO " + strconv.FormatUint(m%masPerTHO, 10) + " mas",
			}
		}

		network := totalMas - aF.Balance - aX.Balance

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"unit":       "mas",
			"foundation": format(aF.Balance),
			"exchange":   format(aX.Balance),
			"network":    format(network),
			"total":      format(totalMas),
		})
	})

	// minting (라이트)
	mux.HandleFunc("/minting", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		aF := eng.GetAccount("tho1foundation")
		aX := eng.GetAccount("tho1exchange")
		aA := eng.GetAccount("tho1alice")
		aB := eng.GetAccount("tho1bob")
		totalMas := aA.Balance + aB.Balance + aF.Balance + aX.Balance
		format := func(m uint64) map[string]any {
			return map[string]any{
				"tho":       m / masPerTHO,
				"mas":       m % masPerTHO,
				"mas_total": m,
				"display":   strconv.FormatUint(m/masPerTHO, 10) + " THO " + strconv.FormatUint(m%masPerTHO, 10) + " mas",
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"basis":      "E_net",
			"epoch":      0,
			"foundation": format(aF.Balance),
			"exchange":   format(aX.Balance),
			"network":    format(totalMas - aF.Balance - aX.Balance),
			"total":      format(totalMas),
		})
	})

	// 라운드 커밋/조회/서명
	mux.HandleFunc("/round/commit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		hdr, ok := eng.CommitRound()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"committed": false, "reason": "no_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"committed": true, "header": hdr})
	})
	mux.HandleFunc("/round/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		hdr, ok := eng.LatestRound()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no_round"})
			return
		}
		_ = json.NewEncoder(w).Encode(hdr)
	})
	mux.HandleFunc("/round/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/round/")

		// /round/{n}/header
		if strings.HasSuffix(rest, "/header") {
			numStr := strings.TrimSuffix(rest, "/header")
			n64, err := strconv.ParseUint(numStr, 10, 64)
			if err != nil || n64 == 0 {
				w.WriteHeader(400)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_round"})
				return
			}
			hdr, ok := eng.GetRound(n64)
			w.Header().Set("Content-Type", "application/json")
			if !ok {
				w.WriteHeader(404)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
				return
			}
			_ = json.NewEncoder(w).Encode(hdr)
			return
		}

		// /round/{n}/signed
		if strings.HasSuffix(rest, "/signed") {
			numStr := strings.TrimSuffix(rest, "/signed")
			n64, err := strconv.ParseUint(numStr, 10, 64)
			if err != nil || n64 == 0 {
				w.WriteHeader(400)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_round"})
				return
			}
			hdr, ok := eng.GetRound(n64)
			w.Header().Set("Content-Type", "application/json")
			if !ok {
				w.WriteHeader(404)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
				return
			}
			sigHex := hdr.SignatureHex
			if sigHex == "" {
				if sig, ok2 := eng.SignRoundHeader(hdr); ok2 {
					sigHex = hex.EncodeToString(sig)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"header":        hdr,
				"algo":          "ed25519",
				"pubkey_hex":    eng.PubKeyHex(),
				"signature_hex": sigHex,
			})
			return
		}

		w.WriteHeader(404)
	})
	mux.HandleFunc("/round/latest/signed", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		hdr, ok := eng.LatestRound()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no_round"})
			return
		}
		sigHex := hdr.SignatureHex
		if sigHex == "" {
			if sig, ok2 := eng.SignRoundHeader(hdr); ok2 {
				sigHex = hex.EncodeToString(sig)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"header":        hdr,
			"algo":          "ed25519",
			"pubkey_hex":    eng.PubKeyHex(),
			"signature_hex": sigHex,
		})
	})

	// tx 제출
	mux.HandleFunc("/tx", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		b, _ := io.ReadAll(r.Body)
		log.Printf("/tx recv ct=%q len=%d", r.Header.Get("Content-Type"), len(b))
		ct := strings.ToLower(r.Header.Get("Content-Type"))

		var t tx.Transfer
		var parseErr error
		switch {
		case strings.HasPrefix(ct, "application/cbor"):
			parseErr = codec.DecodeCBOR(b, &t)
		case strings.HasPrefix(ct, "application/json"), len(b) > 0 && (b[0] == '{' || b[0] == '['):
			parseErr = codec.DecodeJSON(b, &t)
		default:
			parseErr = io.EOF
		}

		w.Header().Set("Content-Type", "application/json")
		if parseErr != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "queued", "parsed": false, "error": parseErr.Error()})
			return
		}

		ok := t.Type == 1 && t.AmountMas > 0
		reason := ""

		// 체인ID
		if t.ChainID != allowedChainID {
			ok = false
			reason = "bad_chain_id"
		}

		// 수수료(0.1%, 최소 1 mas)
		expFeeMas := (t.AmountMas * feeBPS) / 10000
		if expFeeMas < 1 {
			expFeeMas = 1
		}
		if t.FeeMas != expFeeMas {
			ok = false
			if reason == "" {
				reason = "bad_fee"
			}
		}

		// msg_commitment 길이
		if len(t.MsgCommit) > maxMsgCommitLen {
			ok = false
			if reason == "" {
				reason = "msg_commitment_too_large"
			}
		}

		// 만료
		curH := eng.CurrentHeight()
		if t.ExpiryHeight > 0 && curH >= t.ExpiryHeight {
			ok = false
			if reason == "" {
				reason = "expired"
			}
		}

		// 타입/금액 0 체크
		if t.Type != 1 {
			ok = false
			if reason == "" {
				reason = "bad_type"
			}
		}
		if t.AmountMas == 0 {
			ok = false
			if reason == "" {
				reason = "zero_amount"
			}
		}

		// 논스 힌트
		fromAcc := eng.GetAccount(t.From)
		currentNonce := fromAcc.Nonce
		expectedNonce := currentNonce + 1

		applied := false
		if ok {
			if err := eng.ApplyTransfer(t); err != nil {
				reason = "apply:" + err.Error()
			} else {
				applied = true
			}
		}

		rec := eng.StoreReceipt(t, applied, reason)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "queued", "parsed": true, "ok": ok, "applied": applied, "reason": reason,
			"tx_hash": rec.TxHash, "from": rec.From, "to": rec.To,
			"amount_mas": rec.Amount, "fee_mas": rec.Fee,
			"amount": rec.Amount / masPerMicro, "fee": rec.Fee / masPerMicro,
			"nonce": rec.Nonce, "height": rec.Height, "time_utc": rec.TimeUTC,
			"current_nonce": currentNonce, "expected_nonce": expectedNonce,
			"expected_fee_mas": expFeeMas,
			"merkle_root":      hex.EncodeToString(eng.MerkleRoot()),
			"receipts_count":   eng.ReceiptCount(),
		})
	})

	// --- Docs/OpenAPI (env로 가드) ---
	if os.Getenv("THOMAS_ENABLE_DOCS") == "1" {
		// 정적 디렉터리
		openapiDir := http.StripPrefix("/openapi/", http.FileServer(http.Dir("server/static/openapi")))
		docsDir := http.StripPrefix("/docs/", http.FileServer(http.Dir("server/static/docs")))

		// /openapi/* 전체
		mux.Handle("/openapi/", openapiDir)

		// /openapi/merged.json 에만 ETag/Max-Age 적용 (없으면 no-store)
		mergedPath := filepath.Join(getWD(), "server", "static", "openapi", "merged.json")
		mux.Handle("/openapi/merged.json", withStaticETag(mergedPath, openapiDir))

		// Swagger UI
		mux.Handle("/docs/", docsDir)
	}

	// 전역 CORS (THOMAS_CORS_ORIGINS 가 없으면 no-op)
	return wrapCORS(mux)

}

// --- CORS & Cache helpers (lightweight, self-contained) ---

// CORS 허용 도메인 파싱: "*,https://example.com"
func parseOrigins(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func matchOrigin(allow []string, origin string) string {
	if origin == "" {
		return ""
	}
	for _, a := range allow {
		if a == "*" || a == origin {
			return a
		}
	}
	return ""
}

func wrapCORS(next http.Handler) http.Handler {
	origins := parseOrigins(os.Getenv("THOMAS_CORS_ORIGINS")) // 예: "*,https://example.com"
	if len(origins) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allow := matchOrigin(origins, origin)
		if allow != "" {
			if allow == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", allow)
				w.Header().Add("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// merged.json 캐시: THOMAS_CACHE_MAX_AGE(초) 설정 시 ETag/Last-Modified + max-age, 없으면 no-store
func withStaticETag(filePath string, fallback http.Handler) http.Handler {
	maxAgeStr := os.Getenv("THOMAS_CACHE_MAX_AGE")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if maxAgeStr == "" {
			w.Header().Set("Cache-Control", "no-store")
			fallback.ServeHTTP(w, r)
			return
		}
		maxAge, err := strconv.Atoi(maxAgeStr)
		if err != nil || maxAge < 0 {
			maxAge = 0
		}

		// 파일 해시로 약한 ETag 계산 (sha1)
		f, err := os.Open(filePath)
		if err == nil {
			defer f.Close()
			h := sha1.New()
			_, _ = io.Copy(h, f)
			etag := `W/"` + hex.EncodeToString(h.Sum(nil)) + `"`
			if fi, err2 := os.Stat(filePath); err2 == nil {
				w.Header().Set("Last-Modified", fi.ModTime().UTC().Format(http.TimeFormat))
			}
			w.Header().Set("ETag", etag)
			if inm := r.Header.Get("If-None-Match"); inm != "" && inm == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(maxAge))
		fallback.ServeHTTP(w, r)
	})
}

// 작은 헬퍼: SSE용 공개 구독자 API
type sseAdapter interface {
	SubscribeForSSE() chan []byte
	UnsubscribeForSSE(ch chan []byte)
}

func getWD() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
