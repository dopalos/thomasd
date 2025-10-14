//go:build !light_client
// +build !light_client

package app

import (
	"testing"

	"thomasd/internal/rewards"
	"thomasd/internal/state"
)

func TestSupplyStateApplyBlockMint(t *testing.T) {
	var st SupplyState
	epoch, minted := st.ApplyBlockMint(0)
	if minted != rewards.BlockMintAt(0) {
		t.Fatalf("mint mismatch: got %d want %d", minted, rewards.BlockMintAt(0))
	}
	if epoch != rewards.EpochOf(0) {
		t.Fatalf("epoch mismatch: got %d want %d", epoch, rewards.EpochOf(0))
	}
	if st.TotalMintedMas != minted || st.NetworkVaultMas == 0 {
		t.Fatalf("state not updated: %+v", st)
	}

	prevTotal := st.TotalMintedMas
	_, mintedNext := st.ApplyBlockMint(1)
	if st.TotalMintedMas != prevTotal+mintedNext {
		t.Fatalf("cumulative mint wrong: got %d want %d", st.TotalMintedMas, prevTotal+mintedNext)
	}
}

func TestPersistSupplyState(t *testing.T) {
	db := state.NewDB()
	var st SupplyState
	_, minted := st.ApplyBlockMint(0)
	if err := persistSupplyState(db, 0, st); err != nil {
		t.Fatalf("persist current: %v", err)
	}

	loaded, ok := loadSupplyState(db)
	if !ok {
		t.Fatalf("expected current supply state")
	}
	if loaded.TotalMintedMas != minted {
		t.Fatalf("loaded mint mismatch: got %d want %d", loaded.TotalMintedMas, minted)
	}

	loadedAt, ok := loadSupplyStateAt(db, 0)
	if !ok {
		t.Fatalf("expected historical supply state")
	}
	if loadedAt.TotalMintedMas != minted {
		t.Fatalf("historical mint mismatch: got %d want %d", loadedAt.TotalMintedMas, minted)
	}
}
