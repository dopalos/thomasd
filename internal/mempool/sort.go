package mempool

import (
    "sort"
    "sync"
    "time"

    "github.com/zeebo/blake3"
    "thomasd/internal/codec"
    "thomasd/internal/tx"
)

// --------- 결정성 올림 나눗셈(ceil) ---------
func ceilDiv(a, b uint64) uint64 {
    if b == 0 {
        return a
    }
    q := a / b
    if a%b != 0 {
        q++
    }
    return q
}

// --------- 토큰버킷 (Q16.16 정수 고정소수점) ---------
type TokenBucket struct {
    rateQ16   uint64 // 초당 토큰 * 2^16
    burstQ16  uint64 // 최대 토큰 * 2^16
    tokensQ16 uint64 // 현재 토큰 * 2^16
    lastNs    int64  // 마지막 업데이트 시각(ns)
}

func newBucket(ratePerSec, burst uint32, nowNs int64) *TokenBucket {
    const q = uint64(1) << 16
    return &TokenBucket{
        rateQ16:   uint64(ratePerSec) * q,
        burstQ16:  uint64(burst) * q,
        tokensQ16: uint64(burst) * q,
        lastNs:    nowNs,
    }
}

func (tb *TokenBucket) allow(nowNs int64) bool {
    if nowNs < tb.lastNs {
        nowNs = tb.lastNs
    }
    elapsed := uint64(nowNs - tb.lastNs) // ns
    tb.tokensQ16 += (elapsed * tb.rateQ16) / 1_000_000_000
    if tb.tokensQ16 > tb.burstQ16 {
        tb.tokensQ16 = tb.burstQ16
    }
    const one = uint64(1) << 16
    if tb.tokensQ16 < one {
        tb.lastNs = nowNs
        return false
    }
    tb.tokensQ16 -= one
    tb.lastNs = nowNs
    return true
}

// --------- 엔트리/멤풀 ---------
type TxEntry struct {
    Tx          tx.Transfer
    SizeBytes   uint64
    ReceivedSeq uint64 // 단조 증가 시퀀스(결정성 tie-break)
}

type Mempool struct {
    mu       sync.RWMutex
    entries  []TxEntry
    limits   map[string]*TokenBucket
    seq      uint64
    defRate  uint32
    defBurst uint32
}

func NewMempool(ratePerSec, burst uint32) *Mempool {
    return &Mempool{
        entries:  make([]TxEntry, 0),
        limits:   make(map[string]*TokenBucket),
        seq:      0,
        defRate:  ratePerSec,
        defBurst: burst,
    }
}

// 내부에서 CBOR로 사이즈 계산 → 외부 입력에 시간/사이즈 안 씀(결정성)
func (m *Mempool) AddTx(t tx.Transfer) (bool, string) {
    m.mu.Lock()
    defer m.mu.Unlock()

    now := time.Now().UnixNano()
    bucket, ok := m.limits[t.From]
    if !ok {
        bucket = newBucket(m.defRate, m.defBurst, now)
        m.limits[t.From] = bucket
    }
    if !bucket.allow(now) {
        return false, "rate_limit_exceeded"
    }

    // 직렬화 사이즈(0 방지)
    b, _ := codec.EncodeCBORCanonical(t)
    size := uint64(len(b))
    if size == 0 {
        size = 1
    }

    m.seq++
    m.entries = append(m.entries, TxEntry{
        Tx:          t,
        SizeBytes:   size,
        ReceivedSeq: m.seq,
    })
    return true, ""
}

// 정렬: 1) ceil(fee/size) 내림차순 → 2) nonce 오름차순 → 3) ReceivedSeq 오름차순
func (m *Mempool) SortedEntries() []TxEntry {
    m.mu.RLock()
    defer m.mu.RUnlock()
    out := make([]TxEntry, len(m.entries))
    copy(out, m.entries)
    sort.SliceStable(out, func(i, j int) bool {
        wi := ceilDiv(out[i].Tx.FeeMas, out[i].SizeBytes)
        wj := ceilDiv(out[j].Tx.FeeMas, out[j].SizeBytes)
        if wi != wj {
            return wi > wj
        }
        if out[i].Tx.Nonce != out[j].Tx.Nonce {
            return out[i].Tx.Nonce < out[j].Tx.Nonce
        }
        return out[i].ReceivedSeq < out[j].ReceivedSeq
    })
    return out
}

// 정렬 결과 → 각 TX를 CBOR → BLAKE3-256 → hashes를 CBOR → 최종 BLAKE3-256
func (m *Mempool) Hash() [32]byte {
    sorted := m.SortedEntries()
    hashes := make([][]byte, len(sorted))
    for i, e := range sorted {
        b, _ := codec.EncodeCBORCanonical(e.Tx)
        h := blake3.Sum256(b)
        hashes[i] = h[:]
    }
    b, _ := codec.EncodeCBORCanonical(hashes)
    return blake3.Sum256(b)
}