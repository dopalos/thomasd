package api

import (
	"net"
	"net/http"
	"testing"
	"time"
)

func TestLightProof(t *testing.T) {
	srv := NewHTTPServer(":0", NewDummyProofGen())

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() { _ = srv.Serve(ln) }()
	defer func() {
		_ = srv.Close()
	}()

	url := "http://" + ln.Addr().String() + "/block/7/light-proof"
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}
