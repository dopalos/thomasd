//go:build legacy_penalty

package validators

import "thomasd/internal/stake"

// ApplyEvent updates the score record using the shared stake penalty logic.
func (r *ScoreRecord) ApplyEvent(event stake.ValidatorEvent, cfg stake.PenaltyConfig) {
	validator := stake.Validator{
		Address:           r.Address,
		StakeMas:          0,
		Score:             r.Score,
		ConsecutiveMisses: r.ConsecutiveMisses,
		TotalMisses:       r.TotalMisses,
		Jailed:            r.Jailed,
	}

	validator.ApplyEvent(event, cfg)

	r.Score = validator.Score
	r.ConsecutiveMisses = validator.ConsecutiveMisses
	r.TotalMisses = validator.TotalMisses
	r.Jailed = validator.Jailed
}
