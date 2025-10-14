package types

import (
	"fmt"

	"thomasd/internal/codec"
	"thomasd/internal/crypto"
)

type CommitBundle struct {
	Round        uint32   `json:"round"`
	QuorumHash   [32]byte `json:"quorum_hash"`
	Bitmap       []byte   `json:"bitmap"`
	Step         uint8    `json:"step"`                    // 1=proposal, 2=prevote, 3=precommit
	AggregateSig []byte   `json:"aggregate_sig,omitempty"` // optional
}

func (c CommitBundle) canonicalFields() []any {
	bitmap := append([]byte(nil), c.Bitmap...)
	aggregate := append([]byte(nil), c.AggregateSig...)
	return []any{
		uint64(c.Round),
		c.QuorumHash[:],
		bitmap,
		uint64(c.Step),
		aggregate,
	}
}

func (c CommitBundle) CanonicalCBOR() ([]byte, error) {
	return codec.EncodeCBORCanonical(c.canonicalFields())
}

func (c CommitBundle) Hash() ([32]byte, error) {
	b, err := c.CanonicalCBOR()
	if err != nil {
		return [32]byte{}, err
	}
	return crypto.Blake3_256(b), nil
}

func (c CommitBundle) Validate() error {
	if len(c.Bitmap) == 0 {
		return fmt.Errorf("bitmap_required")
	}
	if c.Step == 0 {
		return fmt.Errorf("step_required")
	}
	return nil
}
