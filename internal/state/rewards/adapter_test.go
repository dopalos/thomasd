//go:build !light_client
// +build !light_client

  //go:build light_client
  // +build light_client

package rewards

import (
	"testing"

	"thomasd/internal/rewards"
	"thomasd/internal/state"
	"thomasd/internal/state/validators"
	"thomasd/internal/types"
)

func TestAdapterRoundTrip(t *testing.T) {
	db := state.NewDB()
	adapter := NewAdapter(db, "FND123", "EXC456")

	// Seed validator scores.
	if err := adapter.PutValidatorScore(validators.ScoreRecord{
		Address:        "val1",
		StakeClass:     types.StakeClassG1,
		ConsensusScore: 10,
		Participation:  5,
	}); err != nil {
		t.Fatalf("put score: %v", err)
	}
	if err := adapter.PutValidatorScore(validators.ScoreRecord{
		Address:        "val2",
		StakeClass:     types.StakeClassG2,
		ConsensusScore: -2,
		Participation:  3,
		PoolNode:       true,
	}); err != nil {
		t.Fatalf("put score: %v", err)
	}

	scores, err := adapter.ListValidatorScores()
	if err != nil {
		t.Fatalf("list validator scores: %v", err)
	}
	if len(scores) != 2 {
		t.Fatalf("expected 2 scores, got %d", len(scores))
	}

	bucket := rewards.RewardBucket{
		Total:           1000,
		ValidatorShare:  800,
		FoundationShare: 100,
		ExchangeShare:   100,
	}
	payouts := []rewards.ValidatorPayout{
		{Address: "val1", Amount: 500},
		{Address: "val2", Amount: 300},
	}
	summary := rewards.EpochRewardSummary{
		Epoch:           1,
		Height:          100,
		TotalEmission:   bucket.Total,
		ValidatorShare:  bucket.ValidatorShare,
		FoundationShare: bucket.FoundationShare,
		ExchangeShare:   bucket.ExchangeShare,
	}

	for _, payout := range payouts {
		if err := adapter.PersistValidatorPayout(summary.Epoch, payout); err != nil {
			t.Fatalf("persist payout: %v", err)
		}
	}
	if err := adapter.PersistEpochSummary(summary); err != nil {
		t.Fatalf("persist summary: %v", err)
	}

	if got := db.GetAccount("FND123"); got.Balance != summary.FoundationShare {
		t.Fatalf("foundation balance mismatch: %d", got.Balance)
	}
	if got := db.GetAccount("EXC456"); got.Balance != summary.ExchangeShare {
		t.Fatalf("exchange balance mismatch: %d", got.Balance)
	}
}
