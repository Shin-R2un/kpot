# kpot

`kpot` (= **k**ey **pot**) is an encrypted CLI note vault. One vault is one
file. APIs keys, passwords, SSH info, secret memos all live as
plain‑text **notes** inside an authenticated‑encrypted blob.

This is the first PR (MVP scope). The full design lives in
`/home/shin/.claude/plans/kpot-cli-cuddly-patterson.md`.

## Build

```bash
make build      # produces ./kpot
make test
make install    # → $(go env GOPATH)/bin/kpot
```

Requires Go 1.18+.

## Quick start

```bash
kpot init personal.kpot         # creates an empty vault, prompts for a passphrase
kpot personal.kpot              # opens REPL

kpot:personal> help
kpot:personal> note ai/openai   # opens $EDITOR, save to store
kpot:personal> ls
kpot:personal> read ai/openai
kpot:personal> exit
```

## What MVP does

- `kpot init <file>` create a new vault
- `kpot <file>` enter REPL
- REPL: `ls`, `note <name>`, `read <name>`, `help`, `exit`
- Note names support `/` (e.g. `ai/openai`, `server/fw0`)
- Argon2id (64 MiB / 3 / 1) + XChaCha20‑Poly1305 with header‑bound AAD
- Atomic write + 1‑generation `.bak`
- Editor temp file in `/dev/shm` (Linux) or OS tmp; wiped + unlinked on close

## Out of scope (next PRs)

- `copy`, `find`, `rm`, `edit` shortcut, single‑shot subcommands
- Passphrase change, seed phrase recovery, OS keychain
- Git sync, materialize, MCP/agent integration

See `docs/format.md` for the on‑disk file layout.

## File layout

```
cmd/kpot/main.go             argv routing
internal/crypto              Argon2id + XChaCha20-Poly1305
internal/vault               .kpot file format, atomic write, .bak
internal/store               in-memory note CRUD, name normalization
internal/repl                interactive command loop
internal/editor              $EDITOR launcher, tmpfs temp file
internal/tty                 passphrase prompt (no echo)
```
