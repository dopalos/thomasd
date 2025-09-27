package merkle

import (
	"errors"

	"github.com/zeebo/blake3"
)

const (
	leafTag  = byte(0x00)
	innerTag = byte(0x01)
)

// Blake3Leaf returns the leaf hash used by Eternity headers for tx/state trees.
func Blake3Leaf(data []byte) [32]byte {
	payload := append([]byte{leafTag}, data...)
	sum := blake3.Sum256(payload)
	return sum
}

// Blake3Node returns the inner hash given two children.
func Blake3Node(left, right [32]byte) [32]byte {
	buf := make([]byte, 1+len(left)+len(right))
	buf[0] = innerTag
	copy(buf[1:], left[:])
	copy(buf[1+len(left):], right[:])
	sum := blake3.Sum256(buf)
	return sum
}

// ComputeBlake3Root rebuilds the merkle root using the provided leaf and proof
// information. Directions uses 0 for "leaf is left" and 1 for "leaf is right".
func ComputeBlake3Root(leaf [32]byte, siblings [][]byte, directions []uint8) ([32]byte, error) {
	if len(siblings) != len(directions) {
		return [32]byte{}, errors.New("merkle: mismatched siblings/directions length")
	}
	node := leaf
	for i := range siblings {
		sib, err := BytesTo32(siblings[i])
		if err != nil {
			return [32]byte{}, err
		}
		switch directions[i] {
		case 0:
			node = Blake3Node(node, sib)
		case 1:
			node = Blake3Node(sib, node)
		default:
			return [32]byte{}, errors.New("merkle: invalid direction value")
		}
	}
	return node, nil
}

// BytesTo32 copies a byte slice into a 32-byte array.
func BytesTo32(b []byte) ([32]byte, error) {
	var out [32]byte
	if len(b) != len(out) {
		return out, errors.New("expected 32 byte input")
	}
	copy(out[:], b)
	return out, nil
}

// Blake3RootFromRaw takes raw leaf payloads (before leaf hashing) and returns the
// merkle root using BLAKE3 domain separation.
func Blake3RootFromRaw(rawLeaves [][]byte) [32]byte {
	if len(rawLeaves) == 0 {
		// match ComputeBlake3Root(empty) by hashing the leaf tag only
		sum := blake3.Sum256([]byte{leafTag})
		return sum
	}
	current := make([][32]byte, len(rawLeaves))
	for i, leaf := range rawLeaves {
		current[i] = Blake3Leaf(leaf)
	}
	for len(current) > 1 {
		if len(current)%2 == 1 {
			current = append(current, current[len(current)-1])
		}
		next := make([][32]byte, len(current)/2)
		for i := 0; i < len(current); i += 2 {
			next[i/2] = Blake3Node(current[i], current[i+1])
		}
		current = next
	}
	return current[0]
}
