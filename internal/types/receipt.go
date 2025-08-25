package types

type TxReceipt struct {
    TxHash   string `json:"tx_hash"`
    From     string `json:"from"`
    To       string `json:"to"`
    Amount   uint64 `json:"amount"`
    Fee      uint64 `json:"fee"`
    Nonce    uint64 `json:"nonce"`
    Status   string `json:"status"`  // "applied" 또는 "rejected:<reason>"
    Height   uint64 `json:"height"`
    TimeUTC  int64  `json:"time_utc"`
}
