package types

type SignedHeader struct {
	HeaderCBOR     []byte        `json:"header_cbor"`     // CBOR canonical bytes
	HeaderHash     [32]byte      `json:"header_hash"`     // BLAKE3-256
	ProposerPubKey []byte        `json:"proposer_pubkey"` // ed25519
	Signature      []byte        `json:"signature"`       // ed25519(sig(sign_bytes))
	CommitBundle   *CommitBundle `json:"commit_bundle,omitempty"`
}
