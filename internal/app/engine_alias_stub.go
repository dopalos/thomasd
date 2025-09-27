package app

import "thomasd/internal/tx"

// engine.go에서 t.To == aliasSys 로 비교하므로, 문자열 상수여야 함.
// 실제로 이 주소로 보내는 TX가 없으면 매치되지 않아도 무방 (센티넬)
var aliasSys = "alias:sys"

// (tx.Transfer, int64) -> (bool, error)
// 실제 구현 전까지는 아무 것도 하지 않고 false, nil 반환
func (e *Engine) tryApplyAliasSetTx(t tx.Transfer, nowUTC int64) (bool, error) {
	return false, nil
}
