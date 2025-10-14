//go:build !light_client
// +build !light_client

package rpc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"thomasd/internal/light"
	"thomasd/internal/types"
)

func buildProof() light.Proof {
	header := &types.EternityHeader{
		ChainID:         999,
		Height:          10,
		Round:           2,
		ProposerSetHash: [32]byte{0x55},
	}
	bundle := types.CommitBundle{Height: header.Height, Round: header.Round}
	for i := 0; i < 3; i++ {
		header.Commit = bundle
		header.CommitHash = bundle.Root()
		id := header.BlockID()
		if bundle.BlockID == id {
			break
		}
		bundle.BlockID = id
	}
	header.Commit = bundle
	header.CommitHash = bundle.Root()

	return light.Proof{
		Header:         header,
		CommitBundle:   &bundle,
		ValidatorsRoot: header.ProposerSetHash,
	}
}

func TestLightVerifyEndpoint(t *testing.T) {
	proof := buildProof()
	rootHex := hex.EncodeToString(proof.ValidatorsRoot[:])

	goodPayload := map[string]any{
		"header":              proof.Header,
		"commit_bundle":       proof.CommitBundle,
		"validators_root_hex": rootHex,
	}
	body, err := json.Marshal(goodPayload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/block/light-verify", bytes.NewReader(body))
	resp := httptest.NewRecorder()
	Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.Code)
	}

	badProof := buildProof()
	badProof.Header.ProposerSetHash[0] ^= 0xFF

	badPayload := map[string]any{
		"header":              badProof.Header,
		"commit_bundle":       badProof.CommitBundle,
		"validators_root_hex": rootHex,
	}
	body, err = json.Marshal(badPayload)
	if err != nil {
		t.Fatalf("marshal bad payload: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/block/light-verify", bytes.NewReader(body))
	resp = httptest.NewRecorder()
	Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status=%d body=%s header=%x validatorsRootHex=%s",
			resp.Code, resp.Body.String(), badProof.Header.ProposerSetHash, rootHex)
	}
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad root, got %d", resp.Code)
	}
}
