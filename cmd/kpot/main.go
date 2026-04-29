package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Shin-R2un/kpot/internal/config"
	"github.com/Shin-R2un/kpot/internal/crypto"
	"github.com/Shin-R2un/kpot/internal/editor"
	"github.com/Shin-R2un/kpot/internal/keychain"
	"github.com/Shin-R2un/kpot/internal/recovery"
	"github.com/Shin-R2un/kpot/internal/repl"
	"github.com/Shin-R2un/kpot/internal/serve"
	"github.com/Shin-R2un/kpot/internal/store"
	"github.com/Shin-R2un/kpot/internal/tty"
	"github.com/Shin-R2un/kpot/internal/vault"
)

const usage = `kpot - encrypted CLI note vault

Usage:
  kpot                         Open the default_vault (config) and enter the REPL.
                               Prints this help when no default_vault is set.
  kpot init <name|file> [--recovery seed|key] [--recovery-words 12|24]
                               Create a new encrypted vault. Always issues a
                               recovery key (default: BIP-39 12-word seed).
                               Bare names resolve under <vault_dir>/<name>.kpot.
  kpot <name|file>             Open a vault and enter the REPL. Bare names
                               resolve under <vault_dir> (default ~/.kpot)
                               with .kpot appended automatically.
  kpot <name|file> --recover   Open a vault using its recovery key
  kpot <name|file> --no-cache  Open without consulting the OS keychain cache
  kpot <name|file> --forget    Remove the cached key and exit (or run a follow-up command without using the cache)
  kpot <name|file> <command> ...
                               Run a single command without entering the REPL
  kpot keychain test           Diagnose the OS keychain backend
  kpot config init [--force]   Write a starter config.toml at the OS-default path
  kpot config show             Print the effective configuration (file + defaults)
  kpot config path             Print the OS-default config-file path
  kpot serve <name|file> [--bind ADDR] [--port 8765] [--idle 30] [--no-cache]
                               Read-only WebUI for phone access. Default binds
                               127.0.0.1 (use SSH tunnel). --bind <vpn-iface-IP>
                               for direct VPN access; warns at startup. See
                               docs/serve.md.
  kpot help                    Show this help
  kpot version                 Show the version

Single-shot commands (mirror the REPL):
  ls
  show [<arg>]                 print body of <arg> (note name) — alias for read
  read <name>                  print body to stdout
  note <name>                  (opens $EDITOR)
  cp [<arg>]                   clipboard counterpart of show — alias for copy
  copy <name>                  copy a note's body to the clipboard
  find <query...>
  rm [-y] <name>
  template [show|reset]
  passphrase                   rotate this vault's passphrase (recovery preserved)
  recovery-info                show recovery type (no secrets, no params)
  export [-o path] [--force]   print decrypted JSON to stdout (or write to a file)
  import <json> [--mode merge|replace] [-y]
  bundle <name>... -o <path> [--force]
                               encrypt selected notes into a portable .kpb file
  import-bundle <path> [-y]    decrypt a .kpb (asks for source passphrase) and merge

REPL-only commands (require an interactive session):
  cd <note> | cd .. | cd /     enter / leave a note context
  pwd                          print current note context
  fields                       list fields of the current note
  set <field> [<value>]        update field; secret fields force a TTY prompt
  unset <field>                remove a field from the current note

Environment:
  KPOT_PASSPHRASE              if set, used in place of the TTY prompt
                               (one-time stderr warning)

Config file:
  ~/.config/kpot/config.toml   optional. Supported keys:
                                 editor                  (overrides $EDITOR)
                                 clipboard_clear_seconds (default: 30)
                                 keychain                ("auto" | "always" | "never", default: auto)
                                 idle_lock_minutes       (REPL idle close, default: 10)
                                 vault_dir               (where bare-name args resolve, default: ~/.kpot)
                                 default_vault           (opened by bare 'kpot' with no args)

Recovery model:
  Every vault created with v0.3+ comes with a recovery key (seed phrase
  or secret key) shown ONCE at init time. There is NO way to reissue
  it. Lose the recovery key AND the passphrase → the vault is
  unrecoverable. Vaults created with v0.1/v0.2 (no recovery) keep
  working as-is, but adding recovery to them is not supported.

Examples:
  # Bare-name workflow (v0.7+, default vault_dir = ~/.kpot):
  kpot init personal              # → ~/.kpot/personal.kpot (mkdir + chmod 0700)
  kpot                            # default_vault REPL (set in config.toml)
  kpot personal                   # → ~/.kpot/personal.kpot REPL
  kpot personal read ai/openai    # → single-shot read

  # Path workflow (back-compat, still fully supported):
  kpot init ~/secrets/work.kpot
  kpot ~/secrets/work.kpot
  kpot ~/secrets/work.kpot --recover
  KPOT_PASSPHRASE=secret kpot ~/secrets/work.kpot copy ai/openai

  # Config bootstrapping:
  kpot config init                 # write a commented starter config.toml
  $EDITOR $(kpot config path)      # edit it
  kpot config show                 # verify the effective values
`

// version is the released build version. Overridden at link time by
// goreleaser via -ldflags "-X main.version=...". Unreleased builds keep
// the in-tree placeholder so `kpot version` still prints something useful.
var version = "0.8.1-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(exitCodeFor(err))
	}
}

func run(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	editor.Default = cfg.Editor

	// Bare `kpot` with no args: open the configured default vault if
	// one is set, otherwise print usage. This makes `kpot` a one-key
	// shortcut for the user's primary vault when default_vault is in
	// config.toml.
	if len(args) == 0 {
		if cfg.DefaultVault == "" {
			fmt.Print(usage)
			return nil
		}
		path, err := config.ResolveVault("", cfg)
		if err != nil {
			return err
		}
		return cmdOpen(path, cfg, false)
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	case "version", "-v", "--version":
		fmt.Println(version)
		return nil
	case "init":
		return cmdInit(args[1:], cfg)
	case "keychain":
		return cmdKeychain(args[1:], cfg)
	case "config":
		return cmdConfig(args[1:], cfg)
	case "serve":
		return cmdServe(args[1:], cfg)
	default:
		// Resolve a bare name like `personal` to <vault_dir>/personal.kpot
		// (or to a CWD file if one exists by that name). Path-like
		// inputs (`./vault.kpot`, `/abs/v.kpot`) pass through unchanged
		// for back-compat.
		path, err := config.ResolveVault(args[0], cfg)
		if err != nil {
			return err
		}
		rest := args[1:]
		// Consume leading flags that apply to the whole invocation
		// regardless of whether REPL or single-shot follows.
		var (
			recover bool
			noCache bool
			forget  bool
		)
		for len(rest) > 0 {
			switch rest[0] {
			case "--recover":
				recover = true
			case "--no-cache":
				noCache = true
			case "--forget":
				forget = true
			default:
				goto done
			}
			rest = rest[1:]
		}
	done:
		if recover && (noCache || forget) {
			return errArgs("--recover cannot be combined with --no-cache or --forget")
		}
		if recover {
			return cmdOpenWithRecovery(path, cfg, rest)
		}
		if forget {
			if err := forgetCachedKey(path); err != nil {
				return err
			}
			noCache = true // also skip Set if a subcommand follows
			if len(rest) == 0 {
				fmt.Fprintf(os.Stderr, "forgot cached key for %s\n", path)
				return nil
			}
		}
		if len(rest) == 0 {
			return cmdOpen(path, cfg, noCache)
		}
		return cmdOneShot(path, rest[0], rest[1:], cfg, noCache)
	}
}

// cmdKeychain dispatches the few keychain-management subcommands that
// don't require opening a vault.
func cmdKeychain(args []string, cfg config.Config) error {
	if len(args) == 0 {
		return errArgs("usage: kpot keychain test")
	}
	switch args[0] {
	case "test":
		return cmdKeychainTest(cfg)
	default:
		return errArgs(fmt.Sprintf("unknown keychain subcommand: %s", args[0]))
	}
}

// cmdConfig dispatches `kpot config <sub>`:
//
//	init [--force]   write a starter config.toml at the OS-default
//	                 location. Refuses if the file exists unless
//	                 --force is passed (back-compat with operations
//	                 the user has memorized in the existing file).
//	show             print the effective configuration as TOML on
//	                 stdout. Useful for "what does kpot think the
//	                 settings are right now" debugging — shows
//	                 file-loaded values plus defaults that fill in
//	                 absent keys.
//	path             print the OS-default config path. Stays one
//	                 line so it works in shell substitution like
//	                 `$EDITOR $(kpot config path)`.
func cmdConfig(args []string, cfg config.Config) error {
	if len(args) == 0 {
		return errArgs("usage: kpot config (init [--force] | show | path)")
	}
	switch args[0] {
	case "init":
		return cmdConfigInit(args[1:])
	case "show":
		return cmdConfigShow(cfg)
	case "path":
		return cmdConfigPath()
	default:
		return errArgs(fmt.Sprintf("unknown config subcommand: %s", args[0]))
	}
}

func cmdConfigPath() error {
	p, err := config.DefaultPath()
	if err != nil {
		return err
	}
	fmt.Println(p)
	return nil
}

func cmdConfigInit(args []string) error {
	force := false
	for _, a := range args {
		switch a {
		case "-f", "--force":
			force = true
		default:
			return errArgs(fmt.Sprintf("unknown flag: %s", a))
		}
	}

	path, err := config.DefaultPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists. Use --force to overwrite", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// Create parent dir owner-only — the file may eventually hold
	// per-vault paths or aliases that you'd rather not leak to other
	// local users. Same posture as the vault dir.
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.Chmod(parent, 0o700); err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf("set config dir permissions: %w", err)
	}

	if err := os.WriteFile(path, []byte(config.StarterTemplate), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "✓ wrote %s\n", path)
	fmt.Fprintf(os.Stderr, "  Edit it with: $EDITOR %s\n", path)
	return nil
}

// cmdConfigShow prints the *effective* configuration — the values
// that would influence runtime behavior right now, after defaults
// have been applied. Format is TOML so the output is round-trippable
// (paste back into config.toml and you'd get the same effective
// config). Empty Editor is rendered as a comment so users can see
// "this falls back to $EDITOR" without it looking like a bug.
func cmdConfigShow(cfg config.Config) error {
	path, err := config.DefaultPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "# config file: %s (does not exist — using all defaults)\n", path)
	} else {
		fmt.Fprintf(os.Stderr, "# config file: %s\n", path)
	}
	fmt.Fprintln(os.Stderr, "# values below include defaults applied for absent keys")
	fmt.Fprintln(os.Stderr)

	idle := cfg.IdleLockMinutes
	if idle == 0 {
		idle = config.DefaultIdleLockMinutes
	}
	clip := cfg.ClipboardClearSeconds
	if clip == 0 {
		clip = 30 // matches clipboard.NewManager default
	}
	keychain := cfg.KeychainMode()
	vaultDir := cfg.VaultDir
	vaultDirNote := ""
	if vaultDir == "" {
		if d, err := config.DefaultVaultDir(); err == nil {
			vaultDir = d
			vaultDirNote = "  # default (vault_dir unset)"
		}
	}

	if cfg.Editor == "" {
		fmt.Println(`# editor unset — falls back to $EDITOR / $VISUAL / built-ins`)
	} else {
		fmt.Printf("editor = %q\n", cfg.Editor)
	}
	fmt.Printf("clipboard_clear_seconds = %d\n", clip)
	fmt.Printf("keychain = %q\n", keychain)
	fmt.Printf("idle_lock_minutes = %d\n", idle)
	fmt.Printf("vault_dir = %q%s\n", vaultDir, vaultDirNote)
	if cfg.DefaultVault == "" {
		fmt.Println(`# default_vault unset — bare 'kpot' prints usage`)
	} else {
		fmt.Printf("default_vault = %q\n", cfg.DefaultVault)
	}
	return nil
}

// cmdServe starts the read-only WebUI for a vault. Usage:
//
//	kpot serve <name|file> [--port 8765] [--idle 30] [--no-cache]
//
// The daemon binds 127.0.0.1 only — there's no `--bind` flag on
// purpose. Phone access is meant to go through an SSH tunnel; binding
// to 0.0.0.0 would expose plaintext HTTP to the LAN, contradicting
// docs/security.md's threat model.
func cmdServe(args []string, cfg config.Config) error {
	var (
		path     string
		bindAddr string
		port     = 8765
		idleMin  = 30
		noCache  = false
	)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--bind":
			if i+1 >= len(args) {
				return errArgs("--bind requires a host or IP")
			}
			bindAddr = args[i+1]
			i++
		case a == "--port":
			if i+1 >= len(args) {
				return errArgs("--port requires a number")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 1 || n > 65535 {
				return errArgs(fmt.Sprintf("--port: %q is not a valid port", args[i+1]))
			}
			port = n
			i++
		case a == "--idle":
			if i+1 >= len(args) {
				return errArgs("--idle requires minutes (0 to disable)")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 0 {
				return errArgs(fmt.Sprintf("--idle: %q must be >= 0", args[i+1]))
			}
			idleMin = n
			i++
		case a == "--no-cache":
			noCache = true
		case strings.HasPrefix(a, "-"):
			return errArgs(fmt.Sprintf("unknown flag: %s", a))
		default:
			if path != "" {
				return errArgs("at most one vault path is allowed")
			}
			path = a
		}
	}
	if path == "" {
		return errArgs("usage: kpot serve <name|file> [--bind ADDR] [--port N] [--idle M] [--no-cache]")
	}
	resolved, err := config.ResolveVault(path, cfg)
	if err != nil {
		return err
	}
	if _, err := os.Stat(resolved); err != nil {
		return fmt.Errorf("vault file %q not found. Use 'kpot init %s' to create it", resolved, resolved)
	}
	idle := time.Duration(idleMin) * time.Minute
	if idleMin == 0 {
		// Sentinel: pass -1 to serve.Run so the session-level timer is
		// disabled. Run translates 0 to "use default 30 min".
		idle = -1
	}
	return serve.Run(serve.Options{
		VaultPath: resolved,
		BindAddr:  bindAddr,
		Port:      port,
		Idle:      idle,
		NoCache:   noCache,
		Cfg:       cfg,
	})
}

func cmdKeychainTest(cfg config.Config) error {
	mode := cfg.KeychainMode()
	kc := keychain.Default()
	fmt.Fprintf(os.Stdout, "backend: %s\n", kc.Name())
	fmt.Fprintf(os.Stdout, "available: %v\n", kc.Available())
	fmt.Fprintf(os.Stdout, "config mode: %s\n", mode)
	if !kc.Available() {
		switch runtime.GOOS {
		case "linux":
			fmt.Fprintln(os.Stdout, "hint: install libsecret-tools (apt install libsecret-tools / dnf install libsecret) and ensure DBUS_SESSION_BUS_ADDRESS is set")
		case "darwin":
			fmt.Fprintln(os.Stdout, "hint: /usr/bin/security should be present on every macOS install")
		case "windows":
			fmt.Fprintln(os.Stdout, "hint: advapi32.dll missing or unreadable")
		}
	}
	return nil
}

// forgetCachedKey removes any cached entry for path. Quiet on success
// or "nothing was cached"; only errors on a real backend failure.
func forgetCachedKey(path string) error {
	kc := keychain.Default()
	if !kc.Available() {
		return nil
	}
	account := keychain.CanonicalAccount(path)
	if err := kc.Delete(account); err != nil && !errors.Is(err, keychain.ErrNotFound) {
		return fmt.Errorf("forget keychain entry: %w", err)
	}
	return nil
}

// cmdInit creates a new vault. v0.3+ flow: passphrase + always-on
// recovery key (seed by default, or secret key with --recovery key).
func cmdInit(args []string, cfg config.Config) error {
	var (
		path         string
		recoveryKind = vault.WrapKindSeed
		seedWords    = 12
	)
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--recovery":
			if i+1 >= len(args) {
				return errArgs("--recovery requires seed | key")
			}
			t, err := recovery.ParseType(args[i+1])
			if err != nil {
				return err
			}
			switch t {
			case recovery.TypeSeedBIP39:
				recoveryKind = vault.WrapKindSeed
			case recovery.TypeSecretKey:
				recoveryKind = vault.WrapKindSecretKey
			}
			i++
		case a == "--recovery-words":
			if i+1 >= len(args) {
				return errArgs("--recovery-words requires 12 or 24")
			}
			switch args[i+1] {
			case "12":
				seedWords = 12
			case "24":
				seedWords = 24
			default:
				return errArgs("--recovery-words must be 12 or 24")
			}
			i++
		case strings.HasPrefix(a, "-"):
			return errArgs(fmt.Sprintf("unknown flag: %s", a))
		default:
			if path != "" {
				return errArgs("usage: kpot init <file> [flags]")
			}
			path = a
		}
	}
	if path == "" {
		return errArgs("usage: kpot init <file> [--recovery seed|key] [--recovery-words 12|24]")
	}
	// Apply the same name-resolution rules as `kpot <vault>`: a bare
	// name like `personal` becomes `<vault_dir>/personal.kpot` so init
	// behaves consistently with subsequent open/single-shot calls.
	resolved, err := config.ResolveVault(path, cfg)
	if err != nil {
		return err
	}
	path = resolved
	// Ensure the parent directory exists so first-time use doesn't
	// fail with "no such file or directory" — common case for
	// resolved `~/.kpot/foo.kpot` when the user has never used kpot
	// before. 0o700 keeps the dir owner-only.
	if err := config.EnsureVaultDir(path); err != nil {
		return fmt.Errorf("create vault dir: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists. Refusing to overwrite", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	pass, err := tty.ReadNewPassphrase("New passphrase: ", "Repeat passphrase: ")
	if err != nil {
		return err
	}
	defer crypto.Zero(pass)

	// Generate the recovery secret + KEK.
	recoveryDisplay, recoveryKEK, err := generateRecovery(recoveryKind, seedWords)
	if err != nil {
		return err
	}
	defer crypto.Zero(recoveryKEK)

	v := store.New()
	plaintext, err := v.ToJSON()
	if err != nil {
		return err
	}
	defer crypto.Zero(plaintext)

	dek, _, err := vault.CreateV2WithRecovery(path, pass, recoveryKind, recoveryKEK, plaintext)
	if err != nil {
		return err
	}
	defer crypto.Zero(dek)

	// Display the recovery secret on /dev/tty, wait for ENTER, clear screen.
	header := "⚠️  RECOVERY KEY — write this down NOW. It is the ONLY way to\n" +
		"    recover this vault if you forget your passphrase. It will NOT\n" +
		"    be shown again, and there is no way to reissue it."
	body := recoveryDisplay
	if recoveryKind == vault.WrapKindSeed {
		body = tty.FormatSeedWords(recoveryDisplay)
	}
	if err := tty.DisplayRecoveryOnce(header, body); err != nil {
		// At this point the vault file already exists with valid wraps,
		// but we couldn't show the recovery to the user. Refuse to leave
		// them with a half-secured vault: remove the file and error out.
		_ = os.Remove(path)
		_ = os.Remove(path + ".bak")
		return fmt.Errorf("recovery display failed (vault rolled back): %w", err)
	}

	fmt.Fprintf(os.Stdout, "Created %s\n", path)
	return nil
}

func generateRecovery(kind string, seedWords int) (display string, kek []byte, err error) {
	switch kind {
	case vault.WrapKindSeed:
		mnemonic, err := recovery.GenerateSeed(seedWords)
		if err != nil {
			return "", nil, err
		}
		kek, err := recovery.SeedToKEK(mnemonic)
		if err != nil {
			return "", nil, err
		}
		return mnemonic, kek, nil
	case vault.WrapKindSecretKey:
		display, raw, err := recovery.GenerateSecretKey()
		if err != nil {
			return "", nil, err
		}
		defer crypto.Zero(raw)
		kek, err := recovery.SecretKeyToKEK(raw)
		if err != nil {
			return "", nil, err
		}
		return display, kek, nil
	default:
		return "", nil, fmt.Errorf("unsupported recovery kind: %s", kind)
	}
}

// cmdOpen opens path and enters the REPL. noCache skips both the
// keychain lookup and the post-open caching prompt for this run.
func cmdOpen(path string, cfg config.Config, noCache bool) error {
	sess, err := openSession(path, cfg, noCache)
	if err != nil {
		return err
	}
	defer sess.Close()
	return sess.Run()
}

// cmdOpenWithRecovery opens path using a recovery key (seed or secret).
// The user is prompted for the recovery secret directly — no env-var
// bypass for this path; it's an emergency-only flow. If subcmd is
// non-empty, runs that single command and exits; otherwise enters REPL.
//
// Recovery flow never touches the keychain cache — recovery is rare,
// and silently caching a key obtained via "I forgot my passphrase"
// flow would be surprising.
func cmdOpenWithRecovery(path string, cfg config.Config, subcmd []string) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("vault file %q not found", path)
		}
		return err
	}

	// Sniff what kind of recovery the vault expects so we can phrase
	// the prompt correctly. recovery-info also calls this.
	hdr, err := vault.PeekHeader(path)
	if err != nil {
		return err
	}
	if hdr.Version != 2 || hdr.RecoveryWrap == nil {
		return fmt.Errorf("vault %q has no recovery key (created before v0.3?). Use the passphrase: kpot %s", path, path)
	}

	var kek []byte
	switch hdr.RecoveryWrap.Kind {
	case vault.WrapKindSeed:
		mnemonic, err := tty.ReadLineSecret("Recovery seed (space-separated words): ")
		if err != nil {
			return err
		}
		defer crypto.Zero(mnemonic)
		// SeedToKEK takes string because BIP-39 validation requires it;
		// the conversion produces a string copy that lives until GC.
		// We still zero `mnemonic` so the user-typed bytes don't linger
		// any longer than necessary.
		kek, err = recovery.SeedToKEK(string(mnemonic))
		if err != nil {
			return err
		}
	case vault.WrapKindSecretKey:
		raw, err := tty.ReadLineSecret("Recovery secret key: ")
		if err != nil {
			return err
		}
		defer crypto.Zero(raw)
		key, err := recovery.ParseSecretKey(string(raw))
		if err != nil {
			return err
		}
		defer crypto.Zero(key)
		kek, err = recovery.SecretKeyToKEK(key)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported recovery kind in vault: %s", hdr.RecoveryWrap.Kind)
	}
	defer crypto.Zero(kek)

	plaintext, dek, hdr, err := vault.OpenWithRecovery(path, kek)
	if err != nil {
		if errors.Is(err, crypto.ErrAuthFailed) {
			return errAuth("Wrong passphrase, or the file is corrupted")
		}
		return err
	}
	sess, err := buildSession(path, plaintext, dek, hdr, cfg)
	if err != nil {
		return err
	}
	defer sess.Close()

	fmt.Fprintln(os.Stderr, "⚠️  Opened via recovery key. Run the `passphrase` command to set a new everyday passphrase.")
	if len(subcmd) == 0 {
		return sess.Run()
	}
	if _, err := sess.Exec(subcmd[0], subcmd[1:]); err != nil {
		return err
	}
	return nil
}

// cmdOneShot opens path, runs a single REPL command, and exits.
func cmdOneShot(path, sub string, args []string, cfg config.Config, noCache bool) error {
	// recovery-info doesn't need to open the vault — it only reads the
	// header. Handle it before we ask for a passphrase.
	if sub == "recovery-info" {
		return cmdRecoveryInfo(path)
	}
	sess, err := openSession(path, cfg, noCache)
	if err != nil {
		return err
	}
	defer sess.Close()
	if _, err := sess.Exec(sub, args); err != nil {
		return err
	}
	return nil
}

func cmdRecoveryInfo(path string) error {
	hdr, err := vault.PeekHeader(path)
	if err != nil {
		return err
	}
	if hdr.Version == 1 || hdr.RecoveryWrap == nil {
		fmt.Println("Recovery: none (vault created before v0.3 or without recovery)")
		return nil
	}
	fmt.Printf("Recovery: enabled (type: %s)\n", hdr.RecoveryWrap.Kind)
	fmt.Println("Note: recovery cannot be reissued. Lose the recovery key and the recovery option is permanently lost.")
	return nil
}

// openSession unlocks the vault and returns a wired-up Session.
//
// Unlock order:
//  1. If keychain is enabled (cfg.Keychain != "never", noCache == false,
//     KPOT_PASSPHRASE not set, backend Available) → try cached key.
//  2. On miss / unavailable → prompt passphrase, derive, open.
//  3. On success after step 2 → optionally cache the open key.
//
// Caching policy by mode:
//   - "auto"   : prompt "[Y/n]" when running interactively. Skip in
//     non-interactive runs or when KPOT_PASSPHRASE is set.
//   - "always" : cache silently when the backend is available.
//   - "never"  : never read or write the keychain.
func openSession(path string, cfg config.Config, noCache bool) (*repl.Session, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("vault file %q not found. Use 'kpot init %s' to create it", path, path)
		}
		return nil, err
	}

	mode := cfg.KeychainMode()
	// LookupEnv (not Getenv) so KPOT_PASSPHRASE="" is treated the same
	// way tty.ReadPassphrase treats it: env-set means env-driven, even
	// if the value happens to be empty. Using "!= ""\" here would
	// silently disagree with the tty package and re-prompt instead.
	_, envBypass := os.LookupEnv(tty.PassphraseEnv)
	useCache := !noCache && !envBypass && mode != config.KeychainNever
	account := keychain.CanonicalAccount(path)

	// Step 1: try the cached key.
	if useCache {
		if cachedKey, err := tryKeychainOpen(account); err == nil {
			defer crypto.Zero(cachedKey)
			plaintext, hdr, err := vault.OpenWithKey(path, cachedKey)
			if err == nil {
				key := append([]byte(nil), cachedKey...) // session takes ownership
				return buildSession(path, plaintext, key, hdr, cfg)
			}
			// Cached key didn't work (vault was rotated externally,
			// say): drop it and fall through to passphrase prompt.
			fmt.Fprintln(os.Stderr, "note: cached key rejected; clearing and re-prompting")
			_ = forgetCachedKey(path)
		}
	}

	// Step 2: passphrase prompt → derive → open.
	pass, err := tty.ReadPassphrase("Passphrase: ")
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(pass)

	plaintext, key, hdr, err := vault.Open(path, pass)
	if err != nil {
		if errors.Is(err, crypto.ErrAuthFailed) {
			return nil, errAuth("Wrong passphrase, or the file is corrupted")
		}
		return nil, err
	}

	// Step 3: cache the freshly-derived key per policy.
	if useCache {
		maybeCacheKey(account, key, mode)
	}

	return buildSession(path, plaintext, key, hdr, cfg)
}

// buildSession finalises the Session construction. plaintext is zeroed
// here because store.FromJSON has already consumed it.
func buildSession(path string, plaintext, key []byte, hdr *vault.Header, cfg config.Config) (*repl.Session, error) {
	defer crypto.Zero(plaintext)
	v, err := store.FromJSON(plaintext)
	if err != nil {
		crypto.Zero(key)
		return nil, err
	}
	return repl.NewSessionWith(path, v, key, hdr, repl.SessionOptions{
		ClipboardTTL: cfg.ClipboardTTL(),
		IdleTimeout:  cfg.IdleTimeout(),
		OnRekey: func(prevVersion int) {
			// v2 rekey preserves the DEK, so the cached entry is
			// still valid. Only invalidate after v1 rotations.
			if prevVersion == 1 {
				_ = forgetCachedKey(path)
			}
		},
	}), nil
}

// tryKeychainOpen returns the cached key for account, or an error if
// no key is cached or the backend is unavailable. Nil error → caller
// owns the returned slice and must zero it.
func tryKeychainOpen(account string) ([]byte, error) {
	kc := keychain.Default()
	if !kc.Available() {
		return nil, keychain.ErrUnavailable
	}
	return kc.Get(account)
}

// maybeCacheKey writes key to the keychain per the configured mode.
// Failures are reported to stderr but never propagate — caching is
// best-effort, and a failure here shouldn't block the user from
// using the vault they just successfully unlocked.
func maybeCacheKey(account string, key []byte, mode string) {
	kc := keychain.Default()
	if !kc.Available() {
		if mode == config.KeychainAlways {
			fmt.Fprintf(os.Stderr, "note: keychain unavailable (%s); caching skipped\n", kc.Name())
		}
		return
	}

	want := false
	switch mode {
	case config.KeychainAlways:
		want = true
	case config.KeychainAuto:
		// Only ask interactively. Non-TTY runs (cron, CI) silently
		// skip — they shouldn't be writing to the user's keychain.
		if !tty.IsStdinTTY() {
			return
		}
		ans, err := tty.ReadLine("Cache key in OS keychain so future opens skip the passphrase? [Y/n]: ")
		if err != nil {
			return
		}
		switch strings.ToLower(strings.TrimSpace(ans)) {
		case "", "y", "yes":
			want = true
		}
	}
	if !want {
		return
	}
	if err := kc.Set(account, key); err != nil {
		fmt.Fprintf(os.Stderr, "warning: keychain Set failed: %v\n", err)
	}
}

type argsError struct{ msg string }

func (e *argsError) Error() string { return e.msg }
func errArgs(msg string) error     { return &argsError{msg: msg} }

type authError struct{ msg string }

func (e *authError) Error() string { return e.msg }
func errAuth(msg string) error     { return &authError{msg: msg} }

func exitCodeFor(err error) int {
	var ae *argsError
	if errors.As(err, &ae) {
		return 2
	}
	var auth *authError
	if errors.As(err, &auth) {
		return 3
	}
	if errors.Is(err, os.ErrNotExist) {
		return 4
	}
	return 1
}
