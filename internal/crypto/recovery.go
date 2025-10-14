package crypto

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"

	"github.com/tyler-smith/go-bip39"
	"golang.org/x/crypto/hkdf"
)

// 내부 도메인 구분자: 파생 규칙 고정 (v1)
const (
	hkdfSalt       = "THO-WALLET-v1"
	hkdfInfoMaster = "ed25519-master"
)

// RecoveryBundle: 응답에 쓰는 최소 정보(개인키는 응답하지 않음)
type RecoveryBundle struct {
	Mnemonic    string             `json:"mnemonic"`
	Fingerprint string             `json:"fingerprint"`
	PubKeyHex   string             `json:"pubkey_hex"`
	priv        ed25519.PrivateKey // 서버 응답에 내보내지 않음
}

func NewRecoveryBundle(passphrase string) (*RecoveryBundle, error) {
	entropy, err := bip39.NewEntropy(128) // 12 words
	if err != nil {
		return nil, err
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return nil, err
	}
	pub, priv, err := DeriveFromMnemonic(mnemonic, passphrase)
	if err != nil {
		return nil, err
	}
	fp := sha256.Sum256([]byte(strings.TrimSpace(mnemonic)))
	return &RecoveryBundle{
		Mnemonic:    mnemonic,
		Fingerprint: hex.EncodeToString(fp[:4]), // 짧은 식별자
		PubKeyHex:   hex.EncodeToString(pub),
		priv:        priv,
	}, nil
}

func DeriveFromMnemonic(mnemonic, passphrase string) (ed25519.PublicKey, ed25519.PrivateKey, error) {
	seed := bip39.NewSeed(mnemonic, passphrase) // 64 bytes
	// seed는 아래 defer로 반드시 지움
	defer zero(seed)

	r := hkdf.New(sha256.New, seed, []byte(hkdfSalt), []byte(hkdfInfoMaster))
	keySeed := make([]byte, 32)
	if _, err := io.ReadFull(r, keySeed); err != nil {
		zero(keySeed)
		return nil, nil, err
	}

	priv := ed25519.NewKeyFromSeed(keySeed)
	// seed 재사용 방지
	zero(keySeed)

	pub := priv.Public().(ed25519.PublicKey)
	return pub, priv, nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
