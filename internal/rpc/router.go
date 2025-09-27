// internal/rpc/router.go
package rpc

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"expvar"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	app "thomasd/internal/app"
	"thomasd/internal/buildinfo"
	mycrypto "thomasd/internal/crypto"
	"thomasd/internal/rewards"
	"thomasd/internal/state/validators"
	"thomasd/internal/types"
	"time"

	tx "thomasd/internal/tx"
)

// --- glue: globals & env-backed params (add this block near the top of router.go) ---

// 전역 mux/handler/engine (다른 파일에 없을 때만 필요)
var (
	mux               = http.NewServeMux()
	root http.Handler = mux
	eng  *app.Engine
)

// main에서 엔진 주입용
func SetEngine(e *app.Engine) { eng = e }

// 서버 시작 기준 시각 (uptime 계산용)
var bootTime = time.Now()

// 디버그 로그 레이트리미터 (현재는 no-op로 통과)
func debuglogRateLimit(next http.Handler) http.Handler {
	return next
}

// 통화 단위 변환 상수
// 1 THO = 10,000,000 mas  →  1 micro-THO(μTHO) = 10 mas
const (
	masPerMicro = 10
	masPerTHO   = masPerMicro * 1_000_000 // 10 * 1,000,000 = 10,000,000
)

// 최소 수수료 계산 (basis points)
func minFeeMas(amountMas uint64, feeBps int) uint64 {
	// ceil(amount * bps / 10000), 그리고 최소 1 mas 보장
	minByBps := (amountMas*uint64(feeBps) + 9999) / 10000
	if minByBps < 1 {
		return 1
	}
	return minByBps
}

// 외부에서 HTTP 핸들러 가져갈 때 사용
func Handler() http.Handler { return root }

// r2p 퍼시스턴스 훅 (다른 파일에 구현 없을 경우 no-op)
func callLoadR2PIfExists() {}
func saveR2P()             {}

// 정책 값: 환경변수 → 기본값
func feeBpsEnv() int {
	s := strings.TrimSpace(os.Getenv("THOMAS_FEE_BPS"))
	if s == "" {
		return 25 // 기본 bps
	}
	if v, err := strconv.Atoi(s); err == nil && v >= 0 {
		return v
	}
	return 25
}
func maxMsgCommitLenEnv() int {
	s := strings.TrimSpace(os.Getenv("THOMAS_MAX_MSG_COMMIT_LEN"))
	if s == "" {
		return 256 // 기본 길이
	}
	if v, err := strconv.Atoi(s); err == nil && v > 0 {
		return v
	}
	return 256
}

// 이 파일에서 참조하는 변수들 (다른 파일에 동일 이름이 이미 있으면 이 둘은 제거)
var (
	feeBps          = feeBpsEnv()
	maxMsgCommitLen = maxMsgCommitLenEnv()
)

// ==== 중요: 이 파일 안에서만 필요한 상수/헬퍼 ====

// 체인ID (중복 금지: 이 한 줄만 남기세요)
const expectedChainID = app.DefaultChainID

// EternityHeader의 시간 값을 최대한 호환적으로 꺼낸다.
// - TimeUTCUnix(uint64) / Timestamp(int64) / TimeUTC(int64) 등 지원
func hdrTimeUnix(h *types.EternityHeader) int64 {
	rv := reflect.ValueOf(h).Elem()
	try := func(name string) (int64, bool) {
		f := rv.FieldByName(name)
		if !f.IsValid() {
			return 0, false
		}
		switch f.Kind() {
		case reflect.Int64, reflect.Int, reflect.Int32:
			return f.Int(), true
		case reflect.Uint64, reflect.Uint, reflect.Uint32:
			return int64(f.Uint()), true
		}
		return 0, false
	}
	if v, ok := try("TimeUTCUnix"); ok {
		return v
	}
	if v, ok := try("Timestamp"); ok {
		return v
	}
	if v, ok := try("TimeUTC"); ok {
		return v
	}
	return 0
}

var tBytes32 = reflect.TypeOf([32]byte{})

// EternityHeader의 [32]byte 필드들을 이름 후보들로 찾아서 반환
func hdrBytes32Field(h *types.EternityHeader, names ...string) ([32]byte, bool) {
	rv := reflect.ValueOf(h).Elem()
	for _, n := range names {
		f := rv.FieldByName(n)
		if f.IsValid() && f.Type() == tBytes32 {
			return f.Interface().([32]byte), true
		}
	}
	return [32]byte{}, false
}

func hdrProposerKey(h *types.EternityHeader) ([32]byte, bool) {
	// ProposerKey 또는 ProposerPubKey
	if v, ok := hdrBytes32Field(h, "ProposerKey", "ProposerPubKey"); ok {
		return v, true
	}
	return [32]byte{}, false
}

func hdrProposerSetHash(h *types.EternityHeader) ([32]byte, bool) {
	// ProposerSetHash / ValidatorsRoot / ValidatorSetHash
	if v, ok := hdrBytes32Field(h, "ProposerSetHash", "ValidatorsRoot", "ValidatorSetHash"); ok {
		return v, true
	}
	return [32]byte{}, false
}

func hdrCommitHash(h *types.EternityHeader) ([32]byte, bool) {
	// CommitHash 우선, 다음으로 CommitRoot([32]byte),
	// 아니면 Commit(struct)에 Root() 메서드가 있으면 호출
	if v, ok := hdrBytes32Field(h, "CommitHash"); ok {
		return v, true
	}
	if v, ok := hdrBytes32Field(h, "CommitRoot"); ok {
		return v, true
	}
	rv := reflect.ValueOf(h).Elem()
	cf := rv.FieldByName("Commit")
	if cf.IsValid() {
		// Commit.Root() ([32]byte) 지원
		m := cf.Addr().MethodByName("Root")
		if m.IsValid() && m.Type().NumIn() == 0 && m.Type().NumOut() == 1 && m.Type().Out(0) == tBytes32 {
			out := m.Call(nil)[0].Interface().([32]byte)
			return out, true
		}
	}
	return [32]byte{}, false
}

func hdrPrevHash(h *types.EternityHeader) ([32]byte, bool) {
	return hdrBytes32Field(h, "PrevHash", "PreviousHash", "LastBlockID")
}

func hdrStateRoot(h *types.EternityHeader) ([32]byte, bool) {
	return hdrBytes32Field(h, "StateRoot")
}

func hdrTxRoot(h *types.EternityHeader) ([32]byte, bool) {
	return hdrBytes32Field(h, "TxRoot", "TransactionsRoot")
}

func hdrEvidenceRoot(h *types.EternityHeader) ([32]byte, bool) {
	return hdrBytes32Field(h, "EvidenceRoot")
}

func hdrSignature(h *types.EternityHeader) ([]byte, bool) {
	rv := reflect.ValueOf(h).Elem()
	f := rv.FieldByName("Signature")
	if f.IsValid() && f.Kind() == reflect.Slice && f.Type().Elem().Kind() == reflect.Uint8 {
		return f.Bytes(), true
	}
	return nil, false
}

// header.BlockID() 또는 header.Hash()를 통해 [32]byte 블록ID를 얻는다.
func hdrBlockID32(h *types.EternityHeader) ([32]byte, bool) {
	// pointer receiver를 고려해서 &h로 인터페이스 캐스팅
	if v, ok := any(h).(interface{ BlockID() [32]byte }); ok {
		return v.BlockID(), true
	}
	if v, ok := any(h).(interface{ Hash() ([32]byte, error) }); ok {
		id, _ := v.Hash()
		return id, true
	}
	return [32]byte{}, false
}

// any → hex string (32바이트 배열이면 hex로, []byte도 hex로)
func anyToHex(v any) (string, bool) {
	switch t := v.(type) {
	case [32]byte:
		return hex.EncodeToString(t[:]), true
	case []byte:
		return hex.EncodeToString(t), true
	}
	return "", false
}

func b32ToHex(a [32]byte) string { return hex.EncodeToString(a[:]) }

// ============================
// R2P (메모리 저장; 퍼시스턴스는 별도 파일에서)
// ============================

type r2pRecord struct {
	ID           string `json:"id"`
	From         string `json:"from"`
	To           string `json:"to"`
	AmountMas    uint64 `json:"amount_mas"`
	Memo         string `json:"memo,omitempty"`
	Status       string `json:"status"` // open|paid|declined|canceled
	CreatedUTC   int64  `json:"created_utc"`
	PaidUTC      int64  `json:"paid_utc,omitempty"`
	DeclinedUTC  int64  `json:"declined_utc,omitempty"`
	CanceledUTC  int64  `json:"canceled_utc,omitempty"`
	TxHash       string `json:"tx_hash,omitempty"`
	Alias        string `json:"alias,omitempty"`
	AliasVersion int64  `json:"alias_version,omitempty"`
}

var (
	r2pMu    sync.Mutex
	r2pStore = map[string]*r2pRecord{}
)

// 주의: r2pSaveLocked, incBadSigJSON, incBindMismatch 등은
// 다른 파일(예: r2p_persist.go, metrics.go)에 이미 정의되어 있어야 합니다.
// 여기서는 **재정의하지 않습니다**.

// ============================
// 내부 helpers (쿼리/맵 파싱, 엔진 리플렉션)
// ============================

// 쿼리에서 int64 추출
func extractInt64Query(r *http.Request, key string, def int64) int64 {
	q := r.URL.Query().Get(key)
	if q == "" {
		return def
	}
	v, err := strconv.ParseInt(q, 10, 64)
	if err != nil {
		return def
	}
	return v
}

// 임의 값 → map[string]any (JSON roundtrip)
func toMapFromAny(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return map[string]any{}
	}
	return m
}

func extractInt64Map(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	if v, ok := m[key]; ok {
		switch t := v.(type) {
		case float64:
			return int64(t)
		case int64:
			return t
		case int:
			return int64(t)
		case json.Number:
			i, _ := t.Int64()
			return i
		}
	}
	// Version/ version 대체 키 지원
	if key == "version" {
		if v, ok := m["Version"]; ok {
			if f, ok2 := v.(float64); ok2 {
				return int64(f)
			}
		}
	}
	return 0
}

// 엔진 Alias resolve (반영구 호환 리플렉션)
func callEngResolveAlias(eng *app.Engine, name string) (map[string]any, bool) {
	rv := reflect.ValueOf(eng)
	candidates := []string{"ResolveAliasFile", "ResolveAlias", "AliasResolve", "GetAlias", "GetAliasRecord", "AliasLookup"}
	for _, mname := range candidates {
		m := rv.MethodByName(mname)
		if !m.IsValid() {
			continue
		}
		outs := m.Call([]reflect.Value{reflect.ValueOf(name)})
		switch len(outs) {
		case 1:
			return toMapFromAny(outs[0].Interface()), true
		case 2:
			rec := toMapFromAny(outs[0].Interface())
			ok := false
			if b, ok2 := outs[1].Interface().(bool); ok2 {
				ok = b
			}
			if !ok {
				return map[string]any{}, true
			}
			return rec, true
		}
	}
	return nil, false
}

func callEngReverseAlias(eng *app.Engine, addr string) (name string, implemented bool, found bool) {
	rv := reflect.ValueOf(eng)
	candidates := []string{"ReverseAliasFile", "ReverseAlias", "AliasReverse", "GetAliasByAddress", "AliasOf"}
	for _, mname := range candidates {
		m := rv.MethodByName(mname)
		if !m.IsValid() {
			continue
		}
		outs := m.Call([]reflect.Value{reflect.ValueOf(addr)})
		switch len(outs) {
		case 1:
			if s, ok := outs[0].Interface().(string); ok && s != "" {
				return s, true, true
			}
			return "", true, false
		case 2:
			s, _ := outs[0].Interface().(string)
			b, _ := outs[1].Interface().(bool)
			return s, true, b
		}
	}
	return "", false, false
}

// @alias → 실제 주소로 (엔진 미구현 시 false)
func resolveAddressMaybeAlias(eng *app.Engine, in string) (addr string, aliasNorm string, aliasVer int64, ok bool) {
	if strings.HasPrefix(in, "@") {
		rec, implemented := callEngResolveAlias(eng, in)
		if !implemented {
			return "", "", 0, false
		}
		ad, _ := rec["address"].(string)
		if ad == "" {
			return "", "", 0, false
		}
		return ad, strings.TrimPrefix(in, "@"), extractInt64Map(rec, "version"), true
	}
	return in, "", 0, true
}

func resolveAddrMaybeAlias(eng *app.Engine, in string) (string, error) {
	addr, _, _, ok := resolveAddressMaybeAlias(eng, in)
	if !ok || addr == "" {
		return "", fmt.Errorf("bad_owner_or_alias")
	}
	return addr, nil
}

// ============================
// R2P 유틸
// ============================

func genR2PID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func r2pUpdatedAt(x r2pRecord) int64 {
	m := x.CreatedUTC
	if x.PaidUTC > m {
		m = x.PaidUTC
	}
	if x.DeclinedUTC > m {
		m = x.DeclinedUTC
	}
	if x.CanceledUTC > m {
		m = x.CanceledUTC
	}
	return m
}

// ============================
// 민감 응답 헤더
// ============================

func setSensitiveNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
}

// ============================
// 초기 라우팅 바인딩 (전역 mux/eng/bootTime/feeBps 등은 패키지 내 다른 파일에서 제공)
// ============================

func init() {
	// 디스크 로드 (best-effort) — 구현은 다른 파일에서
	callLoadR2PIfExists()

	// expvar (optional)
	if os.Getenv("THOMAS_ENABLE_VARZ") == "1" {
		mux.Handle("/debug/vars", expvar.Handler())
	}

	// 목록 (updated desc)
	mux.HandleFunc("/r2p/list", r2pListHandlerOpaque)

	mux.HandleFunc("/tx", txHandler) // 단일 디스패처
	mux.HandleFunc("/tx/get", txGetHandler)

	// Debug echo
	mux.HandleFunc("/debug/echo", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(b)
	})

	// Debug stack (pprof no-op 대체)
	mux.HandleFunc("/debug/stack", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_ = pprofGoroutine().WriteTo(w, 2)
	})

	// SSE
	mux.HandleFunc("/events/stream", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		fl, ok := w.(http.Flusher)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
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
				_, _ = w.Write([]byte("event: push\n"))
				_, _ = w.Write([]byte("data: "))
				_, _ = w.Write(msg)
				_, _ = w.Write([]byte("\n\n"))
				fl.Flush()
			}
		}
	})

	// Node info
	mux.HandleFunc("/node/info", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"algo":       "ed25519",
			"pubkey_hex": eng.PubKeyHex(),
		})
	})

	// Height
	mux.HandleFunc("/height", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		h := eng.CurrentHeight()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"height": h})
	})

	// Policy
	mux.HandleFunc("/policy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"unit":               "mas",
			"fee_bps":            feeBps,
			"min_fee_mas":        1,
			"max_msg_commit_len": maxMsgCommitLen,
			"allowed_chain_id":   expectedChainID,
			"alias_enabled":      os.Getenv("THOMAS_FEAT_ALIAS") == "1",
			"r2p_enabled":        os.Getenv("THOMAS_FEAT_R2P") == "1",
			"require": map[string]any{
				"commit":      os.Getenv("THOMAS_REQUIRE_COMMIT") == "1",
				"signature":   os.Getenv("THOMAS_VERIFY_SIG") == "1",
				"from_pubkey": os.Getenv("THOMAS_REQUIRE_FROM_PUBKEY") == "1",
			},
			"signing": map[string]any{
				"algo":             "ed25519",
				"pubkey_hex":       eng.PubKeyHex(),
				"header_canonical": []string{"round", "from_height", "to_height", "tx_count", "root", "time_utc"},
			},
		})
	})

	// Health (canonical)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"time_utc": time.Now().UTC().Format(time.RFC3339),
		})
	})

	// Health invariants
	mux.HandleFunc("/health/invariants", func(w http.ResponseWriter, r *http.Request) {
		h := eng.CurrentHeight()
		receiptCount := uint64(eng.ReceiptCount())
		ok := (h == receiptCount)
		reason := ""
		if !ok {
			reason = "height_mismatch"
		}

		commitRoot, commitHeight, commitOK := eng.LatestCommitRoot()
		if !commitOK {
			ok = false
			reason = "commit_root_unavailable"
		}

		header, headerOK := eng.LatestEternityHeader()
		if !headerOK {
			ok = false
			reason = "header_unavailable"
		}

		validatorsRootOK := true
		var validatorsRoot [32]byte
		if headerOK {
			if vr, ok2 := hdrProposerSetHash(&header); ok2 {
				validatorsRoot = vr
			}
			if scores, err := eng.ListValidatorScores(); err == nil {
				computed := validators.ComputeValidatorsRoot(scores)
				if validatorsRoot != computed {
					ok = false
					reason = "validators_root_mismatch"
					validatorsRootOK = false
				}
			} else {
				ok = false
				reason = "validator_state_unavailable"
				validatorsRootOK = false
			}
		}

		if commitOK && headerOK {
			if hdrCH, ok2 := hdrCommitHash(&header); ok2 {
				if hdrCH != commitRoot {
					ok = false
					reason = "commit_root_mismatch"
				}
			}
		}

		resp := map[string]any{
			"ok":                 ok,
			"reason":             reason,
			"height":             h,
			"receipts_count":     receiptCount,
			"commit_root_height": commitHeight,
			"time_utc":           time.Now().UTC().Format(time.RFC3339),
		}
		if commitOK {
			resp["commit_root_hex"] = b32ToHex(commitRoot)
		}
		if headerOK {
			if ch, ok2 := hdrCommitHash(&header); ok2 {
				resp["header_commit_root_hex"] = b32ToHex(ch)
			}
		}
		if validatorsRootOK {
			resp["validators_root_hex"] = b32ToHex(validatorsRoot)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Account
	mux.HandleFunc("/account/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
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

	// Nonce
	mux.HandleFunc("/nonce/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		addr := strings.TrimPrefix(r.URL.Path, "/nonce/")
		acc := eng.GetAccount(addr)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"nonce": acc.Nonce, "expected_nonce": acc.Nonce + 1,
		})
	})

	// Merkle
	mux.HandleFunc("/merkle", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		root := eng.MerkleRoot()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"root":  hex.EncodeToString(root),
			"count": eng.ReceiptCount(),
		})
	})

	// Supply snapshot
	supplyToJSON := func(st app.SupplyState, minted uint64) map[string]any {
		return map[string]any{
			"height":               st.LastUpdatedHeight,
			"total_minted_mas":     st.TotalMintedMas,
			"network_vault_mas":    st.NetworkVaultMas,
			"foundation_vault_mas": st.FoundationVaultMas,
			"exchange_vault_mas":   st.ExchangeVaultMas,
			"block_mint_mas":       minted,
		}
	}

	mux.HandleFunc("/supply/current", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		st := eng.CurrentSupply()
		minted := rewards.BlockMintAt(st.LastUpdatedHeight)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(supplyToJSON(st, minted))
	})

	mux.HandleFunc("/supply/at/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		heightStr := strings.TrimPrefix(r.URL.Path, "/supply/at/")
		height, err := strconv.ParseUint(heightStr, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_height"})
			return
		}
		st, ok := eng.SupplyAt(height)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
			return
		}
		minted := rewards.BlockMintAt(height)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(supplyToJSON(st, minted))
	})

	// Minting (light)
	mux.HandleFunc("/minting", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
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

	// Rounds
	mux.HandleFunc("/round/commit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
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
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		hdr, ok := eng.LatestRound()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no_round"})
			return
		}
		_ = json.NewEncoder(w).Encode(hdr)
	})

	mux.HandleFunc("/round/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/round/")

		if strings.HasSuffix(rest, "/header") {
			numStr := strings.TrimSuffix(rest, "/header")
			n64, err := strconv.ParseUint(numStr, 10, 64)
			if err != nil || n64 == 0 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_round"})
				return
			}
			hdr, ok := eng.GetRound(n64)
			w.Header().Set("Content-Type", "application/json")
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
				return
			}
			_ = json.NewEncoder(w).Encode(hdr)
			return
		}

		if strings.HasSuffix(rest, "/signed") {
			numStr := strings.TrimSuffix(rest, "/signed")
			n64, err := strconv.ParseUint(numStr, 10, 64)
			if err != nil || n64 == 0 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_round"})
				return
			}
			hdr, ok := eng.GetRound(n64)
			w.Header().Set("Content-Type", "application/json")
			if !ok {
				w.WriteHeader(http.StatusNotFound)
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

		w.WriteHeader(http.StatusNotFound)
	})

	// === Eternity 최신 헤더 (필드 호환 어댑터 사용) ===
	mux.HandleFunc("/eternity/header/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		h, ok := eng.LatestEternityHeader()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no_header"})
			return
		}

		// 공통 필드
		resp := map[string]any{
			"chain_id": expectedChainID,
			"height":   h.Height,
			"round":    h.Round,
			"algo":     "ed25519",
		}

		if ts := hdrTimeUnix(&h); ts != 0 {
			resp["timestamp"] = ts
		}
		if v, ok2 := hdrPrevHash(&h); ok2 {
			resp["prev_hash"] = b32ToHex(v)
		}
		if v, ok2 := hdrStateRoot(&h); ok2 {
			resp["state_root"] = b32ToHex(v)
		}
		if v, ok2 := hdrTxRoot(&h); ok2 {
			resp["tx_root"] = b32ToHex(v)
		}
		if v, ok2 := hdrEvidenceRoot(&h); ok2 {
			resp["evidence_root"] = b32ToHex(v)
		}
		if v, ok2 := hdrProposerKey(&h); ok2 {
			resp["proposer_key"] = b32ToHex(v)
			resp["pubkey_hex"] = b32ToHex(v)
		}
		if v, ok2 := hdrProposerSetHash(&h); ok2 {
			resp["proposer_set"] = b32ToHex(v)
		}
		if v, ok2 := hdrCommitHash(&h); ok2 {
			resp["commit_hash"] = b32ToHex(v)
		}
		if v, ok2 := hdrBlockID32(&h); ok2 {
			resp["block_id_hex"] = b32ToHex(v)
		}
		if sig, ok2 := hdrSignature(&h); ok2 {
			resp["signature_hex"] = hex.EncodeToString(sig)
		}

		// Commit 서브구조(있을 때만 요약)
		rv := reflect.ValueOf(&h).Elem()
		cf := rv.FieldByName("Commit")
		if cf.IsValid() {
			cm := map[string]any{}
			if f := cf.FieldByName("Height"); f.IsValid() && f.CanInt() {
				cm["height"] = f.Int()
			}
			if f := cf.FieldByName("Round"); f.IsValid() && f.CanUint() {
				cm["round"] = f.Uint()
			}
			if f := cf.FieldByName("BlockID"); f.IsValid() && f.Type() == tBytes32 {
				arr := f.Interface().([32]byte)
				cm["block_id_hex"] = hex.EncodeToString(arr[:])
			}

			if f := cf.FieldByName("Signatures"); f.IsValid() && f.Kind() == reflect.Slice {
				cm["signature_count"] = f.Len()
			}
			if len(cm) > 0 {
				resp["commit"] = cm
			}
		}

		_ = json.NewEncoder(w).Encode(resp)
	})

	// === 블록 경량 증명 ===
	mux.HandleFunc("/block/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/light-proof") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		trimmed := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/block/"), "/light-proof")
		height, err := strconv.ParseUint(trimmed, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_height"})
			return
		}

		header, ok := eng.EternityHeaderAt(height)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
			return
		}

		// 커밋 번들은 엔진이 제공하면 사용
		bundleMap, bundleOK := func() (map[string]any, bool) {
			rv := reflect.ValueOf(eng)
			m := rv.MethodByName("CommitBundleAt")
			if !m.IsValid() {
				return nil, false
			}
			outs := m.Call([]reflect.Value{reflect.ValueOf(height)})
			if len(outs) != 2 {
				return nil, false
			}
			okv, _ := outs[1].Interface().(bool)
			if !okv {
				return nil, false
			}
			// 첫번째 아웃이 임의 타입의 CommitBundle
			b := outs[0]
			out := map[string]any{}
			// 필드 추출(있을 때만)
			if f := b.FieldByName("Height"); f.IsValid() && (f.Kind() == reflect.Uint64 || f.Kind() == reflect.Uint || f.Kind() == reflect.Int64 || f.Kind() == reflect.Int) {
				out["height"] = fmt.Sprintf("%v", f.Interface())
			}
			if f := b.FieldByName("Round"); f.IsValid() && (f.Kind() == reflect.Uint32 || f.Kind() == reflect.Uint || f.Kind() == reflect.Int32 || f.Kind() == reflect.Int) {
				out["round"] = fmt.Sprintf("%v", f.Interface())
			}
			if f := b.FieldByName("BlockID"); f.IsValid() && f.Type() == tBytes32 {
				arr := f.Interface().([32]byte)
				out["block_id_hex"] = hex.EncodeToString(arr[:])
			}
			if f := b.FieldByName("Signatures"); f.IsValid() && f.Kind() == reflect.Slice {
				out["signature_count"] = f.Len()
			}
			// Root() 있으면 commit_root_hex도 추가
			if m2 := b.Addr().MethodByName("Root"); m2.IsValid() && m2.Type().NumIn() == 0 && m2.Type().NumOut() == 1 && m2.Type().Out(0) == tBytes32 {
				root := m2.Call(nil)[0].Interface().([32]byte)
				out["commit_root_hex"] = hex.EncodeToString(root[:])
			}
			return out, true
		}()

		// header 쪽 요약
		headerMap := map[string]any{
			"chain_id": expectedChainID,
			"height":   header.Height,
			"round":    header.Round,
		}
		if ts := hdrTimeUnix(&header); ts != 0 {
			headerMap["timestamp"] = ts
		}
		if v, ok2 := hdrPrevHash(&header); ok2 {
			headerMap["prev_hash"] = b32ToHex(v)
		}
		if v, ok2 := hdrStateRoot(&header); ok2 {
			headerMap["state_root"] = b32ToHex(v)
		}
		if v, ok2 := hdrTxRoot(&header); ok2 {
			headerMap["tx_root"] = b32ToHex(v)
		}
		if v, ok2 := hdrEvidenceRoot(&header); ok2 {
			headerMap["evidence_root"] = b32ToHex(v)
		}
		if v, ok2 := hdrProposerKey(&header); ok2 {
			headerMap["proposer_key"] = b32ToHex(v)
			headerMap["pubkey_hex"] = b32ToHex(v)
		}
		if v, ok2 := hdrProposerSetHash(&header); ok2 {
			headerMap["proposer_set"] = b32ToHex(v)
		}
		if v, ok2 := hdrCommitHash(&header); ok2 {
			headerMap["commit_hash"] = b32ToHex(v)
		}
		if v, ok2 := hdrBlockID32(&header); ok2 {
			headerMap["block_id_hex"] = b32ToHex(v)
		}
		if sig, ok2 := hdrSignature(&header); ok2 {
			headerMap["signature_hex"] = hex.EncodeToString(sig)
		}

		resp := map[string]any{
			"header": headerMap,
		}
		if v, ok2 := hdrCommitHash(&header); ok2 {
			resp["commit_root_hex"] = b32ToHex(v)
		}
		if v, ok2 := hdrProposerSetHash(&header); ok2 {
			resp["validators_root_hex"] = b32ToHex(v)
		}
		if bundleOK {
			resp["commit_bundle"] = bundleMap
		} else {
			// 기존 동작에 맞춰 커밋 번들이 없으면 404 처리
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "commit_not_found"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// 최신 라운드 + 서명
	mux.HandleFunc("/round/latest/signed", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		hdr, ok := eng.LatestRound()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusNotFound)
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

	// ----------------------
	// Alias API (reflective)
	// ----------------------
	mux.HandleFunc("/alias/resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		name := r.URL.Query().Get("name")
		if name == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing_name"})
			return
		}
		rec, ok := callEngResolveAlias(eng, name)
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			if rec == nil {
				w.WriteHeader(http.StatusNotImplemented)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_implemented"})
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "record": rec})
	})

	mux.HandleFunc("/alias/reverse", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		addr := r.URL.Query().Get("addr")
		if addr == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing_addr"})
			return
		}
		name, implemented, found := callEngReverseAlias(eng, addr)
		w.Header().Set("Content-Type", "application/json")
		if !implemented {
			w.WriteHeader(http.StatusNotImplemented)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_implemented"})
			return
		}
		if !found {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "alias": name})
	})

	// ----------------------
	// R2P endpoints
	// ----------------------

	// wallet
	mux.HandleFunc("/wallet/create", walletCreateHandler)
	mux.HandleFunc("/wallet/restore", walletRestoreHandler)

	// 생성
	mux.HandleFunc("/r2p/create", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if os.Getenv("THOMAS_FEAT_R2P") != "1" {
			w.WriteHeader(http.StatusNotImplemented)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_implemented"})
			return
		}
		var in struct {
			From      string `json:"from"` // payee
			To        string `json:"to"`   // payer
			AmountMas int64  `json:"amount_mas"`
			Memo      string `json:"memo"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &in); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "bad_json"})
			return
		}
		payeeAddr, aliasNorm, aliasVer, ok1 := resolveAddressMaybeAlias(eng, in.From)
		payerAddr, _, _, ok2 := resolveAddressMaybeAlias(eng, in.To)
		if !ok1 || !ok2 || in.AmountMas <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "bad_params"})
			return
		}

		r2pMu.Lock()
		id := genR2PID()
		rec := &r2pRecord{
			ID:         id,
			From:       payeeAddr,
			To:         payerAddr,
			AmountMas:  uint64(in.AmountMas),
			Memo:       in.Memo,
			CreatedUTC: time.Now().UTC().Unix(),
			Status:     "open",
		}
		if aliasNorm != "" {
			rec.Alias = aliasNorm
			rec.AliasVersion = aliasVer
		}
		r2pStore[id] = rec
		r2pSaveLocked()
		r2pMu.Unlock()

		saveR2P()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": id})
	})

	// 조회
	mux.HandleFunc("/r2p/get", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "missing_id"})
			return
		}
		r2pMu.Lock()
		rec, ok := r2pStore[id]
		r2pMu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "not_found"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "record": rec})
	})

	// 승인 (결제) — dev free 모드 지원
	mux.HandleFunc("/r2p/approve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if os.Getenv("THOMAS_FEAT_R2P") != "1" {
			w.WriteHeader(http.StatusNotImplemented)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_implemented"})
			return
		}

		var in struct {
			ID     string `json:"id"`
			Payer  string `json:"payer"`   // optional
			FeeMas int64  `json:"fee_mas"` // optional
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &in); err != nil || in.ID == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_json"})
			return
		}

		r2pMu.Lock()
		rec, ok := r2pStore[in.ID]
		r2pMu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
			return
		}
		if rec.Status != "open" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "id": rec.ID, "status": rec.Status, "tx_hash": rec.TxHash,
			})
			return
		}

		payer := strings.TrimSpace(in.Payer)
		if payer == "" {
			payer = rec.To
		}
		payerAddr, err := resolveAddrMaybeAlias(eng, payer)
		if err != nil || payerAddr != rec.To {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "payer_mismatch"})
			return
		}

		fee := in.FeeMas
		if fee <= 0 {
			fee = int64(minFeeMas(rec.AmountMas, feeBps))
		}
		acc := eng.GetAccount(payerAddr)
		nonce := acc.Nonce + 1

		t := tx.Transfer{
			Type:         1,
			From:         payerAddr,
			To:           rec.From,
			AmountMas:    rec.AmountMas,
			ExpiryHeight: 0,
			FeeMas:       uint64(fee),
			Nonce:        nonce,
			ChainID:      expectedChainID,
		}

		if err := eng.ApplyTransfer(t); err != nil {
			reason := "apply:" + err.Error()

			// DEV free R2P
			if devFreeR2PEnabled(r) && isInsufficientFunds(err, reason) {
				now := time.Now().UTC().Unix()
				r2pMu.Lock()
				rec.Status = "paid"
				rec.PaidUTC = now
				rec.TxHash = "dev-" + rec.ID
				r2pStore[in.ID] = rec
				r2pSaveLocked()
				r2pMu.Unlock()

				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": rec.ID, "tx_hash": rec.TxHash})
				return
			}

			rc := eng.StoreReceipt(t, false, reason)
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": reason, "receipt": rc})
			return
		}

		rc := eng.StoreReceipt(t, true, "")

		now := time.Now().UTC().Unix()
		r2pMu.Lock()
		rec.Status = "paid"
		rec.PaidUTC = now
		rec.TxHash = rc.TxHash
		r2pStore[in.ID] = rec
		r2pSaveLocked()
		r2pMu.Unlock()

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": rec.ID, "tx_hash": rc.TxHash})
	})

	// 거절
	mux.HandleFunc("/r2p/decline", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if os.Getenv("THOMAS_FEAT_R2P") != "1" {
			w.WriteHeader(http.StatusNotImplemented)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_implemented"})
			return
		}
		var in struct {
			ID    string `json:"id"`
			Payer string `json:"payer"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &in); err != nil || in.ID == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_json"})
			return
		}

		r2pMu.Lock()
		rec, ok := r2pStore[in.ID]
		r2pSaveLocked()
		r2pMu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
			return
		}
		if rec.Status != "open" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "id": rec.ID, "status": rec.Status, "tx_hash": rec.TxHash,
			})
			return
		}

		payer := strings.TrimSpace(in.Payer)
		if payer == "" {
			payer = rec.To
		}
		payerAddr, err := resolveAddrMaybeAlias(eng, payer)
		if err != nil || payerAddr != rec.To {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "payer_mismatch"})
			return
		}

		r2pMu.Lock()
		rec.Status = "declined"
		rec.DeclinedUTC = time.Now().UTC().Unix()
		rec.TxHash = ""
		r2pMu.Unlock()
		saveR2P()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": rec.ID, "status": rec.Status})
	})

	// 취소
	mux.HandleFunc("/r2p/cancel", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if os.Getenv("THOMAS_FEAT_R2P") != "1" {
			w.WriteHeader(http.StatusNotImplemented)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_implemented"})
			return
		}
		var in struct {
			ID    string `json:"id"`
			Payee string `json:"payee"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &in); err != nil || in.ID == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_json"})
			return
		}

		r2pMu.Lock()
		rec, ok := r2pStore[in.ID]
		r2pSaveLocked()
		r2pMu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
			return
		}
		if rec.Status != "open" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "id": rec.ID, "status": rec.Status, "tx_hash": rec.TxHash,
			})
			return
		}

		payee := strings.TrimSpace(in.Payee)
		if payee == "" {
			payee = rec.From
		}
		payeeAddr, err := resolveAddrMaybeAlias(eng, payee)
		if err != nil || payeeAddr != rec.From {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "payee_mismatch"})
			return
		}

		r2pMu.Lock()
		rec.Status = "canceled"
		rec.CanceledUTC = time.Now().UTC().Unix()
		rec.TxHash = ""
		r2pMu.Unlock()
		saveR2P()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": rec.ID, "status": rec.Status})
	})

	// Stats
	mux.HandleFunc("/stats.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"height":         eng.CurrentHeight(),
			"receipts_count": eng.ReceiptCount(),
			"time_utc":       time.Now().UTC().Format(time.RFC3339),
		})
	})

	mux.HandleFunc("/stats.sys", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		up := time.Since(bootTime).Seconds()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"height":         eng.CurrentHeight(),
			"receipts_count": eng.ReceiptCount(),
			"time_utc":       time.Now().UTC().Format(time.RFC3339),
			"uptime_secs":    int64(up),
			"goroutines":     runtime.NumGoroutine(),
			"mem_alloc_kb":   int64(ms.Alloc / 1024),
		})
	})

	// Stats+
	mux.HandleFunc("/stats.plus", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		extras := map[string]any{}
		rv := reflect.ValueOf(eng)
		call0 := func(name string) (any, bool) {
			m := rv.MethodByName(name)
			if !m.IsValid() || m.Type().NumIn() != 0 || m.Type().NumOut() == 0 {
				return nil, false
			}
			outs := m.Call(nil)
			return outs[0].Interface(), true
		}
		tryNames := func(names ...string) (any, bool) {
			for _, n := range names {
				if v, ok := call0(n); ok {
					return v, true
				}
			}
			return nil, false
		}
		if v, ok := tryNames("MerkleRoot", "TxRoot"); ok {
			extras["tx_root"] = v
		}
		if v, ok := tryNames("LastBlockHash", "HeadHash", "BlockHash"); ok {
			extras["last_block_hash"] = v
		}
		if v, ok := tryNames("LastTxHash", "RecentTxHash"); ok {
			extras["last_tx_hash"] = v
		}
		if v, ok := tryNames("MempoolSize", "MempoolLen", "BacklogSize", "PendingCount"); ok {
			extras["backlog_size"] = v
		}
		if v, ok := tryNames("ValidatorsLen", "ValidatorCount", "NumValidators"); ok {
			extras["validators_len"] = v
		}

		resp := map[string]any{
			"height":         eng.CurrentHeight(),
			"receipts_count": eng.ReceiptCount(),
			"time_utc":       time.Now().UTC().Format(time.RFC3339),
		}
		for k, v := range extras {
			resp[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Metrics
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		var mempool int64 = -1
		if v := reflect.ValueOf(eng).MethodByName("MempoolSize"); v.IsValid() && v.Type().NumIn() == 0 && v.Type().NumOut() > 0 {
			mv := v.Call(nil)[0]
			switch mv.Kind() {
			case reflect.Int, reflect.Int64, reflect.Int32:
				mempool = mv.Int()
			case reflect.Uint, reflect.Uint64, reflect.Uint32:
				mempool = int64(mv.Uint())
			}
		} else if v := reflect.ValueOf(eng).MethodByName("MempoolLen"); v.IsValid() && v.Type().NumIn() == 0 && v.Type().NumOut() > 0 {
			mv := v.Call(nil)[0]
			if mv.Kind() == reflect.Int || mv.Kind() == reflect.Int64 || mv.Kind() == reflect.Int32 {
				mempool = mv.Int()
			}
		}
		var validatorsCount int64 = -1
		if v := reflect.ValueOf(eng).MethodByName("ValidatorsLen"); v.IsValid() && v.Type().NumIn() == 0 && v.Type().NumOut() > 0 {
			mv := v.Call(nil)[0]
			if mv.Kind() == reflect.Int || mv.Kind() == reflect.Int64 || mv.Kind() == reflect.Int32 {
				validatorsCount = mv.Int()
			}
		}
		fmt.Fprintf(w, "# TYPE thomas_height gauge\nthomas_height %d\n", eng.CurrentHeight())
		fmt.Fprintf(w, "# TYPE thomas_receipts_total counter\nthomas_receipts_total %d\n", eng.ReceiptCount())
		fmt.Fprintf(w, "# TYPE thomas_uptime_seconds gauge\nthomas_uptime_seconds %d\n", int64(time.Since(bootTime).Seconds()))
		fmt.Fprintf(w, "# TYPE thomas_goroutines gauge\nthomas_goroutines %d\n", runtime.NumGoroutine())
		fmt.Fprintf(w, "# TYPE thomas_mem_alloc_bytes gauge\nthomas_mem_alloc_bytes %d\n", ms.Alloc)
		readyVal := 0
		if eng.CurrentHeight() > 0 {
			readyVal = 1
		}
		fmt.Fprintf(w, "# TYPE thomas_ready gauge\nthomas_ready %d\n", readyVal)
		if mempool >= 0 {
			fmt.Fprintf(w, "# TYPE thomas_mempool_size gauge\nthomas_mempool_size %d\n", mempool)
		}
		if validatorsCount >= 0 {
			fmt.Fprintf(w, "# TYPE thomas_validators gauge\nthomas_validators %d\n", validatorsCount)
		}
	})

	// Readiness
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		h := eng.CurrentHeight()
		status := "starting"
		if h > 0 {
			status = "ready"
		}
		resp := map[string]any{
			"status":      status,
			"height":      h,
			"time_utc":    time.Now().UTC().Format(time.RFC3339),
			"uptime_secs": int64(time.Since(bootTime).Seconds()),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Version
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"version": buildinfo.Version,
			"commit":  buildinfo.Commit,
			"date":    buildinfo.Date,
			"go":      buildinfo.Go,
			"os":      buildinfo.OS,
			"arch":    buildinfo.Arch,
		})
	})

	// Panic-safe round commit
	mux.HandleFunc("/round/commit_safe", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		{
			rc := eng.ReceiptCount()
			hh := eng.CurrentHeight()
			if rc >= 0 {
				if int64(hh) > int64(rc) {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"committed": false,
						"reason":    fmt.Sprintf("inconsistent_state: height=%d receipts_count=%d", hh, rc),
					})
					return
				}
			}
		}
		defer func() {
			if rec := recover(); rec != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"committed": false,
					"reason":    fmt.Sprintf("engine.CommitRound panic: %v", rec),
				})
			}
		}()
		hdr, ok := eng.CommitRound()
		if !ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"committed": false, "reason": "no_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"committed": true, "header": hdr})
	})

	mux.HandleFunc("/round/commit_safe2", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		hasPending := func() (bool, bool) {
			rv := reflect.ValueOf(eng)
			tryCount := func(name string) (bool, bool) {
				m := rv.MethodByName(name)
				if !m.IsValid() || m.Type().NumIn() != 0 || m.Type().NumOut() == 0 {
					return false, false
				}
				out := m.Call(nil)[0]
				switch out.Kind() {
				case reflect.Int, reflect.Int32, reflect.Int64:
					return true, out.Int() > 0
				case reflect.Uint, reflect.Uint32, reflect.Uint64:
					return true, out.Uint() > 0
				case reflect.Bool:
					return true, out.Bool()
				default:
					return true, true
				}
			}
			if ok, v := tryCount("PendingCount"); ok {
				return true, v
			}
			if ok, v := tryCount("BacklogSize"); ok {
				return true, v
			}
			if ok, v := tryCount("MempoolSize"); ok {
				return true, v
			}
			if ok, v := tryCount("MempoolLen"); ok {
				return true, v
			}
			return false, true
		}
		if impl, pend := hasPending(); impl && !pend {
			_ = json.NewEncoder(w).Encode(map[string]any{"committed": false, "reason": "no_pending"})
			return
		}

		{
			rc := eng.ReceiptCount()
			hh := eng.CurrentHeight()
			if rc >= 0 {
				if int64(hh) > int64(rc) {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"committed": false,
						"reason":    fmt.Sprintf("inconsistent_state: height=%d receipts_count=%d", hh, rc),
					})
					return
				}
			}
		}
		defer func() {
			if rec := recover(); rec != nil {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"committed": false,
					"reason":    fmt.Sprintf("engine.CommitRound panic: %v", rec),
				})
			}
		}()

		hdr, ok := eng.CommitRound()
		if !ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"committed": false, "reason": "no_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"committed": true, "header": hdr})
	})

	// ===== 루트 엔드포인트 체인 =====
	root = debuglogRateLimit(
		txPrecheckWith(
			precheckSig(
				precheckFromBindingWith(eng)(
					precheckCommit(mux),
				),
			),
			func() int64 { return int64(eng.CurrentHeight()) },
		),
	)
}

// /tx : POST는 전송, GET은 조회로 강제 분기
func txHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("[/tx] hit:", r.Method) // 임시 로그
	switch r.Method {
	case http.MethodPost:
		txPostHandler(w, r)
	case http.MethodGet:
		txGetHandler(w, r)
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// POST /tx  — 트랜잭션 전송
func txPostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// JSON 읽기
	defer r.Body.Close()
	var in txPre
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &in); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "malformed_json"})
		return
	}

	// txPre -> tx.Transfer 매핑
	t := tx.Transfer{
		Type:         in.Type,
		From:         in.From,
		To:           in.To,
		AmountMas:    uint64(in.AmountMas),
		FeeMas:       uint64(in.FeeMas),
		Nonce:        uint64(in.Nonce),
		ChainID:      in.ChainID,
		ExpiryHeight: uint64(in.ExpiryHeight),
	}

	// 적용 시도
	if err := eng.ApplyTransfer(t); err != nil {
		reason := "apply:" + err.Error()
		rc := eng.StoreReceipt(t, false, reason)
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      false,
			"applied": false,
			"reason":  reason,
			"receipt": rc,
		})
		return
	}

	// 성공 시 영수증 저장
	rc := eng.StoreReceipt(t, true, "")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"applied": true,
		"receipt": rc,
	})
}

// ============================
// R2P list (opaque cursor 없이 단순 최신순)
// ============================

func r2pListHandlerOpaque(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if os.Getenv("THOMAS_FEAT_R2P") != "1" {
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "not_implemented"})
		return
	}

	owner := strings.TrimSpace(r.URL.Query().Get("owner"))
	if owner == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "missing_owner"})
		return
	}
	ownerAddr, _, _, ok := resolveAddressMaybeAlias(eng, owner)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "bad_owner"})
		return
	}

	role := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("role")))
	if role == "" {
		role = "outbox"
	}
	if role != "outbox" && role != "inbox" && role != "all" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "bad_role"})
		return
	}

	state := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("state")))
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if state != "" && status != "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "conflicting_state_status"})
		return
	}
	normalizeStatus := func(s string) (string, bool) {
		switch s {
		case "open", "paid", "declined", "canceled":
			return s, true
		case "approved":
			return "paid", true
		default:
			return "", false
		}
	}

	wantStatus := ""
	if status != "" {
		if ws, ok := normalizeStatus(status); ok {
			wantStatus = ws
		} else {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "bad_status"})
			return
		}
	} else if state != "" {
		if state == "all" {
			wantStatus = ""
		} else if ws, ok := normalizeStatus(state); ok {
			wantStatus = ws
		} else {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "bad_state"})
			return
		}
	} else {
		// 기본은 open만
		wantStatus = "open"
	}

	// from/to 필터
	fromFilter := ""
	if v := strings.TrimSpace(r.URL.Query().Get("from")); v != "" {
		if addr, _, _, ok := resolveAddressMaybeAlias(eng, v); ok {
			fromFilter = addr
		} else {
			fromFilter = v
		}
	}
	toFilter := ""
	if v := strings.TrimSpace(r.URL.Query().Get("to")); v != "" {
		if addr, _, _, ok := resolveAddressMaybeAlias(eng, v); ok {
			toFilter = addr
		} else {
			toFilter = v
		}
	}

	// 수집 + 필터 + 정렬
	r2pMu.Lock()
	out := make([]*r2pRecord, 0, len(r2pStore))
	for _, rec := range r2pStore {
		// role
		if role == "outbox" && rec.From != ownerAddr {
			continue
		}
		if role == "inbox" && rec.To != ownerAddr {
			continue
		}
		if role == "all" && !(rec.From == ownerAddr || rec.To == ownerAddr) {
			continue
		}
		// status
		if wantStatus != "" && rec.Status != wantStatus {
			continue
		}
		// from/to
		if fromFilter != "" && rec.From != fromFilter {
			continue
		}
		if toFilter != "" && rec.To != toFilter {
			continue
		}
		out = append(out, rec)
	}
	r2pMu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedUTC > out[j].CreatedUTC
	})

	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "records": out})
}

// ============================
// Precheck / helpers
// ============================

type bufferingWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (bw *bufferingWriter) Header() http.Header { return bw.header }

func (bw *bufferingWriter) Write(b []byte) (int, error) {
	if bw.status == 0 {
		bw.status = http.StatusOK
	}
	return bw.body.Write(b)
}

func (bw *bufferingWriter) WriteHeader(status int) { bw.status = status }

func copyHeader(dst, src http.Header) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

type txPre struct {
	Type          int    `json:"type"`
	From          string `json:"from"`
	To            string `json:"to"`
	AmountMas     int64  `json:"amount_mas"`
	FeeMas        int64  `json:"fee_mas"`
	Nonce         int64  `json:"nonce"`
	ChainID       string `json:"chain_id"`
	ExpiryHeight  int64  `json:"expiry_height"`
	MsgCommitment string `json:"msg_commitment"`
	Sig           string `json:"sig,omitempty"`
}

func txPrecheckWith(next http.Handler, getHeight func() int64) http.Handler {
	next = jsonizeSigErrors(next)
	next = withJSONErrorFallback(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/tx" {
			buf, _ := io.ReadAll(r.Body)
			r.Body.Close()
			var in txPre
			if err := json.Unmarshal(buf, &in); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "malformed_json"})
				return
			}
			errs := make([]string, 0, 4)
			if in.ChainID != "" && in.ChainID != expectedChainID {
				errs = append(errs, "bad_chain_id")
			}
			if in.AmountMas <= 0 {
				errs = append(errs, "amount_le_0")
			}
			expFee := int64(minFeeMas(func() uint64 {
				if in.AmountMas > 0 {
					return uint64(in.AmountMas)
				}
				return 1
			}(), feeBps))
			if in.FeeMas < expFee {
				errs = append(errs, "fee_below_min")
			}
			if len(in.From) < 4 || len(in.To) < 4 || in.From[:4] != "tho1" || in.To[:4] != "tho1" {
				errs = append(errs, "addr_format")
			}
			h := getHeight()
			if in.ExpiryHeight > 0 && uint64(in.ExpiryHeight) <= uint64(h) {
				errs = append(errs, "expired_height")
			}
			if len(errs) > 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":                false,
					"errors":            errs,
					"reason":            "tx_precheck_failed",
					"expected_fee_mas":  expFee,
					"expected_chain_id": expectedChainID,
					"current_height":    h,
				})
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(buf))
		}
		next.ServeHTTP(w, r)
	})
}

func calcCommit(in txPre) string {
	s := fmt.Sprintf("%d|%s|%s|%d|%d|%d|%s|%d", in.Type, in.From, in.To, in.AmountMas, in.FeeMas, in.Nonce, in.ChainID, in.ExpiryHeight)
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func precheckCommit(next http.Handler) http.Handler {
	must := os.Getenv("THOMAS_REQUIRE_COMMIT") == "1"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/tx" {
			buf, _ := io.ReadAll(r.Body)
			r.Body.Close()
			var in txPre
			if err := json.Unmarshal(buf, &in); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "malformed_json"})
				return
			}
			expected := calcCommit(in)
			if in.MsgCommitment == "" {
				if must {
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "commitment_required", "expected_message": expected})
					return
				}
			} else if must && in.MsgCommitment != expected {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "bad_commitment", "expected_message": expected})
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(buf))
		}
		next.ServeHTTP(w, r)
	})
}

func precheckSig(next http.Handler) http.Handler {
	must := os.Getenv("THOMAS_VERIFY_SIG") == "1"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/tx" {
			buf, _ := io.ReadAll(r.Body)
			r.Body.Close()
			var in txPre
			if err := json.Unmarshal(buf, &in); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "malformed_json"})
				return
			}
			commit := calcCommit(in)
			pkB64 := r.Header.Get("X-PubKey")
			sgB64 := r.Header.Get("X-Sig")
			if sgB64 == "" && in.Sig != "" {
				sgB64 = in.Sig
			}
			if pkB64 == "" || sgB64 == "" {
				if must {
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "signature_required", "expected_message": commit})
					return
				}
			} else {
				pk, e1 := base64.StdEncoding.DecodeString(pkB64)
				sg, e2 := base64.StdEncoding.DecodeString(sgB64)
				if e1 != nil || e2 != nil || len(pk) != ed25519.PublicKeySize || len(sg) != ed25519.SignatureSize {
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "bad_signature_encoding"})
					return
				}
				if !ed25519.Verify(ed25519.PublicKey(pk), []byte(commit), sg) {
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "bad_signature", "expected_message": commit})
					return
				}
			}
			r.Body = io.NopCloser(bytes.NewReader(buf))
		}
		next.ServeHTTP(w, r)
	})
}

func withJSONErrorFallback(next http.Handler) http.Handler { return next }

func jsonizeSigErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap := &bufferingWriter{header: make(http.Header)}
		next.ServeHTTP(cap, r)

		if r.URL.Path != "/tx" {
			copyHeader(w.Header(), cap.header)
			if cap.status != 0 {
				w.WriteHeader(cap.status)
			}
			_, _ = w.Write(cap.body.Bytes())
			return
		}

		// /tx가 아닌 400이면 원본 그대로 통과
		if cap.status != http.StatusBadRequest {
			copyHeader(w.Header(), cap.header)
			if cap.status != 0 {
				w.WriteHeader(cap.status)
			}
			_, _ = w.Write(cap.body.Bytes())
			return
		}

		// 400인 경우 실제 reason이 "서명 관련"인지 확인
		var parsed map[string]any
		if json.Unmarshal(cap.body.Bytes(), &parsed) == nil {
			if s, _ := parsed["reason"].(string); s == "bad_signature" ||
				s == "bad_signature_encoding" || s == "signature_required" ||
				s == "verify:bad_signature" {
				incBadSigJSON()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok": true, "applied": false, "reason": "verify:bad_signature",
				})
				return
			}
		}

		// 서명 관련이 아니면 원본 400 그대로 넘김
		copyHeader(w.Header(), cap.header)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(cap.body.Bytes())
	})
}

func precheckFromBinding(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/tx" {
			buf, _ := io.ReadAll(r.Body)
			r.Body.Close()
			var in txPre
			if err := json.Unmarshal(buf, &in); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "malformed_json"})
				return
			}
			envKey := "THOMAS_PUBKEY_" + in.From
			expected := os.Getenv(envKey)
			provided := r.Header.Get("X-PubKey")
			if expected != "" && provided != "" {
				if provided != expected {
					incBindMismatch()
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "applied": false, "reason": "verify:from_pub_mismatch"})
					return
				}
			}
			r.Body = io.NopCloser(bytes.NewReader(buf))
		}
		next.ServeHTTP(w, r)
	})
}

// From-PubKey 바인딩(엔진 기반 검증; 리플렉션)
func precheckFromBindingWith(eng *app.Engine) func(next http.Handler) http.Handler {
	must := os.Getenv("THOMAS_REQUIRE_FROM_PUBKEY") == "1"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/tx" {
				buf, _ := io.ReadAll(r.Body)
				r.Body.Close()
				var in txPre
				if err := json.Unmarshal(buf, &in); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "malformed_json"})
					return
				}
				pkB64 := r.Header.Get("X-PubKey")
				if pkB64 == "" {
					if must {
						w.WriteHeader(http.StatusBadRequest)
						_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "from_pubkey_required"})
						return
					}
				} else {
					pk, err := base64.StdEncoding.DecodeString(pkB64)
					if err != nil || len(pk) != ed25519.PublicKeySize {
						w.WriteHeader(http.StatusBadRequest)
						_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "bad_pubkey_encoding"})
						return
					}
					implemented, ok := verifyFromBindingReflect(eng, in.From, pk)
					if !implemented {
						if must {
							w.WriteHeader(http.StatusNotImplemented)
							_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "from_binding_not_implemented"})
							return
						}
					} else if !ok {
						incBindMismatch()
						w.WriteHeader(http.StatusBadRequest)
						_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "from_pubkey_mismatch"})
						return
					}
				}
				r.Body = io.NopCloser(bytes.NewReader(buf))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func verifyFromBindingReflect(eng *app.Engine, addr string, pk []byte) (bool, bool) {
	rv := reflect.ValueOf(eng)

	// 1) (addr, pk) → bool
	boolCands := []string{"VerifyFromBinding", "AddressOwnsPubKey", "OwnsPubKey", "VerifySender"}
	for _, name := range boolCands {
		m := rv.MethodByName(name)
		if !m.IsValid() {
			continue
		}
		if m.Type().NumIn() == 2 &&
			m.Type().In(0).Kind() == reflect.String &&
			m.Type().In(1).Kind() == reflect.Slice &&
			m.Type().In(1).Elem().Kind() == reflect.Uint8 &&
			m.Type().NumOut() >= 1 && m.Type().Out(0).Kind() == reflect.Bool {
			outs := m.Call([]reflect.Value{reflect.ValueOf(addr), reflect.ValueOf(pk)})
			return true, outs[0].Bool()
		}
		if m.Type().NumIn() == 2 &&
			m.Type().In(0).Kind() == reflect.String &&
			m.Type().In(1).Kind() == reflect.String &&
			m.Type().NumOut() >= 1 && m.Type().Out(0).Kind() == reflect.Bool {
			outs := m.Call([]reflect.Value{
				reflect.ValueOf(addr),
				reflect.ValueOf(base64.StdEncoding.EncodeToString(pk)),
			})
			return true, outs[0].Bool()
		}
	}

	// 2) (addr) → pubkey 비교
	getCands := []string{"GetAccountPubKey", "AccountPubKey", "PubKeyOf", "PubKeyByAddress"}
	for _, name := range getCands {
		m := rv.MethodByName(name)
		if !m.IsValid() {
			continue
		}
		if m.Type().NumIn() == 1 && m.Type().In(0).Kind() == reflect.String && m.Type().NumOut() >= 1 {
			out := m.Call([]reflect.Value{reflect.ValueOf(addr)})[0].Interface()
			switch v := out.(type) {
			case []byte:
				return true, reflect.DeepEqual(v, pk)
			case string:
				if b, err := hex.DecodeString(v); err == nil {
					return true, reflect.DeepEqual(b, pk)
				}
				if b, err := base64.StdEncoding.DecodeString(v); err == nil {
					return true, reflect.DeepEqual(b, pk)
				}
			}
		}
	}
	return false, false
}

// ===== pprof no-op 대체 =====

type pprofG interface {
	WriteTo(io.Writer, int) error
}

func pprofGoroutine() pprofG { return pprofLookup("goroutine") }

func pprofLookup(name string) pprofG { return noopPprof{} }

type noopPprof struct{}

func (noopPprof) WriteTo(w io.Writer, _ int) error { _, _ = w.Write([]byte("")); return nil }

// ============================
// 钱包 생성/복구
// ============================

// POST /wallet/create
func walletCreateHandler(w http.ResponseWriter, r *http.Request) {
	setSensitiveNoStoreHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Passphrase string `json:"passphrase"`
		Label      string `json:"label"`
	}
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &in)

	b, err := mycrypto.NewRecoveryBundle(strings.TrimSpace(in.Passphrase))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "gen_failed"})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":         true,
		"algo":       "ed25519",
		"pubkey_hex": b.PubKeyHex,
		"recovery": map[string]any{
			"mnemonic":        b.Mnemonic,
			"fingerprint":     b.Fingerprint,
			"passphrase_used": in.Passphrase != "",
		},
		"note": "Store this mnemonic offline. It will not be shown again.",
	})
}

// POST /wallet/restore
func walletRestoreHandler(w http.ResponseWriter, r *http.Request) {
	setSensitiveNoStoreHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Mnemonic   string `json:"mnemonic"`
		Passphrase string `json:"passphrase"`
	}
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &in); err != nil || strings.TrimSpace(in.Mnemonic) == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "bad_json"})
		return
	}
	pub, _, err := mycrypto.DeriveFromMnemonic(strings.TrimSpace(in.Mnemonic), strings.TrimSpace(in.Passphrase))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "derive_failed"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":         true,
		"algo":       "ed25519",
		"pubkey_hex": hexLower(pub),
	})
}

// GET /tx?hash=...  or  /tx/get?hash=...
func txGetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	hash := strings.TrimSpace(r.URL.Query().Get("hash"))
	if hash == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "missing_hash"})
		return
	}

	r2pMu.Lock()
	for _, rec := range r2pStore {
		if rec.TxHash != "" && strings.EqualFold(rec.TxHash, hash) {
			out := map[string]any{
				"ok":   true,
				"hash": rec.TxHash,
				"kind": "r2p_payment",
				"r2p": map[string]any{
					"id":          rec.ID,
					"from":        rec.From,
					"to":          rec.To,
					"amount_mas":  rec.AmountMas,
					"memo":        rec.Memo,
					"status":      rec.Status,
					"created_utc": rec.CreatedUTC,
				},
			}
			if rec.PaidUTC > 0 {
				out["r2p"].(map[string]any)["paid_utc"] = rec.PaidUTC
			}
			r2pMu.Unlock()
			_ = json.NewEncoder(w).Encode(out)
			return
		}
	}
	r2pMu.Unlock()

	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "not_found"})
}

// hexLower: bytes → lower hex
func hexLower(b []byte) string {
	const hexdigits = "0123456789abcdef"
	dst := make([]byte, len(b)*2)
	for i, v := range b {
		dst[i*2] = hexdigits[v>>4]
		dst[i*2+1] = hexdigits[v&0x0f]
	}
	return string(dst)
}

func devFreeR2PEnabled(r *http.Request) bool {
	if os.Getenv("THOMAS_DEV_FREE_R2P") != "1" {
		return false
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	return true
}

func isInsufficientFunds(err error, reason string) bool {
	s := strings.ToLower(reason)
	if s == "" && err != nil {
		s = strings.ToLower(err.Error())
	}
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")
	return strings.Contains(s, "insufficient_funds")
}
