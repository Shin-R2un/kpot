package tty

import (
	"bufio"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/term"
)

// PassphraseEnv is the environment variable that bypasses the TTY
// prompt. Useful for scripted/non-interactive runs; printed warning on
// stderr so users notice when they leave it set in production.
const PassphraseEnv = "KPOT_PASSPHRASE"

var (
	stdinReaderOnce sync.Once
	stdinReader     *bufio.Reader

	envWarnOnce sync.Once
)

// SharedStdin returns a process-wide bufio.Reader bound to os.Stdin.
// Multiple subsystems (passphrase prompt, REPL) MUST share one reader,
// otherwise eager bufio buffering in one reader silently swallows lines
// the next reader expects to see.
func SharedStdin() *bufio.Reader {
	stdinReaderOnce.Do(func() {
		stdinReader = bufio.NewReader(os.Stdin)
	})
	return stdinReader
}

func sharedStdin() *bufio.Reader { return SharedStdin() }

// ResetEnvWarnForTest re-arms the once-per-process warning that fires
// when KPOT_PASSPHRASE is set. Tests that exercise multiple bypass
// paths in one binary need this; production code never calls it.
func ResetEnvWarnForTest() { envWarnOnce = sync.Once{} }

// ReadLine prompts on stderr and reads one line of (echoed) input as
// a string. Use this for non-sensitive input only; sensitive input
// should go through ReadLineSecret so the caller can zero the buffer.
func ReadLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := sharedStdin().ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// ReadLineSecret reads one line of (echoed) input as a byte slice the
// caller is expected to crypto.Zero after use. Use for recovery
// secrets (seed phrases, recovery keys) so the user-typed bytes can
// be wiped explicitly.
//
// Caveat: bufio.Reader internally buffers a copy we cannot reach, and
// any string-typed downstream operation (e.g. BIP-39 validation) will
// produce a string copy that lives until GC. This wipe is best-effort,
// not airtight — same posture as crypto.Zero for keys.
func ReadLineSecret(prompt string) ([]byte, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := sharedStdin().ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return nil, err
	}
	return []byte(strings.TrimRight(line, "\r\n")), nil
}

// ReadPassphrase prompts the user for a passphrase with no echo.
// Falls back to plain stdin reading if the input is not a terminal
// (useful for tests piping a passphrase). All non-TTY reads go through
// a single shared bufio.Reader so consecutive prompts don't lose lines
// to per-call buffering.
//
// If the KPOT_PASSPHRASE environment variable is set, its value is
// returned without prompting (and a one-time warning is printed to
// stderr so the user knows the bypass is active).
func ReadPassphrase(prompt string) ([]byte, error) {
	if env, ok := os.LookupEnv(PassphraseEnv); ok {
		envWarnOnce.Do(func() {
			fmt.Fprintln(os.Stderr, "warning: "+PassphraseEnv+" is set; bypassing passphrase prompt. Not recommended for production.")
		})
		return []byte(env), nil
	}

	fmt.Fprint(os.Stderr, prompt)
	defer fmt.Fprintln(os.Stderr)

	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	line, err := sharedStdin().ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return nil, err
	}
	return []byte(strings.TrimRight(line, "\r\n")), nil
}

// ReadNewPassphrase prompts twice and verifies the entries match.
func ReadNewPassphrase(prompt, confirmPrompt string) ([]byte, error) {
	first, err := ReadPassphrase(prompt)
	if err != nil {
		return nil, err
	}
	if len(first) == 0 {
		return nil, errors.New("passphrase cannot be empty")
	}
	second, err := ReadPassphrase(confirmPrompt)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare(first, second) != 1 {
		// Best-effort wipe of the rejected entries before returning.
		for i := range first {
			first[i] = 0
		}
		for i := range second {
			second[i] = 0
		}
		return nil, errors.New("passphrases do not match")
	}
	for i := range second {
		second[i] = 0
	}
	return first, nil
}
