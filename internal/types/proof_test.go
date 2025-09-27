package types

import (
	"testing"

	"thomasd/internal/merkle"
)

func buildStateLeaf(key, value []byte) [32]byte {
	leafInput := append(append([]byte{}, key...), 0x00)
	leafInput = append(leafInput, value...)
	return merkle.Blake3Leaf(leafInput)
}

func TestTxInclusionProof(t *testing.T) {
	leaf0 := merkle.Blake3Leaf([]byte("tx0"))
	leaf1 := merkle.Blake3Leaf([]byte("tx1"))
	root := merkle.Blake3Node(leaf0, leaf1)

	proof := TxInclusionProof{
		TxHash:     leaf0[:],
		Siblings:   [][]byte{leaf1[:]},
		Directions: []BinaryDirection{DirectionLeft},
	}

	ok, err := proof.Verify(root)
	if err != nil {
		t.Fatalf("unexpected error verifying proof: %v", err)
	}
	if !ok {
		t.Fatalf("expected proof to verify against root")
	}

	wrongRoot := merkle.Blake3Node(leaf1, leaf0)
	ok, err = proof.Verify(wrongRoot)
	if err != nil {
		t.Fatalf("unexpected error for mismatched root: %v", err)
	}
	if ok {
		t.Fatalf("expected proof verification to fail with wrong root")
	}

	bad := TxInclusionProof{TxHash: []byte{0x01}}
	if _, err := bad.Verify(root); err == nil {
		t.Fatalf("expected length check error for tx hash")
	}

	proof.Directions = nil
	if _, err := proof.Verify(root); err == nil {
		t.Fatalf("expected error for mismatched siblings/directions")
	}
}

func TestStateProof(t *testing.T) {
	key := []byte("acctA")
	value := []byte{0x01}
	leafA := buildStateLeaf(key, value)

	proof := StateProof{Key: key, Value: value}
	if ok, err := proof.VerifyBinary(leafA); err != nil || !ok {
		t.Fatalf("single-leaf proof should verify: ok=%v err=%v", ok, err)
	}

	keyB := []byte("acctB")
	valueB := []byte{0x02}
	leafB := buildStateLeaf(keyB, valueB)
	root := merkle.Blake3Node(leafA, leafB)

	proof.Siblings = [][]byte{leafB[:]}
	proof.Directions = []BinaryDirection{DirectionLeft}
	if ok, err := proof.VerifyBinary(root); err != nil || !ok {
		t.Fatalf("two-leaf proof should verify: ok=%v err=%v", ok, err)
	}

	proof.Directions = []BinaryDirection{DirectionRight}
	if ok, err := proof.VerifyBinary(root); err != nil {
		t.Fatalf("unexpected error for mismatched direction: %v", err)
	} else if ok {
		t.Fatalf("proof should not verify when direction is wrong")
	}

	proof.Siblings = nil
	if _, err := proof.VerifyBinary(root); err == nil {
		t.Fatalf("expected mismatched sibling/direction error")
	}
}
