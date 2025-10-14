  //go:build light_client
  // +build light_client

package state

import "testing"

func TestComputeValidatorsRootDeterministic(t *testing.T) {
	recs := []ValidatorScoreRecord{
		{Address: "ValidatorA", StakeClass: "g1", ConsensusScore: 5, Participation: 1},
		{Address: "ValidatorB", StakeClass: "g2", ConsensusScore: 3.5, PoolNode: true},
	}
	root1 := ComputeValidatorsRoot(recs)
	recsSwap := []ValidatorScoreRecord{recs[1], recs[0]}
	root2 := ComputeValidatorsRoot(recsSwap)
	if root1 != root2 {
		t.Fatalf("validator root should be deterministic: %x vs %x", root1, root2)
	}
}
