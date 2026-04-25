//go:build linux

package clipboard

import (
	"os"
	"os/exec"
)

// detect prefers Wayland (wl-copy/wl-paste) when WAYLAND_DISPLAY is set,
// otherwise falls back to xclip with the CLIPBOARD selection.
func detect() (Clipboard, error) {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if _, err := exec.LookPath("wl-copy"); err == nil {
			c := &execClipboard{
				name:    "wl-copy",
				copyCmd: func() *exec.Cmd { return exec.Command("wl-copy") },
			}
			if _, err := exec.LookPath("wl-paste"); err == nil {
				c.pasteCmd = func() *exec.Cmd { return exec.Command("wl-paste", "--no-newline") }
			}
			return c, nil
		}
	}
	if _, err := exec.LookPath("xclip"); err == nil {
		return &execClipboard{
			name:     "xclip",
			copyCmd:  func() *exec.Cmd { return exec.Command("xclip", "-selection", "clipboard", "-in") },
			pasteCmd: func() *exec.Cmd { return exec.Command("xclip", "-selection", "clipboard", "-out") },
		}, nil
	}
	if _, err := exec.LookPath("xsel"); err == nil {
		return &execClipboard{
			name:     "xsel",
			copyCmd:  func() *exec.Cmd { return exec.Command("xsel", "--clipboard", "--input") },
			pasteCmd: func() *exec.Cmd { return exec.Command("xsel", "--clipboard", "--output") },
		}, nil
	}
	return nil, ErrUnavailable
}
