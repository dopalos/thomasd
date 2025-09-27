package rewards

import (
	"encoding/json"
	"fmt"

	rewardlogic "thomasd/internal/rewards"
	"thomasd/internal/state"
	"thomasd/internal/state/validators"
)

// Adapter implements rewardlogic.RewardState on top of state.DB.
type Adapter struct {
	db             *state.DB
	foundationAddr string
	exchangeAddr   string
}

// NewAdapter constructs a reward storage adapter.
func NewAdapter(db *state.DB, foundationAddr, exchangeAddr string) *Adapter {
	return &Adapter{db: db, foundationAddr: foundationAddr, exchangeAddr: exchangeAddr}
}

// ListValidatorScores loads every persisted validator score record.
func (a *Adapter) ListValidatorScores() ([]validators.ScoreRecord, error) {
	prefix := fmt.Sprintf("%s/", validatorScorePrefix)
	records := make([]validators.ScoreRecord, 0)
	var firstErr error

	a.db.RangePrefix(prefix, func(key string, value []byte) bool {
		var rec validators.ScoreRecord
		if err := json.Unmarshal(value, &rec); err != nil {
			firstErr = fmt.Errorf("decode validator score %s: %w", key, err)
			return false
		}
		records = append(records, rec)
		return true
	})

	if firstErr != nil {
		return nil, firstErr
	}
	return records, nil
}

// PutValidatorScore persists the provided validator score record.
func (a *Adapter) PutValidatorScore(record validators.ScoreRecord) error {
	if record.Address == "" {
		return fmt.Errorf("validator score requires address")
	}

	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode validator score %s: %w", record.Address, err)
	}

	a.db.SetKey(ValidatorScoreKey(record.Address), payload)
	return nil
}

// PersistValidatorPayout records a validator payout for the given epoch.
func (a *Adapter) PersistValidatorPayout(epoch uint64, payout rewardlogic.ValidatorPayout) error {
	if payout.Address == "" {
		return fmt.Errorf("validator payout missing address")
	}

	payload, err := json.Marshal(struct {
		Amount uint64 `json:"amount"`
	}{Amount: payout.Amount})
	if err != nil {
		return fmt.Errorf("encode payout %s: %w", payout.Address, err)
	}

	a.db.SetKey(ValidatorPayoutKey(epoch, payout.Address), payload)
	return nil
}

// PersistEpochSummary stores the epoch summary and credits treasury accounts.
func (a *Adapter) PersistEpochSummary(summary rewardlogic.EpochRewardSummary) error {
	payload, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("encode epoch summary %d: %w", summary.Epoch, err)
	}

	a.db.SetKey(EpochSummaryKey(summary.Epoch), payload)

	if a.foundationAddr != "" {
		a.db.AddBalance(a.foundationAddr, summary.FoundationShare)
	}
	if a.exchangeAddr != "" {
		a.db.AddBalance(a.exchangeAddr, summary.ExchangeShare)
	}
	return nil
}

// SetFoundationAccount updates the address receiving foundation rewards.
func (a *Adapter) SetFoundationAccount(addr string) {
	a.foundationAddr = addr
}

// SetExchangeAccount updates the address receiving exchange reserve rewards.
func (a *Adapter) SetExchangeAccount(addr string) {
	a.exchangeAddr = addr
}

// FoundationAccount returns the foundation reward account address.
func (a *Adapter) FoundationAccount() string {
	return a.foundationAddr
}

// ExchangeAccount returns the exchange reserve account address.
func (a *Adapter) ExchangeAccount() string {
	return a.exchangeAddr
}

// LoadValidatorScore loads a single validator score by address.
func (a *Adapter) LoadValidatorScore(address string) (validators.ScoreRecord, bool, error) {
	blob, ok := a.db.GetKey(ValidatorScoreKey(address))
	if !ok {
		return validators.ScoreRecord{}, false, nil
	}

	var rec validators.ScoreRecord
	if err := json.Unmarshal(blob, &rec); err != nil {
		return validators.ScoreRecord{}, false, fmt.Errorf("decode validator score %s: %w", address, err)
	}
	return rec, true, nil
}

// RemoveValidatorScore deletes a validator score entry.
func (a *Adapter) RemoveValidatorScore(address string) {
	a.db.DeleteKey(ValidatorScoreKey(address))
}
