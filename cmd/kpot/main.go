package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/r2un/kpot/internal/config"
	"github.com/r2un/kpot/internal/crypto"
	"github.com/r2un/kpot/internal/editor"
	"github.com/r2un/kpot/internal/recovery"
	"github.com/r2un/kpot/internal/repl"
	"github.com/r2un/kpot/internal/store"
	"github.com/r2un/kpot/internal/tty"
	"github.com/r2un/kpot/internal/vault"
)

const usage = `kpot - encrypted CLI note vault

Usage:
  kpot init <file> [--recovery seed|key] [--recovery-words 12|24]
                               Create a new encrypted vault. Always issues a
                               recovery key (default: BIP-39 12-word seed).
  kpot <file>                  Open a vault and enter the REPL
  kpot <file> --recover        Open a vault using its recovery key
  kpot <file> <command> ...    Run a single command without entering the REPL
  kpot help                    Show this help
  kpot version                 Show the version

Single-shot commands (mirror the REPL):
  ls
  read <name>
  note <name>                  (opens $EDITOR)
  copy <name>
  find <query...>
  rm [-y] <name>
  template [show|reset]
  passphrase                   rotate this vault's passphrase (recovery preserved)
  recovery-info                show recovery type (no secrets, no params)
  export [-o path] [--force]   print decrypted JSON to stdout (or write to a file)
  import <json> [--mode merge|replace] [-y]

Environment:
  KPOT_PASSPHRASE              if set, used in place of the TTY prompt
                               (one-time stderr warning)

Config file:
  ~/.config/kpot/config.toml   optional. Supported keys:
                                 editor                  (overrides $EDITOR)
                                 clipboard_clear_seconds (default: 30)

Recovery model:
  Every vault created with v0.3+ comes with a recovery key (seed phrase
  or secret key) shown ONCE at init time. There is NO way to reissue
  it. Lose the recovery key AND the passphrase → the vault is
  unrecoverable. Vaults created with v0.1/v0.2 (no recovery) keep
  working as-is, but adding recovery to them is not supported.

Examples:
  kpot init personal.kpot
  kpot init personal.kpot --recovery key
  kpot personal.kpot
  kpot personal.kpot --recover
  kpot personal.kpot ls
  kpot personal.kpot read ai/openai
  KPOT_PASSPHRASE=secret kpot personal.kpot copy ai/openai
`

const version = "0.3.0-dev"

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

	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	case "version", "-v", "--version":
		fmt.Println(version)
		return nil
	case "init":
		return cmdInit(args[1:])
	default:
		path := args[0]
		rest := args[1:]
		// `kpot <file> --recover` opens via recovery key.
		if len(rest) == 1 && rest[0] == "--recover" {
			return cmdOpenWithRecovery(path, cfg)
		}
		if len(rest) == 0 {
			return cmdOpen(path, cfg)
		}
		return cmdOneShot(path, rest[0], rest[1:], cfg)
	}
}

// cmdInit creates a new vault. v0.3+ flow: passphrase + always-on
// recovery key (seed by default, or secret key with --recovery key).
func cmdInit(args []string) error {
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

// cmdOpen opens path and enters the REPL.
func cmdOpen(path string, cfg config.Config) error {
	sess, err := openSession(path, cfg)
	if err != nil {
		return err
	}
	defer sess.Close()
	return sess.Run()
}

// cmdOpenWithRecovery opens path using a recovery key (seed or secret).
// The user is prompted for the recovery secret directly — no env-var
// bypass for this path; it's an emergency-only flow.
func cmdOpenWithRecovery(path string, cfg config.Config) error {
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
	defer crypto.Zero(plaintext)

	v, err := store.FromJSON(plaintext)
	if err != nil {
		crypto.Zero(dek)
		return err
	}
	sess := repl.NewSessionWith(path, v, dek, hdr, repl.SessionOptions{
		ClipboardTTL: cfg.ClipboardTTL(),
	})
	defer sess.Close()

	fmt.Fprintln(os.Stderr, "⚠️  Opened via recovery key. Set a new passphrase now: kpot:...> passphrase")
	return sess.Run()
}

// cmdOneShot opens path, runs a single REPL command, and exits.
func cmdOneShot(path, sub string, args []string, cfg config.Config) error {
	// recovery-info doesn't need to open the vault — it only reads the
	// header. Handle it before we ask for a passphrase.
	if sub == "recovery-info" {
		return cmdRecoveryInfo(path)
	}
	sess, err := openSession(path, cfg)
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

// openSession reads the passphrase (TTY or KPOT_PASSPHRASE env),
// decrypts the vault, and returns a wired-up Session honoring config
// overrides. Works for both v1 and v2 vaults via vault.Open dispatch.
func openSession(path string, cfg config.Config) (*repl.Session, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("vault file %q not found. Use 'kpot init %s' to create it", path, path)
		}
		return nil, err
	}

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
	defer crypto.Zero(plaintext)

	v, err := store.FromJSON(plaintext)
	if err != nil {
		crypto.Zero(key)
		return nil, err
	}
	return repl.NewSessionWith(path, v, key, hdr, repl.SessionOptions{
		ClipboardTTL: cfg.ClipboardTTL(),
	}), nil
}

type argsError struct{ msg string }

func (e *argsError) Error() string { return e.msg }
func errArgs(msg string) error    { return &argsError{msg: msg} }

type authError struct{ msg string }

func (e *authError) Error() string { return e.msg }
func errAuth(msg string) error    { return &authError{msg: msg} }

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
