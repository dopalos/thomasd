package app

import (
	"encoding/json"
	"fmt"

	"thomasd/internal/rewards"
	"thomasd/internal/state"
)

const (
	supplyCurrentKey    = "supply/current"
	supplyHistoryPrefix = "supply/history/"
)

// SupplyState keeps track of minted supply totals routed to each vault.
type SupplyState struct {
	TotalMintedMas     uint64 `json:"total_minted_mas"`
	NetworkVaultMas    uint64 `json:"network_vault_mas"`
	FoundationVaultMas uint64 `json:"foundation_vault_mas"`
	ExchangeVaultMas   uint64 `json:"exchange_vault_mas"`
	LastUpdatedHeight  uint64 `json:"last_height"`
}

// ApplyBlockMint updates the supply counters for the provided height and
// returns the reward epoch index along with the minted amount.
func (s *SupplyState) ApplyBlockMint(height uint64) (epoch uint64, minted uint64) {
	minted = rewards.BlockMintAt(height)
	network, foundation, exchange := rewards.SplitMint(minted)
	s.TotalMintedMas += minted
	s.NetworkVaultMas += network
	s.FoundationVaultMas += foundation
	s.ExchangeVaultMas += exchange
	s.LastUpdatedHeight = height
	return rewards.EpochOf(height), minted
}

func supplyHistoryKey(height uint64) string {
	return fmt.Sprintf("%s%020d", supplyHistoryPrefix, height)
}

func loadSupplyState(db *state.DB) (SupplyState, bool) {
	var st SupplyState
	if blob, ok := db.GetKey(supplyCurrentKey); ok {
		if err := json.Unmarshal(blob, &st); err == nil {
			return st, true
		}
	}
	return SupplyState{}, false
}

func loadSupplyStateAt(db *state.DB, height uint64) (SupplyState, bool) {
	blob, ok := db.GetKey(supplyHistoryKey(height))
	if !ok {
		return SupplyState{}, false
	}
	var st SupplyState
	if err := json.Unmarshal(blob, &st); err != nil {
		return SupplyState{}, false
	}
	return st, true
}

func persistSupplyState(db *state.DB, height uint64, st SupplyState) error {
	payload, err := json.Marshal(st)
	if err != nil {
		return err
	}
	db.SetKey(supplyCurrentKey, payload)
	db.SetKey(supplyHistoryKey(height), payload)
	return nil
}
