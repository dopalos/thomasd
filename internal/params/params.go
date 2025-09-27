package params

// EmissionIntervalBlocks controls how often the reward emission pool decays by φ.
const EmissionIntervalBlocks uint64 = 20_000_000

// BaseBlockMintMas is the emission amount (in micro THO) minted per block at
// height 0 before applying the φ decay.
const BaseBlockMintMas uint64 = 1_000_000

// EpochLengthBlocks determines how many blocks form an accounting epoch.
const EpochLengthBlocks uint64 = 100

// SplitPercentages defines the reward split applied to the mint amount.
// All fields are expressed in whole percent (must sum to 100).
var SplitPercentages = struct {
	Network    uint64
	Foundation uint64
	Exchange   uint64
}{
	Network:    80,
	Foundation: 10,
	Exchange:   10,
}

// PhiRatio approximates the golden ratio decay factor used for emission.
const PhiRatio = 0.6180339887498949
