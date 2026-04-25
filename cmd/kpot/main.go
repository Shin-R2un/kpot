package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/r2un/kpot/internal/crypto"
	"github.com/r2un/kpot/internal/repl"
	"github.com/r2un/kpot/internal/store"
	"github.com/r2un/kpot/internal/tty"
	"github.com/r2un/kpot/internal/vault"
)

const usage = `kpot - encrypted CLI note vault

Usage:
  kpot init <file>     Create a new encrypted vault
  kpot <file>          Open a vault and enter interactive mode
  kpot help            Show this help
  kpot version         Show the version

Examples:
  kpot init personal.kpot
  kpot personal.kpot
`

const version = "0.1.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(exitCodeFor(err))
	}
}

func run(args []string) error {
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
		if len(args) != 1 {
			return errArgs("usage: kpot <file>")
		}
		return cmdOpen(args[0])
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

func cmdOpen(path string) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("vault file %q not found. Use 'kpot init %s' to create it", path, path)
		}
		return err
	}

	pass, err := tty.ReadPassphrase("Passphrase: ")
	if err != nil {
		return err
	}
	defer crypto.Zero(pass)

	plaintext, key, hdr, err := vault.Open(path, pass)
	if err != nil {
		if errors.Is(err, crypto.ErrAuthFailed) {
			return errAuth("Wrong passphrase, or the file is corrupted")
		}
		return err
	}
	defer crypto.Zero(plaintext)

	v, err := store.FromJSON(plaintext)
	if err != nil {
		crypto.Zero(key)
		return err
	}
	sess := repl.NewSession(path, v, key, hdr)
	defer sess.Close()
	return sess.Run()
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
