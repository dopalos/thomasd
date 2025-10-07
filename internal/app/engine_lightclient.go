//go:build light_client

package app

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"sync"
	"time"
)

// ──────────────────────────────────────────────────────────────────────────────
// 라이트 전용: 상태 루트 헬퍼 (A6.2 이전엔 zero root 반환)
// ──────────────────────────────────────────────────────────────────────────────
func getStateRoot(_ interface{}) ([32]byte, error) {
	// 이후(A6.2) 실제 상태 머클 계산으로 교체 예정
	return [32]byte{}, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// 라우터가 참조하는 최소 공개 타입
// ──────────────────────────────────────────────────────────────────────────────
type LightHeader struct {
	Height  uint64
	Root    [32]byte // header_commit_root
	TimeUTC int64
}

type LightCommitBundle struct {
	Height     uint64
	Round      uint64 // 라우터 호환용 필드 (stub)
	CommitRoot [32]byte
}

type Policy struct {
	AllowedChainID uint32 `json:"allowed_chain_id"`
}

type Tx struct {
	Type         uint8  `json:"type"`
	From         string `json:"from"`
	To           string `json:"to"`
	AmountMas    uint64 `json:"amount_mas"`
	FeeMas       uint64 `json:"fee_mas"`
	Nonce        uint64 `json:"nonce"`
	ExpiryHeight uint64 `json:"expiry_height"`
	ChainID      uint32 `json:"chain_id"`
	Memo         string `json:"memo,omitempty"`
}

type Receipt struct {
	TxHash  string `json:"tx_hash"`
	From    string `json:"from"`
	To      string `json:"to"`
	Amount  uint64 `json:"amount"`
	Fee     uint64 `json:"fee"`
	Nonce   uint64 `json:"nonce"`
	Status  string `json:"status"` // "applied"
	Height  uint64 `json:"height"`
	TimeUTC int64  `json:"time_utc"`
}

// ──────────────────────────────────────────────────────────────────────────────
// 라이트 엔진 (in-mem 스텁)
// ──────────────────────────────────────────────────────────────────────────────
type Engine struct {
	mu sync.RWMutex
	db interface{} // getStateRoot(e.db) 시그니처만 맞추기용(실사용 X)

	// Height/라운드
	height     uint64
	fromHeight uint64
	chainOK    bool

	// 간단 상태
	headers  map[uint64]LightHeader
	bundles  map[uint64]LightCommitBundle
	receipts map[string]Receipt
	nonces   map[string]uint64

	policy         Policy
	lastCommitRoot [32]byte
	supply         uint64 // 라이트 경로에서 관리하지 않음(0 고정)
}

// 전역 인스턴스 (라우터가 함수형/수신자형 둘 다 사용 가능)
var Eng = NewLightEngine()

func NewLightEngine() *Engine {
	e := &Engine{
		headers:  make(map[uint64]LightHeader),
		bundles:  make(map[uint64]LightCommitBundle),
		receipts: make(map[string]Receipt),
		nonces:   make(map[string]uint64),
		policy:   Policy{AllowedChainID: 1},
	}
	// genesis
	e.height = 0
	e.fromHeight = 0
	e.lastCommitRoot = [32]byte{}
	return e
}

// ──────────────────────────────────────────────────────────────────────────────
// 내부 유틸
// ──────────────────────────────────────────────────────────────────────────────
func computeCommitRoot(height uint64, seed [32]byte) [32]byte {
	// 단순 예시: height || seed 로 sha256
	buf := make([]byte, 8+32)
	binary.LittleEndian.PutUint64(buf[:8], height)
	copy(buf[8:], seed[:])
	return sha256.Sum256(buf)
}

func hashReceiptBody(body interface{}) string {
	b, _ := json.Marshal(body)
	sum := sha256.Sum256(b)
	return hex32(sum)
}

func hex32(b [32]byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 64)
	for i := 0; i < 32; i++ {
		out[i*2] = hexdigits[b[i]>>4]
		out[i*2+1] = hexdigits[b[i]&0x0f]
	}
	return string(out)
}

// ──────────────────────────────────────────────────────────────────────────────
// 공개 메서드 (수신자형) — 아래에 동일 시그니처의 top-level 함수도 제공
// ──────────────────────────────────────────────────────────────────────────────
func (e *Engine) EternityHeaderAt(height uint64) (LightHeader, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	h, ok := e.headers[height]
	return h, ok
}

func (e *Engine) CommitBundleAt(height uint64) (LightCommitBundle, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	b, ok := e.bundles[height]
	return b, ok
}

func (e *Engine) RoundCommit() (toHeight uint64, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	next := e.height + 1

	// Codex 요구: getStateRoot(e.db) 호출 경로 보장
	stateRoot, _ := getStateRoot(e.db)

	// header/bundle이 동일한 root를 갖도록 seed=stateRoot 사용
	root := computeCommitRoot(next, stateRoot)

	header := LightHeader{
		Height:  next,
		Root:    root,
		TimeUTC: time.Now().UTC().Unix(),
	}
	bundle := LightCommitBundle{
		Height:     next,
		Round:      next, // 단순히 height를 round로 둠(스텁)
		CommitRoot: root,
	}

	e.headers[next] = header
	e.bundles[next] = bundle
	e.fromHeight = e.height
	e.height = next
	e.lastCommitRoot = root
	e.chainOK = true
	return e.height, nil
}

func (e *Engine) RoundLatest() (fromHeight, toHeight uint64) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.fromHeight, e.height
}

func (e *Engine) GetPolicy() Policy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.policy
}

func (e *Engine) ExpectedNonce(addr string) uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.nonces[addr]
}

func (e *Engine) ApplyTx(tx Tx) (Receipt, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// 논스 자동증가 (엄격검증은 라이트에서 OFF)
	if tx.Nonce == 0 {
		tx.Nonce = e.nonces[tx.From]
	}
	e.nonces[tx.From] = tx.Nonce + 1

	h := e.height
	now := time.Now().UTC().Unix()

	raw := struct {
		Tx      Tx     `json:"tx"`
		Height  uint64 `json:"height"`
		TimeUTC int64  `json:"time_utc"`
	}{
		Tx: tx, Height: h, TimeUTC: now,
	}
	txHash := hashReceiptBody(raw)

	rc := Receipt{
		TxHash:  txHash,
		From:    tx.From,
		To:      tx.To,
		Amount:  tx.AmountMas,
		Fee:     tx.FeeMas,
		Nonce:   tx.Nonce,
		Status:  "applied",
		Height:  h,
		TimeUTC: now,
	}
	e.receipts[txHash] = rc
	return rc, nil
}

func (e *Engine) GetReceipt(hash string) (Receipt, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	rc, ok := e.receipts[hash]
	return rc, ok
}

func (e *Engine) ChainHealthy() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.chainOK
}

// 과거 풀노드 이름 호환 스텁
func (e *Engine) LatestCommitRoot() [32]byte {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastCommitRoot
}

func (e *Engine) CurrentSupply() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.supply // 라이트 경로에선 0 고정
}

// ──────────────────────────────────────────────────────────────────────────────
// 동일 시그니처의 top-level 함수 (라우터가 함수형 API를 사용해도 대응)
// ──────────────────────────────────────────────────────────────────────────────
func EternityHeaderAt(height uint64) (LightHeader, bool)     { return Eng.EternityHeaderAt(height) }
func CommitBundleAt(height uint64) (LightCommitBundle, bool) { return Eng.CommitBundleAt(height) }
func RoundCommit() (uint64, error)                           { return Eng.RoundCommit() }
func RoundLatest() (uint64, uint64)                          { return Eng.RoundLatest() }
func GetPolicy() Policy                                      { return Eng.GetPolicy() }
func ExpectedNonce(addr string) uint64                       { return Eng.ExpectedNonce(addr) }
func ApplyTx(tx Tx) (Receipt, error)                         { return Eng.ApplyTx(tx) }
func GetReceipt(hash string) (Receipt, bool)                 { return Eng.GetReceipt(hash) }
func ChainHealthy() bool                                     { return Eng.ChainHealthy() }
func LatestCommitRoot() [32]byte                             { return Eng.LatestCommitRoot() }
func CurrentSupply() uint64                                  { return Eng.CurrentSupply() }
