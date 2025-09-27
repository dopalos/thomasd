//go:build legacy_penalty

package state

import (
	"sort"
	"strings"
)

type ValidatorScoreRecord struct {
	Address           string  `json:"address"`
	StakeClass        string  `json:"stake_class"`
	ConsensusScore    float64 `json:"consensus_score"`
	Participation     float64 `json:"participation"`
	PoolNode          bool    `json:"pool_node"`
	ConsecutiveMisses uint32  `json:"consecutive_misses"`
	LastUpdatedHeight uint64  `json:"last_updated_height"`
	LastRewardedEpoch uint64  `json:"last_rewarded_epoch"`
}

type ValidatorPayoutRecord struct {
	Address string `json:"address"`
	Amount  uint64 `json:"amount"`
	Epoch   uint64 `json:"epoch"`
	Height  uint64 `json:"height,omitempty"`
	TimeUTC int64  `json:"time_utc,omitempty"`
}

type EpochRewardSummary struct {
	Epoch           uint64 `json:"epoch"`
	Height          uint64 `json:"height"`
	TotalEmission   uint64 `json:"total_emission"`
	ValidatorShare  uint64 `json:"validator_share"`
	FoundationShare uint64 `json:"foundation_share"`
	ExchangeShare   uint64 `json:"exchange_share"`
}

func (db *DB) UpsertValidatorScore(rec ValidatorScoreRecord) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.validatorScores[strings.ToLower(rec.Address)] = rec
}

func (db *DB) GetValidatorScore(address string) (ValidatorScoreRecord, bool) {
	db.mu.Lock()
	defer db.mu.Unlock()
	rec, ok := db.validatorScores[strings.ToLower(address)]
	return rec, ok
}

func (db *DB) ListValidatorScores() []ValidatorScoreRecord {
	db.mu.Lock()
	defer db.mu.Unlock()
	out := make([]ValidatorScoreRecord, 0, len(db.validatorScores))
	for _, rec := range db.validatorScores {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Address) < strings.ToLower(out[j].Address)
	})
	return out
}

func (db *DB) RecordValidatorPayout(epoch uint64, payout ValidatorPayoutRecord) {
	db.mu.Lock()
	defer db.mu.Unlock()
	bucket, ok := db.epochPayouts[epoch]
	if !ok {
		bucket = make(map[string]ValidatorPayoutRecord)
		db.epochPayouts[epoch] = bucket
	}
	bucket[strings.ToLower(payout.Address)] = payout
}

func (db *DB) EpochPayouts(epoch uint64) []ValidatorPayoutRecord {
	db.mu.Lock()
	defer db.mu.Unlock()
	bucket, ok := db.epochPayouts[epoch]
	if !ok {
		return nil
	}
	out := make([]ValidatorPayoutRecord, 0, len(bucket))
	for _, rec := range bucket {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Address) < strings.ToLower(out[j].Address)
	})
	return out
}

func (db *DB) SetEpochRewardSummary(summary EpochRewardSummary) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.epochSummaries[summary.Epoch] = summary
}

func (db *DB) GetEpochRewardSummary(epoch uint64) (EpochRewardSummary, bool) {
	db.mu.Lock()
	defer db.mu.Unlock()
	summary, ok := db.epochSummaries[epoch]
	return summary, ok
}
