# CLAUDE.md — kpot project memory

> Conventions and gotchas specific to the kpot codebase. Read alongside
> the global `~/.claude/CLAUDE.md` (which holds re-usable Go / gh /
> shell patterns). When the two conflict, this file wins for
> kpot-specific decisions.

## Project at a glance

- **What**: encrypted CLI note vault. 1 file = 1 vault. No daemon.
- **Where**: <https://github.com/Shin-R2un/kpot> (public, MIT)
- **Module path**: `github.com/Shin-R2un/kpot` (Go module)
- **Crypto**: XChaCha20-Poly1305 (AEAD) + Argon2id (KDF) + BIP-39 recovery seed
- **Go**: 1.18+ (CI runs 1.22)

## Build & test commands (`make help` lists everything)

```
make build              # → ./kpot
make check              # vet + gofmt -l + test (mirrors CI exactly)
make fmt                # gofmt -w .
make release-patch      # v0.6.0 → v0.6.1: tag, push, trigger release.yml
make release-minor      # v0.6.0 → v0.7.0
make release-major      # v0.6.0 → v1.0.0
make install-hooks      # .git/hooks/pre-push runs `make check`
```

`make check` is the canonical local gate. Always run it before pushing —
it catches gofmt drift between Go 1.18 (local default) and Go 1.22 (CI)
that would otherwise blow up CI.

## Three-tier release automation

Implemented in `Makefile` + `scripts/release.sh` + `scripts/pre-push`.
Diagram:

```
edit code
    ↓ make check          ← Tier 1: local quality gate (CI-equivalent)
    ↓ git push            ← Tier 3: pre-push hook re-runs make check
    ↓ PR review + merge
    ↓ git checkout main && git pull
    ↓ make release-minor  ← Tier 2: scripts/release.sh
        ├── refuse if dirty / not on main / out-of-sync
        ├── make check
        ├── pick latest semver tag, bump, prevent clobber
        ├── auto-generate tag message from `git log <prev>..HEAD --no-merges`
        ├── confirmation prompt (skip with YES=1)
        └── git tag -a + git push origin <tag>
              ↓
              GitHub Actions release.yml
              ├── goreleaser builds 5 archives (linux/darwin amd64+arm64, windows amd64)
              ├── publishes to GitHub Releases with checksums.txt
              └── pushes scoop manifest to Shin-R2un/scoop-bucket via SCOOP_TOKEN
```

When choosing patch vs minor vs major, follow the commit prefix
convention: a `feat:` commit since the last tag = at minimum a minor
bump. Don't `release-patch` after a feat commit — the version contradicts
the commit prefix and confuses downstream readers.

## Architecture invariants

```
cmd/kpot/main.go       argv routing only — no business logic
internal/crypto        Argon2id + XChaCha20-Poly1305 + Wrap/Unwrap
internal/vault         .kpot file format, atomic write, .bak rotation
internal/store         in-memory note CRUD + name normalization
internal/repl          interactive command loop, prompter, completion
internal/editor        $EDITOR launcher
internal/clipboard     cross-platform copy + auto-clear manager
internal/notefmt       editor frontmatter render/strip
internal/fields        key:value field parser (v0.6+; for cd context UX)
internal/bundle        .kpb selective-transfer format
internal/config        ~/.config/kpot/config.toml loader
internal/recovery      BIP-39 / Crockford-Base32 + KEK derivation
internal/keychain      OS keychain (no third-party deps)
internal/tty           passphrase prompt + shared bufio reader
docs/format.md         on-disk file format spec (v1+v2)
docs/security.md       threat model (read before crypto changes)
```

**Dependency direction**: `cmd → internal/<top> → internal/<low>`.
Never circular. Never import from `cmd` into `internal`.

**External I/O**: stdin / clipboard / `$EDITOR` / OS keychain are isolated
in their own packages so they can be mocked in tests. Don't sneak `os`
or filesystem calls into business logic packages.

## Field parser convention (`internal/fields/`)

Notes are free-form text. The fields package extracts simple
`key: value` rows for context-aware commands (`show <field>`,
`set <field>`, etc.).

**Lookup rules**:
- Keys lower-cased for matching, original case preserved on update
- Allowed key chars: `[A-Za-z0-9_.-]`. List bullets (`- id:`) intentionally
  do NOT match because the regex anchors at line start
- Frontmatter (`--- … ---`) and fenced code blocks are skipped

**Update rules**:
- `Set(body, key, value)` updates in place if key exists, else inserts
  after the last contiguous field block (or after the title heading
  if no field block exists)
- `formatLine` preserves both before-colon AND after-colon whitespace
  (covers `URL: x`, `URL :x`, `url:  x`, `url:\tx`, `url:x` cases)

**Secret-field protection**: `IsSecretField()` is the source of truth
for which keys must NOT accept their value as a command argument
(would land in REPL liner history). The list covers passphrase
family + API tokens + 2FA + private keys. When adding new secret
field types, update both the map AND `TestIsSecretField` to lock
the contract.

## REPL context UX (v0.6+)

`Session.currentNote` tracks the active context. Every command that
relies on it must:

1. Check if `currentNote != ""` first
2. `s.Vault.Get(s.currentNote)` to retrieve the note
3. If `!ok` (note vanished mid-session): `s.currentNote = ""` AND
   return a "current note vanished — context cleared" error

The vanish branch exists in show / cp / set / unset / fields. Tested
via `TestShowAfterNoteVanishedClearsContext` — that one test covers
the pattern; if the vanish handling regresses anywhere, this catches
it (since the resolution code is structurally identical across
commands).

**`cd` resolution order**: exact match → prefix-group (lists candidates,
no context change) → error. `cd ..` and `cd /` both clear to root
(MVP: no hierarchical traversal).

**`show` / `cp` dispatch**: 0 args → current note body. 1 arg → if it
matches a note name exactly, use that note's body; else treat as
field of current note. **Note-name match wins over field-name match**
when both exist — explicit name takes priority. This is intentional
and tested.

## Threat model doc (`docs/security.md`) structure

- TL;DR table at the top: ✅ defends against / ❌ does not / ⚠️ best-effort
- Each crypto choice gets a "why this primitive" subsection (XChaCha20
  vs AES-GCM, Argon2id vs PBKDF2, BIP-39 for recovery, etc.)
- Honest about Go memzero limits (best-effort due to runtime)
- Explicit "supply chain: today only SHA-256 verification; sigstore
  & SLSA are roadmap"
- Format deprecation policy: **post-v1.0 only** for the 6-month
  window. v0.x is explicitly unstable per semver — committing to a
  6-month deprecation as a single author would either be violated
  on the next breaking change or force keeping unwanted readers alive

When editing crypto, read this doc first, then update both the doc
and the implementation in the same PR.

## SECURITY.md timeline

7-day acknowledgement target with explicit **best-effort + 14-day
upper bound** qualifier for single-author project realities. Don't
strip this qualifier even if it looks less professional — better to
honestly describe the achievable response time than to publish a
policy you'll inevitably break during a busy week.

GitHub Security Advisories is the primary reporting channel
(<https://github.com/Shin-R2un/kpot/security/advisories/new>);
shin@r2un.com is the backup. PGP key fingerprints will be published
once available — until then the doc honestly says "TBD" rather than
faking a key.

## Common gotchas in this codebase

- **Vault.Put preserves CreatedAt** (only bumps UpdatedAt for existing
  notes). `TestSetPreservesCreatedAt` locks this; if you touch
  store.Put, re-run that test specifically.
- **`peterh/liner` history**: any value passed as an argv to a REPL
  command is appended to liner's in-memory history. Do not pass
  secrets as args — see `IsSecretField` and the `set` rejection
  pattern.
- **`/dev/tty` for one-shot secret display** (recovery seed at `init`).
  No fallback to stdout/stderr. If TTY isn't available, fail loudly
  rather than degrade.
- **Shared bufio.Reader for stdin** (`internal/tty/SharedStdin()`).
  Never construct `bufio.NewReader(os.Stdin)` independently in a
  subsystem — see global CLAUDE.md "Go の罠" for why (heredoc input
  loss).
- **Versioning**: `var version = "0.5.0-dev"` in `cmd/kpot/main.go`
  is intentionally `var` (not `const`) so goreleaser can inject
  via `-ldflags "-X main.version=..."` at release time.

## Things explicitly NOT in scope (for now)

- mlock / VirtualLock for memory hardening — see threat model "what
  kpot does not defend against"
- Sigstore / cosign signing — planned, not blocking
- SLSA provenance — planned, not blocking
- Asymmetric crypto (currently zero PKI; quantum exposure is
  symmetric-only at ~128-bit post-Grover)
- Hierarchical `cd ..` (MVP: jumps straight to root)
- Path-aware `NormalizeName` rejection of `../` (pre-existing,
  benign because vault is in-memory map not filesystem)

When evaluating new feature requests against these, push back if
they conflict with the threat model — the model's job is to make
"no" decisions defensible.
