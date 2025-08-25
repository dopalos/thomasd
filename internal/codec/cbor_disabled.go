//go:build !cbor

package codec

import (
"errors"
	"thomasd/internal/tx"
)

var ErrCBORDisabled = errors.New("cbor_not_enabled")

func DecodeCBOR(b []byte, out *tx.Transfer) error { return ErrCBORDisabled }

