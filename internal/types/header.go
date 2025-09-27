package types

import (
	"fmt"

	"thomasd/internal/codec"
	"thomasd/internal/crypto"
)

type RoundHeader struct {
	Round        uint64 `json:"round"`
	FromHeight   uint64 `json:"from_height"`
	ToHeight     uint64 `json:"to_height"`
	TxCount      uint64 `json:"tx_count"`
	Root         string `json:"root"` // hex
	TimeUTC      int64  `json:"time_utc"`
	SignatureHex string `json:"signature_hex,omitempty"`
}

const (
	EternityHeaderVersionV1 uint32 = 1
	maxExtraDataLen                = 64
)

var EternityHeaderSignDomain = []byte("EternityHeader|v1|")

type EternityHeader struct {
	Version             uint32   `json:"version"`
	ChainID             string   `json:"chain_id"`
	Height              uint64   `json:"height"`
	Round               uint32   `json:"round"`
	TimeUTCUnix         int64    `json:"time_utc_unix"`
	PrevHash            [32]byte `json:"prev_hash"`
	StateRoot           [32]byte `json:"state_root"`
	TxRoot              [32]byte `json:"tx_root"`
	ReceiptsRoot        [32]byte `json:"receipts_root"`
	CommitRoot          [32]byte `json:"commit_root"`
	ProposerPubKey      [32]byte `json:"proposer_pubkey"`
	EmissionEpoch       uint64   `json:"emission_epoch"`
	ConsensusParamsHash [32]byte `json:"consensus_params_hash"`
	FeeRoot             [32]byte `json:"fee_root"`
	ExtraData           []byte   `json:"extra_data,omitempty"`
}

func (h EternityHeader) Validate() error {
	if h.Version != EternityHeaderVersionV1 {
		return fmt.Errorf("unsupported_version:%d", h.Version)
	}
	if len(h.ExtraData) > maxExtraDataLen {
		return fmt.Errorf("extra_data_too_large:%d", len(h.ExtraData))
	}
	return nil
}

func (h EternityHeader) canonicalFields() []any {
	extra := append([]byte(nil), h.ExtraData...)
	return []any{
		uint64(h.Version),
		h.ChainID,
		h.Height,
		uint64(h.Round),
		h.TimeUTCUnix,
		h.PrevHash[:],
		h.StateRoot[:],
		h.TxRoot[:],
		h.ReceiptsRoot[:],
		h.CommitRoot[:],
		h.ProposerPubKey[:],
		h.EmissionEpoch,
		h.ConsensusParamsHash[:],
		h.FeeRoot[:],
		extra,
	}
}

func (h EternityHeader) CanonicalCBOR() ([]byte, error) {
	return codec.EncodeCBORCanonical(h.canonicalFields())
}

func (h EternityHeader) Hash() ([32]byte, error) {
	b, err := h.CanonicalCBOR()
	if err != nil {
		return [32]byte{}, err
	}
	return crypto.Blake3_256(b), nil
}

func (h EternityHeader) SignBytes() ([]byte, error) {
	b, err := h.CanonicalCBOR()
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(EternityHeaderSignDomain)+len(b))
	out = append(out, EternityHeaderSignDomain...)
	out = append(out, b...)
	return out, nil
}
