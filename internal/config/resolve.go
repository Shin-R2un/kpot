package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultVaultDir is the fallback location when Config.VaultDir is
// empty. Picked once and exposed so init / cmd code can both call
// `os.MkdirAll` on the same path without re-resolving HOME.
func DefaultVaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kpot"), nil
}

// effectiveVaultDir returns cfg.VaultDir, or the default ~/.kpot path
// when empty. Errors from HOME lookup are propagated so callers can
// distinguish "no HOME" from "no config".
func effectiveVaultDir(cfg Config) (string, error) {
	if cfg.VaultDir != "" {
		return cfg.VaultDir, nil
	}
	return DefaultVaultDir()
}

// ResolveVault converts a user-supplied vault designator (or empty
// for "use default_vault") into a filesystem path kpot should read
// or write. It does NOT verify the file exists; callers handle that
// based on context (init wants it absent, open wants it present).
//
// Resolution rules:
//
//  1. arg empty + cfg.DefaultVault empty → error (caller prints usage).
//  2. arg empty: use cfg.DefaultVault as the input and continue below.
//  3. arg contains a path separator (/ or \): use as-is. The user
//     gave us a path, don't second-guess.
//  4. Add `.kpot` suffix if missing.
//  5. If the candidate exists in the current working directory, use
//     it (back-compat: `kpot vault.kpot` keeps working when run from
//     the directory holding the file).
//  6. Else: <effective vault dir>/<candidate>. Default vault dir is
//     ~/.kpot when cfg.VaultDir is empty.
//
// Whitespace around arg is trimmed before any of the above. Returned
// paths are NOT normalized to absolute — relative paths come back
// relative when that's what the resolution produced (CWD case).
func ResolveVault(arg string, cfg Config) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		if cfg.DefaultVault == "" {
			return "", errors.New("no vault specified and no default_vault configured")
		}
		arg = strings.TrimSpace(cfg.DefaultVault)
		if arg == "" {
			return "", errors.New("default_vault is set but empty after trim")
		}
	}

	// Path-like input — user knows what they want.
	if strings.ContainsAny(arg, "/\\") {
		return arg, nil
	}

	candidate := arg
	if !strings.HasSuffix(candidate, ".kpot") {
		candidate += ".kpot"
	}

	// Back-compat: file in CWD wins. This keeps the historical
	// `cd /path/to/dir && kpot vault.kpot` workflow intact.
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	dir, err := effectiveVaultDir(cfg)
	if err != nil {
		return "", fmt.Errorf("locate default vault dir: %w", err)
	}
	return filepath.Join(dir, candidate), nil
}

// EnsureVaultDir creates the resolved vault parent directory if it
// doesn't exist. Used by `kpot init <name>` so the user doesn't get
// "no such file or directory" the first time they create a vault
// under the default location.
//
// If the path already has a parent that exists, this is a no-op.
// Returns nil for an absolute path that's already inside an existing
// directory.
func EnsureVaultDir(vaultPath string) error {
	parent := filepath.Dir(vaultPath)
	if parent == "" || parent == "." {
		return nil
	}
	return os.MkdirAll(parent, 0o700)
}
