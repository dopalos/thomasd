package rpc

import (










    "encoding/hex"
    "encoding/json"
    "io"
    "log"
    "net/http"
    "runtime/pprof"
    "strconv"
    "strings"
    "time"

    "thomasd/internal/app"
    "thomasd/internal/codec"
    "thomasd/internal/tx"
	"runtime"
	"reflect"
	"fmt"
	"sync"
	"bytes"
	"os"
	"crypto/sha256"
	"encoding/base64"
	"crypto/ed25519"

	"expvar")
 


 


 
// AUTO: TX Precheck middleware (chain_id / expiry / min fee_bps)



type txPre struct {
  Type         int    `json:"type"`
  From         string `json:"from"`
  To           string `json:"to"`
  AmountMas    int64  `json:"amount_mas"`
  FeeMas       int64  `json:"fee_mas"`
  Nonce        int64  `json:"nonce"`
  ChainID      string `json:"chain_id"`
  ExpiryHeight int64  `json:"expiry_height"`
  MsgCommitment string `json:"msg_commitment"`
  Sig          string `json:"sig,omitempty"`
}

func minFeeMas(amount int64, bps int) int64 {
  f := (amount * int64(bps)) / 10000
  if f < 1 { f = 1 }
  return f
}



 
// AUTO: debuglog rate limit (token bucket)
var dbgMu sync.Mutex
var dbgTokens int64
var dbgCap int64 = 100          // burst
var dbgRefillPerSec float64 = 50 // refill per second
var dbgLast time.Time

func dbgRefill(now time.Time) {
    if dbgLast.IsZero() {
        dbgLast = now
        dbgTokens = dbgCap
        return
    }
    elapsed := now.Sub(dbgLast).Seconds()
    if elapsed <= 0 { return }
    add := int64(elapsed * dbgRefillPerSec)
    if add > 0 {
        dbgTokens += add
        if dbgTokens > dbgCap { dbgTokens = dbgCap }
        dbgLast = now
    }
}
func dbgAllow() bool {
    dbgMu.Lock()
    defer dbgMu.Unlock()
    dbgRefill(time.Now())
    if dbgTokens > 0 {
        dbgTokens--
        return true
    }
    return false
}
func debuglogRateLimit(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path == "/tx" && r.Method == http.MethodPost && r.URL.Query().Get("debuglog") == "1" {
            if !dbgAllow() {
                w.Header().Set("Content-Type","application/json")
                w.Header().Set("Retry-After","1")
                w.WriteHeader(http.StatusTooManyRequests)
                _ = json.NewEncoder(w).Encode(map[string]any{
                    "error":"rate_limited",
                    "retry_after_ms": 1000,
                })
                return
            }
        }
        next.ServeHTTP(w, r)
    })
}

var bootTime = time.Now()


const (
    feeBPS          = 10 // 0.1%
    allowedChainID  = "thomas-dev-1"
    masPerTHO       = 10_000_000
    masPerMicro     = 10
    maxMsgCommitLen = 64
)

func NewRouter(eng *app.Engine) http.Handler {




    mux := http.NewServeMux()





    mux.Handle("/debug/vars", expvar.Handler())
    // --- Debug: echo ---
    mux.HandleFunc("/debug/echo", func(w http.ResponseWriter, r *http.Request) {
        b, _ := io.ReadAll(r.Body)
        w.Header().Set("Content-Type", "application/octet-stream")
        w.Write(b)
    })

    // --- Debug: stack dump ---
    mux.HandleFunc("/debug/stack", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
            w.WriteHeader(http.StatusMethodNotAllowed)
            return
        }
        w.Header().Set("Content-Type", "text/plain; charset=utf-8")
        _ = pprof.Lookup("goroutine").WriteTo(w, 2)
    })

    // --- Health ---
    mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]string{
            "status":   "ok",
            "time_utc": time.Now().UTC().Format(time.RFC3339),
        })
    })

    // --- SSE ---
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
                w.Write([]byte("event: push\n"))
                w.Write([]byte("data: "))
                w.Write(msg)
                w.Write([]byte("\n\n"))
                fl.Flush()
            }
        }
    })

    // --- Node info ---
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

    // --- Height ---
    mux.HandleFunc("/height", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
            w.WriteHeader(http.StatusMethodNotAllowed)
            return
        }
        h := eng.CurrentHeight()
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{"height": h})
    })

    // --- Policy ---
    mux.HandleFunc("/policy", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
            w.WriteHeader(http.StatusMethodNotAllowed)
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

    // --- Account ---
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

    // --- Nonce ---
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

    // --- Merkle ---
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

    // --- TX 조회: GET /tx/{hash} ---
    mux.HandleFunc("/tx/", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
            w.WriteHeader(http.StatusMethodNotAllowed)
            return
        }
        h := strings.TrimPrefix(r.URL.Path, "/tx/")
        w.Header().Set("Content-Type", "application/json")
        if h == "" {
            w.WriteHeader(http.StatusBadRequest)
            _ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_hash"})
            return
        }
        rec, ok := eng.GetReceipt(h)
        if !ok {
            w.WriteHeader(http.StatusNotFound)
            _ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
            return
        }
        _ = json.NewEncoder(w).Encode(rec)
    })

    // --- Supply snapshot ---
    mux.HandleFunc("/supply/current", func(w http.ResponseWriter, r *http.Request) {
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

    // --- Minting (라이트) ---
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

    // --- Rounds ---
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

    // --- TX 제출: POST /tx ---
    mux.HandleFunc("/tx", func(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        w.WriteHeader(http.StatusMethodNotAllowed)
        return
    }

    // 디버그 숏컷
    if r.URL.Query().Get("debug") == "ping" {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "reason": "ping"})
        return
    }

    b, _ := io.ReadAll(r.Body)
    ct := strings.ToLower(r.Header.Get("Content-Type"))
    if strings.Contains(r.URL.RawQuery, "debuglog=1") { log.Printf("/tx recv ct=%q len=%d", ct, len(b)) }// 디버그: 파싱 스킵 (절대 블로킹 금지)
    if r.URL.Query().Get("debug") == "skipapply" {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{
            "status":  "queued",
            "parsed":  false,
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
        "expected_fee_mas": expFeeMas, "root_deferred": true,
        "receipts_count":   eng.ReceiptCount(),
    })
    })

    
    // GET /stats : 러닝 상태 요약
    mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
            w.WriteHeader(http.StatusMethodNotAllowed)
            return
        }
        w.Header().Set("Content-Type","application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{
            "height":         eng.CurrentHeight(),
            "receipts_count": eng.ReceiptCount(),
            "time_utc":       time.Now().UTC().Format(time.RFC3339),
        })
    })

    // AUTO: /stats.json(JSON)
    mux.HandleFunc("/stats.json", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet { w.WriteHeader(http.StatusMethodNotAllowed); return }
        w.Header().Set("Content-Type","application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{
            "height":         eng.CurrentHeight(),
            "receipts_count": eng.ReceiptCount(),
            "time_utc":       time.Now().UTC().Format(time.RFC3339),
        })
    })

    // AUTO: /stats.sys (system metrics)
    mux.HandleFunc("/stats.sys", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet { w.WriteHeader(http.StatusMethodNotAllowed); return }
        var ms runtime.MemStats
        runtime.ReadMemStats(&ms)
        up := time.Since(bootTime).Seconds()
        w.Header().Set("Content-Type","application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{
            "height":         eng.CurrentHeight(),
            "receipts_count": eng.ReceiptCount(),
            "time_utc":       time.Now().UTC().Format(time.RFC3339),
            "uptime_secs":    int64(up),
            "goroutines":     runtime.NumGoroutine(),
            "mem_alloc_kb":   int64(ms.Alloc/1024),
        })
    })

    // AUTO: /stats.plus — base + optional extras via reflection
        // AUTO: /metrics — minimal Prometheus text exposition

    // AUTO: /stats.plus — base + optional extras via reflection (extended)
    mux.HandleFunc("/stats.plus", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet { w.WriteHeader(http.StatusMethodNotAllowed); return }
        extras := map[string]any{}
        rv := reflect.ValueOf(eng)

        // helpers
        call0 := func(name string) (any, bool) {
            m := rv.MethodByName(name)
            if !m.IsValid() || m.Type().NumIn() != 0 || m.Type().NumOut() == 0 { return nil, false }
            outs := m.Call(nil)
            return outs[0].Interface(), true
        }
        // try multiple name variants
        tryNames := func(names ...string) (any, bool) {
            for _, n := range names {
                if v, ok := call0(n); ok { return v, true }
            }
            return nil, false
        }

        if v, ok := tryNames("MerkleRoot","TxRoot"); ok { extras["tx_root"] = v }
        if v, ok := tryNames("LastBlockHash","HeadHash","BlockHash"); ok { extras["last_block_hash"] = v }
        if v, ok := tryNames("LastTxHash","RecentTxHash"); ok { extras["last_tx_hash"] = v }
        if v, ok := tryNames("MempoolSize","MempoolLen","BacklogSize","PendingCount"); ok { extras["backlog_size"] = v }
        if v, ok := tryNames("ValidatorsLen","ValidatorCount","NumValidators"); ok { extras["validators_len"] = v }

        resp := map[string]any{
            "height":         eng.CurrentHeight(),
            "receipts_count": eng.ReceiptCount(),
            "time_utc":       time.Now().UTC().Format(time.RFC3339),
        }
        for k, v := range extras { resp[k] = v }

        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(resp)
    })
    // AUTO: /metrics — Prometheus text (extended if available)
    mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet { w.WriteHeader(http.StatusMethodNotAllowed); return }
        var ms runtime.MemStats
        runtime.ReadMemStats(&ms)

        // optional via reflection
        var mempool int64 = -1
        if v := reflect.ValueOf(eng).MethodByName("MempoolSize"); v.IsValid() && v.Type().NumIn()==0 && v.Type().NumOut()>0 {
            mv := v.Call(nil)[0]
            switch mv.Kind() {
            case reflect.Int, reflect.Int64, reflect.Int32: mempool = mv.Int()
            case reflect.Uint, reflect.Uint64, reflect.Uint32: mempool = int64(mv.Uint())
            }
        } else if v := reflect.ValueOf(eng).MethodByName("MempoolLen"); v.IsValid() && v.Type().NumIn()==0 && v.Type().NumOut()>0 {
            mv := v.Call(nil)[0]
            if mv.Kind()==reflect.Int || mv.Kind()==reflect.Int64 || mv.Kind()==reflect.Int32 { mempool = mv.Int() }
        }

        var validators int64 = -1
        if v := reflect.ValueOf(eng).MethodByName("ValidatorsLen"); v.IsValid() && v.Type().NumIn()==0 && v.Type().NumOut()>0 {
            mv := v.Call(nil)[0]
            if mv.Kind()==reflect.Int || mv.Kind()==reflect.Int64 || mv.Kind()==reflect.Int32 { validators = mv.Int() }
        }

        fmt.Fprintf(w, "# TYPE thomas_height gauge\nthomas_height %d\n", eng.CurrentHeight())
        fmt.Fprintf(w, "# TYPE thomas_receipts_total counter\nthomas_receipts_total %d\n", eng.ReceiptCount())
        fmt.Fprintf(w, "# TYPE thomas_uptime_seconds gauge\nthomas_uptime_seconds %d\n", int64(time.Since(bootTime).Seconds()))
        fmt.Fprintf(w, "# TYPE thomas_goroutines gauge\nthomas_goroutines %d\n", runtime.NumGoroutine())
        fmt.Fprintf(w, "# TYPE thomas_mem_alloc_bytes gauge\nthomas_mem_alloc_bytes %d\n", ms.Alloc)
                readyVal := 0
        if eng.CurrentHeight() > 0 { readyVal = 1 }
        fmt.Fprintf(w, "# TYPE thomas_ready gauge\nthomas_ready %d\n", readyVal)
        if mempool >= 0 { fmt.Fprintf(w, "# TYPE thomas_mempool_size gauge\nthomas_mempool_size %d\n", mempool) }
        if validators >= 0 { fmt.Fprintf(w, "# TYPE thomas_validators gauge\nthomas_validators %d\n", validators) }
    })

    // AUTO: /readyz — readiness probe (net/http)
    mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet { w.WriteHeader(http.StatusMethodNotAllowed); return }
        h := eng.CurrentHeight()
        status := "starting"
        if h > 0 { status = "ready" }
        resp := map[string]any{
            "status":      status,
            "height":      h,
            "time_utc":    time.Now().UTC().Format(time.RFC3339),
            "uptime_secs": int64(time.Since(bootTime).Seconds()),
        }
        w.Header().Set("Content-Type","application/json")
        _ = json.NewEncoder(w).Encode(resp)
    })

    // AUTO: rate limit status
    mux.HandleFunc("/ratelimit.debuglog", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet { w.WriteHeader(http.StatusMethodNotAllowed); return }
        dbgMu.Lock()
        cap := dbgCap; tokens := dbgTokens; rps := dbgRefillPerSec
        dbgMu.Unlock()
        w.Header().Set("Content-Type","application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{
            "capacity": cap,
            "refill_per_sec": rps,
            "tokens": tokens,
            "time_utc": time.Now().UTC().Format(time.RFC3339),
        })
    })
return debuglogRateLimit(txPrecheckWith(precheckSig(precheckFromBinding(precheckCommit(mux))), func() int64 { return int64(eng.CurrentHeight()) }))
}

// (옵션) SSE용 인터페이스
type sseAdapter interface {
    SubscribeForSSE() chan []byte
    UnsubscribeForSSE(ch chan []byte)
}




















//// === AUTO: MISSING PRECHECKS (safe minimal) ===


var expectedChainID = func() string {
    if v := os.Getenv("THOMAS_CHAIN_ID"); v != "" { return v }
    return "thomas-dev-1"
}()
var feeBps = func() int {
    if v := os.Getenv("THOMAS_FEE_BPS"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n > 0 { return n }
    }
    return 0
}()

func txPrecheckWith(next http.Handler, getHeight func() int64) http.Handler {
    next = jsonizeSigErrors(next)
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method==http.MethodPost && r.URL.Path=="/tx" {
            buf,_ := io.ReadAll(r.Body); r.Body.Close()
            var in txPre
            if err:=json.Unmarshal(buf,&in); err!=nil {
                w.WriteHeader(http.StatusBadRequest)
                _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"malformed_json"})
                return
            }
            errs:=make([]string,0,4)
            if in.ChainID!="" && in.ChainID!=expectedChainID { errs=append(errs,"bad_chain_id") }
            if in.AmountMas<=0 { errs=append(errs,"amount_le_0") }
            expFee := minFeeMas(in.AmountMas, feeBps)
            if in.FeeMas<expFee { errs=append(errs,"fee_below_min") }
            if len(in.From)<4 || len(in.To)<4 || in.From[:4]!="tho1" || in.To[:4]!="tho1" { errs=append(errs,"addr_format") }
            h := getHeight(); if in.ExpiryHeight>0 && in.ExpiryHeight<=h { errs=append(errs,"expired_height") }
            if len(errs)>0 {
                w.Header().Set("Content-Type","application/json")
                w.WriteHeader(http.StatusBadRequest)
                _ = json.NewEncoder(w).Encode(map[string]any{
                    "ok":false,"reason":"tx_precheck_failed","errors":errs,
                    "expected_fee_mas":expFee,"expected_chain_id":expectedChainID,"current_height":h,
                })
                return
            }
            r.Body = io.NopCloser(bytes.NewReader(buf))
        }
        next.ServeHTTP(w,r)
    })
}

func calcCommit(in txPre) string {
    s := fmt.Sprintf("%d|%s|%s|%d|%d|%d|%s|%d", in.Type,in.From,in.To,in.AmountMas,in.FeeMas,in.Nonce,in.ChainID,in.ExpiryHeight)
    sum := sha256.Sum256([]byte(s))
    return hex.EncodeToString(sum[:])
}

func precheckCommit(next http.Handler) http.Handler {
    must := os.Getenv("THOMAS_REQUIRE_COMMIT") == "1"
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method==http.MethodPost && r.URL.Path=="/tx" {
            buf,_ := io.ReadAll(r.Body); r.Body.Close()
            var in txPre
            if err:=json.Unmarshal(buf,&in); err!=nil {
                w.WriteHeader(http.StatusBadRequest)
                _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"malformed_json"})
                return
            }
            expected := calcCommit(in)
            if in.MsgCommitment=="" {
                if must {
                    w.WriteHeader(http.StatusBadRequest)
                    _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"commitment_required","expected_message":expected})
                    return
                }
            } else if in.MsgCommitment!=expected {
                w.WriteHeader(http.StatusBadRequest)
                _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"bad_commitment","expected_message":expected})
                return
            }
            r.Body = io.NopCloser(bytes.NewReader(buf))
        }
        next.ServeHTTP(w,r)
    })
}

func precheckSig(next http.Handler) http.Handler {
    must := os.Getenv("THOMAS_VERIFY_SIG") == "1"
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method==http.MethodPost && r.URL.Path=="/tx" {
            buf,_ := io.ReadAll(r.Body); r.Body.Close()
            var in txPre
            if err:=json.Unmarshal(buf,&in); err!=nil {
                w.WriteHeader(http.StatusBadRequest)
                _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"malformed_json"})
                return
            }
            commit := calcCommit(in)
            pkB64 := r.Header.Get("X-PubKey")
            sgB64 := r.Header.Get("X-Sig")
            if sgB64=="" && in.Sig!="" { sgB64=in.Sig }
            if pkB64=="" || sgB64=="" {
                if must {
                    w.WriteHeader(http.StatusBadRequest)
                    _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"signature_required","expected_message":commit})
                    return
                }
            } else {
                pk, e1 := base64.StdEncoding.DecodeString(pkB64)
                sg, e2 := base64.StdEncoding.DecodeString(sgB64)
                if e1!=nil || e2!=nil || len(pk)!=ed25519.PublicKeySize || len(sg)!=ed25519.SignatureSize {
                    w.WriteHeader(http.StatusBadRequest)
                    _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"bad_signature_encoding"})
                    return
                }
                if !ed25519.Verify(ed25519.PublicKey(pk), []byte(commit), sg) {
                    w.WriteHeader(http.StatusBadRequest)
                    _ = json.NewEncoder(w).Encode(map[string]any{"ok":false,"reason":"bad_signature","expected_message":commit})
                    return
                }
            }
            r.Body = io.NopCloser(bytes.NewReader(buf))
        }
        next.ServeHTTP(w,r)
    })
}
//// === END AUTO ===








