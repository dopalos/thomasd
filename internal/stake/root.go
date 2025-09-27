package stake

import (
	"thomasd/internal/codec"

	"github.com/zeebo/blake3"
)

// leaf = b3( CBOR([pubkey32, stake_mas, node_type_u8, score_q, jailed_bool]) )
func leafFor(v Validator) [32]byte {
	entry := []any{
		v.PubKey[:],
		v.StakeMas,
		NormalizeNodeType(v.NodeType),
		v.ScoreQ,
		v.Jailed,
	}
	b, _ := codec.EncodeCBORCanonical(entry)
	return blake3.Sum256(b)
}

// 상위노드 = b3( CBOR([left_hash32, right_hash32]) ), 레벨마다 홀수면 마지막 복제
func merkleRoot(leaves [][32]byte) [32]byte {
	var zero [32]byte
	if len(leaves) == 0 {
		return zero
	}
	level := leaves
	for len(level) > 1 {
		next := make([][32]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			l := level[i]
			r := l
			if i+1 < len(level) {
				r = level[i+1]
			}
			b, _ := codec.EncodeCBORCanonical([]any{l[:], r[:]})
			next = append(next, blake3.Sum256(b))
		}
		level = next
	}
	return level[0]
}

// 공개 시그니처(슬라이스 입력 유지) — 내부적으로 PubKey 오름차순 강제
func ComputeValidatorsRoot(vals []Validator) [32]byte {
	sorted := SortValidators(vals)
	leaves := make([][32]byte, len(sorted))
	for i := range sorted {
		leaves[i] = leafFor(sorted[i])
	}
	return merkleRoot(leaves)
}

// 진단용: PubKey 배열만으로 결정성 해시
func DeterministicHash(vs *ValSet) [32]byte {
	cp := vs.Clone()
	cp.SortPubKeyAsc()
	arr := make([][]byte, len(cp.Validators))
	for i := range cp.Validators {
		arr[i] = cp.Validators[i].PubKey[:]
	}
	b, _ := codec.EncodeCBORCanonical(arr)
	return blake3.Sum256(b)
}
