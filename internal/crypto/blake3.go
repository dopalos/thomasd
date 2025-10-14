package crypto

import "github.com/zeebo/blake3"

// Blake3_256 returns the 32-byte BLAKE3 digest of the provided data.
func Blake3_256(b []byte) [32]byte { return blake3.Sum256(b) }
