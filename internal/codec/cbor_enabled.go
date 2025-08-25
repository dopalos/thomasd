//go:build cbor

package codec

import (
"thomasd/internal/tx"

	"github.com/fxamacker/cbor/v2"
)

func DecodeCBOR(b []byte, out *tx.Transfer) error { return cbor.Unmarshal(b, out) }

