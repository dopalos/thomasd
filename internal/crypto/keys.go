// internal/crypto/keys.go
package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Generate creates a new ed25519 keypair.
func Generate() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	return pub, priv, err
}

// FromPrivHex restores keypair from a hex string.
//
// Accepts:
// - 128-hex (64-byte) ed25519 private key
// - 64-hex (32-byte) seed -> ed25519.NewKeyFromSeed
func FromPrivHex(hexStr string) (ed25519.PublicKey, ed25519.PrivateKey, error) {
	hexStr = strings.TrimSpace(hexStr)
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, nil, err
	}
	switch len(b) {
	case ed25519.PrivateKeySize: // 64
		priv := ed25519.PrivateKey(b)
		pub := priv.Public().(ed25519.PublicKey)
		return pub, priv, nil
	case ed25519.SeedSize: // 32
		priv := ed25519.NewKeyFromSeed(b)
		pub := priv.Public().(ed25519.PublicKey)
		return pub, priv, nil
	default:
		return nil, nil, errors.New("bad private key length")
	}
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// defaultKeyPath decides a safe key file path from optional args/env/cwd.
// Priority:
// 1) args[0]: if dir -> join(dir,"node.key"); if file -> as-is
// 2) THOMAS_DATA_DIR -> join(env,"node.key")
// 3) ./node.key
func defaultKeyPath(args ...string) string {
	if len(args) > 0 && args[0] != "" {
		a0 := args[0]
		if isDir(a0) || strings.HasSuffix(a0, "/") || strings.HasSuffix(a0, `\`) {
			return filepath.Join(a0, "node.key")
		}
		return a0
	}
	if dd := strings.TrimSpace(os.Getenv("THOMAS_DATA_DIR")); dd != "" {
		return filepath.Join(dd, "node.key")
	}
	return "node.key"
}

// LoadOrCreate returns (pub, priv, err) to match engine.go usage.
// It loads existing key from file or creates/saves a new one.
//
// Usage examples:
//
//	LoadOrCreate()                   -> ./node.key
//	LoadOrCreate("C:\\data")         -> C:\data\node.key
//	LoadOrCreate("C:\\data\\my.key") -> that exact file
func LoadOrCreate(args ...string) (ed25519.PublicKey, ed25519.PrivateKey, error) {
	keyPath := defaultKeyPath(args...)

	// Try load
	if b, err := os.ReadFile(keyPath); err == nil {
		txt := strings.TrimSpace(string(b))
		return FromPrivHex(txt)
	}

	// Create new
	pub, priv, err := Generate()
	if err != nil {
		return nil, nil, err
	}

	// Ensure dir then save as hex(private key, 64 bytes -> 128 hex)
	dir := filepath.Dir(keyPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
		}
	}
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(priv)+"\n"), 0o600); err != nil {
		return nil, nil, err
	}
	return pub, priv, nil
}
