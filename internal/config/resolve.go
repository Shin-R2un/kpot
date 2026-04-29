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
//  3. arg starts with `~/` or is bare `~`: expand to the user's home
//     before any other rule fires. Lets `kpot ~/vaults/work.kpot`
//     work the same way as `vault_dir = "~/..."` in config.
//  4. arg contains a path separator (`/` or `\`): use as-is. The
//     user gave us a path, don't second-guess.
//  5. arg ALREADY ends with `.kpot` AND a file by that name exists
//     in the current working directory: use the CWD path. This
//     preserves the historical `cd /repo && kpot vault.kpot`
//     workflow. Bare names without the suffix DO NOT trigger this
//     branch — that would let a malicious repo ship a
//     `personal.kpot` and shadow the user's real vault.
//  6. Add `.kpot` suffix if missing, then resolve under
//     <effective vault dir>/<candidate>. Default vault dir is
//     ~/.kpot when cfg.VaultDir is empty.
//
// Whitespace around arg is trimmed before any of the above. Returned
// paths are NOT normalized to absolute — relative paths come back
// relative when that's what the resolution produced.
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

	// Tilde expansion BEFORE the path-separator check so `~/foo` is
	// treated as a real path rather than as a bare name. Shell
	// usually expands this for us, but quoted args, scripted args,
	// and Windows shells don't always.
	expanded, err := expandHome(arg)
	if err != nil {
		return "", err
	}
	arg = expanded

	// Path-like input — user knows what they want.
	if strings.ContainsAny(arg, "/\\") {
		return arg, nil
	}

	hadSuffix := strings.HasSuffix(arg, ".kpot")
	candidate := arg
	if !hadSuffix {
		candidate += ".kpot"
	}

	// Back-compat: file in CWD wins ONLY when the user typed the
	// `.kpot` suffix explicitly. A bare name like `personal` skips
	// this check so a malicious repo's `personal.kpot` can't shadow
	// the user's real `~/.kpot/personal.kpot` just because they
	// happen to have `cd`-ed into the repo.
	if hadSuffix {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	dir, err := effectiveVaultDir(cfg)
	if err != nil {
		return "", fmt.Errorf("locate default vault dir: %w", err)
	}
	return filepath.Join(dir, candidate), nil
}

// EnsureVaultDir creates the resolved vault parent directory if it
// doesn't exist AND tightens its permissions to 0o700 if it does.
// Used by `kpot init <name>` so:
//
//  1. The user doesn't get "no such file or directory" the first
//     time they create a vault under the default location.
//  2. A pre-existing `~/.kpot/` (created with a more permissive
//     mode by another tool, or by a shell rc that ran `mkdir -p`
//     under the default umask) is brought back to owner-only
//     access. `os.MkdirAll` is a no-op for existing dirs and only
//     applies its mode arg to newly created components, so the
//     explicit Chmod is the load-bearing step.
//
// Returns nil for an absolute path that's already inside an existing
// directory (parent == ".").
func EnsureVaultDir(vaultPath string) error {
	parent := filepath.Dir(vaultPath)
	if parent == "" || parent == "." {
		return nil
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	// Tighten an existing dir if some other tool created it with a
	// looser mode. Best effort — Windows ignores Unix mode bits and
	// returns nil here, which is fine: NTFS ACLs are the user's
	// responsibility on that platform.
	if err := os.Chmod(parent, 0o700); err != nil {
		return fmt.Errorf("set vault dir permissions: %w", err)
	}
	return nil
}
