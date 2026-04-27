# kpot security & threat model

> **Audience**: anyone deciding whether to trust kpot with secrets,
> reviewers, and security researchers about to file a report.
>
> **Scope**: this document describes what kpot v0.5.x is designed to
> defend against, what it explicitly is **not**, and the rationale
> for each crypto / OS choice. The vulnerability reporting flow is
> in [SECURITY.md](../SECURITY.md).

## TL;DR

kpot is a **single-file encrypted note vault** for a trusted machine.

| Threat | Defense | Notes |
|---|---|---|
| Lost laptop / stolen disk image | ✅ Strong | XChaCha20-Poly1305 + Argon2id |
| Cloud sync of `.kpot` (Drive/Dropbox/iCloud) | ✅ Strong | Same — encrypted at rest |
| Casual snooping of an unattended terminal | ✅ Idle lock | Configurable timeout |
| Forgotten passphrase | ✅ BIP-39 recovery seed | Mandatory at `init` |
| Compromised host (keylogger, malware) | ❌ No defense | Outside kpot's threat model |
| Memory dump / debugger on a running process | ⚠️ Best-effort | Go runtime limits effectiveness |
| Side-channel timing / power analysis | ❌ No defense | Single-user CLI assumption |
| Quantum adversary | ❌ No defense | Classical crypto only |

If you need defense in the bottom four rows, kpot is the wrong tool —
look at hardware-backed solutions (YubiKey FIDO2, Apple Keychain with
Secure Enclave, OS HSM-backed credential stores).

## What kpot defends against

### 1. Lost or stolen `.kpot` file

The vault file is **encrypted at rest** with XChaCha20-Poly1305
(authenticated encryption with associated data). Without the
passphrase or the recovery seed, the file is opaque ciphertext.

- **Cipher**: XChaCha20-Poly1305 (RFC 8439 + 24-byte XChaCha20 nonce).
  Standard, peer-reviewed AEAD construction.
- **KDF**: Argon2id, defaults `memory=64 MiB, iterations=3, parallelism=1`.
  Memory-hard, resists GPU/ASIC offline attacks better than PBKDF2/scrypt.
- **Key**: 32-byte derived KEK; in format v2, the KEK wraps a separate
  random DEK so multiple unwrap paths (passphrase + recovery seed) can
  exist without re-encrypting the payload.
- **Tamper detection**: Poly1305 MAC (16 bytes appended to ciphertext).
  Modified vault files fail to decrypt — **no silent corruption**.
- **Salt / nonce**: per-vault random 16-byte salt, per-encrypt random
  24-byte nonce. Re-encrypted on every save, so file diffs leak no
  structural information.

This means: **dropping a `.kpot` file in Google Drive, emailing it to
yourself, or losing the laptop it's on does not leak the contents**,
provided your passphrase is strong enough to resist offline brute force.

Argon2id at the default parameters costs roughly **0.5 seconds per
attempt on a modern laptop**. A 5-word Diceware passphrase
(~64 bits of entropy) takes a well-funded attacker on the order of
years to exhaust offline; a 6-word passphrase takes centuries.

### 2. Forgotten passphrase

A 12- or 24-word **BIP-39 recovery seed** is generated at `init` and
displayed once on `/dev/tty`. The seed unlocks the vault via a
parallel wrap of the same DEK, so changing the passphrase does not
invalidate the seed (and vice versa).

The seed is **mandatory** — kpot refuses to `init` without a TTY,
so the seed never ends up in CI logs, shell scrollback, or pipes.

If you lose the passphrase **and** the seed, the vault is
unrecoverable. There is no backdoor and no key escrow.

### 3. Casual snooping at an unattended terminal

The REPL caches the unwrapped DEK only for a configurable idle
timeout. After the timeout, the next command re-prompts for the
passphrase.

`copy` clears the OS clipboard after a configurable timeout
(default 30 s) so secrets don't sit in clipboard history.

`read` writes to stdout (your choice — convenient but visible);
prefer `copy` for "show but don't display."

## What kpot explicitly does **not** defend against

### 1. A compromised host

If your machine is running a keylogger, screen recorder, or malware
with your user privileges, **kpot cannot help you**. The attacker
can log your passphrase as you type it, screen-record `read`
output, or attach `ptrace` to the running process.

This is by design. A pure-software CLI on a compromised host
has no trusted boundary. The right escalation is a hardware token
(YubiKey, Secure Enclave) where the secret never enters host memory.

### 2. Memory dumps of a running kpot process

While kpot calls `crypto.Zero` on passphrases, DEKs, and seeds as
soon as they're done being used, **Go's runtime makes complete
memory hygiene impractical**:

- The garbage collector copies values across the heap as it grows
  generations. Old copies linger until reclaimed.
- Strings are immutable; once a passphrase is converted to a Go
  `string` (e.g., for upstream library APIs that demand it), the
  original buffer cannot be safely zeroed.
- Pages are not `mlock`'d, so the kernel can swap them to disk.

What kpot does:

- Reads passphrases / seeds into `[]byte` and zeros them after use.
- Avoids `string` conversions where feasible.
- For the few unavoidable conversions (e.g., BIP-39's
  `IsMnemonicValid(string)`), comments mark them as best-effort.

What kpot does **not** do (yet — see
[kpot-ideas.html #24](../kpot-ideas.html)):

- `mlock(2)` / `VirtualLock` to prevent swap-out.
- Locked memory pools for sensitive allocations.

So: **assume that an attacker with `gcore` or a debugger attached
to a live kpot process can recover whatever is currently unlocked**.
Lock the vault when stepping away.

### 3. Side-channel attacks

kpot is a single-user CLI. We do not defend against:

- Timing analysis of cryptographic operations.
- Power analysis or EM side channels (irrelevant for software).
- CPU cache timing across SMT cores (assumes you trust the local
  user).

Our crypto primitives (the `golang.org/x/crypto/chacha20poly1305`
package) are constant-time at the operations level, which is
incidental hardening, not a designed defense.

### 4. Quantum adversaries

kpot uses classical symmetric crypto (XChaCha20-Poly1305, Argon2id).
Symmetric primitives lose at most one bit of effective security
against Grover's algorithm, so a 256-bit DEK still has ~128-bit
post-quantum security. Argon2id is unaffected.

Asymmetric crypto **is not used** in kpot — there is no PKI, no
forward secrecy story, no signed bundles. This means there is
nothing for Shor's algorithm to break in the current design.

If we add asymmetric crypto later (e.g., for inter-user `.kpb`
sharing with public-key wrapping), we will need to revisit this.

### 5. Supply chain attacks on the binary

Today, kpot release binaries are produced by goreleaser in GitHub
Actions and uploaded to GitHub Releases. **The release pipeline is
trusted but not provable** — there is no signature, no attestation,
and no reproducible-build manifest.

What you can do today:

- Verify SHA-256 against `checksums.txt` (the install scripts
  do this automatically).
- Build from source: `git clone … && go build ./cmd/kpot`.

What's planned (see [kpot-ideas.html #21–23](../kpot-ideas.html)):

- Sigstore / cosign signed releases.
- SLSA provenance attestations.
- Reproducible-build documentation.

## Crypto choices, in detail

### Why XChaCha20-Poly1305 over AES-GCM?

- **No nonce-reuse footgun**: 24-byte XChaCha20 nonces are large
  enough that random nonces collide with negligible probability. AES-GCM
  has 12-byte nonces — random selection is borderline at scale.
- **Software performance**: XChaCha20 is faster than AES on devices
  without AES-NI (older ARM, embedded). Comparable on modern x86.
- **No hardware bias**: works the same everywhere. We don't need to
  conditionally use AES-NI vs software fallback.

### Why Argon2id over PBKDF2 or scrypt?

- **PBKDF2** is iteration-only — cheap on GPUs/ASICs.
- **scrypt** is memory-hard but old; some implementations leak
  timing.
- **Argon2id** is the [PHC winner](https://www.password-hashing.net/),
  hybrid memory-hard + iteration-hard, and side-channel-resistant
  (the `id` variant blends `i` and `d` modes).

Defaults (`memory=64 MiB, iterations=3`) target ~0.5 s on a 2020-era
laptop. Tweak via the planned `--argon2-target` flag (see
[kpot-ideas.html #18](../kpot-ideas.html) — auto-calibration).

### Why BIP-39 for the recovery seed?

BIP-39 is the de facto standard for human-transcribable secrets,
familiar to anyone who has used a hardware wallet. The wordlist
(2048 words) is unambiguous, the checksum catches single-word
typos, and 12 words encodes 128 bits of entropy (24 words = 256).

We **do not** derive any cryptocurrency keys from the seed.
It is purely a backup unwrap path for the DEK. Reusing the BIP-39
encoding gives us:

- A vetted wordlist.
- Built-in checksum.
- Existing user familiarity.

### Why store the unwrapped DEK in the OS keychain?

Convenience-vs-security trade-off, opt-in. The OS keychain
(macOS Keychain, Linux Secret Service, Windows Credential Manager)
holds the DEK for the duration of an OS user session, so the user
doesn't re-type the passphrase on every command.

**Risk**: if the keychain is compromised (e.g., a malicious app
with keychain entitlements), the DEK is exposed. We accept this
trade-off because the alternative — re-prompting the passphrase
on every CLI invocation — drives users to weaker passphrases.

The keychain integration uses **only OS-native APIs** (no
third-party Go dependencies), reducing the supply-chain surface.
See `internal/keychain/` for the per-OS implementations.

## Format versioning and migration

The on-disk format is documented in [`docs/format.md`](format.md)
and versioned in the file's `version` field.

- **v1**: passphrase-only, KEK encrypts payload directly.
- **v2**: KEK wraps a random DEK; multiple wraps (passphrase +
  recovery seed) can exist concurrently.

`kpot` reads both v1 and v2 transparently. v2 vaults remain v2
forever (no auto-downgrade); v1 vaults are upgraded to v2 the
first time the passphrase is rotated or the recovery flow is used.

Format changes go through a **deprecation period of at least 6
months** before a reading version is removed. Any breaking change
will be called out in release notes with explicit migration steps.

## Reporting suspected issues

**Read [SECURITY.md](../SECURITY.md) for the full reporting flow.**

In short: **GitHub Security Advisories first**, email
**shin@r2un.com** as a backup. Acknowledgement within 7 days,
coordinated disclosure on a 60–90 day timeline.

## Changelog of this document

- **2026-04-28**: Initial threat model for v0.5.x.
