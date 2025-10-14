//go:build !light_client
// +build !light_client

package rewards

import (
	"math"
	"testing"

	"thomasd/internal/params"
	"thomasd/internal/types"
)

func TestEmissionSchedule(t *testing.T) {
	sched := EmissionSchedule{
		BaseEmission: 1_000_000,
		DecayRatio:   0.618,
		EpochBlocks:  20,
	}

	if got := sched.EmissionAt(0); got != 1_000_000 {
		t.Fatalf("emission at height 0 = %d", got)
	}
	expected := uint64(float64(sched.BaseEmission) * sched.DecayRatio)
	if got := sched.EmissionAt(25); got != expected {
		t.Fatalf("emission at epoch 1 = %d want %d", got, expected)
	}
}

func TestComputeRewards(t *testing.T) {
	engine := NewRewardEngine(
		EmissionSchedule{BaseEmission: 1_000_000, DecayRatio: 0.618, EpochBlocks: 10},
		WeightConfig{StakeBase: map[types.StakeClass]uint64{
			types.StakeClassG1: 100,
			types.StakeClassG2: 200,
		}, PoolMultiplier: 2},
	)

	validators := []ValidatorScore{
		{Address: "v1", StakeClass: types.StakeClassG1, ConsensusScore: 50, Participation: 10, PoolNode: false},
		{Address: "v2", StakeClass: types.StakeClassG2, ConsensusScore: -20, Participation: 0, PoolNode: true},
	}

	bucket, payouts, summary := engine.Compute(5, validators)

	if bucket.Total != 1_000_000 {
		t.Fatalf("total bucket = %d", bucket.Total)
	}
	if bucket.ValidatorShare != 800_000 || bucket.FoundationShare != 100_000 || bucket.ExchangeShare != 100_000 {
		t.Fatalf("bucket shares unexpected: %+v", bucket)
	}
	if summary.ValidatorShare != bucket.ValidatorShare {
		t.Fatalf("summary mismatch: %+v", summary)
	}

	if len(payouts) != 2 {
		t.Fatalf("payouts length = %d", len(payouts))
	}
	var sum uint64
	for _, p := range payouts {
		sum += p.Amount
	}
	if sum != bucket.ValidatorShare {
		t.Fatalf("payout sum %d != validator share %d", sum, bucket.ValidatorShare)
	}

	// We expect higher weight for v2 due to pool multiplier.
	var v2 uint64
	for _, p := range payouts {
		if p.Address == "v2" {
			v2 = p.Amount
		}
	}
	if v2 == 0 {
		t.Fatalf("v2 payout missing")
	}
}

func TestBlockMintHelpers(t *testing.T) {
	heights := []uint64{0, 1, 99, 100, params.EmissionIntervalBlocks, params.EmissionIntervalBlocks + 1}
	var prev uint64 = math.MaxUint64
	for _, h := range heights {
		mint := BlockMintAt(h)
		if mint > prev {
			t.Fatalf("mint at height %d = %d greater than previous %d", h, mint, prev)
		}
		prev = mint
	}

	expected := uint64(math.Round(float64(params.BaseBlockMintMas) * params.PhiRatio))
	got := BlockMintAt(params.EmissionIntervalBlocks)
	if got != expected {
		t.Fatalf("mint at decay boundary = %d want %d", got, expected)
	}
}

func TestSplitMint(t *testing.T) {
	n, f, x := SplitMint(0)
	if n != 0 || f != 0 || x != 0 {
		t.Fatalf("zero input split unexpected: %d %d %d", n, f, x)
	}

	n, f, x = SplitMint(100)
	if n != 80 || f != 10 || x != 10 {
		t.Fatalf("split percentages mismatch: %d %d %d", n, f, x)
	}

	n, f, x = SplitMint(1)
	if n != 1 || f != 0 || x != 0 {
		t.Fatalf("remainder should stay with network: %d %d %d", n, f, x)
	}

	if n+f+x != 1 {
		t.Fatalf("split total mismatch: %d", n+f+x)
	}
}

func TestEpochOf(t *testing.T) {
	cases := map[uint64]uint64{
		0:   0,
		99:  0,
		100: 1,
		199: 1,
	}
	for height, want := range cases {
		if got := EpochOf(height); got != want {
			t.Fatalf("epoch of %d = %d want %d", height, got, want)
		}
	}
}
