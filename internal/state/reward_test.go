//go:build !light_client
// +build !light_client

//go:build legacy_penalty

package state_test

import (
	"testing"

	"thomasd/internal/state"
)

func TestRewardStateStorage(t *testing.T) {
	db := state.NewDB()

	rec := state.ValidatorScoreRecord{
		Address:           "ValidatorOne",
		StakeClass:        "g1",
		ConsensusScore:    10,
		Participation:     5,
		PoolNode:          true,
		LastUpdatedHeight: 12,
		LastRewardedEpoch: 1,
	}

	db.UpsertValidatorScore(rec)

	if _, ok := db.GetValidatorScore("validatorone"); !ok {
		t.Fatalf("expected score record")
	}

	scores := db.ListValidatorScores()
	if len(scores) != 1 {
		t.Fatalf("expected 1 score record, got %d", len(scores))
	}

	payout := state.ValidatorPayoutRecord{Address: "validatorone", Amount: 100, Epoch: 2, Height: 20}
	db.RecordValidatorPayout(2, payout)

	payouts := db.EpochPayouts(2)
	if len(payouts) != 1 || payouts[0].Amount != 100 {
		t.Fatalf("unexpected payout list: %+v", payouts)
	}

	summary := state.EpochRewardSummary{
		Epoch:           2,
		Height:          21,
		TotalEmission:   300,
		ValidatorShare:  200,
		FoundationShare: 50,
		ExchangeShare:   50,
	}
	db.SetEpochRewardSummary(summary)

	gotSummary, ok := db.GetEpochRewardSummary(2)
	if !ok {
		t.Fatalf("expected epoch summary")
	}
	if gotSummary.TotalEmission != 300 {
		t.Fatalf("summary mismatch: %+v", gotSummary)
	}
}

func TestValidatorScoreEvents(t *testing.T) {
	rec := state.ValidatorScoreRecord{StakeClass: "g1"}
	rec.ApplyEvent(state.ScoreEventProposalMiss, 5)
	if rec.ConsecutiveMisses != 1 || rec.ConsensusScore != 0 {
		t.Fatalf("first miss should not penalize: %+v", rec)
	}
	rec.ApplyEvent(state.ScoreEventProposalMiss, 6)
	if rec.ConsensusScore >= 0 {
		t.Fatalf("second miss should reduce score: %+v", rec)
	}
	rec.ApplyEvent(state.ScoreEventProposalSuccess, 7)
	if rec.ConsecutiveMisses != 0 {
		t.Fatalf("success should reset misses: %+v", rec)
	}
	rec.ApplyEvent(state.ScoreEventEpochStability, 8)
	if rec.ConsensusScore <= 0 {
		t.Fatalf("epoch stability bonus not applied")
	}
	rec.ApplyEvent(state.ScoreEventBackupRefusal, 9)
	if rec.ConsensusScore >= 0 {
		t.Fatalf("backup refusal should decrease score")
	}
}
