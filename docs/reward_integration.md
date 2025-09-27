# Reward Engine Integration Blueprint

## Storage Schema

- **Validator Score Record** (`validators/{addr}/score`)
  - `address`
  - `stake_class`
  - `consensus_score`
  - `participation`
  - `pool_node`
  - `last_updated_height`
  - `last_rewarded_epoch`
  - Suggested serialization: protobuf `ValidatorScoreRecord` to be added in `proto/state.proto`.

- **Validator Payout** (`reward/{epoch}/v/{addr}`)
  - `amount` (microTHO)
  - Optional metadata: inclusion height/timestamp.

- **Epoch Summary** (`reward/{epoch}/summary`)
  - `height`
  - `total_emission`
  - `validator_share`
  - `foundation_share`
  - `exchange_share`
  - Optional: proof roots / top earners.

All entries should reside inside the ThomasChain state trie so the resulting root is anchored on the Eternity chain.

## Consensus Flow Hooks

1. **During Block Execution**
   - When a proposal/prevote/precommit succeeds or fails, update the corresponding `ScoreRecord` (`ConsensusScore`, `Participation`, penalties) and bump `LastUpdatedHeight`.

2. **End of Block (Before Commit)**
   - Fetch all score records via a concrete `RewardState` implementation.
   - Call `RewardEngine.Apply(height, state)`, which will compute φ-decayed emission, split 80/10/10 buckets, persist payouts & epoch summary, and reset per-epoch counters.

3. **State Commit**
   - Ensure the modified records and reward outputs are staged before finalizing the block so the `state_root` captures the latest reward distribution.
   - Emit events/logs for RPC/indexing.

4. **Epoch Transition**
   - On `height % EpochBlocks == 0`, perform any extra housekeeping (snapshotting, optional stake-class refresh).

## Consensus Header Linkage

- The Eternity header draft (`docs/consensus_headers.md`) now defines `emission_epoch`, `state_root`, `commit_hash`, and the `CommitBundle` fields explicitly.
- The Go implementation (`internal/types/eternity_header.go`, `/internal/app/engine.go`) populates `EmissionEpochAtHeight` and surfaces commit metadata via RPC.
- Reward outputs must finalize **before** the header hash is computed so the `state_root` captures the latest reward distribution.
- Commit signatures are still pass-through; validator-weight adjustments (penalties/bonuses) will ultimately influence the commit bundle and reward weighting.
- `validators_root` in the header is produced via `internal/state/validators.ComputeValidatorsRoot`, which sorts and normalises score records prior to hashing.

### Validator Scoring & Penalties

- Shared logic lives in `internal/stake/validator.go`, with state records bridging through `ScoreRecord.ApplyEvent` (`internal/state/validators/penalty.go`).
- Events follow the 2025-07-30 rules, except the “permanent ban” is replaced with a steep −100 score penalty on the fourth consecutive miss.
- Summary of the current weights:
  - Proposal success: +10 (resets consecutive misses)
  - 1st miss: 0, 2nd miss: −0.5, 3rd miss: −2, 4th miss: −100 (capped; validator remains active)
  - Backup refusal: −10
  - Epoch stability bonus: +0.001 per epoch
- These adjustments feed directly into validator weights, reward shares, and the validator root hashed into Eternity headers.

### Light Client Integration

- Light clients can fetch `GET /block/{height}/light-proof`, which returns the canonical header, commit bundle and
  validator root. Verification helper lives in `internal/light/verify.go`.
- The commit bundle root is deterministic because signatures are sorted prior to hashing, allowing peers to rebuild the
  same root even if signature arrival order differs.
- Full signature verification is intentionally left to the light client.

## Follow-Up Tasks

- Implement the concrete `RewardState` adapter in the state layer.
- Extend `proto/state.proto` with reward and proof messages (`CommitBundle`, `CommitSig`, `StateProof`) once the schema is finalized (currently stubbed in Go).
- Expose RPC endpoints (e.g., `GET /rewards/epoch/{n}`) backed by the stored data.
- Add integration tests once consensus/state wiring is in place.
- Provide test vectors for header + proof validation (ties to the new `internal/types` helpers).

### RPC Examples

- `GET /supply/current`

  ```json
  {
    "height": 42,
    "total_minted_mas": 42000000,
    "network_vault_mas": 33600000,
    "foundation_vault_mas": 4200000,
    "exchange_vault_mas": 4200000,
    "block_mint_mas": 1000000
  }
  ```

- `GET /supply/at/41`

  ```json
  {
    "height": 41,
    "total_minted_mas": 41000000,
    "network_vault_mas": 32800000,
    "foundation_vault_mas": 4100000,
    "exchange_vault_mas": 4100000,
    "block_mint_mas": 1000000
  }
  ```
