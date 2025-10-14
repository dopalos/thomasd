// cmd/ed25519tool/main.go
package main

import (
	"crypto/ed25519"
	crypto_rand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
)

func main() {
	gen := flag.Bool("gen", false, "generate keypair")
	skIn := flag.String("sk", "", "private key or seed (base64 by default)")
	msg := flag.String("m", "", "message to sign (the SHA-256 hex string)")
	asHex := flag.Bool("hex", false, "interpret -sk as hex instead of base64")
	flag.Parse()

	if *gen {
		_, sk, _ := ed25519.GenerateKey(crypto_rand.Reader)
		pk := sk.Public().(ed25519.PublicKey)
		fmt.Printf("PK:%s\nSK:%s\n",
			base64.StdEncoding.EncodeToString(pk),
			base64.StdEncoding.EncodeToString(sk))
		return
	}
	if *skIn == "" || *msg == "" {
		fmt.Fprintln(os.Stderr, "usage: ed25519tool -sk <b64-or-hex> [-hex] -m <message>   or   ed25519tool -gen")
		os.Exit(2)
	}

	var raw []byte
	var err error
	if *asHex {
		raw, err = hex.DecodeString(*skIn)
	} else {
		raw, err = base64.StdEncoding.DecodeString(*skIn)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad private key encoding")
		os.Exit(2)
	}

	var sk ed25519.PrivateKey
	switch len(raw) {
	case ed25519.PrivateKeySize:
		sk = ed25519.PrivateKey(raw) // 64B privkey
	case ed25519.SeedSize:
		sk = ed25519.NewKeyFromSeed(raw) // 32B seed
	default:
		fmt.Fprintln(os.Stderr, "bad private key length")
		os.Exit(2)
	}

	sig := ed25519.Sign(sk, []byte(*msg))
	pk := sk.Public().(ed25519.PublicKey)
	fmt.Printf("PK:%s\nSIG:%s\n",
		base64.StdEncoding.EncodeToString(pk),
		base64.StdEncoding.EncodeToString(sig))
}

