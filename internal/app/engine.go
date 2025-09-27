package app

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	mycrypto "thomasd/internal/crypto"
	"thomasd/internal/merkle"
	"thomasd/internal/rewards"
	"thomasd/internal/state"
	"thomasd/internal/state/validators"
	"thomasd/internal/tx"
	"thomasd/internal/types"
)

type Engine struct {
	mu  sync.Mutex
	txq [][]byte
	db  *state.DB

	receipts map[string]types.TxReceipt
	leaves   [][]byte
	height   uint64

	statePathV1 string
	statePathV2 string

	rounds              []types.RoundHeader
	lastCommittedHeight uint64
	eternityHeaders     []types.EternityHeader
	lastBlockID         [32]byte
	lastCommitRoot      [32]byte
	lastCommitHeight    uint64
	commitRootReady     bool

	supply            SupplyState
	lastMintedMas     uint64
	lastEmissionEpoch uint64

	ledgerPath string

	priv    ed25519.PrivateKey
	pub     ed25519.PublicKey
	keyPath string

	subs map[chan []byte]struct{}

	commitFeed []commitRecord

	aliasMem map[string]map[string][]byte
}

// 커밋 기록(높이, 루트, 번들) 보관
type commitRecord struct {
	Height uint64
	Root   [32]byte
	Bundle types.CommitBundle
}

const DefaultChainID = "thomas-dev-1"

func NewEngine() *Engine {
	db := state.NewDB()
	db.SetFeeReceiver("tho1foundation")

	e := &Engine{
		db:          db,
		receipts:    make(map[string]types.TxReceipt),
		statePathV1: filepath.Join("data", "state.json"),
		statePathV2: filepath.Join("data", "state_v2.json"),
		ledgerPath:  filepath.Join("data", "ledger_v1.json"),
		keyPath:     filepath.Join("data", "node_key.json"),
		subs:        make(map[chan []byte]struct{}),
		aliasMem:    make(map[string]map[string][]byte),
	}

	// 키 로드/생성
	if e.keyPath != "" {
		_ = os.MkdirAll(filepath.Dir(e.keyPath), 0o755)
	}
	if pubLoaded, privLoaded, err := mycrypto.LoadOrCreate(e.keyPath); err == nil &&
		len(pubLoaded) == ed25519.PublicKeySize && len(privLoaded) == ed25519.PrivateKeySize {
		e.pub, e.priv = pubLoaded, privLoaded
	} else {
		if pub2, priv2, err2 := ed25519.GenerateKey(rand.Reader); err2 == nil {
			e.pub, e.priv = pub2, priv2
		}
	}

	// 상태 로드 (v2 우선 / v1→v2 승격)
	if !e.loadStateMas(e.statePathV2) {
		if m, ok := e.loadStateMicro(e.statePathV1); ok {
			for k, v := range m {
				v.Balance *= 10 // micro-THO → mas
				m[k] = v
			}
			e.db.Restore(m)
			_ = e.saveStateMas(e.statePathV2)
		} else {
			db.InitGenesis(map[string]uint64{
				"tho1alice":      2_000_000 * 10, // 2 THO = 20,000,000 mas
				"tho1foundation": 0,
				"tho1exchange":   0,
			})
			_ = e.saveStateMas(e.statePathV2)
		}
	}

	_ = e.loadLedger(e.ledgerPath)

	if st, ok := loadSupplyState(e.db); ok {
		e.supply = st
		e.lastMintedMas = rewards.BlockMintAt(st.LastUpdatedHeight)
		e.lastEmissionEpoch = rewards.EpochOf(st.LastUpdatedHeight)
	}
	return e
}

func (e *Engine) PushRawTx(b []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cp := make([]byte, len(b))
	copy(cp, b)
	e.txq = append(e.txq, cp)
}

func (e *Engine) TxCount() int { e.mu.Lock(); defer e.mu.Unlock(); return len(e.txq) }

func (e *Engine) GetAccount(addr string) state.Account { return e.db.GetAccount(addr) }
func (e *Engine) CurrentHeight() uint64                { e.mu.Lock(); defer e.mu.Unlock(); return e.height }

func (e *Engine) ApplyTransfer(t tx.Transfer) error {
	// 별도 alias 트랜잭션 경로
	if os.Getenv("THOMAS_FEAT_ALIAS") == "1" && t.To == aliasSys {
		if handled, err := e.tryApplyAliasSetTx(t, int64(e.CurrentHeight())); handled {
			if err != nil {
				return err
			}
			return e.saveStateMas(e.statePathV2)
		}
	}

	if err := e.db.ApplyTransfer(t.From, t.To, t.AmountMas, t.FeeMas, t.Nonce); err != nil {
		return err
	}
	return e.saveStateMas(e.statePathV2)
}

func (e *Engine) StoreReceipt(t tx.Transfer, applied bool, reason string) types.TxReceipt {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.height++
	status := "applied"
	if !applied {
		status = "rejected:" + reason
	}
	r := types.TxReceipt{
		TxHash:  t.Hash(),
		From:    t.From,
		To:      t.To,
		Amount:  t.AmountMas,
		Fee:     t.FeeMas,
		Nonce:   t.Nonce,
		Status:  status,
		Height:  uint64(len(e.leaves)),
		TimeUTC: time.Now().UTC().Unix(),
	}
	b, _ := json.Marshal(r)
	e.receipts[r.TxHash] = r
	e.leaves = append(e.leaves, b)

	_ = e.saveLedger(e.ledgerPath)
	e.broadcastAsync("receipt", r)
	return r
}

func (e *Engine) GetReceipt(hash string) (types.TxReceipt, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	r, ok := e.receipts[hash]
	return r, ok
}

func (e *Engine) MerkleRoot() []byte { e.mu.Lock(); defer e.mu.Unlock(); return merkle.Root(e.leaves) }
func (e *Engine) ReceiptCount() int  { e.mu.Lock(); defer e.mu.Unlock(); return len(e.leaves) }

func (e *Engine) CommitRound() (types.RoundHeader, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	leavesLen := uint64(len(e.leaves))
	rc := uint64(len(e.leaves))
	if e.height != rc {
		e.height = rc
	}
	from := e.lastCommittedHeight + 1
	to := e.height

	if to > leavesLen {
		to = leavesLen
	}
	if from > to {
		return types.RoundHeader{}, false
	}

	if err := e.applyBlockMint(to); err != nil {
		return types.RoundHeader{}, false
	}

	sub := append([][]byte(nil), e.leaves[from-1:to]...)
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
	e.broadcastAsync("round", hdr)

	if header, bundle, err := e.buildEternityHeader(sub, hdr); err == nil {
		e.eternityHeaders = append(e.eternityHeaders, header)
		if len(e.commitFeed) >= 64 {
			e.commitFeed = e.commitFeed[1:]
		} else {
			// 🔴 여기서 정확한 실패 원인이 콘솔에 찍힙니다.
			fmt.Printf("engine: buildEternityHeader failed at height=%d round=%d: %v\n",
				hdr.ToHeight, hdr.Round, err)
		}
		e.commitFeed = append(e.commitFeed, commitRecord{
			Height: header.Height,
			Root:   header.CommitRoot,
			Bundle: bundle,
		})
	}
	return hdr, true
}

func (e *Engine) GetRound(n uint64) (types.RoundHeader, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if n == 0 || int(n) > len(e.rounds) {
		return types.RoundHeader{}, false
	}
	return e.rounds[n-1], true
}

func (e *Engine) LatestEternityHeader() (types.EternityHeader, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.eternityHeaders) == 0 {
		return types.EternityHeader{}, false
	}
	return e.eternityHeaders[len(e.eternityHeaders)-1], true
}

func (e *Engine) EternityHeaderAt(height uint64) (types.EternityHeader, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.eternityHeaders) - 1; i >= 0; i-- {
		h := e.eternityHeaders[i]
		if h.Height == height {
			return h, true
		}
	}
	return types.EternityHeader{}, false
}

func (e *Engine) LatestCommitRoot() ([32]byte, uint64, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.commitRootReady {
		return [32]byte{}, 0, false
	}
	return e.lastCommitRoot, e.lastCommitHeight, true
}

func (e *Engine) LatestRound() (types.RoundHeader, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.rounds) == 0 {
		return types.RoundHeader{}, false
	}
	return e.rounds[len(e.rounds)-1], true
}

func (e *Engine) PubKeyHex() string {
	if len(e.pub) == 0 {
		return ""
	}
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

// EternityHeader + CommitBundle 생성
func (e *Engine) buildEternityHeader(raw [][]byte, hdr types.RoundHeader) (types.EternityHeader, types.CommitBundle, error) {
	var out types.EternityHeader
	var bundle types.CommitBundle

	if len(e.pub) != ed25519.PublicKeySize || len(e.priv) != ed25519.PrivateKeySize {
		return out, bundle, errors.New("engine: proposer key unavailable")
	}

	txRoot := merkle.Blake3RootFromRaw(raw)
	stateRoot := e.db.StateRoot()

	// 검증인 루트 계산(로컬)
	scores, _ := loadAllValidatorScores(e.db)
	vroot := computeValidatorsRoot(scores)

	// CommitBundle (최소 구성)
	bundle = types.CommitBundle{
		Round:      uint32(hdr.Round),
		QuorumHash: vroot,
		Bitmap:     []byte{1}, // v1: non-empty 보장
		Step:       3,         // precommit
	}
	cbHash, err := bundle.Hash()
	if err != nil {
		return out, bundle, err
	}

	// Header
	out.Version = types.EternityHeaderVersionV1
	out.ChainID = DefaultChainID
	out.Height = hdr.ToHeight
	if out.Height == 0 {
		out.Height = e.height
	}
	out.Round = uint32(hdr.Round)
	out.TimeUTCUnix = hdr.TimeUTC
	out.PrevHash = e.lastBlockID
	out.StateRoot = stateRoot
	out.TxRoot = txRoot
	out.ReceiptsRoot = [32]byte{}
	out.FeeRoot = [32]byte{}
	out.ConsensusParamsHash = [32]byte{}
	out.CommitRoot = cbHash
	out.ProposerPubKey = bytesTo32(e.pub)
	out.EmissionEpoch = rewards.EpochOf(out.Height)

	// 해시 계산 (서명 바이트는 필요 시 호출부에서 사용)
	if _, err := out.SignBytes(); err != nil {
		return out, bundle, err
	}
	blockID, err := out.Hash()
	if err != nil {
		return out, bundle, err
	}

	// 캐시 갱신
	e.lastBlockID = blockID
	e.lastCommitRoot = cbHash
	e.lastCommitHeight = out.Height
	e.commitRootReady = true

	return out, bundle, nil
}

func (e *Engine) applyBlockMint(height uint64) error {
	epoch, minted := e.supply.ApplyBlockMint(height)
	if err := persistSupplyState(e.db, height, e.supply); err != nil {
		return err
	}
	e.lastMintedMas = minted
	e.lastEmissionEpoch = epoch
	return nil
}

func (e *Engine) CurrentSupply() SupplyState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.supply
}

func (e *Engine) SupplyAt(height uint64) (SupplyState, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if height == e.supply.LastUpdatedHeight {
		return e.supply, true
	}
	return loadSupplyStateAt(e.db, height)
}

func (e *Engine) ListValidatorScores() ([]validators.ScoreRecord, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return loadAllValidatorScores(e.db)
}

// 번들 조회
func (e *Engine) CommitBundleAt(height uint64) (types.CommitBundle, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.commitFeed) - 1; i >= 0; i-- {
		if e.commitFeed[i].Height == height {
			return e.commitFeed[i].Bundle, true
		}
	}
	return types.CommitBundle{}, false
}

// 외부 번들 주입
func (e *Engine) RecordCommitBundle(bundle types.CommitBundle) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if root, err := bundle.Hash(); err == nil {
		e.lastCommitRoot = root
	}
	e.lastCommitHeight = e.height
	e.commitRootReady = true

	rec := commitRecord{
		Height: e.lastCommitHeight,
		Root:   e.lastCommitRoot,
		Bundle: bundle,
	}
	if len(e.commitFeed) >= 64 {
		e.commitFeed = e.commitFeed[1:]
	}
	e.commitFeed = append(e.commitFeed, rec)
}

func (e *Engine) SignRoundHeader(h types.RoundHeader) (sig []byte, ok bool) {
	if len(e.priv) == 0 {
		return nil, false
	}
	msg := CanonicalRoundHeaderBytes(h)
	s := ed25519.Sign(e.priv, msg)
	return s, true
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

func (e *Engine) broadcastAsync(kind string, payload any) {
	_ = kind
	go func(v any) {
		b, err := json.Marshal(v)
		if err != nil {
			return
		}
		e.mu.Lock()
		chans := make([]chan []byte, 0, len(e.subs))
		for ch := range e.subs {
			chans = append(chans, ch)
		}
		e.mu.Unlock()
		for _, ch := range chans {
			select {
			case ch <- b:
			default:
			}
		}
	}(payload)
}

func (e *Engine) saveStateMas(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	snap := e.db.Snapshot()
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (e *Engine) loadStateMas(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var m map[string]state.Account
	if err := json.Unmarshal(b, &m); err != nil {
		return false
	}
	e.db.Restore(m)
	return true
}

func (e *Engine) loadStateMicro(path string) (map[string]state.Account, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var m map[string]state.Account
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, false
	}
	return m, true
}

type ledgerV1 struct {
	Version             int                 `json:"version"`
	Height              uint64              `json:"height"`
	LastCommittedHeight uint64              `json:"last_committed_height"`
	Receipts            []types.TxReceipt   `json:"receipts"`
	Rounds              []types.RoundHeader `json:"rounds"`
}

func (e *Engine) saveLedger(path string) error {
	list := make([]types.TxReceipt, 0, len(e.receipts))
	for _, r := range e.receipts {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Height < list[j].Height })

	led := ledgerV1{
		Version:             1,
		Height:              uint64(len(e.leaves)),
		LastCommittedHeight: e.lastCommittedHeight,
		Receipts:            list,
		Rounds:              append([]types.RoundHeader(nil), e.rounds...),
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(led, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (e *Engine) loadLedger(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var led ledgerV1
	if err := json.Unmarshal(b, &led); err != nil {
		return err
	}

	e.height = uint64(len(led.Receipts))
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

/*
=== From→PubKey binding (file-backed) ===
  - 파일: data/from_pubkeys.json
  - 예시: { "tho1alice": "<hex or base64>", "tho1bob": "<hex or base64>" }
*/
func (e *Engine) getFromPubKeyMap() map[string]string {
	path := filepath.Join("data", "from_pubkeys.json")
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return nil
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// (addr, pk) → bool : 바인딩 검증
func (e *Engine) VerifyFromBinding(addr string, pk []byte) bool {
	m := e.getFromPubKeyMap()
	if m == nil {
		return false
	}
	s, ok := m[addr]
	if !ok || s == "" {
		return false
	}
	if hb, err := hex.DecodeString(s); err == nil {
		return bytes.Equal(hb, pk)
	}
	if bb, err := base64.StdEncoding.DecodeString(s); err == nil {
		return bytes.Equal(bb, pk)
	}
	return false
}

// (string) -> string (hex/base64)
func (e *Engine) GetAccountPubKey(addr string) string {
	m := e.getFromPubKeyMap()
	if m == nil {
		return ""
	}
	return m[addr]
}

/* === Minimal alias resolver (file-backed) ==================================
   - 파일: data/alias_map.json
     예시: { "alice": "tho1alice", "bob": "tho1bob" }
   - Router에서 reflective 조회 지원:
     ResolveAlias(name) (map[string]any, bool)
     ReverseAlias(addr) (string, bool)
=============================================================================*/

func (e *Engine) aliasMap() map[string]string {
	path := filepath.Join("data", "alias_map.json")
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		var m map[string]string
		if json.Unmarshal(b, &m) == nil && len(m) > 0 {
			return m
		}
	}
	return map[string]string{
		"alice": "tho1alice",
		"bob":   "tho1bob",
	}
}

func (e *Engine) ResolveAlias(name string) (map[string]any, bool) {
	if name == "" {
		return map[string]any{}, false
	}
	if strings.HasPrefix(name, "@") {
		name = strings.TrimPrefix(name, "@")
	}
	m := e.aliasMap()
	if owner, ok := m[name]; ok && owner != "" {
		return map[string]any{"owner": owner, "version": int64(1)}, true
	}
	return map[string]any{}, true
}

func (e *Engine) ReverseAlias(addr string) (string, bool) {
	if addr == "" {
		return "", false
	}
	m := e.aliasMap()
	for k, v := range m {
		if v == addr {
			return k, true
		}
	}
	return "", true
}

// bytesTo32: 최대 32바이트를 [32]byte로 복사
func bytesTo32(b []byte) (out [32]byte) {
	copy(out[:], b)
	return
}

// 검증인 루트(간단 버전): 주소들(소문자, 정렬) 이어붙여 blake3 해시
func computeValidatorsRoot(scores []validators.ScoreRecord) [32]byte {
	if len(scores) == 0 {
		return [32]byte{}
	}
	cp := append([]validators.ScoreRecord(nil), scores...)
	sort.Slice(cp, func(i, j int) bool {
		return strings.ToLower(cp[i].Address) < strings.ToLower(cp[j].Address)
	})
	var buf bytes.Buffer
	for _, s := range cp {
		addr := strings.ToLower(strings.TrimSpace(s.Address))
		buf.WriteString(addr)
		buf.WriteByte('\n')
	}
	return mycrypto.Blake3_256(buf.Bytes())
}
