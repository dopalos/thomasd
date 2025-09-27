//go:build legacy_penalty

package state

import (
	"math"
	"strings"
)

type ScoreEvent int

const (
	ScoreEventProposalSuccess ScoreEvent = iota
	ScoreEventProposalMiss
	ScoreEventBackupRefusal
	ScoreEventEpochStability
)

func (rec *ValidatorScoreRecord) ensureDefaults(address, class string) {
	if rec.Address == "" {
		rec.Address = address
	}
	if rec.StakeClass == "" {
		rec.StakeClass = class
	}
}

func (rec *ValidatorScoreRecord) ApplyEvent(event ScoreEvent, height uint64) {
	switch event {
	case ScoreEventProposalSuccess:
		rec.ConsensusScore += 10
		rec.ConsecutiveMisses = 0
	case ScoreEventProposalMiss:
		rec.ConsecutiveMisses++
		switch rec.ConsecutiveMisses {
		case 1:
		case 2:
			rec.ConsensusScore -= 0.5
		case 3:
			rec.ConsensusScore -= 2
		default:
			rec.ConsensusScore -= 100
		}
	case ScoreEventBackupRefusal:
		rec.ConsensusScore -= 10
	case ScoreEventEpochStability:
		rec.ConsensusScore += 0.001
	}
	rec.LastUpdatedHeight = height
}

func (rec ValidatorScoreRecord) Weight(base map[string]uint64, poolMultiplier uint64) uint64 {
	weight := base[strings.ToLower(rec.StakeClass)]
	if rec.ConsensusScore > 0 {
		weight += uint64(math.Round(rec.ConsensusScore))
	}
	if rec.Participation > 0 {
		weight += uint64(math.Round(rec.Participation))
	}
	if rec.PoolNode && poolMultiplier > 1 {
		weight *= poolMultiplier
	}
	return weight
}

func (db *DB) ApplyValidatorEvent(address, class string, event ScoreEvent, height uint64) {
	db.mu.Lock()
	defer db.mu.Unlock()
	key := strings.ToLower(address)
	rec := db.validatorScores[key]
	rec.ensureDefaults(address, class)
	rec.ApplyEvent(event, height)
	db.validatorScores[key] = rec
}
