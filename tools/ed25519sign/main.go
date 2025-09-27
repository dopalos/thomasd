package main

import (
	"crypto/ed25519"
	crypto_rand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
)

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERR:", err)
		os.Exit(1)
	}
}

func readKey(path string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	dec := make([]byte, base64.StdEncoding.DecodedLen(len(b)))
	n, err := base64.StdEncoding.Decode(dec, bytesTrimSpace(b))
	if err == nil {
		dec = dec[:n]
	} else {
		// try hex
		dec2 := make([]byte, hex.DecodedLen(len(bytesTrimSpace(b))))
		n2, err2 := hex.Decode(dec2, bytesTrimSpace(b))
		if err2 != nil {
			return nil, nil, errors.New("key decode failed (b64/hex)")
		}
		dec = dec2[:n2]
	}

	switch len(dec) {
	case ed25519.SeedSize:
		priv := ed25519.NewKeyFromSeed(dec)
		pub := priv.Public().(ed25519.PublicKey)
		return priv, pub, nil
	case ed25519.PrivateKeySize:
		priv := ed25519.PrivateKey(dec)
		pub := priv.Public().(ed25519.PublicKey)
		return priv, pub, nil
	default:
		return nil, nil, fmt.Errorf("unexpected key length: %d", len(dec))
	}
}

func bytesTrimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\r' || b[j-1] == '\t') {
		j--
	}
	return b[i:j]
}

func main() {
	gen := flag.Bool("gen", false, "generate new ed25519 seed key")
	keyPath := flag.String("key", "ed25519.key", "key file path (seed or private key; base64 or hex)")
	out := flag.String("out", "", "output path (for -gen); default: print to stdout (base64)")
	pub := flag.Bool("pub", false, "print public key (base64)")
	msgHex := flag.String("msghex", "", "message to sign (hex)")
	msgB64 := flag.String("msgb64", "", "message to sign (base64)")
	sign := flag.Bool("sign", false, "sign given message")
	flag.Parse()

	if *gen {
		_, priv, err := ed25519.GenerateKey(crypto_rand.Reader)
		must(err)
		seed := priv.Seed()
		data := base64.StdEncoding.EncodeToString(seed)
		if *out != "" {
			must(os.WriteFile(*out, []byte(data), 0600))
			fmt.Println("wrote seed(base64):", *out)
		} else {
			fmt.Println(data)
		}
		return
	}

	priv, pubKey, err := readKey(*keyPath)
	must(err)

	if *pub {
		fmt.Println(base64.StdEncoding.EncodeToString(pubKey))
		return
	}

	if *sign {
		var msg []byte
		if *msgHex != "" {
			msg, err = hex.DecodeString(*msgHex)
			must(err)
		} else if *msgB64 != "" {
			msg, err = base64.StdEncoding.DecodeString(*msgB64)
			must(err)
		} else {
			must(errors.New("provide -msghex or -msgb64"))
		}
		sig := ed25519.Sign(priv, msg)
		fmt.Println(base64.StdEncoding.EncodeToString(sig))
		return
	}

	fmt.Println("nothing to do; use -gen / -pub / -sign")
}
