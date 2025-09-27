package rpc

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// 저장 파일 경로
var r2pPath string

func SetR2PStorage(path string) {
	// 이미 설정돼 있으면 무시 (덮어쓰기 방지)
	if r2pPath != "" {
		log.Printf("r2p: storage path already set to %s (ignoring %s)", r2pPath, path)
		return
	}
	r2pPath = path
	log.Printf("r2p: storage path set to %s", r2pPath)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("r2p: mkdir failed: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		log.Printf("r2p: no existing file (yet): %v", err)
		return
	}
	if len(b) == 0 {
		log.Printf("r2p: file exists but empty: %s", path)
		return
	}

	var loaded map[string]*r2pRecord
	if err := json.Unmarshal(b, &loaded); err != nil || loaded == nil {
		log.Printf("r2p: unmarshal failed: %v", err)
		return
	}
	r2pMu.Lock()
	r2pStore = loaded
	n := len(r2pStore)
	r2pMu.Unlock()
	log.Printf("r2p: loaded %d records from %s", n, r2pPath)
}

// r2pMu.Lock() 상태에서만 호출해야 함
func r2pSaveLocked() {
	if r2pPath == "" {
		log.Printf("r2p: save skipped (empty path)") // 디버깅용
		return
	}
	_ = os.MkdirAll(filepath.Dir(r2pPath), 0o755)

	b, err := json.MarshalIndent(r2pStore, "", "  ")
	if err != nil {
		log.Printf("r2p: marshal failed: %v", err)
		return
	}
	tmp := r2pPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Printf("r2p: write tmp failed: %v", err)
		return
	}
	_ = os.Remove(r2pPath) // Windows 호환
	if err := os.Rename(tmp, r2pPath); err != nil {
		log.Printf("r2p: rename failed: %v", err)
		return
	}
	log.Printf("r2p: saved %d records to %s (%d bytes)", len(r2pStore), r2pPath, len(b))
}
