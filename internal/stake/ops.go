// internal/stake/ops.go
package stake

type OpType uint8

const (
	OpAdd    OpType = 0
	OpUpdate OpType = 1
	OpRemove OpType = 2
)

type Op struct {
	Type     OpType
	PubKey   [32]byte
	StakeMas *uint64
	NodeType *uint8
	ScoreQ   *uint32
	Jailed   *bool
}

// ApplyOps: 원본 보존, 사본에 적용. 끝에 PubKey 오름차순 보장.
func ApplyOps(vs *ValSet, ops []Op) *ValSet {
	out := vs.Clone()

	for _, op := range ops {
		switch op.Type {
		case OpAdd:
			pos, ok := out.findIndex(op.PubKey)
			if ok {
				v := out.Validators[pos]
				if op.StakeMas != nil {
					v.StakeMas = *op.StakeMas
				}
				if op.NodeType != nil {
					v.NodeType = NormalizeNodeType(*op.NodeType)
				}
				if op.ScoreQ != nil {
					v.ScoreQ = ClampScoreQ(*op.ScoreQ)
				}
				if op.Jailed != nil {
					v.Jailed = *op.Jailed
				}
				out.Validators[pos] = v
				break
			}
			v := Validator{
				PubKey:   op.PubKey,
				StakeMas: 0,
				NodeType: NodeLight,
				ScoreQ:   ScoreQ_Max,
				Jailed:   false,
			}
			if op.StakeMas != nil {
				v.StakeMas = *op.StakeMas
			}
			if op.NodeType != nil {
				v.NodeType = NormalizeNodeType(*op.NodeType)
			}
			if op.ScoreQ != nil {
				v.ScoreQ = ClampScoreQ(*op.ScoreQ)
			}
			if op.Jailed != nil {
				v.Jailed = *op.Jailed
			}
			out.Validators = append(out.Validators, v)

		case OpUpdate:
			pos, ok := out.findIndex(op.PubKey)
			if !ok {
				break
			}
			v := out.Validators[pos]
			if op.StakeMas != nil {
				v.StakeMas = *op.StakeMas
			}
			if op.NodeType != nil {
				v.NodeType = NormalizeNodeType(*op.NodeType)
			}
			if op.ScoreQ != nil {
				v.ScoreQ = ClampScoreQ(*op.ScoreQ)
			}
			if op.Jailed != nil {
				v.Jailed = *op.Jailed
			}
			out.Validators[pos] = v

		case OpRemove:
			pos, ok := out.findIndex(op.PubKey)
			if ok {
				out.Validators = append(out.Validators[:pos], out.Validators[pos+1:]...)
			}
		}
	}

	out.SortPubKeyAsc()
	return out
}
