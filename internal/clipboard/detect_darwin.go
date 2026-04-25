//go:build darwin

package clipboard

import "os/exec"

func detect() (Clipboard, error) {
	if _, err := exec.LookPath("pbcopy"); err != nil {
		return nil, ErrUnavailable
	}
	c := &execClipboard{
		name:    "pbcopy",
		copyCmd: func() *exec.Cmd { return exec.Command("pbcopy") },
	}
	if _, err := exec.LookPath("pbpaste"); err == nil {
		c.pasteCmd = func() *exec.Cmd { return exec.Command("pbpaste") }
	}
	return c, nil
}
