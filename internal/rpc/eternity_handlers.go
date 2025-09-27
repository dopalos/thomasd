// internal/rpc/eternity_handlers.go
package rpc

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"thomasd/internal/types"

	"github.com/fxamacker/cbor/v2"
)

// Engine이 이 인터페이스를 만족하면 됩니다.
type EternityProvider interface {
	LatestSignedHeader() (types.SignedHeader, error)
	SignedHeaderByHeight(h uint64) (types.SignedHeader, error)
}

type commitBundleJSON struct {
	Round        uint32 `json:"round"`
	QuorumHash   string `json:"quorum_hash"`             // hex-32
	Bitmap       string `json:"bitmap"`                  // base64
	Step         uint8  `json:"step"`                    // 1/2/3
	AggregateSig string `json:"aggregate_sig,omitempty"` // base64
}

type signedHeaderJSON struct {
	HeaderCBOR     string            `json:"header_cbor"`     // base64
	HeaderHash     string            `json:"header_hash"`     // hex-32
	ProposerPubKey string            `json:"proposer_pubkey"` // base64(ed25519)
	Signature      string            `json:"signature"`       // base64
	CommitBundle   *commitBundleJSON `json:"commit_bundle,omitempty"`
}

type eternityHeaderJSON struct {
	Version             uint32 `json:"version"`
	ChainID             string `json:"chain_id"`
	Height              uint64 `json:"height"`
	Round               uint32 `json:"round"`
	TimeUTCUnix         int64  `json:"time_utc_unix"`
	PrevHash            string `json:"prev_hash"`       // hex-32
	StateRoot           string `json:"state_root"`      // hex-32
	TxRoot              string `json:"tx_root"`         // hex-32
	ReceiptsRoot        string `json:"receipts_root"`   // hex-32
	CommitRoot          string `json:"commit_root"`     // hex-32
	ProposerPubKey      string `json:"proposer_pubkey"` // hex-32 (32바이트)
	EmissionEpoch       uint64 `json:"emission_epoch"`
	ConsensusParamsHash string `json:"consensus_params_hash"` // hex-32
	FeeRoot             string `json:"fee_root"`              // hex-32
	ExtraData           string `json:"extra_data,omitempty"`  // base64 (<=64B)
}

// —— helpers ——
func hex32(b [32]byte) string { return hex.EncodeToString(b[:]) }
func b64(b []byte) string     { return base64.StdEncoding.EncodeToString(b) }

func toCommitBundleJSON(cb *types.CommitBundle) *commitBundleJSON {
	if cb == nil {
		return nil
	}
	out := &commitBundleJSON{
		Round:      cb.Round,
		QuorumHash: hex32(cb.QuorumHash),
		Bitmap:     b64(cb.Bitmap),
		Step:       cb.Step,
	}
	if len(cb.AggregateSig) > 0 {
		out.AggregateSig = b64(cb.AggregateSig)
	}
	return out
}

func toSignedHeaderJSON(sh types.SignedHeader) signedHeaderJSON {
	return signedHeaderJSON{
		HeaderCBOR:     b64(sh.HeaderCBOR),
		HeaderHash:     hex.EncodeToString(sh.HeaderHash[:]),
		ProposerPubKey: b64(sh.ProposerPubKey),
		Signature:      b64(sh.Signature),
		CommitBundle:   toCommitBundleJSON(sh.CommitBundle),
	}
}

// CBOR(bytes, 배열형)를 EternityHeader JSON으로 변환
func cborToEternityHeaderJSON(cborBytes []byte) (eternityHeaderJSON, error) {
	var arr []any
	if err := cbor.Unmarshal(cborBytes, &arr); err != nil {
		return eternityHeaderJSON{}, err
	}
	// 필드 순서: version, chain_id, height, round, time, prev, state, tx, receipts,
	//            commit, proposer_pubkey, emission_epoch, params_hash, fee_root, extra
	getB32 := func(i int) string {
		if i >= len(arr) || arr[i] == nil {
			return ""
		}
		if bb, ok := arr[i].([]byte); ok && len(bb) == 32 {
			return hex.EncodeToString(bb)
		}
		return ""
	}
	out := eternityHeaderJSON{
		Version:             uint32(asUint64(arr, 0)),
		ChainID:             asString(arr, 1),
		Height:              asUint64(arr, 2),
		Round:               uint32(asUint64(arr, 3)),
		TimeUTCUnix:         asInt64(arr, 4),
		PrevHash:            getB32(5),
		StateRoot:           getB32(6),
		TxRoot:              getB32(7),
		ReceiptsRoot:        getB32(8),
		CommitRoot:          getB32(9),
		ProposerPubKey:      getB32(10),
		EmissionEpoch:       asUint64(arr, 11),
		ConsensusParamsHash: getB32(12),
		FeeRoot:             getB32(13),
	}
	// extra_data (<=64B)
	if len(arr) > 14 {
		if bb, ok := arr[14].([]byte); ok && len(bb) > 0 {
			out.ExtraData = b64(bb)
		}
	}
	return out, nil
}

func asUint64(arr []any, i int) uint64 {
	if i >= len(arr) || arr[i] == nil {
		return 0
	}
	switch v := arr[i].(type) {
	case uint64:
		return v
	case uint32:
		return uint64(v)
	case int64:
		if v < 0 {
			return 0
		}
		return uint64(v)
	case int:
		if v < 0 {
			return 0
		}
		return uint64(v)
	}
	return 0
}
func asInt64(arr []any, i int) int64 {
	if i >= len(arr) || arr[i] == nil {
		return 0
	}
	switch v := arr[i].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case uint64:
		return int64(v)
	}
	return 0
}
func asString(arr []any, i int) string {
	if i >= len(arr) || arr[i] == nil {
		return ""
	}
	if s, ok := arr[i].(string); ok {
		return s
	}
	return ""
}

// —— routes ——
func RegisterEternityEndpoints(mux *http.ServeMux, p EternityProvider) {
	// latest/signed
	mux.HandleFunc("/eternity/round/latest/signed", func(w http.ResponseWriter, r *http.Request) {
		sh, err := p.LatestSignedHeader()
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, toSignedHeaderJSON(sh))
	})

	// {height}/signed  &  {height}/header
	mux.HandleFunc("/eternity/round/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/eternity/round/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 0 {
			http.NotFound(w, r)
			return
		}
		// latest/header
		if parts[0] == "latest" && len(parts) == 2 && parts[1] == "header" {
			sh, err := p.LatestSignedHeader()
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			h, err := cborToEternityHeaderJSON(sh.HeaderCBOR)
			if err != nil {
				http.Error(w, "decode_error:"+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, h)
			return
		}

		// {height}/signed or {height}/header
		h64, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			http.Error(w, "invalid height", http.StatusBadRequest)
			return
		}
		if len(parts) == 2 && parts[1] == "signed" {
			sh, err := p.SignedHeaderByHeight(h64)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSON(w, toSignedHeaderJSON(sh))
			return
		}
		if len(parts) == 2 && parts[1] == "header" {
			sh, err := p.SignedHeaderByHeight(h64)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			h, err := cborToEternityHeaderJSON(sh.HeaderCBOR)
			if err != nil {
				http.Error(w, "decode_error:"+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, h)
			return
		}
		http.NotFound(w, r)
	})
}

// 공통 JSON 응답 헬퍼 (기존에 있다면 이 부분은 생략)
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
