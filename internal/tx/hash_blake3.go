//go:build blake3

package tx

import "github.com/zeebo/blake3"

func hashBytes(b []byte) []byte {
    h := blake3.New()
    h.Write(b)
    out := make([]byte, 32)
    h.Sum(out[:0])
    return out
}
