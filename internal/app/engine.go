package app

import (
"crypto/ed25519"
    "encoding/hex"
    "encoding/json"
    "os"
    "path/filepath"
    "sort"
    "sync"
    "time"

    mycrypto "thomasd/internal/crypto"
    "thomasd/internal/merkle"
    "thomasd/internal/state"
    "thomasd/internal/tx"
    "thomasd/internal/types"
)

type Engine struct {
    mu        sync.Mutex
    txq       [][]byte
    db        *state.DB
    receipts  map[string]types.TxReceipt
    leaves    [][]byte
    height    uint64

    statePathV1 string // μTHO (legacy)
    statePathV2 string // mas

    rounds              []types.RoundHeader
    lastCommittedHeight uint64

    ledgerPath string // receipts + rounds

    // 노드 서명키
    priv ed25519.PrivateKey
    pub  ed25519.PublicKey
    keyPath string

    // SSE 구독자
    subs map[chan []byte]struct{}
}

func NewEngine() *Engine {
    db := state.NewDB()
    db.SetFeeReceiver("tho1foundation") // 수수료 재단 적립

    e := &Engine{
        db:          db,
        receipts:    make(map[string]types.TxReceipt),
        statePathV1: filepath.Join("data", "state.json"),
        statePathV2: filepath.Join("data", "state_v2.json"),
        ledgerPath:  filepath.Join("data", "ledger_v1.json"),
        keyPath:     filepath.Join("data", "node_key.json"),
        subs:        make(map[chan []byte]struct{}),
    }

    // 키 로드/생성
    if priv, pub, err := mycrypto.LoadOrCreate(e.keyPath); err == nil {
        e.priv, e.pub = priv, pub
    }

    // 계정 상태 로드 (v2 우선, 없으면 v1→변환, 둘 다 없으면 제네시스)
    if !e.loadStateMas(e.statePathV2) {
        if m, ok := e.loadStateMicro(e.statePathV1); ok {
            for k, v := range m { v.Balance *= 10; m[k] = v } // μTHO→mas
            e.db.Restore(m); _ = e.saveStateMas(e.statePathV2)
        } else {
            // ✅ 중복 없는 제네시스
            db.InitGenesis(map[string]uint64{
                "tho1alice":      2_000_000 * 10, // 2 THO = 20,000,000 mas
                "tho1foundation": 0,
                "tho1exchange":   0,
            })
            _ = e.saveStateMas(e.statePathV2)
        }
    }

    // 레저 로드
    _ = e.loadLedger(e.ledgerPath)
    return e
}

func (e *Engine) PushRawTx(b []byte) { e.mu.Lock(); defer e.mu.Unlock(); cp:=make([]byte,len(b)); copy(cp,b); e.txq=append(e.txq,cp) }
func (e *Engine) TxCount() int        { e.mu.Lock(); defer e.mu.Unlock(); return len(e.txq) }
func (e *Engine) GetAccount(addr string) state.Account { return e.db.GetAccount(addr) }
func (e *Engine) CurrentHeight() uint64 { e.mu.Lock(); defer e.mu.Unlock(); return e.height }

func (e *Engine) ApplyTransfer(t tx.Transfer) error {
    if err := e.db.ApplyTransfer(t.From, t.To, t.AmountMas, t.FeeMas, t.Nonce); err != nil { return err }
    return e.saveStateMas(e.statePathV2)
}

func (e *Engine) StoreReceipt(t tx.Transfer, applied bool, reason string) types.TxReceipt {
    e.mu.Lock()
    defer e.mu.Unlock()

    e.height++
    status := "applied"; if !applied { status = "rejected:" + reason }
    r := types.TxReceipt{
        TxHash: t.Hash(), From: t.From, To: t.To,
        Amount: t.AmountMas, Fee: t.FeeMas,
        Nonce: t.Nonce, Status: status, Height: e.height, TimeUTC: time.Now().UTC().Unix(),
    }
    b, _ := json.Marshal(r)
    e.receipts[r.TxHash] = r
    e.leaves = append(e.leaves, b)

    _ = e.saveLedger(e.ledgerPath)

    // SSE 브로드캐스트
    e.broadcastAsync("receipt", r)
    return r
}

func (e *Engine) GetReceipt(hash string) (types.TxReceipt, bool) { e.mu.Lock(); defer e.mu.Unlock(); r,ok := e.receipts[hash]; return r,ok }
func (e *Engine) MerkleRoot() []byte { e.mu.Lock(); defer e.mu.Unlock(); return merkle.Root(e.leaves) }
func (e *Engine) ReceiptCount() int  { e.mu.Lock(); defer e.mu.Unlock(); return len(e.leaves) }

// ---- 라운드 커밋/조회 ----
func (e *Engine) CommitRound() (types.RoundHeader, bool) {
    e.mu.Lock(); defer e.mu.Unlock()

    from := e.lastCommittedHeight + 1
    to := e.height
    if from > to { return types.RoundHeader{}, false }

    sub := make([][]byte, to-from+1)
    copy(sub, e.leaves[from-1:to])
    root := merkle.Root(sub)
    hdr := types.RoundHeader{
        Round:      uint64(len(e.rounds) + 1),
        FromHeight: from,
        ToHeight:   to,
        TxCount:    uint64(len(sub)),
        Root:       hex.EncodeToString(root),
        TimeUTC:    time.Now().UTC().Unix(),
    }
    if sig, ok := e.SignRoundHeader(hdr); ok {
        hdr.SignatureHex = hex.EncodeToString(sig)
    }

    e.rounds = append(e.rounds, hdr)
    e.lastCommittedHeight = to

    _ = e.saveLedger(e.ledgerPath)

    // SSE 브로드캐스트
    e.broadcastAsync("round", hdr)
    return hdr, true
}
func (e *Engine) GetRound(n uint64) (types.RoundHeader, bool) { e.mu.Lock(); defer e.mu.Unlock(); if n==0 || int(n)>len(e.rounds){return types.RoundHeader{},false}; return e.rounds[n-1],true }
func (e *Engine) LatestRound() (types.RoundHeader, bool) { e.mu.Lock(); defer e.mu.Unlock(); if len(e.rounds)==0{return types.RoundHeader{},false}; return e.rounds[len(e.rounds)-1], true }

// ---- 서명 관련 ----
func (e *Engine) PubKeyHex() string {
    if len(e.pub) == 0 { return "" }
    return hex.EncodeToString(e.pub)
}

func CanonicalRoundHeaderBytes(h types.RoundHeader) []byte {
    type canon struct {
        Round      uint64 `json:"round"`
        FromHeight uint64 `json:"from_height"`
        ToHeight   uint64 `json:"to_height"`
        TxCount    uint64 `json:"tx_count"`
        Root       string `json:"root"`
        TimeUTC    int64  `json:"time_utc"`
    }
    b, _ := json.Marshal(canon{
        Round: h.Round, FromHeight: h.FromHeight, ToHeight: h.ToHeight,
        TxCount: h.TxCount, Root: h.Root, TimeUTC: h.TimeUTC,
    })
    return b
}

func (e *Engine) SignRoundHeader(h types.RoundHeader) (sig []byte, ok bool) {
    if len(e.priv) == 0 { return nil, false }
    msg := CanonicalRoundHeaderBytes(h)
    s := ed25519.Sign(e.priv, msg)
    return s, true
}

// ---- SSE ----
func (e *Engine) subscribe() chan []byte {
    e.mu.Lock(); defer e.mu.Unlock()
    ch := make(chan []byte, 16)
    e.subs[ch] = struct{}{}
    return ch
}
func (e *Engine) unsubscribe(ch chan []byte) {
    e.mu.Lock(); defer e.mu.Unlock()
    if _, ok := e.subs[ch]; ok {
        delete(e.subs, ch)
        close(ch)
    }
}
func (e *Engine) broadcast(evtType string, payload any) {
    e.mu.Lock()
    var targets []chan []byte
    for ch := range e.subs { targets = append(targets, ch) }
    e.mu.Unlock()
    b, _ := json.Marshal(map[string]any{"type": evtType, "data": payload})
    for _, ch := range targets {
        select { case ch <- b: default: /* drop if slow */ }
    }
}

// ---- 파일 저장/로드: 계정 상태(mas) ----
func (e *Engine) saveStateMas(path string) error {
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { return err }
    snap := e.db.Snapshot()
    b, err := json.MarshalIndent(snap, "", "  "); if err != nil { return err }
    return os.WriteFile(path, b, 0o644)
}
func (e *Engine) loadStateMas(path string) bool {
    b, err := os.ReadFile(path); if err != nil { return false }
    var m map[string]state.Account
    if err := json.Unmarshal(b, &m); err != nil { return false }
    e.db.Restore(m); return true
}
func (e *Engine) loadStateMicro(path string) (map[string]state.Account, bool) {
    b, err := os.ReadFile(path); if err != nil { return nil, false }
    var m map[string]state.Account
    if err := json.Unmarshal(b, &m); err != nil { return nil, false }
    return m, true
}

// ---- 레저 저장/로드 ----
type ledgerV1 struct {
    Version             int                 `json:"version"`
    Height              uint64              `json:"height"`
    LastCommittedHeight uint64              `json:"last_committed_height"`
    Receipts            []types.TxReceipt   `json:"receipts"`
    Rounds              []types.RoundHeader `json:"rounds"`
}

func (e *Engine) saveLedger(path string) error {
    list := make([]types.TxReceipt, 0, len(e.receipts))
    for _, r := range e.receipts { list = append(list, r) }
    sort.Slice(list, func(i, j int) bool { return list[i].Height < list[j].Height })

    led := ledgerV1{
        Version:             1,
        Height:              e.height,
        LastCommittedHeight: e.lastCommittedHeight,
        Receipts:            list,
        Rounds:              append([]types.RoundHeader(nil), e.rounds...),
    }
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { return err }
    b, err := json.MarshalIndent(led, "", "  "); if err != nil { return err }
    return os.WriteFile(path, b, 0o644)
}

func (e *Engine) loadLedger(path string) error {
    b, err := os.ReadFile(path)
    if err != nil { return nil }
    var led ledgerV1
    if err := json.Unmarshal(b, &led); err != nil { return err }

    e.height = led.Height
    e.lastCommittedHeight = led.LastCommittedHeight
    e.rounds = led.Rounds
    e.receipts = make(map[string]types.TxReceipt, len(led.Receipts))
    e.leaves = e.leaves[:0]
    sort.Slice(led.Receipts, func(i, j int) bool { return led.Receipts[i].Height < led.Receipts[j].Height })
    for _, r := range led.Receipts {
        e.receipts[r.TxHash] = r
        rb, _ := json.Marshal(r)
        e.leaves = append(e.leaves, rb)
    }
    return nil
}
func (e *Engine) SubscribeForSSE() chan []byte {
    e.mu.Lock()
    if e.subs == nil {
        e.subs = make(map[chan []byte]struct{})
    }
    ch := make(chan []byte, 32)
    e.subs[ch] = struct{}{}
    e.mu.Unlock()
    return ch
}


func (e *Engine) UnsubscribeForSSE(ch chan []byte) {
    e.mu.Lock()
    if _, ok := e.subs[ch]; ok {
        delete(e.subs, ch)
        close(ch)
    }
    e.mu.Unlock()
}

 // broadcastAsync: DEADLOCK-SAFE fanout with JSON marshaling.
func (e *Engine) broadcastAsync(kind string, payload any) {
    _ = kind
    go func(v any) {
        b, err := json.Marshal(v)
        if err != nil {
            return
        }
        // snapshot under lock
        e.mu.Lock()
        chans := make([]chan []byte, 0, len(e.subs))
        for ch := range e.subs {
            chans = append(chans, ch)
        }
        e.mu.Unlock()
        // non-blocking send
        for _, ch := range chans {
            select {
            case ch <- b:
            default:
            }
        }
    }(payload)
}


