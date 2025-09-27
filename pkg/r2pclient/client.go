package r2pclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type R2PRecord struct {
	ID          string `json:"id"`
	From        string `json:"from"`
	To          string `json:"to"`
	Status      string `json:"status"`
	CreatedUTC  int64  `json:"created_utc"`
	UpdatedUTC  int64  `json:"updated_utc"`
	PaidUTC     int64  `json:"paid_utc"`
	DeclinedUTC int64  `json:"declined_utc"`
	CanceledUTC int64  `json:"canceled_utc"`
}

// ?レ옄/臾몄옄??而ㅼ꽌 紐⑤몢 ?덉슜
type Cursor string

func (c *Cursor) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*c = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*c = Cursor(s)
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(b, &num); err != nil {
		return err
	}
	*c = Cursor(num.String())
	return nil
}

type listResp struct {
	OK         bool        `json:"ok"`
	Records    []R2PRecord `json:"records"`
	NextCursor Cursor      `json:"next_cursor"`
}

func FetchAllR2P(base, owner, status, role string, limit int) ([]R2PRecord, error) {
	if role == "" {
		role = "payee"
	}
	if limit <= 0 {
		limit = 50
	}

	client := &http.Client{Timeout: 10 * time.Second}
	cursor := ""
	all := make([]R2PRecord, 0, 256)

	for i := 0; i < 100; i++ {
		u, _ := url.Parse(base + "/r2p/list")
		q := u.Query()
		q.Set("owner", owner)
		if status != "" {
			q.Set("status", status)
		}
		q.Set("role", role)
		q.Set("limit", fmt.Sprint(limit))
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		u.RawQuery = q.Encode()

		resp, err := client.Get(u.String())
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("bad status %d", resp.StatusCode)
		}

		var lr listResp
		if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
			return nil, err
		}
		if !lr.OK {
			return nil, fmt.Errorf("ok=false")
		}

		all = append(all, lr.Records...)
		if string(lr.NextCursor) == "" {
			break
		}
		cursor = string(lr.NextCursor)
	}
	return all, nil
}

