//go:build darwin

package keychain

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const securityBin = "/usr/bin/security"

// Apple's "errSecItemNotFound" surfaces from the security CLI as
// exit code 44. Used to map "no such entry" to ErrNotFound cleanly.
const errSecItemNotFoundExit = 44

type macBackend struct{}

func defaultBackend() Backend { return &macBackend{} }

func (*macBackend) Name() string { return "macos-keychain" }

func (*macBackend) Available() bool {
	_, err := exec.LookPath(securityBin)
	return err == nil
}

func (*macBackend) Get(account string) ([]byte, error) {
	cmd := exec.Command(securityBin,
		"find-generic-password",
		"-s", Service,
		"-a", account,
		"-w", // -w prints just the password to stdout
	)
	out, err := cmd.Output()
	if err != nil {
		if isExit(err, errSecItemNotFoundExit) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("macos keychain Get: %w", err)
	}
	return DecodeSecret(strings.TrimRight(string(out), "\n"))
}

func (*macBackend) Set(account string, secret []byte) error {
	encoded := EncodeSecret(secret)
	// -U updates the entry if it already exists, instead of failing
	// with "already exists" (errSecDuplicateItem, exit 45).
	cmd := exec.Command(securityBin,
		"add-generic-password",
		"-s", Service,
		"-a", account,
		"-w", encoded,
		"-U",
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("macos keychain Set: %w", err)
	}
	return nil
}

func (*macBackend) Delete(account string) error {
	cmd := exec.Command(securityBin,
		"delete-generic-password",
		"-s", Service,
		"-a", account,
	)
	if err := cmd.Run(); err != nil {
		if isExit(err, errSecItemNotFoundExit) {
			return ErrNotFound
		}
		return fmt.Errorf("macos keychain Delete: %w", err)
	}
	return nil
}

func isExit(err error, code int) bool {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	return ee.ExitCode() == code
}
