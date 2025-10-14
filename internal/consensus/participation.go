package consensus

import "thomasd/internal/stake"

// 규칙 요약:
// 성공: +10점 (miss=0)
// miss 1회: 0
// miss 2회: -0.5
// miss 3회: -2
// miss >=4: -100 (클램프 0)
// RefusedAsBackup: -10 (miss 변화 없음)
// Epoch bonus: +0.001

func UpdateScoreOnSuccess(v *stake.Validator, missCount *uint32) {
	*missCount = 0
	v.ScoreQ = stake.AddPointsQ(v.ScoreQ, +10, 1)
}

func UpdateScoreOnMiss(v *stake.Validator, missCount *uint32) {
	if *missCount < ^uint32(0) {
		*missCount++
	}
	switch *missCount {
	case 1:
		// no-op
	case 2:
		v.ScoreQ = stake.AddPointsQ(v.ScoreQ, -1, 2) // -0.5
	case 3:
		v.ScoreQ = stake.AddPointsQ(v.ScoreQ, -2, 1) // -2
	default:
		v.ScoreQ = stake.AddPointsQ(v.ScoreQ, -100, 1) // -100
	}
}

func UpdateScoreOnRefusedAsBackup(v *stake.Validator) {
	v.ScoreQ = stake.AddPointsQ(v.ScoreQ, -10, 1)
}

func UpdateEpochBonus(v *stake.Validator) {
	v.ScoreQ = stake.AddPointsQ(v.ScoreQ, +1, 1000) // +0.001
}
