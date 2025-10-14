package state

import (
	"strings"
	"sync"
)

type Account struct {
	Balance uint64 `json:"balance"`
	Nonce   uint64 `json:"nonce"`
}

type DB struct {
	mu          sync.Mutex
	accts       map[string]*Account
	feeReceiver string

	kv map[string][]byte

	validatorScores map[string]ValidatorScoreRecord
	epochPayouts    map[uint64]map[string]ValidatorPayoutRecord
	epochSummaries  map[uint64]EpochRewardSummary
}

func NewDB() *DB {
	return &DB{
		accts:           make(map[string]*Account),
		kv:              make(map[string][]byte),
		validatorScores: make(map[string]ValidatorScoreRecord),
		epochPayouts:    make(map[uint64]map[string]ValidatorPayoutRecord),
		epochSummaries:  make(map[uint64]EpochRewardSummary),
	}
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
		db.get(addr).Balance += amt
	}
}

func (db *DB) AddBalance(addr string, amount uint64) {
	if amount == 0 {
		return
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	db.get(addr).Balance += amount
}

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
	fa.Balance -= need
	fa.Nonce++

	db.get(to).Balance += amount
	if db.feeReceiver != "" {
		db.get(db.feeReceiver).Balance += fee
	}
	return nil
}

func (db *DB) GetAccount(addr string) Account {
	db.mu.Lock()
	defer db.mu.Unlock()
	return *db.get(addr)
}

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
		val := v
		db.accts[k] = &val
	}
}

// simple key/value storage (used by reward adapters)
func (db *DB) SetKey(key string, value []byte) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.kv[key] = append([]byte(nil), value...)
}

func (db *DB) GetKey(key string) ([]byte, bool) {
	db.mu.Lock()
	defer db.mu.Unlock()
	val, ok := db.kv[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), val...), true
}

func (db *DB) DeleteKey(key string) {
	db.mu.Lock()
	defer db.mu.Unlock()
	delete(db.kv, key)
}

func (db *DB) RangePrefix(prefix string, fn func(key string, value []byte) bool) {
	db.mu.Lock()
	defer db.mu.Unlock()
	for k, v := range db.kv {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if !fn(k, append([]byte(nil), v...)) {
			break
		}
	}
}

func (db *DB) get(addr string) *Account {
	if acc, ok := db.accts[addr]; ok {
		return acc
	}
	acc := &Account{}
	db.accts[addr] = acc
	return acc
}

type errStr string

func (e errStr) Error() string { return string(e) }
