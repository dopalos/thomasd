package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	flagBase   = flag.String("base", "", "Base URL of thomasd (e.g. http://127.0.0.1:62533)")
	flagListen = flag.String("listen", ":63080", "Listen address for proxy (e.g. :63080)")
	httpc      = &http.Client{Timeout: 5 * time.Second}
)

type signedResp struct {
	Algo   string `json:"algo"`
	Header hdr    `json:"header"`
	PubHex string `json:"pubkey_hex"`
	SigHex string `json:"signature_hex"`
}

type hdr struct {
	Round      int    `json:"round"`
	FromHeight int    `json:"from_height"`
	ToHeight   int    `json:"to_height"`
	TxCount    int    `json:"tx_count"`
	Root       string `json:"root"`
	TimeUTC    int64  `json:"time_utc"`
}

type outMsg struct {
	Algo           string `json:"algo"`
	Header         hdr    `json:"header"`
	MessageHex     string `json:"message_hex,omitempty"`
	MessageB64     string `json:"message_b64,omitempty"`
	PubKeyHex      string `json:"pubkey_hex"`
	SignatureHex   string `json:"signature_hex"`
	SignatureValid bool   `json:"signature_valid"`
}

func main() {
	flag.Parse()
	base := firstNonEmpty(*flagBase, os.Getenv("THOMAS_BASE"))
	if base == "" {
		log.Fatal("missing -base (or THOMAS_BASE)")
	}

	mux := http.NewServeMux()

	// health passthrough
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		j, code, _ := proxyGET(base + "/health")
		writeJSON(w, code, j)
	})

	// latest
	mux.HandleFunc("/round/latest/signed_msg", func(w http.ResponseWriter, r *http.Request) {
		handleSignedMsg(w, r, base, "latest")
	})

	// by round
	mux.HandleFunc("/round/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/round/"), "/")
		if len(parts) == 2 && parts[1] == "signed_msg" {
			if parts[0] == "latest" {
				handleSignedMsg(w, r, base, "latest")
				return
			}
			n, err := strconv.Atoi(parts[0])
			if err != nil {
				writeErr(w, http.StatusBadRequest, "bad_request", "round must be integer or 'latest'")
				return
			}
			handleSignedMsg(w, r, base, strconv.Itoa(n))
			return
		}
		writeErr(w, http.StatusNotFound, "not_found", "unknown path")
	})

	// optional passthrough
	mux.HandleFunc("/tx/", func(w http.ResponseWriter, r *http.Request) {
		hash := strings.TrimPrefix(r.URL.Path, "/tx/")
		if hash == "" {
			writeErr(w, http.StatusBadRequest, "bad_request", "missing tx hash")
			return
		}
		j, code, err := proxyGET(base + "/tx/" + hash)
		if err != nil && code == 0 {
			writeErr(w, http.StatusBadGateway, "bad_gateway", err.Error())
			return
		}
		writeJSON(w, code, j)
	})

	s := &http.Server{
		Addr:              *flagListen,
		Handler:           logMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("[signedmsg-proxy] base=%s listen=http://127.0.0.1%s", base, *flagListen)
	if err := s.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func handleSignedMsg(w http.ResponseWriter, r *http.Request, base string, round string) {
	url := fmt.Sprintf("%s/round/%s/signed", base, round)
	j, code, err := proxyGET(url)
	if err != nil && code == 0 {
		writeErr(w, http.StatusBadGateway, "bad_gateway", err.Error())
		return
	}
	if code != 200 {
		writeJSON(w, code, j)
		return
	}

	var s signedResp
	if err := json.Unmarshal(j, &s); err != nil {
		writeErr(w, http.StatusBadGateway, "bad_gateway", "unmarshal backend signed")
		return
	}

	hjson, err := json.Marshal(hdr{
		Round:      s.Header.Round,
		FromHeight: s.Header.FromHeight,
		ToHeight:   s.Header.ToHeight,
		TxCount:    s.Header.TxCount,
		Root:       s.Header.Root,
		TimeUTC:    s.Header.TimeUTC,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal", "marshal header")
		return
	}
	msg := "THOMAS|hdr|v1|" + string(hjson)
	msgHex := hex.EncodeToString([]byte(msg))
	msgB64 := base64.StdEncoding.EncodeToString([]byte(msg))

	ok, _ := verifySig(s.PubHex, s.SigHex, []byte(msg))
	resp := outMsg{
		Algo:           firstNonEmpty(s.Algo, "ed25519"),
		Header:         s.Header,
		MessageHex:     msgHex,
		MessageB64:     msgB64,
		PubKeyHex:      s.PubHex,
		SignatureHex:   s.SigHex,
		SignatureValid: ok,
	}
	writeJSON(w, 200, mustJSON(resp))
}

func proxyGET(url string) (jsonBytes []byte, code int, err error) {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b, resp.StatusCode, nil
}

func verifySig(pubHex, sigHex string, msg []byte) (bool, error) {
	pub, err := hex.DecodeString(strings.TrimSpace(pubHex))
	if err != nil {
		return false, errors.New("bad pubkey_hex")
	}
	sig, err := hex.DecodeString(strings.TrimSpace(sigHex))
	if err != nil {
		return false, errors.New("bad signature_hex")
	}
	if len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return false, errors.New("invalid key/signature length")
	}
	ok := ed25519.Verify(ed25519.PublicKey(pub), msg, sig)
	return ok, nil
}

func writeJSON(w http.ResponseWriter, code int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	w.Write(body)
}

func writeErr(w http.ResponseWriter, code int, codeStr, detail string) {
	type errBody struct {
		Applied bool `json:"applied"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Detail  string `json:"detail,omitempty"`
		} `json:"error"`
		Ok bool `json:"ok"`
	}
	var e errBody
	e.Applied = false
	e.Ok = false
	e.Error.Code = codeStr
	// keep message empty for compatibility; put detail
	e.Error.Message = ""
	e.Error.Detail = detail
	b, _ := json.Marshal(e)
	writeJSON(w, code, b)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

