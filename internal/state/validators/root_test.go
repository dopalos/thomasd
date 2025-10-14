//go:build !light_client
// +build !light_client

//go:build legacy_penalty

package validators

import (
	"encoding/hex"
	"testing"

	"thomasd/internal/types"
)

func TestComputeValidatorsRootDeterministic(t *testing.T) {
	records := []ScoreRecord{
		{Address: "VAL1", StakeClass: types.StakeClassG1, ConsensusScore: 10, Score: 50.5},
		{Address: "val2", StakeClass: types.StakeClassG2, ConsensusScore: -3, Score: 12.25, PoolNode: true},
	}

	root1 := ComputeValidatorsRoot(records)
	root2 := ComputeValidatorsRoot([]ScoreRecord{records[1], records[0]})
	if root1 != root2 {
		t.Fatalf("roots differ despite permutation: %s vs %s", hex.EncodeToString(root1[:]), hex.EncodeToString(root2[:]))
	}

	// Mutation safety via Clone
	records[0].Address = "mutated"
	root3 := ComputeValidatorsRoot(records)
	if root3 == root1 {
		t.Fatalf("mutation should produce different root")
	}

	next := ComputeNextValidatorsRoot([]ScoreRecord{})
	if next != ComputeValidatorsRoot(nil) {
		t.Fatalf("next validators root mismatch")
	}
}
