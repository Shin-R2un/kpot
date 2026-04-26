//go:build windows

package tty

import (
	"io"
	"os"
)

// stdoutSink wraps os.Stdout so it satisfies io.ReadWriteCloser
// (we never call Read on it — DisplayRecoveryOnce reads via stdin —
// but the interface requires it). Close is a no-op so the caller's
// `defer sink.Close()` doesn't accidentally close stdout.
type stdoutSink struct{}

func (stdoutSink) Read(p []byte) (int, error)  { return 0, io.EOF }
func (stdoutSink) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdoutSink) Close() error                { return nil }

// openSecretSink on Windows uses os.Stdout, since /dev/tty doesn't
// exist. The DisplayRecoveryOnce caller has already verified that
// stdout is a real console (IsStdoutTTY()), so this matches the
// "TTY-only" guarantee the Unix path provides via /dev/tty.
//
// Caveat: on Windows, redirecting stdout to a file would normally be
// caught by IsStdoutTTY() returning false, so the fallback to a
// captured stream isn't a concern here.
func openSecretSink() (io.ReadWriteCloser, error) {
	return stdoutSink{}, nil
}

// waitForENTER reads from os.Stdin via the shared bufio.Reader. Same
// rationale as openSecretSink: stdin was verified to be a real
// console before we got here.
func waitForENTER() {
	_, _ = SharedStdin().ReadString('\n')
}
