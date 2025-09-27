package r2pclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type Pager struct {
	Base   string
	Owner  string
	Role   string // payee|payer|any
	Status string // open|paid|declined|canceled|""=all
	Limit  int

	client *http.Client
	cursor string
	done   bool
}

func NewPager(base, owner, role, status string, limit int) *Pager {
	if limit <= 0 {
		limit = 50
	}
	return &Pager{
		Base: base, Owner: owner, Role: role, Status: status, Limit: limit,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *Pager) Next() ([]R2PRecord, bool, error) {
	if p.done {
		return nil, true, nil
	}

	u, _ := url.Parse(p.Base + "/r2p/list")
	q := u.Query()
	q.Set("owner", p.Owner)
	if p.Role != "" {
		q.Set("role", p.Role)
	}
	if p.Status != "" {
		q.Set("status", p.Status)
	}
	q.Set("limit", fmt.Sprint(p.Limit))
	if p.cursor != "" {
		q.Set("cursor", p.cursor)
	}
	u.RawQuery = q.Encode()

	resp, err := p.client.Get(u.String())
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	var lr struct {
		OK         bool        `json:"ok"`
		Records    []R2PRecord `json:"records"`
		NextCursor string      `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return nil, false, err
	}
	if !lr.OK {
		return nil, false, fmt.Errorf("ok=false")
	}

	if lr.NextCursor == "" {
		p.done = true
	} else {
		p.cursor = lr.NextCursor
	}
	return lr.Records, p.done, nil
}

