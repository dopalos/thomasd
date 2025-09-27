package state

import (
	"encoding/json"
	"sort"

	"thomasd/internal/merkle"
)

// StateRoot computes the BLAKE3 merkle root over the prefixed account set.
func (db *DB) StateRoot() [32]byte {
	snapshot := db.Snapshot()
	if len(snapshot) == 0 {
		return merkle.Blake3RootFromRaw(nil)
	}
	keys := make([]string, 0, len(snapshot))
	for k := range snapshot {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	raw := make([][]byte, 0, len(keys))
	for _, addr := range keys {
		acc := snapshot[addr]
		value, _ := json.Marshal(acc)
		buf := make([]byte, 0, len("account/")+len(addr)+1+len(value))
		buf = append(buf, "account/"...)
		buf = append(buf, addr...)
		buf = append(buf, 0x00)
		buf = append(buf, value...)
		raw = append(raw, buf)
	}
	return merkle.Blake3RootFromRaw(raw)
}
