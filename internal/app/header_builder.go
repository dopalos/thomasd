package app

import (
	"crypto/ed25519"
	"fmt"
	"time"

	"thomasd/internal/crypto"
	"thomasd/internal/types"
)

type BuildHeaderParams struct {
	ChainID             string
	Height              uint64
	Round               uint32
	TimeUTC             time.Time
	PrevHash            [32]byte
	StateRoot           [32]byte
	TxRoot              [32]byte
	ReceiptsRoot        [32]byte
	CommitBundle        *types.CommitBundle
	ProposerPubKey      [32]byte
	EmissionEpoch       uint64
	ConsensusParamsHash [32]byte
	FeeRoot             [32]byte
	ExtraData           []byte
}

// 순수 조립 (서명 X)
func BuildEternityHeader(p BuildHeaderParams) (types.EternityHeader, error) {
	if p.TimeUTC.IsZero() {
		p.TimeUTC = time.Now().UTC()
	}
	var commitRoot [32]byte
	if p.CommitBundle != nil {
		h, err := p.CommitBundle.Hash()
		if err != nil {
			return types.EternityHeader{}, err
		}
		commitRoot = h
	}
	h := types.EternityHeader{
		Version:             types.EternityHeaderVersionV1,
		ChainID:             p.ChainID,
		Height:              p.Height,
		Round:               p.Round,
		TimeUTCUnix:         p.TimeUTC.UTC().Unix(),
		PrevHash:            p.PrevHash,
		StateRoot:           p.StateRoot,
		TxRoot:              p.TxRoot,
		ReceiptsRoot:        p.ReceiptsRoot,
		CommitRoot:          commitRoot,
		ProposerPubKey:      p.ProposerPubKey,
		EmissionEpoch:       p.EmissionEpoch,
		ConsensusParamsHash: p.ConsensusParamsHash,
		FeeRoot:             p.FeeRoot,
		ExtraData:           append([]byte(nil), p.ExtraData...),
	}
	if err := h.Validate(); err != nil {
		return types.EternityHeader{}, err
	}
	return h, nil
}

// 서명까지 포함
func BuildSignedEternityHeader(p BuildHeaderParams, priv ed25519.PrivateKey) (types.EternityHeader, types.SignedHeader, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return types.EternityHeader{}, types.SignedHeader{}, fmt.Errorf("invalid_ed25519_private_key")
	}
	h, err := BuildEternityHeader(p)
	if err != nil {
		return types.EternityHeader{}, types.SignedHeader{}, err
	}
	signBytes, err := h.SignBytes()
	if err != nil {
		return types.EternityHeader{}, types.SignedHeader{}, err
	}
	sig := ed25519.Sign(priv, signBytes)
	cborBytes, err := h.CanonicalCBOR()
	if err != nil {
		return types.EternityHeader{}, types.SignedHeader{}, err
	}
	hash, err := h.Hash()
	if err != nil {
		return types.EternityHeader{}, types.SignedHeader{}, err
	}
	sh := types.SignedHeader{
		HeaderCBOR:     cborBytes,
		HeaderHash:     hash,
		ProposerPubKey: append([]byte(nil), p.ProposerPubKey[:]...),
		Signature:      sig,
		CommitBundle:   p.CommitBundle,
	}
	return h, sh, nil
}

// 간단 유틸: 비어있는 해시 여부
func Bytes32IsZero(b [32]byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// 임시: StateRoot가 비어있으면 ValidatorsRoot로 대체하는 예시
func DeriveStateRootFallback(validatorsRoot [32]byte, stateRoot [32]byte) [32]byte {
	if !Bytes32IsZero(stateRoot) {
		return stateRoot
	}
	// v1 임시 정책: validatorsRoot 단독으로 StateRoot를 구성 (후속 PR에서 계정/스토리지 포함 확장)
	return crypto.Blake3_256(append([]byte("state|v1|"), validatorsRoot[:]...))
}
