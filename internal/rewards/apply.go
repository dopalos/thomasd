//go:build legacy_penalty

package rewards

import (
	"fmt"

	"thomasd/internal/state/validators"
)

// RewardState defines the storage contract the reward engine interacts with.
type RewardState interface {
	ListValidatorScores() ([]validators.ScoreRecord, error)
	PutValidatorScore(record validators.ScoreRecord) error
	PersistValidatorPayout(epoch uint64, payout ValidatorPayout) error
	PersistEpochSummary(summary EpochRewardSummary) error
}

// Apply calculates rewards at the given height and persists the resulting state.
func (eng *RewardEngine) Apply(height uint64, state RewardState) (RewardBucket, []ValidatorPayout, EpochRewardSummary,
	error) {
	if eng == nil {
		return RewardBucket{}, nil, EpochRewardSummary{}, fmt.Errorf("reward engine not initialized")
	}
	if state == nil {
		return RewardBucket{}, nil, EpochRewardSummary{}, fmt.Errorf("reward state not provided")
	}

	scores, err := state.ListValidatorScores()
	if err != nil {
		return RewardBucket{}, nil, EpochRewardSummary{}, fmt.Errorf("list validator scores: %w", err)
	}

	validatorInputs := make([]ValidatorScore, len(scores))
	for i, rec := range scores {
		validatorInputs[i] = ValidatorScore{
			Address:        rec.Address,
			StakeClass:     rec.StakeClass,
			ConsensusScore: rec.ConsensusScore,
			Participation:  rec.Participation,
			PoolNode:       rec.PoolNode,
		}
	}

	bucket, payouts, summary := eng.Compute(height, validatorInputs)

	if err := state.PersistEpochSummary(summary); err != nil {
		return RewardBucket{}, nil, EpochRewardSummary{}, fmt.Errorf("persist epoch summary: %w", err)
	}

	for _, payout := range payouts {
		if err := state.PersistValidatorPayout(summary.Epoch, payout); err != nil {
			return RewardBucket{}, nil, EpochRewardSummary{}, fmt.Errorf("persist payout for %s: %w", payout.Address,
				err)
		}
	}

	for _, rec := range scores {
		updated := rec.Clone()
		updated.ResetForEpoch(summary.Epoch, height)
		if err := state.PutValidatorScore(updated); err != nil {
			return RewardBucket{}, nil, EpochRewardSummary{}, fmt.Errorf("update validator score %s: %w", rec.Address,
				err)
		}
	}

	return bucket, payouts, summary, nil
}
