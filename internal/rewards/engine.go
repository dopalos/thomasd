package rewards

import (
	"math"

	"thomasd/internal/params"
	"thomasd/internal/types"
)

// EmissionMultiplierAt returns the φ decay multiplier for the given block height.
// Height 0 starts at multiplier 1.0 and every EmissionIntervalBlocks applies a
// factor of PhiRatio.
func EmissionMultiplierAt(height uint64) float64 {
	interval := params.EmissionIntervalBlocks
	if interval == 0 {
		return 1
	}
	epochs := height / interval
	return math.Pow(params.PhiRatio, float64(epochs))
}

// BlockMintAt returns the amount of tokens (in micro THO) minted at the given
// height after applying the φ decay curve. Rounding is deterministic using
// math.Round.
func BlockMintAt(height uint64) uint64 {
	multiplier := EmissionMultiplierAt(height)
	emission := float64(params.BaseBlockMintMas) * multiplier
	return uint64(math.Round(emission))
}

// SplitMint distributes the mint amount across the network/foundation/exchange
// buckets. Any rounding remainder is assigned back to the network share to
// ensure conservation of the total input.
func SplitMint(total uint64) (network, foundation, exchange uint64) {
	if total == 0 {
		return 0, 0, 0
	}
	split := params.SplitPercentages
	network = total * split.Network / 100
	foundation = total * split.Foundation / 100
	exchange = total * split.Exchange / 100
	assigned := network + foundation + exchange
	if assigned < total {
		network += total - assigned
	}
	return
}

// EpochOf returns the reward epoch index for a given block height.
func EpochOf(height uint64) uint64 {
	length := params.EpochLengthBlocks
	if length == 0 {
		return 0
	}
	return height / length
}

type EmissionSchedule struct {
	BaseEmission uint64
	DecayRatio   float64
	EpochBlocks  uint64
}

func (s EmissionSchedule) EmissionAt(height uint64) uint64 {
	if s.EpochBlocks == 0 {
		return s.BaseEmission
	}
	epoch := height / s.EpochBlocks
	coeff := math.Pow(s.DecayRatio, float64(epoch))
	if coeff < 0 {
		coeff = 0
	}
	return uint64(float64(s.BaseEmission) * coeff)
}

type WeightConfig struct {
	StakeBase      map[types.StakeClass]uint64
	PoolMultiplier uint64
}

type ValidatorScore struct {
	Address        string
	StakeClass     types.StakeClass
	ConsensusScore int64
	Participation  int64
	PoolNode       bool
}

type ValidatorWeight struct {
	Address string
	Weight  uint64
}

type RewardBucket struct {
	Total           uint64
	ValidatorShare  uint64
	FoundationShare uint64
	ExchangeShare   uint64
}

type ValidatorPayout struct {
	Address string
	Amount  uint64
}

type EpochRewardSummary struct {
	Height          uint64
	Epoch           uint64
	TotalEmission   uint64
	ValidatorShare  uint64
	FoundationShare uint64
	ExchangeShare   uint64
}

type RewardEngine struct {
	Schedule EmissionSchedule
	Weights  WeightConfig
}

func NewRewardEngine(schedule EmissionSchedule, weights WeightConfig) *RewardEngine {
	return &RewardEngine{Schedule: schedule, Weights: weights}
}

func (eng *RewardEngine) Compute(height uint64, validators []ValidatorScore) (RewardBucket, []ValidatorPayout,
	EpochRewardSummary) {
	emission := eng.Schedule.EmissionAt(height)
	if emission == 0 {
		emission = BlockMintAt(height)
	}
	netShare, foundationShare, exchangeShare := SplitMint(emission)
	bucket := RewardBucket{
		Total:           emission,
		ValidatorShare:  netShare,
		FoundationShare: foundationShare,
		ExchangeShare:   exchangeShare,
	}

	summary := EpochRewardSummary{
		Height:          height,
		Epoch:           eng.currentEpoch(height),
		TotalEmission:   emission,
		ValidatorShare:  bucket.ValidatorShare,
		FoundationShare: bucket.FoundationShare,
		ExchangeShare:   bucket.ExchangeShare,
	}

	var totalWeight uint64
	weights := make([]ValidatorWeight, len(validators))
	for i, v := range validators {
		base := eng.Weights.StakeBase[v.StakeClass]
		weight := int64(base) + v.ConsensusScore + v.Participation
		if weight < 0 {
			weight = 0
		}
		out := uint64(weight)
		if v.PoolNode {
			out *= eng.Weights.PoolMultiplier
		}
		weights[i] = ValidatorWeight{Address: v.Address, Weight: out}
		totalWeight += out
	}

	payouts := make([]ValidatorPayout, 0, len(validators))
	if totalWeight == 0 || bucket.ValidatorShare == 0 {
		return bucket, payouts, summary
	}

	remaining := bucket.ValidatorShare
	for _, w := range weights {
		if w.Weight == 0 {
			continue
		}
		amount := (bucket.ValidatorShare * w.Weight) / totalWeight
		remaining -= amount
		payouts = append(payouts, ValidatorPayout{Address: w.Address, Amount: amount})
	}
	for i := 0; remaining > 0 && i < len(payouts); i++ {
		payouts[i].Amount++
		remaining--
	}

	return bucket, payouts, summary
}

func (eng *RewardEngine) currentEpoch(height uint64) uint64 {
	if eng.Schedule.EpochBlocks != 0 {
		return height / eng.Schedule.EpochBlocks
	}
	return EpochOf(height)
}
