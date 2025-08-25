package types

type RoundHeader struct {
    Round       uint64 `json:"round"`
    FromHeight  uint64 `json:"from_height"`
    ToHeight    uint64 `json:"to_height"`
    TxCount     uint64 `json:"tx_count"`
    Root        string `json:"root"`           // hex
    TimeUTC     int64  `json:"time_utc"`
    SignatureHex string `json:"signature_hex,omitempty"` // NEW
}
