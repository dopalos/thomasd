//go:build legacy_penalty

package validators

import "thomasd/internal/types"

// ScoreRecord represents the validator score data stored on-chain.
type ScoreRecord struct {
	Address           string
	StakeClass        types.StakeClass
	ConsensusScore    int64
	Participation     int64
	PoolNode          bool
	LastUpdatedHeight uint64
	LastRewardedEpoch uint64
	Score             float64
	ConsecutiveMisses uint32
	TotalMisses       uint64
	Jailed            bool
}

// Clone returns a defensive copy so callers can't mutate shared state.
func (r ScoreRecord) Clone() ScoreRecord {
	return ScoreRecord{
		Address:           r.Address,
		StakeClass:        r.StakeClass,
		ConsensusScore:    r.ConsensusScore,
		Participation:     r.Participation,
		PoolNode:          r.PoolNode,
		LastUpdatedHeight: r.LastUpdatedHeight,
		LastRewardedEpoch: r.LastRewardedEpoch,
		Score:             r.Score,
		ConsecutiveMisses: r.ConsecutiveMisses,
		TotalMisses:       r.TotalMisses,
		Jailed:            r.Jailed,
	}
}

// ResetForEpoch clears epoch-accumulated counters after rewards are applied.
func (r *ScoreRecord) ResetForEpoch(epoch, height uint64) {
	r.Participation = 0
	r.LastRewardedEpoch = epoch
	r.LastUpdatedHeight = height
}
