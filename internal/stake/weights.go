package stake

import "math/big"

const (
	ScoreQ_One uint32 = 65536
	ScoreQ_Max uint32 = 65536
)

// Node types (정식)
const (
	NodeLight uint8 = 1
	NodePool  uint8 = 2
	NodeFull  uint8 = 3
)

// 레거시 입력(0/1) 정규화: 0→light(1), 1→full(3)
func NormalizeNodeType(nt uint8) uint8 {
	switch nt {
	case 0:
		return NodeLight
	case 1:
		return NodeFull
	case 2, 3:
		return nt
	default:
		return NodeLight
	}
}

// 1e3 스케일 가중치
func GetNodeTypeWeight(nodeType uint8) uint32 {
	switch NormalizeNodeType(nodeType) {
	case NodeLight:
		return 1000
	case NodePool:
		return 2000
	case NodeFull:
		return 1500
	default:
		return 1000
	}
}

func ClampScoreQ(q uint32) uint32 {
	if q > ScoreQ_Max {
		return ScoreQ_Max
	}
	return q
}

// 포인트 가감: delta = ScoreQ_One * num / (100 * denom) (내림, 클램프)
func AddPointsQ(q uint32, num int64, denom int64) uint32 {
	if denom == 0 {
		return q
	}
	neg := false
	if num < 0 {
		neg = true
		num = -num
	}
	n := big.NewInt(int64(ScoreQ_One))
	n.Mul(n, big.NewInt(num))    // ScoreQ_One * num
	d := big.NewInt(100 * denom) // 100 * denom
	n.Quo(n, d)                  // 내림
	delta := uint32(n.Uint64())

	if neg {
		if delta >= q {
			return 0
		}
		return q - delta
	}
	return ClampScoreQ(q + delta)
}

// EffectivePower = floor(stake_mas * weight(1e3) * scoreQ / (65536 * 1000))
func (v *Validator) EffectivePower() uint64 {
	w := uint64(GetNodeTypeWeight(v.NodeType))
	num := new(big.Int).SetUint64(v.StakeMas)
	num.Mul(num, new(big.Int).SetUint64(w))
	num.Mul(num, new(big.Int).SetUint64(uint64(v.ScoreQ)))
	den := new(big.Int).SetUint64(uint64(ScoreQ_One) * 1000)
	num.Quo(num, den) // 내림
	return num.Uint64()
}
