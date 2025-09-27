//go:build !legacy_penalty

package validators

import (
	"thomasd/internal/consensus"
	"thomasd/internal/stake"
)

// 이벤트 (레거시 호환)
type ValidatorEvent uint8

const (
	EventProposalSuccess ValidatorEvent = iota
	EventMissPrevote
	EventMissPrecommit
	EventRefusedAsBackup
)

// 설정 (레거시 자리표시자)
type PenaltyConfig struct{}

// 레거시 rewards/adapter가 기대하는 최소 구조
type ScoreRecord struct {
	Address string
}

// --- 레거시 API ↔ 신규 API 브리지 ---

// 레거시 코드가 다양한 형태로 호출하는 ComputeValidatorsRoot를 단일 엔트리로 제공.
// * stake.ValSet, []stake.Validator, []validators.ScoreRecord 등을 받아들인다.
func ComputeValidatorsRoot(x any) [32]byte {
	switch v := x.(type) {
	case *stake.ValSet:
		return stake.ComputeValidatorsRoot(v.Validators)
	case []stake.Validator:
		return stake.ComputeValidatorsRoot(v)
	case []ScoreRecord:
		// 레거시 ScoreRecord만으로는 pubkey/스테이크/점수 정보가 없어
		// 실제 validators_root를 구성할 수 없다.
		// 기본 빌드에서는 0 루트를 반환해 런타임 의존을 우회한다.
		// (legacy_penalty 태그를 켜면 기존 구현이 사용됨)
		var zero [32]byte
		return zero
	default:
		var zero [32]byte
		return zero
	}
}

func ValidatorsRootFromSlice(vals []stake.Validator) [32]byte {
	return stake.ComputeValidatorsRoot(vals)
}

func ValidatorsRootFromSet(vs *stake.ValSet) [32]byte {
	return stake.ComputeValidatorsRoot(vs.Validators)
}

func NextValidatorsRoot(vs *stake.ValSet, ops []stake.Op) [32]byte {
	next := stake.ApplyOps(vs, ops)
	return stake.ComputeValidatorsRoot(next.Validators)
}

// --- 이벤트 → 점수 규칙 매핑 ---

// missCount는 호출측에서 보관(연속 미스 카운터)
func ApplyEvent(v *stake.Validator, ev ValidatorEvent, missCount *uint32) {
	switch ev {
	case EventProposalSuccess:
		consensus.UpdateScoreOnSuccess(v, missCount)
	case EventMissPrevote, EventMissPrecommit:
		consensus.UpdateScoreOnMiss(v, missCount)
	case EventRefusedAsBackup:
		consensus.UpdateScoreOnRefusedAsBackup(v)
	}
}
