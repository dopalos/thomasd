package stake

// ValidatorEvent enumerates significant consensus events that influence
// validator scoring. Values are string-based for easy logging and eventual
// serialization.
type ValidatorEvent string

const (
	EventProposalSuccess ValidatorEvent = "proposal_success"
	EventProposalMiss    ValidatorEvent = "proposal_miss"
	EventBackupRefusal   ValidatorEvent = "backup_refusal"
	EventEpochStability  ValidatorEvent = "epoch_stability_bonus"
)

// Validator models the consensus-facing metadata for a validator.
type Validator struct {
	Address           string
	PubKey            []byte
	StakeMas          uint64
	Score             float64
	ConsecutiveMisses uint32
	TotalMisses       uint64
	Jailed            bool
}

// PenaltyConfig captures the weights applied to validator events.
type PenaltyConfig struct {
	SuccessReward        float64
	SecondMissPenalty    float64
	ThirdMissPenalty     float64
	FourthMissPenalty    float64
	BackupRefusalPenalty float64
	EpochStabilityBonus  float64
}

// DefaultPenaltyConfig is the canonical rule-set for validator scoring.
var DefaultPenaltyConfig = PenaltyConfig{
	SuccessReward:        10,
	SecondMissPenalty:    -0.5,
	ThirdMissPenalty:     -2,
	FourthMissPenalty:    -100,
	BackupRefusalPenalty: -10,
	EpochStabilityBonus:  0.001,
}

// ApplyEvent mutates the validator score according to the provided event. It
// intentionally avoids permanent bans; instead, the fourth consecutive miss
// applies a large penalty while keeping the validator active.
func (v *Validator) ApplyEvent(event ValidatorEvent, cfg PenaltyConfig) {
	switch event {
	case EventProposalSuccess:
		v.ConsecutiveMisses = 0
		v.Score += cfg.SuccessReward
	case EventProposalMiss:
		v.ConsecutiveMisses++
		v.TotalMisses++
		switch v.ConsecutiveMisses {
		case 1:
			// no penalty on first miss
		case 2:
			v.Score += cfg.SecondMissPenalty
		case 3:
			v.Score += cfg.ThirdMissPenalty
		case 4:
			v.Score += cfg.FourthMissPenalty
		default:
			v.ConsecutiveMisses = 4
		}
	case EventBackupRefusal:
		v.Score += cfg.BackupRefusalPenalty
	case EventEpochStability:
		v.Score += cfg.EpochStabilityBonus
	}
}
