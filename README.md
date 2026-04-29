# kpot

`kpot` (= **k**ey **pot**) is an encrypted CLI note vault. One vault is
one file: API keys, passwords, SSH info, and free-form secret memos all
live as plain-text **notes** inside an authenticated-encrypted blob.

Pitch: SQLite-style "1 file = 1 vault" portability, plus a friendly
REPL with `$EDITOR` integration. No daemon, no server, no SaaS.

The full design lives in
`/home/shin/.claude/plans/kpot-cli-cuddly-patterson.md`.

## Install

**One-liner** (Linux / macOS):

```bash
curl -sSL https://raw.githubusercontent.com/Shin-R2un/kpot/main/install.sh | bash
```

**One-liner** (Windows PowerShell 5+):

```powershell
irm https://raw.githubusercontent.com/Shin-R2un/kpot/main/install.ps1 | iex
```

**Scoop** (Windows, package manager):

```powershell
scoop bucket add shin-r2un https://github.com/Shin-R2un/scoop-bucket
scoop install kpot
```

Both scripts auto-detect OS/arch, fetch the latest release, verify its
SHA-256 against `checksums.txt`, and place `kpot` (or `kpot.exe`) on
disk. Defaults: `/usr/local/bin/kpot` on Unix (uses `sudo` if
needed), `%USERPROFILE%\bin\kpot.exe` on Windows. Override with:

| Variable | Purpose |
|---|---|
| `KPOT_VERSION` | Pin to a tag (e.g. `v0.5.0`) instead of latest |
| `KPOT_INSTALL_DIR` | Install to a custom directory |

If `curl … \| bash` makes you uncomfortable (reasonable for a secret
manager), pick one of these instead:

**`go install`** (Go 1.18+):

```bash
go install github.com/Shin-R2un/kpot/cmd/kpot@latest
```

**Manual download** — grab the matching archive from
<https://github.com/Shin-R2un/kpot/releases/latest>, verify the
SHA-256 yourself against `checksums.txt`, and drop the binary on
your `PATH`. Targets: linux amd64/arm64, darwin amd64/arm64,
windows amd64.

**From source**:

```bash
git clone https://github.com/Shin-R2un/kpot && cd kpot
make build      # → ./kpot
make test       # → go test ./...
make install    # → $(go env GOPATH)/bin/kpot
```

If `~/go/bin` isn't on your `PATH`, build directly into a directory
that is:

```bash
go build -o ~/bin/kpot ./cmd/kpot
```

## Quick start

```bash
kpot init personal.kpot          # create vault, prompt for passphrase,
                                 # and DISPLAY YOUR RECOVERY SEED ONCE.
                                 # Write it down — there's no reissue.

kpot personal.kpot               # open REPL with passphrase (everyday)
kpot personal.kpot --recover     # open REPL with the recovery seed
                                 # (emergency only — then run `passphrase`)

kpot:personal> help                 # full command list
kpot:personal> note ai/openai       # create new note (or open existing)
kpot:personal> ls                   # list note names
kpot:personal> find openai          # case-insensitive name + body search
kpot:personal> rm  ai/openai        # asks "remove note 'ai/openai'? [y/N]"

# v0.6: cd into a note and use context-aware commands
kpot:personal> cd ai/openai         # enter note context (prompt updates)
kpot:personal/ai/openai> show       # print the whole body
kpot:personal/ai/openai> show url   # print just the `url:` field
kpot:personal/ai/openai> fields     # list field keys (id, url, apikey, …)
kpot:personal/ai/openai> cp apikey  # copy that field's value to clipboard
kpot:personal/ai/openai> set url https://api.openai.com   # update field
kpot:personal/ai/openai> set apikey                       # secret prompt
kpot:personal/ai/openai> unset old_field
kpot:personal/ai/openai> cd ..      # leave context (cd / works too)

# Original commands still work and are unchanged
kpot:personal> read ai/openai       # print the body to stdout
kpot:personal> copy ai/openai       # → clipboard, auto-clears (30s default)
kpot:personal> template show        # inspect new-note template
kpot:personal> template             # edit the template in $EDITOR
kpot:personal> passphrase           # rotate this vault's passphrase
kpot:personal> export               # print decrypted JSON to stdout
kpot:personal> exit
```

Or run a single command without entering the REPL:

```bash
kpot personal.kpot ls
kpot personal.kpot read ai/openai
kpot personal.kpot copy ai/openai
kpot personal.kpot rm -y ai/openai
kpot personal.kpot export -o backup.json --force
kpot personal.kpot import backup.json --mode merge
```

For automation, set `KPOT_PASSPHRASE` to bypass the TTY prompt — kpot
prints a one-time stderr warning so you notice when it's set:

```bash
KPOT_PASSPHRASE='hunter2' kpot personal.kpot ls
```

Note: `init` always issues a recovery key and refuses to run if stdin/
stdout aren't real terminals — that's deliberate, so the seed never
ends up in CI logs or shell scrollback. Run `init` interactively, then
automate everyday operations after.

Multi-line paste works as-is: the REPL uses `peterh/liner` which
honors bracketed-paste mode. For longer content prefer `note <name>`
(opens `$EDITOR`).

Anywhere in the REPL, **TAB** completes the command at the start of the
line, or the note name after a command that takes one (`note` / `read` /
`copy` / `rm`). `template <TAB>` completes to `show` / `reset`.
`↑` / `↓` walk the in-session history.

## Commands

| command | shape | what it does |
|---|---|---|
| `ls` | – | list all note names, sorted |
| `note <name>` | `<name>` | open `$EDITOR`. Existing → edit; new → seed with template |
| `read <name>` | `<name>` | print the note body to stdout |
| `copy <name>` | `<name>` | put body on the clipboard, auto-clear after configured TTL |
| `find <query>` | free text | case-insensitive substring over name **and** body |
| `rm [-y] <name>` | flag + name | remove a note (`-y` / `--yes` skips the `[y/N]` prompt) |
| `template` | – | edit the per-vault new-note template in `$EDITOR` |
| `template show` | – | print the current template + which source (vault / built-in) |
| `template reset` | – | drop the per-vault template, fall back to the built-in default |
| `passphrase` | – | rotate this vault's passphrase (the previous `.bak` is removed so an old-passphrase copy doesn't linger; on v2 vaults the recovery key is preserved) |
| `recovery-info` | – | print the vault's recovery type (`seed-bip39` / `secret-key` / none). No params, no secrets. |
| `export [-o p] [--force]` | flags | print decrypted JSON to stdout, or write to a file (file write needs `--force` to overwrite) |
| `import <json> [--mode merge\|replace] [-y]` | path + flags | merge (default) or replace using JSON produced by `export`. Merge conflicts kept under `<name>.conflict-YYYYMMDD[-N]` |
| `bundle <name>... -o <path>` | names + path | encrypt selected notes into a portable `.kpb` file (asks for a passphrase you'll share with the recipient) |
| `import-bundle <path> [-y]` | path + flag | decrypt a `.kpb` (asks for source passphrase), preview, and merge in. Same conflict-naming as `import` |
| `help` / `?` | – | show this list |
| `exit` / `quit` / `q` / Ctrl-D | – | close the vault and quit |

`Ctrl-C` cancels the in-progress line but keeps the REPL alive.

Note names: lowercase ASCII `[a-z0-9._/-]`, 1..128 chars, no leading/
trailing `/`, no `//`. Hierarchical names (`ai/openai`, `server/fw0`)
are encouraged — they make `ls` / `find` / TAB completion easier to
navigate.

## New-note template & frontmatter

When `note <name>` opens for an entry that doesn't yet exist, `$EDITOR`
receives a frontmatter block plus a starter template body. Example:

```markdown
---
created: 2026-04-25T21:35:12+09:00
updated: 2026-04-25T21:35:12+09:00
---

# ai/openai

- id:
- url:
- password:
- api_key:

## memo

```

- The `---` frontmatter is **regenerated each open** from JSON metadata
  (the source of truth for timestamps) and **stripped on save**. Editing
  the timestamps in the body has no effect — the displayed values
  always reflect the current `created_at` / `updated_at`.
- The starter body is the **template**, customizable per vault:
  - `template show` — print current template + source
  - `template` — open in `$EDITOR`; saving stores it inside the vault
  - `template reset` — clear the override, fall back to the built-in
- **Placeholders** are expanded once when a new note is created. They
  do not run on subsequent edits — substituted values become part of
  the saved body.

  | placeholder | example for `note ai/openai` |
  |---|---|
  | `{{name}}` | `ai/openai` |
  | `{{basename}}` | `openai` |
  | `{{date}}` | `2026-04-25` |
  | `{{time}}` | `21:35` |
  | `{{datetime}}` | `2026-04-25T21:35:12+09:00` |

  Unknown `{{tokens}}` are left untouched, so writing a literal `{{x}}`
  in the body is safe.
- Saving an unmodified template (no edits between open and `:wq`) skips
  the write — kpot prints `(template unchanged; not saved)`.

## Crypto & on-disk layout

- KDF: **Argon2id** (64 MiB / 3 iters / 1 parallelism) → 32-byte key.
  Parameters stored in the header so a future upgrade can decrypt old
  vaults.
- AEAD: **XChaCha20-Poly1305** with a fresh 24-byte nonce per write.
- AAD binds the header (KDF params, cipher choice) to the ciphertext —
  any tampering fails authentication with the standard error.
- Atomic write: `<file>.tmp` → `fsync` → swap with `<file>` → keep prior
  generation as `<file>.bak`. A crash at any step leaves at least one
  decryptable file behind.
- Wrong passphrase and a corrupted file return the **same** error
  (`Wrong passphrase, or the file is corrupted`) — the binary doesn't
  leak which one it was.

See `docs/format.md` for the byte-level layout (note: the plaintext
payload also carries an optional `template` field, omitted when unset).

## Clipboard

`copy <name>` shells out to a platform-specific tool:

| OS | preferred | fallback |
|---|---|---|
| Linux | `wl-copy` / `wl-paste` (when `WAYLAND_DISPLAY` is set) | `xclip` → `xsel` |
| macOS | `pbcopy` / `pbpaste` | – |
| Windows | PowerShell `Set-Clipboard` / `Get-Clipboard` | – |

After `copy`, kpot waits 30 seconds and clears the clipboard — but
**only if it still holds what kpot put there**. If you copy something
else in the meantime, your value is left alone. On REPL exit, any
still-pending wipe runs synchronously so a secret never outlives the
session.

If no backend is found, `copy` errors out; everything else still works.

## Editor integration

- `$EDITOR` → fallback to `$VISUAL` → `nano` / `vim` / `vi` / `notepad`.
- Temp file lives in `/dev/shm` on Linux (tmpfs, never hits disk),
  otherwise the OS temp dir. Permissions are `0600`.
- On editor exit (success or failure) the temp file is overwritten with
  zeros and unlinked.

## Configuration

Optional, lives at `~/.config/kpot/config.toml` (or the platform
equivalent of `os.UserConfigDir()`). All keys are optional; a missing
file is fine.

```toml
# Editor preferred over $EDITOR / $VISUAL (so a personal preference
# applies regardless of the parent shell).
editor = "vim"

# Override the 30-second clipboard auto-clear.
clipboard_clear_seconds = 60

# OS keychain caching: "auto" (prompt once per vault), "always" (cache
# silently), or "never" (disabled).
keychain = "auto"

# REPL auto-closes after N minutes of no command activity (default 10).
# Single-shot subcommands are unaffected.
idle_lock_minutes = 10

# v0.7+: where `kpot <bare-name>` looks for vaults. Default ~/.kpot.
# `~/` is expanded at load time. `kpot init personal` creates the
# directory if missing (chmod 0700).
vault_dir = "~/.kpot"

# v0.7+: vault opened by bare `kpot` with no positional argument.
# Goes through the same name resolution as a CLI argument: bare
# names get `.kpot` appended and resolve under vault_dir.
default_vault = "personal"
```

### Vault name resolution (v0.7+)

`kpot <name>` no longer requires a path. With the defaults above:

```bash
kpot                    # → opens ~/.kpot/personal.kpot (default_vault)
kpot personal           # → ~/.kpot/personal.kpot
kpot work read api/foo  # → ~/.kpot/work.kpot, single-shot read
kpot init shared        # → creates ~/.kpot/shared.kpot (mkdir -p first)

# Path-like inputs still pass through unchanged:
kpot ./local.kpot       # CWD file
kpot /srv/team.kpot     # absolute path
kpot ../sibling.kpot    # relative with separator
```

Resolution order for a bare name like `personal`:

1. If the input contains `/` or `\\`, use as-is.
2. Otherwise append `.kpot` if missing.
3. If `<candidate>` exists in the current working directory, use it (back-compat).
4. Else fall back to `<vault_dir>/<candidate>`.

Editor lookup order: config `editor` → `$EDITOR` → `$VISUAL` → `nano` /
`vim` / `vi` (or `notepad` on Windows).

### Managing the config file (v0.7+)

```bash
kpot config init        # write a starter config.toml at the default path
kpot config show        # print effective config (file values + defaults)
kpot config path        # print where kpot looks for the file
$EDITOR $(kpot config path)
```

`config init` refuses to clobber an existing file. Pass `--force` to
overwrite. The starter template is fully commented; uncomment lines
to deviate from defaults.

## Recovery key (v0.3+)

Every vault created with v0.3+ ships with a **recovery key** displayed
once at `init` time:

| flag | result | typical use |
|---|---|---|
| `kpot init <file>` | 12-word BIP-39 seed (default) | best for paper backup |
| `kpot init <file> --recovery seed --recovery-words 24` | 24-word BIP-39 seed | paranoid mode, 256-bit |
| `kpot init <file> --recovery key` | 32-byte secret rendered as Crockford Base32 | best for password manager paste |

Recovery is **issued once and cannot be reissued**. The vault file
embeds an immutable `recovery_wrap` alongside the everyday
`passphrase_wrap`, so:

- Forgot the passphrase → `kpot <file> --recover` opens the vault
  using the recovery key, then `passphrase` rotates to a new everyday
  passphrase. Recovery key continues to work.
- Lost both → vault is **permanently unrecoverable**. No backdoor.
- v1 vaults (created with v0.1/v0.2) keep working without recovery.
  Adding recovery requires `export` → new vault → `import`.

Display safety: `init` refuses to run when stdin or stdout aren't
real TTYs, and writes the seed to `/dev/tty` (not stdout/stderr) so
it doesn't leak into shell scrollback, log files, or CI artifacts.

## OS keychain caching (v0.4+)

By default, the first time you open a vault interactively kpot asks
whether to cache the per-vault open key in the OS-native secret store.
On subsequent runs the passphrase prompt and the ~100ms Argon2id
derivation are both skipped.

```bash
kpot personal.kpot
Passphrase: ********
Cache key in OS keychain so future opens skip the passphrase? [Y/n]: y
Opened personal.kpot (3 notes)
kpot:personal>

# Next invocation:
kpot personal.kpot ls
ai/openai
server/fw0
```

Backends per OS (no third-party Go dependencies — the project shells
out to system tooling or calls OS APIs directly):

| OS | backend | requirement |
|---|---|---|
| macOS | `/usr/bin/security` (Keychain Services) | shipped with macOS |
| Linux | `secret-tool` (libsecret + GNOME Keyring / KWallet) | `apt install libsecret-tools` (or `dnf install libsecret`); needs a session D-Bus |
| Windows | `wincred` syscall via `golang.org/x/sys/windows` | shipped with Windows |

Flags & commands:

| invocation | effect |
|---|---|
| `kpot <file>` | use cached key if present, else prompt + (interactively) ask to cache |
| `kpot <file> --no-cache` | skip the cache for this run (still uses passphrase) |
| `kpot <file> --forget` | drop the cached entry and exit (or precede a single subcommand) |
| `kpot keychain test` | report which backend is in use and whether it's reachable |

`KPOT_PASSPHRASE` always disables both Get and Set so CI/script runs
don't accidentally pollute (or leak from) the user's keychain.

The `passphrase` rotation command interacts with the cache version-
aware:

- v1 vaults: derived key changes → cached entry is invalidated
- v2 vaults: DEK is preserved across rotations → cached entry stays
  valid (this is the whole point of the v2 envelope)

Recovery flow (`--recover`) intentionally never touches the cache.

Headless / SSH / container considerations:
- Linux without `DBUS_SESSION_BUS_ADDRESS` reports `available: false`
  and falls back to the passphrase prompt every time. No warning
  unless the config is set to `keychain = "always"`.
- iCloud Keychain sync on macOS may replicate entries to other Apple
  devices. If that's not desired, set `keychain = "never"` and rely
  on the passphrase.
- Sleep/wake: macOS keychain may auto-lock; Linux/Windows keep entries
  available for the duration of the login session.

Known limitation — macOS argv exposure:
- The `Set` path uses `/usr/bin/security add-generic-password -w <hex>`,
  which means the hex-encoded key briefly appears in the process's
  command line. macOS Big Sur+ restricts `ps` argv visibility to the
  same UID, so this matches the same threat boundary as the keychain
  entry itself (a same-user attacker who can read your keychain can
  also read your `ps`). Linux uses stdin pipe and Windows uses syscall,
  so neither is affected. If this matters for your model, set
  `keychain = "never"` on macOS.

## Selective transfer between vaults (v0.5+)

When you want to move a few notes from `b.kpot` into `a.kpot` —
without exposing the rest of `b.kpot` or doing a full vault merge —
use the **bundle** flow:

```bash
# On the source side: pick which notes to transfer
kpot b.kpot bundle ai/openai server/fw0 -o transfer.kpb
Bundle passphrase (recipient will need it): ********
wrote 2 notes to transfer.kpb

# Move transfer.kpb to the other machine (USB / Drive / email — the
# file is already encrypted, so the transport doesn't have to be
# trusted).

# On the destination side: import the bundle into your own vault
kpot a.kpot import-bundle transfer.kpb
Source bundle passphrase: ********
bundle contains 2 notes:
  ai/openai                        OPENAI_API_KEY=sk-xxx...
  server/fw0                       ssh user@fw0
import 2 notes into this vault? [y/N]: y
imported: +2 new, 0 conflicts renamed
```

A `.kpb` (kpot bundle) is a self-contained encrypted blob — the
recipient never needs the source vault file, just the bundle and the
bundle passphrase. Same crypto primitives as the vault format
(Argon2id over the passphrase + XChaCha20-Poly1305 AEAD with header
bound as AAD); name collisions on import land under
`<name>.conflict-YYYYMMDD[-N]` so nothing is silently overwritten.

This is intentionally **selective** rather than a full vault merge:
the common workflow is "I want to move a few entries from one vault
to another," not "combine everything." Use `export` / `import` if you
genuinely want a full-vault merge (those operate on plaintext JSON).

## Idle lock

When stdin is a real TTY, kpot starts a 10-minute idle timer at REPL
launch. Any command, Ctrl-C, or empty ENTER resets the timer. If it
fires, kpot wipes the in-memory key and exits the process:

```
kpot:personal>
   ... 10 minutes pass ...
(idle timeout — vault locked)
$
```

Single-shot subcommands (`kpot <file> ls` etc.) don't enter the REPL
and are unaffected. Adjust the period via `idle_lock_minutes` in
`config.toml`.

## Out of scope (future PRs)
- v0.5: transport-agnostic vault primitives — `kpot merge a.kpot b.kpot`,
  `<file>.lock`, optional payload metadata for merge automation. Bytes
  shipping (Git / Drive / USB / Syncthing) is intentionally **not**
  bundled — pick whichever transport you prefer
- v0.6: `kpot materialize` (`/run/kpot/<name>.env`)
- v0.7: TUI mode (bubbletea)
- v0.8: MCP / agent integration

## Development

Dev tooling is under `make help`. The most-used targets:

```bash
make build              # → ./kpot
make check              # vet + gofmt-check + test (mirrors CI exactly)
make fmt                # gofmt -w .

make release-patch      # v0.6.0 → v0.6.1 (auto: bump + tag-message + push)
make release-minor      # v0.6.0 → v0.7.0
make release-major      # v0.6.0 → v1.0.0

make install-hooks      # adds .git/hooks/pre-push that runs 'make check'
                        # before every push (skip with --no-verify)
make uninstall-hooks
```

The `release-*` targets run `make check` first, refuse on a dirty
tree or out-of-sync `main`, generate a tag message from `git log`,
ask for confirmation, then `git push origin v0.X.Y` — which triggers
the GitHub Actions release workflow (binaries + scoop manifest auto-
update). Append `YES=1` (e.g. `make release-patch YES=1`) to skip
the prompt.

## Security

kpot stores secrets, so security posture matters:

- **Threat model**: see [`docs/security.md`](docs/security.md) for what
  kpot defends against (lost laptop, cloud sync, casual snooping)
  and what it explicitly does not (compromised host, memory dumps,
  side channels).
- **Vulnerability reports**: see [`SECURITY.md`](SECURITY.md). Use
  GitHub Security Advisories first; backup contact is shin@r2un.com.
  Acknowledgement within 7 days, coordinated disclosure on a 60–90
  day timeline.

## License

[MIT](LICENSE) © 2026 Shin-R2un

## File layout

```
cmd/kpot/main.go             argv routing
internal/crypto              Argon2id + XChaCha20-Poly1305
internal/vault               .kpot file format, atomic write, .bak
internal/store               in-memory note CRUD, name normalization, search
internal/repl                interactive command loop, prompter, TAB completion
internal/editor              $EDITOR launcher, tmpfs temp file
internal/clipboard           cross-platform copy + 30s auto-clear manager
internal/notefmt             editor frontmatter render/strip, template, placeholders
internal/bundle              .kpb selective-transfer format (Argon2id + XChaCha20)
internal/config              ~/.config/kpot/config.toml loader (BurntSushi/toml)
internal/recovery            BIP-39 seed + Crockford-Base32 secret-key encoders, KEK derivation
internal/keychain            macOS Keychain / Linux secret-tool / Windows wincred (no third-party Go deps)
internal/tty                 passphrase prompt (no echo, KPOT_PASSPHRASE bypass), TTY-only recovery display
docs/format.md               on-disk file format spec (v1)
```
