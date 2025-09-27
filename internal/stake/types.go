package stake

import (
	"bytes"
	"fmt"
	"slices"
)

// ScoreQ: Q16.16 (1.0 == 65536 == 100 points)

type Validator struct {
	PubKey   [32]byte // Ed25519 public key
	StakeMas uint64   // Stake in mas (μTHO)
	NodeType uint8    // 1=light, 2=pool, 3=full (legacy 0/1 값은 Normalize로 교정)
	ScoreQ   uint32   // Q16.16, [0..65536]
	Jailed   bool
}

type ValSet struct {
	Validators []Validator // 항상 PubKey 오름차순 유지
}

// Clone deep copy
func (vs *ValSet) Clone() *ValSet {
	out := &ValSet{Validators: make([]Validator, len(vs.Validators))}
	copy(out.Validators, vs.Validators)
	return out
}

// --- 정렬: PubKey 오름차순 "단일 기준"(결정성 유지) ---
func lessByPubKey(a, b Validator) bool {
	return bytes.Compare(a.PubKey[:], b.PubKey[:]) < 0
}
func (vs *ValSet) SortPubKeyAsc() {
	slices.SortFunc(vs.Validators, func(a, b Validator) int {
		if lessByPubKey(a, b) {
			return -1
		}
		if lessByPubKey(b, a) {
			return 1
		}
		return 0
	})
}

// 공개 함수(슬라이스 입력) — 기존 네 코드와 호환되는 형태의 정렬 복제본 반환
func SortValidators(vals []Validator) []Validator {
	cp := make([]Validator, len(vals))
	copy(cp, vals)
	slices.SortFunc(cp, func(a, b Validator) int {
		if lessByPubKey(a, b) {
			return -1
		}
		if lessByPubKey(b, a) {
			return 1
		}
		return 0
	})
	return cp
}

// PubKey 이진 탐색
func (vs *ValSet) findIndex(pk [32]byte) (idx int, ok bool) {
	lo, hi := 0, len(vs.Validators)
	for lo < hi {
		m := (lo + hi) >> 1
		c := bytes.Compare(vs.Validators[m].PubKey[:], pk[:])
		if c == 0 {
			return m, true
		}
		if c < 0 {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo, false // 삽입 위치
}

func (v Validator) String() string {
	return fmt.Sprintf("PubKey:%x Stake:%d Type:%d ScoreQ:%d Jailed:%v",
		v.PubKey[:4], v.StakeMas, v.NodeType, v.ScoreQ, v.Jailed)
}
