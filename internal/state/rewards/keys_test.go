package rewards

import "testing"

func TestValidatorScoreKey(t *testing.T) {
	key := ValidatorScoreKey("VAL01")
	if key != "validators/val01/score" {
		t.Fatalf("unexpected key %q", key)
	}
}

func TestValidatorPayoutKey(t *testing.T) {
	key := ValidatorPayoutKey(42, "VAL01")
	if key != "reward/epoch_000000000000002a/v/val01" {
		t.Fatalf("unexpected key %q", key)
	}
}

func TestEpochSummaryKey(t *testing.T) {
	key := EpochSummaryKey(42)
	if key != "reward/epoch_000000000000002a/summary" {
		t.Fatalf("unexpected summary key %q", key)
	}
}

func TestValidatorScoreKeyPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic on empty address")
		}
	}()
	_ = ValidatorScoreKey("")
}

func TestValidatorPayoutKeyPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic on empty address")
		}
	}()
	_ = ValidatorPayoutKey(0, "")
}
