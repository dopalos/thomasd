// internal/light/verify.go
package light

import (
	"errors"

	"thomasd/internal/types"
)

var (
	ErrNilHeader              = errors.New("light: header is nil")
	ErrNilCommitBundle        = errors.New("light: commit bundle is nil")
	ErrCommitRootMismatch     = errors.New("light: commit root mismatch")
	ErrValidatorRootMismatch  = errors.New("light: validators root mismatch")
	ErrCommitMetadataMismatch = errors.New("light: commit metadata mismatch")
)

// Proof contains the items required for a light client verification round.
type Proof struct {
	Header         *types.EternityHeader
	CommitBundle   *types.CommitBundle
	ValidatorsRoot [32]byte
}

// Verify checks that the provided proof is self-consistent.
// It does NOT perform signature verification.
func Verify(p Proof) error {
	if p.Header == nil {
		return ErrNilHeader
	}
	if p.CommitBundle == nil {
		return ErrNilCommitBundle
	}

	// 1) Commit metadata consistency (Round)
	if p.Header.Round != p.CommitBundle.Round {
		return ErrCommitMetadataMismatch
	}

	// (선택) CommitBundle의 quorum_hash가 주어진 validators root와 일치하는지 확인
	// v1 규약에서 quorum_hash는 검증자/파워 스냅샷 해시로 사용됨.
	var zero32 [32]byte
	if p.CommitBundle.QuorumHash != zero32 && p.CommitBundle.QuorumHash != p.ValidatorsRoot {
		return ErrCommitMetadataMismatch
	}

	// 2) CommitBundle 해시 == Header.CommitRoot
	cbHash, err := p.CommitBundle.Hash()
	if err != nil {
		// 해시 계산 실패도 루트 불일치로 취급
		return ErrCommitRootMismatch
	}
	if cbHash != p.Header.CommitRoot {
		return ErrCommitRootMismatch
	}

	// 3) 제공된 ValidatorsRoot == Header.StateRoot
	// (구 스펙의 ProposerSetHash 역할을 v1에서는 StateRoot로 본다)
	if p.Header.StateRoot != p.ValidatorsRoot {
		return ErrValidatorRootMismatch
	}

	return nil
}
