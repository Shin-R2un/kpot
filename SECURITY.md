# Security Policy

## Reporting a vulnerability

kpot stores secrets, so security reports are taken seriously.
**Please do not file public GitHub issues** for security problems —
disclose privately first.

### Preferred: GitHub Security Advisories

1. Open <https://github.com/Shin-R2un/kpot/security/advisories/new>
2. Provide:
   - The kpot version (`kpot version`)
   - Your OS / arch / Go version (if relevant)
   - Reproduction steps or PoC
   - Your assessment of the impact
3. We'll respond within **7 days** with an acknowledgement and an
   initial triage assessment.

### Backup channel

If GitHub Security Advisories is unavailable to you, email
**shin@r2un.com** with the subject prefix `[kpot security]`.
PGP key fingerprints will be published here once available.

## Disclosure timeline

- **Day 0**: Report received, acknowledgement within 7 days.
- **Day 0–14**: Triage. Severity assessment (CVSS or qualitative).
- **Day 14–60**: Fix developed and tested in a private branch.
  Coordinated disclosure date agreed with the reporter.
- **Day 60–90**: Public release of the fix in a tagged version.
  Advisory and CVE (if applicable) published.
- **Beyond 90 days**: For complex issues, the timeline may be
  extended by mutual agreement. We will not pre-emptively publish
  before the reporter is ready.

If we cannot reach the reporter for **30 days**, we may proceed with
disclosure independently after a 90-day total embargo.

## Scope

Reports are in scope if they affect:

- The `kpot` binary or any package under `internal/`
- The vault file format (`docs/format.md`)
- The bundle file format (`.kpb`)
- The recovery seed encoding
- The OS keychain integration
- The release pipeline (`.goreleaser.yml`, `.github/workflows/`)
- The install scripts (`install.sh`, `install.ps1`)

Out of scope:

- Issues that require root/admin on the user's machine
  (kpot does not defend against host compromise — see
  [docs/security.md](docs/security.md))
- Memory-dump attacks against a running process
  (Go's runtime makes complete mitigation impractical;
  we use best-effort zeroing)
- Reports against third-party dependencies that are already
  publicly tracked (file those upstream)
- Social engineering / phishing scenarios

## Hall of fame

Valid reports will be credited (with the reporter's permission) in
the release notes of the version that fixes them and in this
section after responsible disclosure.

_(empty — be the first)_

## What is kpot's threat model?

See [docs/security.md](docs/security.md) for the full threat model
— what kpot defends against and what it explicitly does not.
Reading it before reporting helps both sides:

- It clarifies whether your finding is in scope.
- It avoids re-reporting known design limitations.
