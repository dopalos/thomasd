package rewards

import (
	"encoding/binary"
	"fmt"
	"strings"
)

const (
	validatorScorePrefix  = "validators"
	validatorRewardPrefix = "reward"
)

// ValidatorScorePrefix exposes the storage prefix so other packages can iterate
// over validator score entries deterministically.
func ValidatorScorePrefix() string { return validatorScorePrefix }

// ValidatorScoreKey builds the storage key for a validator's score record.
func ValidatorScoreKey(address string) string {
	if address == "" {
		panic("validator score key: empty address")
	}
	return fmt.Sprintf("%s/%s/score", validatorScorePrefix, normalize(address))
}

// ValidatorPayoutKey builds the storage key for a validator payout within an epoch.
func ValidatorPayoutKey(epoch uint64, address string) string {
	if address == "" {
		panic("validator payout key: empty address")
	}
	return fmt.Sprintf("%s/%s/v/%s", validatorRewardPrefix, epochKey(epoch), normalize(address))
}

// EpochSummaryKey returns the key used to store an epoch reward summary.
func EpochSummaryKey(epoch uint64) string {
	return fmt.Sprintf("%s/%s/summary", validatorRewardPrefix, epochKey(epoch))
}

func epochKey(epoch uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], epoch)
	return fmt.Sprintf("epoch_%x", buf)
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
