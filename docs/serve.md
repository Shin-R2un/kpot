# `kpot serve` — mobile WebUI (v0.9+)

Read-only web interface for accessing a kpot vault from a smartphone via
SSH tunnel. Designed for the workflow:

> 「外出先のスマホから、自宅サーバの kpot vault のパスワードをコピーして
> ブラウザに貼りたい」

## Threat model addendum

`kpot serve` ships under the same threat model as the rest of kpot
(`docs/security.md`), with two specific properties:

- **Listens on `127.0.0.1` only.** There is no `--bind` flag. The plain-
  HTTP boundary is the SSH tunnel, not TLS. Exposing the port on the
  LAN would contradict the "compromised host out of scope" boundary.
- **Read-only.** No endpoint mutates the vault. Edits remain a REPL/CLI
  responsibility because the vault format has no file lock yet — REPL
  + `serve` writing concurrently would race.

If your phone is shoulder-surfed or stolen unlocked while a kpot
session is active, the attacker can read every note. Mitigations:

- Set a short `--idle` (e.g. `--idle 5`) so the session locks quickly.
- Sign out via the lock icon when you put the phone down.

## Architecture

```
[Phone Safari/Chrome]                    [FW0 host]
      │
      │ VPN to FW0 LAN
      ▼
   FW0 reachable
      │
      │ ssh -L 8765:127.0.0.1:8765 user@fw0
      ▼
http://localhost:8765/                    kpot serve <vault>
                                            └── 127.0.0.1:8765 only
```

## Quick start

On the host (FW0 in our running example):

```bash
# Bare-name resolution (v0.7+) — vault under ~/.kpot/
kpot serve 1pswd

# Or absolute path
kpot serve /srv/secrets.kpot --port 8765 --idle 30
```

The first line above prints the SSH tunnel command you need from the
phone side. Defaults:

| Flag | Default | Meaning |
|---|---|---|
| `--port` | `8765` | TCP port on `127.0.0.1` |
| `--idle` | `30` | Per-session idle minutes (`0` = disable) |
| `--no-cache` | off | Skip OS keychain even if a DEK is cached. Forces every visit through the web passphrase form. |

If the OS keychain holds a cached DEK for this vault (because you've
previously opened it via `kpot <vault>`), the daemon **silently unlocks
at startup** and the first phone visit needs no passphrase. After
`--idle` minutes of inactivity, the session locks and the user re-
enters the passphrase via the web form. The keychain bootstrap is not
re-consulted for re-auth — only the initial cookie mint.

If the keychain has no cached DEK, the user types the passphrase at
the web form on first visit.

## Two access patterns

The daemon supports two deployment shapes. Pick one:

### Pattern A — SSH tunnel (default, simplest setup)

Daemon binds `127.0.0.1` only. Phone connects via an SSH client app
that forwards `localhost:8765` to the daemon.

```bash
# Host
kpot serve 1pswd            # binds 127.0.0.1:8765 by default

# Phone (Termius / Blink / a-Shell etc.)
ssh -L 8765:127.0.0.1:8765 user@host
# → mobile Safari: http://localhost:8765/
```

Pros: zero firewall changes, plaintext HTTP wrapped in SSH transport.
Cons: SSH session has to stay alive; reconnect on iOS background drop.

### Pattern B — Direct VPN access (Safari-only UX)

Daemon binds the VPN-interface IP. Phone reaches it directly through
WireGuard / Tailscale / OpenVPN. **Plain HTTP** so the VPN itself
must provide transport encryption (WireGuard's ChaCha20-Poly1305 does).

```bash
# Host (one-time)
sudo ufw allow in on wg0 to any port 8765 proto tcp
# (or restrict to a single phone source: `... from 10.0.0.5`)

# Host (daemon)
kpot serve 1pswd --bind 10.0.0.1   # ← FW0's WireGuard interface IP

# Phone
# 1. Tap WireGuard VPN ON
# 2. Open bookmark: http://10.0.0.1:8765/
```

Pros: Safari-only daily UX (one tap WG ON, one tap bookmark).
Cons: requires VPN setup + firewall hygiene. Misconfigured UFW = LAN exposure.

Required hygiene:
- Bind to the **VPN interface IP**, not `0.0.0.0`. The daemon literally
  cannot accept connections from non-VPN paths if bound to `wg0`'s IP.
- UFW rule should restrict by interface (`in on wg0`) AND/OR by source
  IP if multiple devices share the VPN.
- Confirm with `lsof -iTCP:8765 -sTCP:LISTEN` that the daemon binds the
  expected address only.
- The daemon prints a `⚠️ WARNING` to stderr when bound to a non-loopback
  address. Don't ignore it.

For Tailscale: same shape, replace `wg0` with `tailscale0` and use the
machine's Tailscale IP (100.x.y.z). Tailscale ACLs replace UFW rules.

## SSH tunnel from the phone

Recommended SSH clients:

- **iOS**: [Termius](https://termius.com/) (free tier supports local
  port forwarding) or [Blink Shell](https://blink.sh/) (paid, more
  full-featured).
- **Android**: [Termius](https://termius.com/), [JuiceSSH](https://juicessh.com/),
  or `ssh` from Termux.

Configure a port-forward of `8765 → 127.0.0.1:8765` on your FW0 SSH
host. Once the SSH session is up, open
**`http://localhost:8765/`** in Safari / Chrome on the phone. The
cookie-based session will persist as long as the SSH connection is
alive.

## API

JSON over HTTP. All endpoints under `/api/*` require an active session
(or auto-mint one if keychain bootstrap is in effect). See
`internal/serve/serve.go` for the routing table.

| Method | Path | Notes |
|---|---|---|
| `POST` | `/api/login` | `{"passphrase":"..."}` → `200` + cookie / `401` / `429` |
| `POST` | `/api/logout` | `204` |
| `GET` | `/api/status` | `{state:"active"\|"locked"\|"none", idle_remaining_s}` |
| `GET` | `/api/notes?q=...` | Search; empty `q` returns all names. Body field is **never** included in this endpoint. |
| `GET` | `/api/notes/{name}` | Detail. Secret-field values are redacted in `body_redacted`. |
| `GET` | `/api/notes/{name}/field/{key}` | Real value of one field (incl. secrets). Used by click-to-copy. |
| `GET` | `/api/notes/{name}/url` | `302` redirect to the note's `url:` field. `404` if absent. |

Note names with `/` get URL-encoded: `ai/openai` → `ai%2Fopenai`.

`POST` / `PUT` / `DELETE` on `/api/notes/*` always return `405`.

## Mobile clipboard caveats

iOS Safari is strict about `navigator.clipboard.writeText`: it works
**only inside a synchronous user-gesture handler**. Async patterns
(await fetch, then write) silently fail.

To work around this, the WebUI:

1. Pre-fetches every field value when you open a note's detail view.
2. Caches the values in a JS closure for 30 seconds.
3. Wires `[Copy]` buttons to call `clipboard.writeText` synchronously
   on click.
4. After 30s, the cache is overwritten with `''` and the button label
   becomes `[Re-fetch]`. Tap once to re-fetch, then again to copy.

**iOS clipboard cannot be programmatically cleared.** After the in-
page 30s timer fires, the password is gone from the WebUI's memory
but **still sits in the iOS clipboard** until you copy something
else. Make a habit of copying a non-sensitive value (your name,
"hello") after using a password to overwrite the clipboard slot.

## Auto-fill — explicitly not supported

We considered this and concluded it's architecturally impossible
without a native iOS/Android app:

- iOS Password AutoFill requires an
  [`apple-app-site-association`](https://developer.apple.com/documentation/security/password_autofill/setting_up_an_app_s_associated_domains)
  served from a public HTTPS origin and a shipping native app with
  the matching bundle ID. Localhost-over-tunnel cannot satisfy AASA.
- Android autofill needs `assetlinks.json` on a public HTTPS origin
  and a shipping app, same shape.
- Bookmarklets can fill forms only on the *current* page, not on a
  page you switch to in another tab — cross-origin script injection
  is what browsers exist to prevent.

Realistic UX is **two taps**:

1. Tap `[Open URL]` → site opens in a new browser tab.
2. Tap your username field, paste username (which you copied first).
3. Switch back to kpot tab, tap `[Copy password]`.
4. Switch to site tab, tap password field, paste.

This is how every "self-hosted password manager without a browser
extension" works on phones today.

## Operational notes

- Press `Ctrl-C` in the terminal running `kpot serve` to stop. All
  in-memory DEKs are zeroed before exit.
- Multiple SSH sessions to the same daemon work — each phone gets its
  own cookie and idle timer.
- One daemon = one vault. To serve two vaults run a second instance
  on a different `--port`.
- Logs are stderr-only, request-method+path level (no query strings,
  no note names).
- The static frontend is embedded in the binary via `go:embed`. No
  external assets, works fully offline once the page loads.

## Security verification checklist

When deploying, double-check:

- [ ] `lsof -iTCP:8765 -sTCP:LISTEN` shows `127.0.0.1:8765` (not `*:8765`).
- [ ] Phone visits succeed only when SSH tunnel is active. Without
  the tunnel, browser hits `localhost:8765` get connection refused.
- [ ] `kpot config show` confirms `keychain` is `auto` (or `always`)
  to enable the bootstrap shortcut. `keychain = "never"` means the
  web form is the only way in.
- [ ] After `--idle` minutes of inactivity, the next phone request
  bounces to the lock screen.
- [ ] Logging out clears the cookie and the session.

## Out of scope

- Auto-fill, multi-vault, RW from web — see the threat-model rationale
  in the corresponding `docs/security.md` and the v0.9 plan file.
- `--bind 0.0.0.0` — refused on principle; the daemon is loopback-only.
- TLS — unnecessary on loopback; the SSH tunnel is the transport
  encryption.
