package main

import (
    "crypto/ed25519"
    "crypto/rand"
    "encoding/base64"
    "fmt"
    "os"
)

func usage() {
    fmt.Println("usage:")
    fmt.Println("  go run sig_util.go gen")
    fmt.Println("  go run sig_util.go pub  <sk_b64>")
    fmt.Println("  go run sig_util.go sign <sk_b64> <message>")
    os.Exit(2)
}

func main() {
    if len(os.Args) < 2 {
        usage()
    }
    switch os.Args[1] {
    case "gen":
        _, sk, _ := ed25519.GenerateKey(rand.Reader)
        pk := sk[32:]
        fmt.Println(base64.StdEncoding.EncodeToString(pk))
        fmt.Println(base64.StdEncoding.EncodeToString(sk))
    case "pub":
        if len(os.Args) < 3 {
            usage()
        }
        skb, err := base64.StdEncoding.DecodeString(os.Args[2])
        if err != nil {
            fmt.Println("bad sk")
            os.Exit(2)
        }
        fmt.Println(base64.StdEncoding.EncodeToString(ed25519.PrivateKey(skb)[32:]))
    case "sign":
        if len(os.Args) < 4 {
            usage()
        }
        skb, err := base64.StdEncoding.DecodeString(os.Args[2])
        if err != nil {
            fmt.Println("bad sk")
            os.Exit(2)
        }
        msg := []byte(os.Args[3])
        sig := ed25519.Sign(ed25519.PrivateKey(skb), msg)
        fmt.Println(base64.StdEncoding.EncodeToString(ed25519.PrivateKey(skb)[32:])) // pub
        fmt.Println(base64.StdEncoding.EncodeToString(sig))                          // sig
    default:
        usage()
    }
}
