package codec

import (
	"encoding/json"
	"thomasd/internal/tx"
)

// JSON 蹂몃Ц?먯꽌 UTF-8 BOM(0xEF,0xBB,0xBF) ?먮룞 ?쒓굅 ???뚯떛
func DecodeJSON(b []byte, out *tx.Transfer) error {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	return json.Unmarshal(b, out)
}
