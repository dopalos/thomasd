package app

import (
	"encoding/json"
	"fmt"

	"thomasd/internal/state"
	"thomasd/internal/state/rewards"
	"thomasd/internal/state/validators"
)

func loadAllValidatorScores(db *state.DB) ([]validators.ScoreRecord, error) {
	// reuse the rewards adapter prefix/key format
	prefix := fmt.Sprintf("%s/", rewards.ValidatorScorePrefix())
	records := make([]validators.ScoreRecord, 0)
	var firstErr error

	db.RangePrefix(prefix, func(key string, value []byte) bool {
		var rec validators.ScoreRecord
		if err := json.Unmarshal(value, &rec); err != nil {
			firstErr = fmt.Errorf("decode validator score %s: %w", key, err)
			return false
		}
		records = append(records, rec)
		return true
	})

	if firstErr != nil {
		return nil, firstErr
	}
	return records, nil
}
