package main

import (
"flag"
    "os"

    "github.com/fxamacker/cbor/v2"
)

func main() {
    var amount, fee, nonce int
    flag.IntVar(&amount, "amount", 10, "amount_mas")
    flag.IntVar(&fee, "fee", 1, "fee_mas")
    flag.IntVar(&nonce, "nonce", 1, "nonce")
    flag.Parse()

    m := map[string]any{
        "type":           1,
        "from":           "tho1alice",
        "to":             "tho1bob",
        "amount_mas":     amount,
        "fee_mas":        fee,
        "nonce":          nonce,
        "chain_id":       "thomas-dev-1",
        "expiry_height":  0,
        "msg_commitment": "",
    }
    b, _ := cbor.Marshal(m)
    _ = os.WriteFile("tx.cbor", b, 0o644)
}

