//go:build legacy_penalty

package validators

import (
	"bytes"
	"encoding/binary"
	"sort"
	"strings"

	"thomasd/internal/merkle"
)

// ComputeValidatorsRoot returns a deterministic BLAKE3 merkle root of the
// supplied validator score records. Records are normalised by lower-casing the
// address and sorted to guarantee consistent ordering across peers.
func ComputeValidatorsRoot(records []ScoreRecord) [32]byte {
	if len(records) == 0 {
		return merkle.Blake3RootFromRaw(nil)
	}
	copies := make([]ScoreRecord, len(records))
	for i, r := range records {
		copies[i] = r.Clone()
	}
	sort.Slice(copies, func(i, j int) bool {
		return strings.ToLower(copies[i].Address) < strings.ToLower(copies[j].Address)
	})

	leaves := make([][]byte, len(copies))
	for i, rec := range copies {
		leaves[i] = encodeScoreRecord(rec)
	}
	return merkle.Blake3RootFromRaw(leaves)
}

// ComputeNextValidatorsRoot is currently an alias for ComputeValidatorsRoot. It
// exists so future logic (e.g. pending set updates) can be added without
// touching callers.
func ComputeNextValidatorsRoot(records []ScoreRecord) [32]byte {
	return ComputeValidatorsRoot(records)
}

func encodeScoreRecord(rec ScoreRecord) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, 128))
	addr := strings.ToLower(strings.TrimSpace(rec.Address))
	buf.WriteString(addr)
	buf.WriteByte(0)

	writeUint64(buf, uint64(rec.StakeClass))
	writeInt64(buf, rec.ConsensusScore)
	writeInt64(buf, rec.Participation)
	if rec.PoolNode {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}
	writeUint64(buf, rec.LastUpdatedHeight)
	writeUint64(buf, rec.LastRewardedEpoch)

	// float64 score stored using IEEE 754 bits for deterministic encoding
	if err := binary.Write(buf, binary.LittleEndian, rec.Score); err != nil {
		panic(err)
	}
	writeUint32(buf, rec.ConsecutiveMisses)
	writeUint64(buf, rec.TotalMisses)
	if rec.Jailed {
		buf.WriteByte(1)
	} else {
		buf.WriteByte(0)
	}

	return buf.Bytes()
}

func writeUint64(buf *bytes.Buffer, v uint64) {
	var scratch [8]byte
	binary.LittleEndian.PutUint64(scratch[:], v)
	buf.Write(scratch[:])
}

func writeUint32(buf *bytes.Buffer, v uint32) {
	var scratch [4]byte
	binary.LittleEndian.PutUint32(scratch[:], v)
	buf.Write(scratch[:])
}

func writeInt64(buf *bytes.Buffer, v int64) {
	writeUint64(buf, uint64(v))
}
