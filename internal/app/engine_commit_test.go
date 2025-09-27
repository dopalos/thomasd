package app

import (
	"testing"

	"thomasd/internal/types"
)

func TestLatestCommitRootUpdates(t *testing.T) {
	eng := NewEngine()
	if _, _, ok := eng.LatestCommitRoot(); ok {
		t.Fatalf("expected no commit root yet")
	}

	eng.mu.Lock()
	eng.leaves = append(eng.leaves, []byte("tx"))
	eng.height = uint64(len(eng.leaves))
	header, err := eng.buildEternityHeader(nil, types.RoundHeader{
		Round:    1,
		ToHeight: eng.height,
		TimeUTC:  0,
	})
	eng.mu.Unlock()
	if err != nil {
		t.Fatalf("build header: %v", err)
	}

	bundle := types.CommitBundle{
		Height:  header.Height,
		Round:   header.Round,
		BlockID: header.BlockID(),
		Signatures: []types.CommitSig{
			{ValidatorIndex: 0, Step: types.CommitStepPrecommit, Signature: []byte{0xAA}},
		},
	}
	eng.RecordCommitBundle(bundle)

	root, height, ok := eng.LatestCommitRoot()
	if !ok {
		t.Fatalf("expected commit root after header build")
	}
	bundle, ok = eng.CommitBundleAt(height)
	if !ok {
		t.Fatalf("expected commit bundle in feed")
	}
	if len(bundle.Signatures) == 0 {
		t.Fatalf("expected signatures in commit bundle")
	}
	if root != bundle.Root() {
		t.Fatalf("commit root mismatch")
	}
}
