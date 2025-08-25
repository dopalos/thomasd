//go:build !blake3

package tx

import "crypto/sha256"

func hashBytes(b []byte) []byte {
    sum := sha256.Sum256(b)
    return sum[:]
}
