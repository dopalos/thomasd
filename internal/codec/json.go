package codec

import (
"encoding/json"
	"thomasd/internal/tx"
)

// JSON 본문에서 UTF-8 BOM(0xEF,0xBB,0xBF) 자동 제거 후 파싱
func DecodeJSON(b []byte, out *tx.Transfer) error {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	return json.Unmarshal(b, out)
}

