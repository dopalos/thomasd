//go:build !light_client
// +build !light_client

  //go:build light_client
  // +build light_client

package rewards

import (
	"fmt"
	"testing"

	"thomasd/internal/state/validators"
	"thomasd/internal/types"
)

type memRewardState struct {
	scores  map[string]validators.ScoreRecord
	summary []EpochRewardSummary
	payouts []struct {
		Epoch  uint64
		Payout ValidatorPayout
	}
	failList bool
}

func newMemRewardState(records []validators.ScoreRecord) *memRewardState {
	m := &memRewardState{scores: make(map[string]validators.ScoreRecord)}
	for _, rec := range records {
		m.scores[rec.Address] = rec.Clone()
	}
	return m
}

func (m *memRewardState) ListValidatorScores() ([]validators.ScoreRecord, error) {
	if m.failList {
		return nil, errForced
	}
	out := make([]validators.ScoreRecord, 0, len(m.scores))
	for _, rec := range m.scores {
		out = append(out, rec.Clone())
	}
	return out, nil
}

func (m *memRewardState) PutValidatorScore(record validators.ScoreRecord) error {
	m.scores[record.Address] = record.Clone()
	return nil
}

func (m *memRewardState) PersistValidatorPayout(epoch uint64, payout ValidatorPayout) error {
	m.payouts = append(m.payouts, struct {
		Epoch  uint64
		Payout ValidatorPayout
	}{Epoch: epoch, Payout: payout})
	return nil
}

func (m *memRewardState) PersistEpochSummary(summary EpochRewardSummary) error {
	m.summary = append(m.summary, summary)
	return nil
}

var errForced = fmt.Errorf("forced error")

func TestApplyRewards(t *testing.T) {
	engine := NewRewardEngine(
		EmissionSchedule{BaseEmission: 1_000_000, DecayRatio: 0.618, EpochBlocks: 10},
		WeightConfig{StakeBase: map[types.StakeClass]uint64{
			types.StakeClassG1: 100,
			types.StakeClassG2: 200,
		}, PoolMultiplier: 2},
	)

	store := newMemRewardState([]validators.ScoreRecord{
		{Address: "v1", StakeClass: types.StakeClassG1, ConsensusScore: 40, Participation: 15, PoolNode: false},
		{Address: "v2", StakeClass: types.StakeClassG2, ConsensusScore: -10, Participation: 5, PoolNode: true},
	})

	bucket, payouts, summary, err := engine.Apply(12, store)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	if bucket.ValidatorShare == 0 {
		t.Fatalf("expected validator share > 0")
	}

	if len(payouts) != 2 {
		t.Fatalf("expected 2 payouts, got %d", len(payouts))
	}

	if len(store.summary) != 1 || store.summary[0].Epoch != summary.Epoch {
		t.Fatalf("summary not persisted: %+v", store.summary)
	}

	for _, rec := range store.scores {
		if rec.Participation != 0 {
			t.Fatalf("participation not reset for %s", rec.Address)
		}
		if rec.LastRewardedEpoch != summary.Epoch {
			t.Fatalf("last rewarded epoch mismatch for %s", rec.Address)
		}
	}
}

func TestApplyRewardsErrors(t *testing.T) {
	engine := NewRewardEngine(EmissionSchedule{}, WeightConfig{})
	store := newMemRewardState(nil)
	store.failList = true

	if _, _, _, err := engine.Apply(1, store); err == nil {
		t.Fatalf("expected error from failing list")
	}
}
