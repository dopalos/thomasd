package merkle

import "crypto/sha256"

// Leaf = H(0x00 || data), Inner = H(0x01 || L || R)
func leafHash(b []byte) []byte {
    s := sha256.Sum256(append([]byte{0x00}, b...))
    return s[:]
}
func innerHash(l, r []byte) []byte {
    buf := make([]byte, 1+len(l)+len(r))
    buf[0] = 0x01
    copy(buf[1:], l)
    copy(buf[1+len(l):], r)
    s := sha256.Sum256(buf)
    return s[:]
}

func Root(leaves [][]byte) []byte {
    n := len(leaves)
    if n == 0 {
        z := sha256.Sum256([]byte{0x00})
        return z[:]
    }
    level := make([][]byte, n)
    for i := 0; i < n; i++ {
        level[i] = leafHash(leaves[i])
    }
    for len(level) > 1 {
        if len(level)%2 == 1 {
            level = append(level, level[len(level)-1]) // odd → duplicate last
        }
        next := make([][]byte, len(level)/2)
        for i := 0; i < len(level); i += 2 {
            next[i/2] = innerHash(level[i], level[i+1])
        }
        level = next
    }
    return level[0]
}
