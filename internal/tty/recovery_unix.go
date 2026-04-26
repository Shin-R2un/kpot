//go:build !windows

package tty

import (
	"io"
	"os"
)

// openSecretSink returns a writer/reader bound to the controlling TTY
// (`/dev/tty`). Even when stdout/stderr have been redirected to a log,
// /dev/tty stays attached to the real terminal — so the recovery
// secret never reaches a captured stream.
func openSecretSink() (io.ReadWriteCloser, error) {
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}

// waitForENTER reads (and discards) one line of input from the secret
// sink. We open /dev/tty fresh here so that even if the calling
// process's stdin is piped, we still block on the human's ENTER.
func waitForENTER() {
	tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		return
	}
	defer tty.Close()
	buf := make([]byte, 256)
	_, _ = tty.Read(buf)
}
