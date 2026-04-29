// Package config loads optional user-level defaults from
// ~/.config/kpot/config.toml. Every field is optional — a missing file
// produces a zero-value Config and is not an error.
//
// Precedence rules live in the consuming packages: the editor package
// treats Editor as a tier between $EDITOR/$VISUAL and the built-in
// fallbacks (config wins, env beats config in some Unix tools but here
// config is preferred so a personal preference sticks reliably).
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Keychain mode controls whether kpot caches the per-vault open key
// in the OS-native secret store. Empty == KeychainAuto.
const (
	KeychainAuto   = "auto"   // prompt once per vault on first cache miss
	KeychainAlways = "always" // cache silently after every successful open
	KeychainNever  = "never"  // never read or write the keychain
)

// Config holds the values read from config.toml. New fields must be
// optional (toml: omitempty implied — an absent key just leaves the
// zero value in place).
type Config struct {
	// Editor is preferred over $EDITOR / $VISUAL when set. Empty means
	// "fall back to environment variables / built-in candidates".
	Editor string `toml:"editor"`

	// ClipboardClearSeconds overrides the 30-second auto-clear default.
	// Zero means "use the default". Negative is rejected at load time.
	ClipboardClearSeconds int `toml:"clipboard_clear_seconds"`

	// Keychain controls OS-keychain caching of vault open keys.
	// Valid values: "auto" (default), "always", "never". Validated
	// at load time.
	Keychain string `toml:"keychain"`

	// IdleLockMinutes auto-closes a REPL session after N minutes of
	// no command activity. Zero means "use the default" (10 minutes).
	// Negative is rejected at load time. -1 / "off" semantics: set to
	// a very large number if you really want effectively-never.
	IdleLockMinutes int `toml:"idle_lock_minutes"`

	// VaultDir is the directory bare-name vault arguments are resolved
	// against. `kpot personal` → `<VaultDir>/personal.kpot` when no
	// matching file exists in CWD. Empty falls back to ~/.kpot. The
	// `~/` prefix is expanded at load time so callers see an absolute
	// path. Trailing slashes are tolerated.
	VaultDir string `toml:"vault_dir"`

	// DefaultVault is the vault opened when the user runs bare `kpot`
	// with no positional argument. The value goes through the same
	// resolution as a CLI argument: bare names get `.kpot` appended
	// and resolve under VaultDir; absolute / slash-containing values
	// are used as-is. Empty means "no default" — bare `kpot` prints
	// usage as before.
	DefaultVault string `toml:"default_vault"`
}

// DefaultIdleLockMinutes is the fallback when config doesn't set a
// value. Plan §2.2 specifies 10 minutes.
const DefaultIdleLockMinutes = 10

// IdleTimeout returns the configured idle-lock duration. Zero in the
// config field maps to the default; the caller never sees zero.
func (c Config) IdleTimeout() time.Duration {
	mins := c.IdleLockMinutes
	if mins == 0 {
		mins = DefaultIdleLockMinutes
	}
	return time.Duration(mins) * time.Minute
}

// KeychainMode normalizes Config.Keychain, defaulting empty to
// KeychainAuto. Always returns one of the three KeychainXxx constants.
func (c Config) KeychainMode() string {
	switch c.Keychain {
	case KeychainAlways, KeychainNever:
		return c.Keychain
	default:
		return KeychainAuto
	}
}

// ClipboardTTL returns the configured clipboard auto-clear duration,
// or 0 if unset (caller should treat 0 as "use the package default").
func (c Config) ClipboardTTL() time.Duration {
	if c.ClipboardClearSeconds <= 0 {
		return 0
	}
	return time.Duration(c.ClipboardClearSeconds) * time.Second
}

// DefaultPath is ~/.config/kpot/config.toml on Unix, the platform
// equivalent (via os.UserConfigDir) elsewhere.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kpot", "config.toml"), nil
}

// Load reads DefaultPath. A missing file is treated as "no overrides"
// and returns a zero-value Config with nil error.
func Load() (Config, error) {
	path, err := DefaultPath()
	if err != nil {
		return Config{}, err
	}
	return LoadFrom(path)
}

// LoadFrom reads the given path. Missing file → zero-value Config.
// Used by tests to point at a temp file, and by Load internally.
func LoadFrom(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return Config{}, err
	}
	if cfg.ClipboardClearSeconds < 0 {
		return Config{}, errors.New("config: clipboard_clear_seconds must be >= 0")
	}
	if cfg.IdleLockMinutes < 0 {
		return Config{}, errors.New("config: idle_lock_minutes must be >= 0")
	}
	switch cfg.Keychain {
	case "", KeychainAuto, KeychainAlways, KeychainNever:
		// ok
	default:
		return Config{}, fmt.Errorf("config: keychain must be auto | always | never (got %q)", cfg.Keychain)
	}
	if expanded, err := expandHome(cfg.VaultDir); err != nil {
		return Config{}, fmt.Errorf("config: vault_dir: %w", err)
	} else {
		cfg.VaultDir = expanded
	}
	return cfg, nil
}

// ExpandHome converts a leading `~/` or bare `~` into the user's home
// directory. Other forms (absolute paths, relative paths, empty) pass
// through unchanged. Exported so cmd/kpot can normalize CLI args
// the same way config.toml values are normalized at load time.
func ExpandHome(p string) (string, error) { return expandHome(p) }

// expandHome converts a leading `~/` or bare `~` into the user's home
// directory. Other forms (absolute paths, relative paths, empty) pass
// through unchanged.
func expandHome(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if p == "~" {
			return home, nil
		}
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}
