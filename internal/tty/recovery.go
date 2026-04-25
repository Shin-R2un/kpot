package tty

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrNoTTY is returned by recovery-display helpers when stdin/stdout
// aren't terminals. The pipe-or-redirect case is rejected on purpose:
// secrets must not flow into log files, scrollback buffers, or CI
// artifacts. Users running scripted setups have to take the manual
// route (init interactively, then automate after).
var ErrNoTTY = errors.New("recovery operations require a TTY (no pipes / redirects allowed)")

// DisplayRecoveryOnce shows a recovery secret directly to the
// controlling TTY (NOT stdout/stderr — those can be piped or logged),
// waits for the user to acknowledge, and then ANSI-clears the screen.
// There is no API to redisplay; lose the paper, lose the recovery.
//
// header is the leading "WRITE THIS DOWN" warning block. body is the
// secret itself (mnemonic words or formatted secret-key string).
func DisplayRecoveryOnce(header, body string) error {
	if !IsStdinTTY() || !IsStdoutTTY() {
		return ErrNoTTY
	}

	// Always go through /dev/tty. We deliberately do NOT fall back to
	// stderr: in containers / CI / systemd the parent may capture
	// stderr, and writing the secret there would defeat the whole
	// "TTY-only" guarantee even though stdin+stdout looked terminal-y.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("%w (cannot open /dev/tty: %v)", ErrNoTTY, err)
	}
	defer tty.Close()

	fmt.Fprintln(tty)
	fmt.Fprintln(tty, "════════════════════════════════════════════════════════════════")
	fmt.Fprintln(tty, header)
	fmt.Fprintln(tty, "════════════════════════════════════════════════════════════════")
	fmt.Fprintln(tty)
	fmt.Fprintln(tty, body)
	fmt.Fprintln(tty)
	fmt.Fprintln(tty, "────────────────────────────────────────────────────────────────")
	fmt.Fprint(tty, "書き留めましたか？ ENTER で画面を消去します… ")

	buf := make([]byte, 256)
	_, _ = tty.Read(buf) // best effort; we just want the user's ENTER
	fmt.Fprint(tty, "\033[2J\033[H") // ANSI: clear screen + home cursor
	return nil
}

// FormatSeedWords renders 12/24 BIP-39 words as a 4-column numbered
// grid for easy hand-copying.
func FormatSeedWords(mnemonic string) string {
	words := strings.Fields(mnemonic)
	var b strings.Builder
	for i, w := range words {
		fmt.Fprintf(&b, "%2d. %-12s", i+1, w)
		if (i+1)%4 == 0 {
			b.WriteByte('\n')
		}
	}
	if len(words)%4 != 0 {
		b.WriteByte('\n')
	}
	return b.String()
}

