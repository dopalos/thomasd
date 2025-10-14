//go:build !light_client
// +build !light_client

package stake

import "testing"

func TestApplyEventScoring(t *testing.T) {
	cfg := DefaultPenaltyConfig
	v := &Validator{Address: "val1"}

	v.ApplyEvent(EventProposalSuccess, cfg)
	if v.Score != cfg.SuccessReward {
		t.Fatalf("expected score %f, got %f", cfg.SuccessReward, v.Score)
	}

	v.ApplyEvent(EventProposalMiss, cfg) // first miss, no penalty
	if v.Score != cfg.SuccessReward {
		t.Fatalf("unexpected penalty on first miss: %f", v.Score)
	}

	v.ApplyEvent(EventProposalMiss, cfg)
	expected := cfg.SuccessReward + cfg.SecondMissPenalty
	if v.Score != expected {
		t.Fatalf("second miss mismatch: want %f got %f", expected, v.Score)
	}

	v.ApplyEvent(EventProposalMiss, cfg)
	expected += cfg.ThirdMissPenalty
	if v.Score != expected {
		t.Fatalf("third miss mismatch: want %f got %f", expected, v.Score)
	}

	v.ApplyEvent(EventProposalMiss, cfg)
	expected += cfg.FourthMissPenalty
	if v.Score != expected {
		t.Fatalf("fourth miss mismatch: want %f got %f", expected, v.Score)
	}
	if v.ConsecutiveMisses != 4 {
		t.Fatalf("consecutive misses capped at 4, got %d", v.ConsecutiveMisses)
	}

	v.ApplyEvent(EventEpochStability, cfg)
	expected += cfg.EpochStabilityBonus
	if v.Score != expected {
		t.Fatalf("epoch bonus mismatch: want %f got %f", expected, v.Score)
	}

	v.ApplyEvent(EventBackupRefusal, cfg)
	expected += cfg.BackupRefusalPenalty
	if v.Score != expected {
		t.Fatalf("backup refusal mismatch: want %f got %f", expected, v.Score)
	}

	// Success resets miss counter
	v.ApplyEvent(EventProposalSuccess, cfg)
	if v.ConsecutiveMisses != 0 {
		t.Fatalf("consecutive misses not reset after success: %d", v.ConsecutiveMisses)
	}
}
