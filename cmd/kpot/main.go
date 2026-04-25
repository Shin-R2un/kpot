package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/r2un/kpot/internal/config"
	"github.com/r2un/kpot/internal/crypto"
	"github.com/r2un/kpot/internal/editor"
	"github.com/r2un/kpot/internal/repl"
	"github.com/r2un/kpot/internal/store"
	"github.com/r2un/kpot/internal/tty"
	"github.com/r2un/kpot/internal/vault"
)

const usage = `kpot - encrypted CLI note vault

Usage:
  kpot init <file>             Create a new encrypted vault
  kpot <file>                  Open a vault and enter the REPL
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
  passphrase                   rotate this vault's passphrase
  export [-o path] [--force]   print decrypted JSON to stdout (or write to a file)
  import <json> [--mode merge|replace] [-y]

Environment:
  KPOT_PASSPHRASE              if set, used in place of the TTY prompt
                               (one-time stderr warning)

Config file:
  ~/.config/kpot/config.toml   optional. Supported keys:
                                 editor                  (overrides $EDITOR)
                                 clipboard_clear_seconds (default: 30)

Examples:
  kpot init personal.kpot
  kpot personal.kpot
  kpot personal.kpot ls
  kpot personal.kpot read ai/openai
  KPOT_PASSPHRASE=secret kpot personal.kpot copy ai/openai
`

const version = "0.2.0-dev"

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
		if len(args) != 2 {
			return errArgs("usage: kpot init <file>")
		}
		return cmdInit(args[1])
	default:
		path := args[0]
		if len(args) == 1 {
			return cmdOpen(path, cfg)
		}
		return cmdOneShot(path, args[1], args[2:], cfg)
	}
}

func cmdInit(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists. Refusing to overwrite", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	pass, err := tty.ReadNewPassphrase(
		"New passphrase: ",
		"Repeat passphrase: ",
	)
	if err != nil {
		return err
	}
	defer crypto.Zero(pass)

	v := store.New()
	plaintext, err := v.ToJSON()
	if err != nil {
		return err
	}
	defer crypto.Zero(plaintext)

	key, _, err := vault.Create(path, pass, plaintext)
	if err != nil {
		return err
	}
	defer crypto.Zero(key)

	fmt.Fprintf(os.Stdout, "Created %s\n", path)
	fmt.Fprintln(os.Stdout, "Keep this passphrase safe — there is no recovery if you lose it.")
	return nil
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

// cmdOneShot opens path, runs a single REPL command, and exits.
// Persistent commands (note / rm / template / passphrase / import) save
// inside their handler; we don't need to do anything extra here.
func cmdOneShot(path, sub string, args []string, cfg config.Config) error {
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

// openSession is the shared open path used by both REPL and one-shot
// modes. It reads the passphrase (TTY or KPOT_PASSPHRASE env), decrypts
// the vault, and returns a wired-up Session honoring config overrides.
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
