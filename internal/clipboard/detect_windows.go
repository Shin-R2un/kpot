//go:build windows

package clipboard

import "os/exec"

func detect() (Clipboard, error) {
	if _, err := exec.LookPath("powershell"); err != nil {
		return nil, ErrUnavailable
	}
	return &execClipboard{
		name:     "powershell-clipboard",
		copyCmd:  func() *exec.Cmd { return exec.Command("powershell", "-NoProfile", "-Command", "$in = [Console]::In.ReadToEnd(); Set-Clipboard -Value $in") },
		pasteCmd: func() *exec.Cmd { return exec.Command("powershell", "-NoProfile", "-Command", "Get-Clipboard -Raw") },
	}, nil
}
