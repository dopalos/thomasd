//go:build !light_client
// +build !light_client

package validators

import (
	"testing"

	"thomasd/internal/stake"
)

func TestScoreRecordApplyEvent(t *testing.T) {
	rec := ScoreRecord{Address: "val1"}
	cfg := stake.DefaultPenaltyConfig

	rec.ApplyEvent(stake.EventProposalSuccess, cfg)
	if rec.Score != cfg.SuccessReward {
		t.Fatalf("success reward mismatch: %f", rec.Score)
	}

	rec.ApplyEvent(stake.EventProposalMiss, cfg)
	rec.ApplyEvent(stake.EventProposalMiss, cfg) // trigger penalty
	if rec.Score >= cfg.SuccessReward {
		t.Fatalf("expected negative adjustment after misses: %f", rec.Score)
	}

	rec.ApplyEvent(stake.EventEpochStability, cfg)
	if rec.Score <= 0 {
		t.Fatalf("epoch bonus not applied")
	}
}
