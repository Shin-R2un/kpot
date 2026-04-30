# kpot Performance Baseline

This file records benchmark numbers for the v0.10 quality-hardening
cycle. They are **informational baselines**, not regression gates —
CI does not enforce them because per-runner variance (especially for
Argon2id, which is CPU-bound) makes flaky test gates worse than no
gate.

## Why benchmarks at all

The 1Password import workflow can land 300–1000 notes in a kpot
vault. v0.10 introduced "every navigation persists" semantics
(`cd` / `show` / `cp` all touch `Recent` and call `vault.Save`), so
the per-keystroke cost of save matters. Without numbers, "is it fast
enough" is gut feel; with numbers, regressions show up as `make bench`
diffs in PR reviews.

## How to run

```bash
make bench                                          # quick (~10s)
go test ./... -run=^$ -bench=. -benchtime=3s        # release-grade baseline
go test ./... -run=^$ -bench=. -benchmem -count=3   # memory profile + averaging
```

`-benchtime=1x` runs each benchmark exactly once, useful for fast
sanity checks but yields high variance.

## Reference numbers

Captured on **Intel Core i5-14400F (Pop!\_OS 22.04, Go 1.22.10)**
with `-benchtime=1x`. Different CPUs (especially anything pre-2020)
will see Argon2id costs scale up significantly.

| Benchmark | Time/op | Notes |
|---|---|---|
| `Find_1000Notes` | ~6 ms | full-text scan over 1000 notes (~1 KB body each), worst-case query that matches every note |
| `SetField_1000Notes` | ~4 µs | in-memory body mutation only; persist cost is captured separately |
| `TrackRecent` | ~2 µs | dedupe + prepend on a 20-entry recent list |
| `OpenVault_100Notes` | ~130 ms | dominated by Argon2id KDF; payload size is irrelevant |
| `OpenVault_1000Notes` | ~120 ms | confirms Argon2id is the entire cost, not payload decode |
| `Save_1000Notes` | ~14 ms | full re-encrypt + atomic write; this is the "every cd persists" cost |

Read these as: opening a vault is a one-time tax (paid by Argon2id);
once open, every action is sub-second even on a 1000-note vault.

## Decisions informed by these numbers

- **`cd` / `show` / `cp` may safely call `vault.Save`** every time
  to persist `Recent` updates. ~14 ms per save on 1000 notes is well
  below human-perceptible navigation latency.
- **Find on 1000+ notes does not need an index** at v0.10 scale.
  At 5000+ notes the picture would change; defer the optimisation
  until the regression actually shows up.
- **Argon2id parameters do not need to scale** with vault size. Tune
  them via `--argon2-target` (when shipped) only when the user signals
  the open-time delay is bothering them.

## When to update this doc

- After any change to `internal/crypto` (KDF parameters)
- After any change to `internal/vault/io.go` (header format)
- After any change to `internal/store/store.go` (note structure)
- Before tagging a minor release — capture current numbers so
  the next release diff is meaningful.
