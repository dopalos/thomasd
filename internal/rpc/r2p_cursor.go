package rpc

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// updatedAt 기준 커서를 "ts|id" → base64url(= 제거)로 인코딩
func encodeR2PCursor(ts int64, id string) string {
	raw := fmt.Sprintf("%d|%s", ts, id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// base64url 커서를 복호화. 구버전 숫자 커서(생성시각)도 수용.
func decodeR2PCursor(s string) (ts int64, id string, ok bool) {
	if s == "" {
		return 0, "", true
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		parts := strings.SplitN(string(b), "|", 2)
		if len(parts) == 2 {
			if t, e := strconv.ParseInt(parts[0], 10, 64); e == nil {
				return t, parts[1], true
			}
		}
	}
	// 레거시 호환(숫자 커서 = created_utc)
	if t, err := strconv.ParseInt(s, 10, 64); err == nil {
		return t, "", true
	}
	return 0, "", false
}
