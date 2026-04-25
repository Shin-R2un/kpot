//go:build linux

package keychain

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// secretToolBin is the libsecret-tools CLI. Available via:
//   Debian/Ubuntu : apt install libsecret-tools
//   Fedora/RHEL   : dnf install libsecret
//   Arch          : pacman -S libsecret
const secretToolBin = "secret-tool"

// secret-tool exit codes (from man page):
//   0  = success
//   1  = item not found / generic error (it's not very precise)
// We probe stderr to distinguish "not found" from real errors.

type linuxBackend struct{}

func defaultBackend() Backend { return &linuxBackend{} }

func (*linuxBackend) Name() string { return "linux-secret-tool" }

// Available checks both that the CLI exists AND that a D-Bus session
// bus is reachable (Secret Service is a session-scoped service).
// Headless / SSH sessions without a D-Bus session bus return false.
func (*linuxBackend) Available() bool {
	if _, err := exec.LookPath(secretToolBin); err != nil {
		return false
	}
	// secret-tool needs a session D-Bus to talk to
	// org.freedesktop.secrets. Without DBUS_SESSION_BUS_ADDRESS the
	// call hangs or errors; bail early.
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		return false
	}
	return true
}

func (l *linuxBackend) Get(account string) ([]byte, error) {
	if !l.Available() {
		return nil, ErrUnavailable
	}
	cmd := exec.Command(secretToolBin,
		"lookup",
		"service", Service,
		"account", account,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// secret-tool returns exit 1 both for "not found" and real
		// errors. The not-found case prints nothing on stderr.
		if stderr.Len() == 0 && stdout.Len() == 0 {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("secret-tool lookup: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return DecodeSecret(strings.TrimRight(stdout.String(), "\n"))
}

func (l *linuxBackend) Set(account string, secret []byte) error {
	if !l.Available() {
		return ErrUnavailable
	}
	encoded := EncodeSecret(secret)
	// `secret-tool store` reads the secret from stdin (so it doesn't
	// appear in /proc/<pid>/cmdline) and uses --label for the human-
	// readable name shown in keyring browsers.
	cmd := exec.Command(secretToolBin,
		"store",
		"--label", "kpot vault key",
		"service", Service,
		"account", account,
	)
	cmd.Stdin = strings.NewReader(encoded)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("secret-tool store: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (l *linuxBackend) Delete(account string) error {
	if !l.Available() {
		return ErrUnavailable
	}
	// `secret-tool clear` removes any matching entries; it doesn't
	// distinguish "removed" vs "nothing to remove" via exit code on
	// older versions, so we Get first to provide ErrNotFound semantics.
	if _, err := l.Get(account); errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	cmd := exec.Command(secretToolBin,
		"clear",
		"service", Service,
		"account", account,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("secret-tool clear: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
