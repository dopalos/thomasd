// ?뚯씪: cmd/r2p-list/main.go
package main

import (
	"flag"
	"fmt"

	"thomasd/pkg/r2pclient"
)

func updatedOf(r r2pclient.R2PRecord) int64 {
	// ?쒕쾭 ?뺣젹?ㅼ? ?숈씪???곗꽑?쒖쐞 ?먮굦?쇰줈 ?좏깮(?꾩슂???쒕쾭 濡쒖쭅??留욎떠 議곗젙)
	if r.PaidUTC != 0 {
		return r.PaidUTC
	}
	if r.DeclinedUTC != 0 {
		return r.DeclinedUTC
	}
	if r.CanceledUTC != 0 {
		return r.CanceledUTC
	}
	if r.UpdatedUTC != 0 {
		return r.UpdatedUTC
	}
	return r.CreatedUTC
}

func main() {
	base := flag.String("base", "http://127.0.0.1:8081", "base URL of thomasd")
	owner := flag.String("owner", "@alice", "owner (@alias or address)")
	status := flag.String("status", "paid", "status filter (open|paid|declined|canceled or empty)")
	role := flag.String("role", "payee", "role (payee|payer|any)")
	limit := flag.Int("limit", 2, "page size (1..200)")
	flag.Parse()

	recs, err := r2pclient.FetchAllR2P(*base, *owner, *status, *role, *limit)
	if err != nil {
		panic(err)
	}

	fmt.Printf("total=%d\n", len(recs))
	for _, r := range recs {
		fmt.Printf("%s  %s  %s  %s  updated=%d\n", r.ID, r.From, r.To, r.Status, updatedOf(r))
	}
}

