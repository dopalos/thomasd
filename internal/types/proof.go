package types

import (
	"errors"

	"thomasd/internal/merkle"
)

type BinaryDirection uint8

const (
	DirectionLeft  BinaryDirection = 0
	DirectionRight BinaryDirection = 1
)

type TxInclusionProof struct {
	TxHash     []byte
	Siblings   [][]byte
	Directions []BinaryDirection
}

func (p TxInclusionProof) Verify(root [32]byte) (bool, error) {
	if len(p.TxHash) != 32 {
		return false, errors.New("tx proof: tx hash must be 32 bytes")
	}
	leaf, err := merkle.BytesTo32(p.TxHash)
	if err != nil {
		return false, err
	}
	dirs := make([]uint8, len(p.Directions))
	for i, d := range p.Directions {
		dirs[i] = uint8(d)
	}
	computed, err := merkle.ComputeBlake3Root(leaf, p.Siblings, dirs)
	if err != nil {
		return false, err
	}
	return computed == root, nil
}

type StateProof struct {
	Key        []byte
	Value      []byte
	Siblings   [][]byte
	Directions []BinaryDirection
	Version    []byte
}

func (p StateProof) VerifyBinary(root [32]byte) (bool, error) {
	leafInput := append(append([]byte{}, p.Key...), 0x00)
	leafInput = append(leafInput, p.Value...)
	leaf := merkle.Blake3Leaf(leafInput)
	dirs := make([]uint8, len(p.Directions))
	for i, d := range p.Directions {
		dirs[i] = uint8(d)
	}
	computed, err := merkle.ComputeBlake3Root(leaf, p.Siblings, dirs)
	if err != nil {
		return false, err
	}
	return computed == root, nil
}
