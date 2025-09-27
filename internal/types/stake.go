package types

// StakeClass captures the validator staking tier classification.
type StakeClass int

const (
	StakeClassG1 StakeClass = iota
	StakeClassG2
	StakeClassG3
	StakeClassG4
	StakeClassG5
)
