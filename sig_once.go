package main
import (
"crypto/ed25519"
"crypto/rand"
"encoding/base64"
"fmt"
"os"
)
func main(){
 m := []byte(os.Args[1])
 _, sk, _ := ed25519.GenerateKey(rand.Reader)
 fmt.Println(base64.StdEncoding.EncodeToString(sk[32:]))
 fmt.Println(base64.StdEncoding.EncodeToString(ed25519.Sign(sk, m)))
}
