//go:build legacy_penalty

package state

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sort"
	"strings"
)

func ComputeValidatorsRoot(records []ValidatorScoreRecord) [32]byte {
	clone := append([]ValidatorScoreRecord(nil), records...)
	sort.Slice(clone, func(i, j int) bool {
		return strings.ToLower(clone[i].Address) < strings.ToLower(clone[j].Address)
	})
	h := sha256.New()
	var buf [8]byte
	for _, rec := range clone {
		h.Write([]byte(strings.ToLower(rec.Address)))
		h.Write([]byte(strings.ToLower(rec.StakeClass)))
		binary.BigEndian.PutUint64(buf[:], math.Float64bits(rec.ConsensusScore))
		h.Write(buf[:])
		binary.BigEndian.PutUint64(buf[:], math.Float64bits(rec.Participation))
		h.Write(buf[:])
		if rec.PoolNode {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
		binary.BigEndian.PutUint32(buf[:4], rec.ConsecutiveMisses)
		h.Write(buf[:4])
		binary.BigEndian.PutUint64(buf[:], rec.LastUpdatedHeight)
		h.Write(buf[:])
		binary.BigEndian.PutUint64(buf[:], rec.LastRewardedEpoch)
		h.Write(buf[:])
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func ComputeNextValidatorsRoot(records []ValidatorScoreRecord) [32]byte {
	return ComputeValidatorsRoot(records)
}
