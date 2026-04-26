package tty

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoTTY is returned by recovery-display helpers when stdin/stdout
// aren't terminals. The pipe-or-redirect case is rejected on purpose:
// secrets must not flow into log files, scrollback buffers, or CI
// artifacts. Users running scripted setups have to take the manual
// route (init interactively, then automate after).
var ErrNoTTY = errors.New("recovery operations require a TTY (no pipes / redirects allowed)")

// DisplayRecoveryOnce shows a recovery secret directly to the user's
// terminal (NOT into stdout/stderr if those have been redirected
// somewhere capturable), waits for the user to acknowledge, and then
// ANSI-clears the screen. There is no API to redisplay; lose the
// paper, lose the recovery.
//
// On Unix the sink is `/dev/tty` (so even with stdout/stderr piped to
// a logger, the secret never reaches the pipe). On Windows there's
// no /dev/tty, so the sink is os.Stdout — which is the actual
// console because the IsStdinTTY/IsStdoutTTY check above ensures we
// only run when both ends are real terminals.
//
// header is the leading "WRITE THIS DOWN" warning block. body is the
// secret itself (mnemonic words or formatted secret-key string).
func DisplayRecoveryOnce(header, body string) error {
	if !IsStdinTTY() || !IsStdoutTTY() {
		return ErrNoTTY
	}

	sink, err := openSecretSink()
	if err != nil {
		return fmt.Errorf("%w (cannot open secret sink: %v)", ErrNoTTY, err)
	}
	defer sink.Close()

	fmt.Fprintln(sink)
	fmt.Fprintln(sink, "════════════════════════════════════════════════════════════════")
	fmt.Fprintln(sink, header)
	fmt.Fprintln(sink, "════════════════════════════════════════════════════════════════")
	fmt.Fprintln(sink)
	fmt.Fprintln(sink, body)
	fmt.Fprintln(sink)
	fmt.Fprintln(sink, "────────────────────────────────────────────────────────────────")
	fmt.Fprint(sink, "書き留めましたか？ ENTER で画面を消去します… ")

	// Wait for any input from the same TTY (typically just ENTER).
	// On Unix that means reading from /dev/tty; on Windows we read
	// from os.Stdin, which we already verified is a console.
	waitForENTER()
	fmt.Fprint(sink, "\033[2J\033[H") // ANSI: clear screen + home cursor
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
