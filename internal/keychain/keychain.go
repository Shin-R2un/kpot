// Package keychain stores and retrieves the per-vault open key (the
// Argon2id-derived key for v1 vaults, the DEK for v2 vaults) in the
// host OS's native secret store. The aim is to let `kpot <file>`
// skip the passphrase prompt + Argon2id derivation on subsequent
// invocations.
//
// All backends speak to OS-provided facilities (Apple Security
// Services, GNOME Secret Service, Microsoft Credential Manager) via
// either system binaries or syscalls — no third-party Go modules
// are pulled in for this functionality, so the trust surface stays
// at "the OS we're already running on".
//
// Each entry is keyed by (Service, Account) where Service is the
// constant "kpot" and Account is the canonical absolute path of the
// vault file. Symlink-resolved paths are expected from callers so
// the same vault accessed under different names hits the same entry.
package keychain

import (
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
)

// Service is the constant label every kpot entry registers under.
// Distinguishes our entries from anything else the user may store.
const Service = "kpot"

// ErrUnavailable means no usable backend exists in the current
// process environment. Reasons vary per OS: missing system binary
// (libsecret-tools on Linux), no D-Bus session (headless / SSH),
// disabled by config, etc. Callers should treat this as a soft
// "caching off" signal — never as a fatal error.
var ErrUnavailable = errors.New("keychain backend not available on this system")

// ErrNotFound means the requested entry isn't in the store. Distinct
// from ErrUnavailable because the backend itself works; the lookup
// just had no hit.
var ErrNotFound = errors.New("keychain entry not found")

// Backend is the per-OS adapter. Implementations live in macos.go /
// linux.go / windows.go behind build tags; other_unix.go provides a
// no-op for unsupported platforms.
//
// All methods are safe to call concurrently from multiple goroutines.
// Callers are responsible for zeroing any returned secret bytes.
type Backend interface {
	// Available reports whether this backend can actually reach its
	// underlying store right now. Cheap and side-effect-free.
	// Returns false on missing system binaries, no D-Bus session, etc.
	Available() bool

	// Get returns the secret bytes stored under account, or
	// ErrNotFound. Returns ErrUnavailable if the backend is offline.
	Get(account string) ([]byte, error)

	// Set stores secret under account, overwriting any prior value.
	Set(account string, secret []byte) error

	// Delete removes the entry for account. Returns ErrNotFound if no
	// entry existed (callers can usually ignore this).
	Delete(account string) error

	// Name identifies the backend in diagnostic output ("macos-keychain",
	// "linux-secret-tool", "windows-credential-manager", "none").
	Name() string
}

// Default returns the OS-native backend for the current GOOS. The
// returned backend may report Available() == false; callers should
// check before relying on it.
func Default() Backend { return defaultBackend() }

// CanonicalAccount returns the absolute, symlink-resolved form of
// path. Callers should always pipe vault paths through this before
// calling Get / Set / Delete so that two different ways of naming the
// same file hit the same keychain entry.
//
// On error (missing file, broken symlink), it falls back to the
// absolute path without symlink resolution rather than failing the
// caller — caching is a UX optimization, not a correctness gate.
func CanonicalAccount(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// EncodeSecret renders raw key bytes for storage in text-only secret
// stores (security CLI on macOS, secret-tool on Linux pass strings).
// Hex is reversible and unambiguous; the slight size cost vs base64
// is irrelevant for 32-byte keys.
func EncodeSecret(raw []byte) string {
	return hex.EncodeToString(raw)
}

// DecodeSecret reverses EncodeSecret. Returns an error for malformed
// input; callers should treat this as ErrNotFound (the entry exists
// but isn't in our format — likely written by something else).
func DecodeSecret(s string) ([]byte, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode secret: %w", err)
	}
	return b, nil
}
