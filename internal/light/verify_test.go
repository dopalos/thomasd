package light

import (
	"testing"

	"thomasd/internal/types"
)

func buildConsistentProof() Proof {
	header := &types.EternityHeader{
		ChainID:         999,
		Height:          1,
		Round:           2,
		ProposerSetHash: [32]byte{0xAA},
	}

	bundle := types.CommitBundle{Height: 1, Round: 2}
	for i := 0; i < 3; i++ {
		header.Commit = bundle
		header.CommitHash = bundle.Root()
		newID := header.BlockID()
		if bundle.BlockID == newID {
			break
		}
		bundle.BlockID = newID
	}
	header.Commit = bundle
	header.CommitHash = bundle.Root()
	if finalID := header.BlockID(); bundle.BlockID != finalID {
		bundle.BlockID = finalID
		header.Commit = bundle
		header.CommitHash = bundle.Root()
	}

	bundleCopy := bundle
	return Proof{
		Header:         header,
		CommitBundle:   &bundleCopy,
		ValidatorsRoot: header.ProposerSetHash,
	}
}

func TestVerifySuccess(t *testing.T) {
	proof := buildConsistentProof()
	if err := Verify(proof); err != nil {
		t.Fatalf("expected proof to verify, got %v", err)
	}
}

func TestVerifyFailures(t *testing.T) {
	if err := Verify(Proof{}); err != ErrNilHeader {
		t.Fatalf("expected ErrNilHeader, got %v", err)
	}

	proof := Proof{Header: &types.EternityHeader{}}
	if err := Verify(proof); err != ErrNilCommitBundle {
		t.Fatalf("expected ErrNilCommitBundle, got %v", err)
	}

	proof = buildConsistentProof()
	proof.CommitBundle.BlockID[0] ^= 0xFF
	if err := Verify(proof); err != ErrCommitMetadataMismatch {
		t.Fatalf("expected metadata mismatch, got %v", err)
	}

	proof = buildConsistentProof()
	proof.CommitBundle.Signatures = append(proof.CommitBundle.Signatures, types.CommitSig{ValidatorIndex: 1, Step: types.CommitStepPrecommit, Signature: []byte{0x01}})
	if err := Verify(proof); err != ErrCommitRootMismatch {
		t.Fatalf("expected commit root mismatch, got %v", err)
	}

	proof = buildConsistentProof()
	proof.ValidatorsRoot = [32]byte{}
	if err := Verify(proof); err != ErrValidatorRootMismatch {
		t.Fatalf("expected validator root mismatch, got %v", err)
	}
}
