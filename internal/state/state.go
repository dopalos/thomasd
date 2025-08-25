package state

import "sync"

type Account struct {
    Balance uint64 `json:"balance"`
    Nonce   uint64 `json:"nonce"`
}

type DB struct {
    mu          sync.Mutex
    accts       map[string]*Account
    feeReceiver string
}

func NewDB() *DB {
    return &DB{accts: make(map[string]*Account)}
}

func (db *DB) SetFeeReceiver(addr string) {
    db.mu.Lock()
    defer db.mu.Unlock()
    db.feeReceiver = addr
}

func (db *DB) InitGenesis(balances map[string]uint64) {
    db.mu.Lock()
    defer db.mu.Unlock()
    for addr, amt := range balances {
        a := db.get(addr)
        a.Balance += amt
    }
}

func (db *DB) get(addr string) *Account {
    if a, ok := db.accts[addr]; ok {
        return a
    }
    a := &Account{}
    db.accts[addr] = a
    return a
}

func (db *DB) GetAccount(addr string) Account {
    db.mu.Lock()
    defer db.mu.Unlock()
    a := db.get(addr)
    return *a
}

// ApplyTransfer: 기본 검증(잔고/nonce) + 상태 갱신 + 수수료 적립(재단)
func (db *DB) ApplyTransfer(from, to string, amount, fee, nonce uint64) error {
    db.mu.Lock()
    defer db.mu.Unlock()

    fa := db.get(from)
    if fa.Nonce+1 != nonce {
        return errStr("bad_nonce")
    }
    need := amount + fee
    if fa.Balance < need {
        return errStr("insufficient_funds")
    }
    // 차감/증가
    fa.Balance -= need
    fa.Nonce++
    ta := db.get(to)
    ta.Balance += amount

    // 수수료 → 재단 운영계좌
    if db.feeReceiver != "" {
        va := db.get(db.feeReceiver)
        va.Balance += fee
    }
    return nil
}

// ---- 스냅샷/복원 (계정 상태만 저장) ----
func (db *DB) Snapshot() map[string]Account {
    db.mu.Lock()
    defer db.mu.Unlock()
    out := make(map[string]Account, len(db.accts))
    for k, v := range db.accts {
        out[k] = *v
    }
    return out
}

func (db *DB) Restore(m map[string]Account) {
    db.mu.Lock()
    defer db.mu.Unlock()
    db.accts = make(map[string]*Account, len(m))
    for k, v := range m {
        a := v
        db.accts[k] = &a
    }
}

type errStr string
func (e errStr) Error() string { return string(e) }
