# Thomasd Consensus Header & Proof Spec (Draft)

## Scope

This note captures the first pass at the Eternity Chain header layout and proof material. It packages the
2025-07-30 ~ 2025-08-27 decisions into something we can wire into code right away.

## Block Header (Eternity Chain)

All fields are serialized via protobuf (to be defined) but we keep the wire order stable here.

| Field            | Type            | Notes |
|------------------|-----------------|-------|
| `chain_id`       | `uint32`        | Fixed per chain, matches tx envelope domain separation. |
| `height`         | `uint64`        | Monotonic block height. |
| `round`          | `uint32`        | Consensus round index inside the height. |
| `proposer`       | `bytes32`       | Ed25519 public key of the proposer. |
| `prev_hash`      | `bytes32`       | BLAKE3-256 hash of the previous header. |
| `state_root`     | `bytes32`       | BLAKE3 root of canonical state trie. |
| `tx_root`        | `bytes32`       | BLAKE3 merkle root of executed tx batch (ThomasChain proof anchor). |
| `commit_hash`    | `bytes32`       | Tree hash of aggregated precommit signatures. |
| `timestamp`      | `uint64`        | Seconds since UNIX epoch (round start). |
| `emission_epoch` | `uint64`        | Derived via `EmissionEpochAtHeight(height)`. |
| `flags`          | `uint32`        | Reserved bit-field for future extensions. |
| `commit`         | `CommitBundle`  | Captures block ID + signature set (see below). |
| `validators_root`| `bytes32`       | BLAKE3 root of the current validator score set. |
| `next_validators_root` | `bytes32` | Planned root applied at the next block (future work). |

`CommitBundle` now lives in Go code (`internal/types/eternity_header.go`) and surfaces via the
`/eternity/header/latest` RPC. It tracks height, round, the proposer's block ID and the signature slice. This is
the shape we intend to stabilise in proto once the serialization format is locked in.

The 5-second round schedule (0.5 proposal / 1.5 prevote / 1.5 precommit / 1.5 buffer) is enforced by the
scheduler; failed rounds rotate to the next proposer drawn from the 42-slot queue.

## Proof Material

The Eternity Chain must provide proofs for:

1. **StateProof** — membership/non-membership inside the canonical state trie. We stick to a BLAKE3-branch
   Merkle tree for now (Patricia trie design pending). Each proof is: ordered list of sibling hashes, leaf payload
   describing the account or validator record, final computed root compared with `state_root`.
2. **TxProof** — proof of ThomasChain batch inclusion. Each ThomasChain block provides its own root; Eternity
   anchors the `tx_root`. The proof chunk reuses the same structure as `StateProof` but with the ThomasChain
   namespace.
3. **CommitProof** — the aggregated commit signatures. Validators sign `SignBytes(height, round, block_id)` with
   Ed25519. A light client checks that at least 2/3 of voting power (via validator weights) is present and that
   each signature verifies.

We record all proof types in proto as:

```protobuf
message StateProof {
  bytes key = 1;
  bytes value = 2;
  repeated bytes siblings = 3;   // left -> right order
}

message TxProof {
  bytes tx_hash = 1;
  repeated bytes siblings = 2;
}
```

`CommitProof` maps directly onto `CommitBundle`. For now the RPC exposes only a signature count; validator-weight
verification remains TODO. `validators_root` (and its future `next_*` counterpart) are produced via
`internal/state/validators.ComputeValidatorsRoot`, which normalises and sorts validator records prior to hashing.

### Light Proofs

- Endpoint: `GET /block/{height}/light-proof`
  - Includes the serialized Eternity header fields, the associated `CommitBundle`, the commit root (`commit_root_hex`)
    and the validator root (`validators_root_hex`).
  - The commit bundle is rehashed via `types.CommitBundle.Root()`, which sorts signatures by validator index/step to
    keep the hash deterministic regardless of ordering in storage.
- Verification helper: `internal/light/verify.Verify`, which checks
  1. Header `BlockID` consistency
  2. `CommitBundle.Root()` matches `header.CommitHash`
  3. Provided validator root matches `header.ProposerSetHash`
  Signature validation is intentionally left to higher layers.
- Endpoint: `POST /block/light-verify`
  - Accepts a JSON payload with the header, commit bundle, and `validators_root_hex` (32-byte hex string).
  - Responds with `{ "ok": true }` on success, or `{ "ok": false, "error": "..." }` with HTTP 400 when
    `light.Verify` reports a mismatch.

## Outstanding Items

- Patricia trie vs flat Merkle decision (affects `siblings` ordering + key encoding).
- How to encode proposer queue & penalties inside header metadata. For now we rely on `state_root` to carry the
  score tables (see `internal/state/validators`).
- Light-client compatibility test vectors (initial unit tests exist in `internal/types/proof_test.go`).

This draft is enough to unblock the next steps: code-level structs, proof verifiers, and the doc set.
