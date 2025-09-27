package types

import (
	"encoding/hex"
	"testing"
)

func TestCommitBundleRootDeterministic(t *testing.T) {
	var blockID [32]byte
	blockID[0] = 0xAA

	bundle := CommitBundle{
		Height:  1,
		Round:   2,
		BlockID: blockID,
		Signatures: []CommitSig{
			{ValidatorIndex: 2, Step: CommitStepPrecommit, PublicKey: []byte{0x02, 0x03}, Signature: []byte{0x0A}},
			{ValidatorIndex: 1, Step: CommitStepPrevote, PublicKey: []byte{0x01}, Signature: []byte{0x0B, 0x0C}},
		},
	}

	root1 := bundle.Root()
	bundlePermuted := CommitBundle{
		Height:  1,
		Round:   2,
		BlockID: blockID,
		Signatures: []CommitSig{
			bundle.Signatures[1],
			bundle.Signatures[0],
		},
	}
	root2 := bundlePermuted.Root()
	if root1 != root2 {
		t.Fatalf("roots differ for permutation: %s vs %s", hex.EncodeToString(root1[:]), hex.EncodeToString(root2[:]))
	}

	modified := bundle
	modified.Signatures[0].Signature = []byte{0xFF}
	root3 := modified.Root()
	if root3 == root1 {
		t.Fatalf("expected signature change to alter root")
	}

	emptyRoot := (CommitBundle{}).Root()
	if emptyRoot == ([32]byte{}) {
		t.Fatalf("empty bundle root should not be zero")
	}
}
