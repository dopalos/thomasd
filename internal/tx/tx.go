package tx

import (
	"encoding/base64"

	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type Transfer struct {
	Type         int    `json:"type"           cbor:"type"`
	From         string `json:"from"           cbor:"from"`
	To           string `json:"to"             cbor:"to"`
	AmountMas    uint64 `json:"amount_mas"     cbor:"amount_mas"`
	FeeMas       uint64 `json:"fee_mas"        cbor:"fee_mas"`
	Nonce        uint64 `json:"nonce"          cbor:"nonce"`
	ChainID      string `json:"chain_id"       cbor:"chain_id"`
	ExpiryHeight uint64 `json:"expiry_height"  cbor:"expiry_height"`
	MsgCommit    string `json:"msg_commitment" cbor:"msg_commitment"`
} // ?명솚 ?낅젰: amount/fee(關THO) ?먮뒗 amount_關THO/fee_關THO ??mas濡?蹂??횞10)
func (t *Transfer) UnmarshalJSON(b []byte) error {
	type Alias Transfer
	var aux struct {
		Alias
		// mas ?곗꽑
		AmountMas *uint64 `json:"amount_mas"`
		FeeMas    *uint64 `json:"fee_mas"`
		// legacy 關THO(?????덉슜)
		AmountMicro1 *uint64 `json:"amount"`
		AmountMicro2 *uint64 `json:"amount_關THO"`
		FeeMicro1    *uint64 `json:"fee"`
		FeeMicro2    *uint64 `json:"fee_關THO"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	*t = Transfer(aux.Alias)

	// amount
	if aux.AmountMas != nil {
		t.AmountMas = *aux.AmountMas
	} else if aux.AmountMicro1 != nil {
		t.AmountMas = *aux.AmountMicro1 * 10
	} else if aux.AmountMicro2 != nil {
		t.AmountMas = *aux.AmountMicro2 * 10
	}
	// fee
	if aux.FeeMas != nil {
		t.FeeMas = *aux.FeeMas
	} else if aux.FeeMicro1 != nil {
		t.FeeMas = *aux.FeeMicro1 * 10
	} else if aux.FeeMicro2 != nil {
		t.FeeMas = *aux.FeeMicro2 * 10
	}
	return nil
}

// ?댁떆: canonical mas ?꾨뱶 湲곗??쇰줈 JSON ??SHA-256
func (t Transfer) Hash() string {
	type Canon struct {
		Type         uint64 `json:"type"`
		From         string `json:"from"`
		To           string `json:"to"`
		AmountMas    uint64 `json:"amount_mas"`
		FeeMas       uint64 `json:"fee_mas"`
		Nonce        uint64 `json:"nonce"`
		ChainID      string `json:"chain_id"`
		ExpiryHeight uint64 `json:"expiry_height"`
		MsgCommit    []byte `json:"msg_commitment"`
	}
	b, _ := json.Marshal(Canon{
		Type: uint64(t.Type), From: t.From, To: t.To, AmountMas: t.AmountMas, FeeMas: t.FeeMas,
		Nonce: t.Nonce, ChainID: t.ChainID, ExpiryHeight: t.ExpiryHeight, MsgCommit: decodeMsgCommit(t.MsgCommit),
	})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func decodeMsgCommit(s string) []byte {
	if s == "" {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		// base64 ?꾨땲硫??먮Ц??洹몃?濡?諛붿씠?몃줈
		return []byte(s)
	}
	return b
}
