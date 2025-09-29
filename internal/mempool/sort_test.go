package mempool_test

import (
    "bytes"
    "testing"

    "thomasd/internal/mempool"
    "thomasd/internal/tx"
)

func TestSort_Basic(t *testing.T) {
    m := mempool.NewMempool(10, 50)

    // fee/size 가중치 → 200 > 100
    t1 := tx.Transfer{From: "a", To: "b", AmountMas: 1, FeeMas: 100, Nonce: 1}
    t2 := tx.Transfer{From: "a", To: "b", AmountMas: 1, FeeMas: 200, Nonce: 1}

    if ok, _ := m.AddTx(t1); !ok { t.Fatal("add t1") }
    if ok, _ := m.AddTx(t2); !ok { t.Fatal("add t2") }

    s := m.SortedEntries()
    if len(s) != 2 || s[0].Tx.FeeMas != 200 {
        t.Fatalf("fee/size sort failed: %+v", s)
    }
}

func TestSort_TieNonce(t *testing.T) {
    m := mempool.NewMempool(10, 50)

    // 동일 weight → nonce 오름차순
    t1 := tx.Transfer{From: "a", To: "b", AmountMas: 1, FeeMas: 100, Nonce: 1}
    t2 := tx.Transfer{From: "a", To: "b", AmountMas: 1, FeeMas: 100, Nonce: 2}

    _, _ = m.AddTx(t2)
    _, _ = m.AddTx(t1)

    s := m.SortedEntries()
    if s[0].Tx.Nonce != 1 || s[1].Tx.Nonce != 2 {
        t.Fatalf("nonce tie-break failed: %+v", s)
    }
}

func TestDeterministicHash(t *testing.T) {
    m1 := mempool.NewMempool(10, 50)
    m2 := mempool.NewMempool(10, 50)

    txs := []tx.Transfer{
        {From: "a", To: "b", AmountMas: 1, FeeMas: 100, Nonce: 1},
        {From: "a", To: "b", AmountMas: 1, FeeMas: 200, Nonce: 1},
    }
    for _, x := range txs {
        _, _ = m1.AddTx(x)
        _, _ = m2.AddTx(x)
    }
    for i := 0; i < 10; i++ {
        h1 := m1.Hash()
        h2 := m2.Hash()
        if !bytes.Equal(h1[:], h2[:]) {
            t.Fatalf("non-deterministic: %x vs %x", h1, h2)
        }
    }
}

func TestRateLimit(t *testing.T) {
    m := mempool.NewMempool(1, 2) // r=1/s, burst=2
    x := tx.Transfer{From: "alice", To: "bob", AmountMas: 1, FeeMas: 1, Nonce: 1}
    ok, _ := m.AddTx(x)
    if !ok { t.Fatal("first token should pass") }
    ok, _ = m.AddTx(x)
    if !ok { t.Fatal("second token should pass") }
    ok, reason := m.AddTx(x)
    if ok || reason != "rate_limit_exceeded" {
        t.Fatalf("rate limit should block third: ok=%v reason=%v", ok, reason)
    }
}