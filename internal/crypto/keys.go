package crypto

import (
"crypto/ed25519"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "os"
)

type NodeKey struct {
    Algo   string `json:"algo"`     // "ed25519"
    Priv   string `json:"priv_hex"` // 64바이트(HEX)
    Pub    string `json:"pub_hex"`  // 32바이트(HEX)
}

func LoadOrCreate(path string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
    // 있으면 로드
    if b, err := os.ReadFile(path); err == nil {
        var nk NodeKey
        if json.Unmarshal(b, &nk) == nil && nk.Algo == "ed25519" {
            priv, err1 := hex.DecodeString(nk.Priv)
            pub,  err2 := hex.DecodeString(nk.Pub)
            if err1 == nil && err2 == nil && len(priv) == ed25519.PrivateKeySize && len(pub) == ed25519.PublicKeySize {
                return ed25519.PrivateKey(priv), ed25519.PublicKey(pub), nil
            }
        }
        // 손상 시 삭제하고 재생성
        _ = os.Remove(path)
    }

    // 생성
    pub, priv, err := ed25519.GenerateKey(rand.Reader)
    if err != nil { return nil, nil, err }
    nk := NodeKey{
        Algo: "ed25519",
        Priv: hex.EncodeToString(priv),
        Pub:  hex.EncodeToString(pub),
    }
    if err := os.MkdirAll("data", 0o755); err != nil { return nil, nil, err }
    if b, err := json.MarshalIndent(nk, "", "  "); err == nil {
        _ = os.WriteFile(path, b, 0o600) // 개인키 파일 권한 보수적
    }
    return priv, pub, nil
}

