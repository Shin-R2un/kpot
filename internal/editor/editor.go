package editor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Edit launches the user's editor on a temporary file initialized with
// `initial`, then returns the bytes the user saved.
// Temp files are written to tmpfs (e.g. /dev/shm) when available, and
// the file is unlinked on return whether the edit succeeded or not.
func Edit(initial []byte, hint string) ([]byte, error) {
	editor, args, err := pickEditor()
	if err != nil {
		return nil, err
	}

	dir := tempDir()
	pattern := "kpot-" + sanitize(hint) + "-*.md"
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := f.Name()
	defer wipeAndRemove(tmpPath)

	if _, err := f.Write(initial); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return nil, err
	}

	cmd := exec.Command(editor, append(args, tmpPath)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("editor failed: %w", err)
	}
	return os.ReadFile(tmpPath)
}

func pickEditor() (string, []string, error) {
	if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
		fields := strings.Fields(e)
		return fields[0], fields[1:], nil
	}
	if e := strings.TrimSpace(os.Getenv("VISUAL")); e != "" {
		fields := strings.Fields(e)
		return fields[0], fields[1:], nil
	}
	candidates := []string{"nano", "vim", "vi"}
	if runtime.GOOS == "windows" {
		candidates = []string{"notepad"}
	}
	for _, c := range candidates {
		if path, err := exec.LookPath(c); err == nil {
			return path, nil, nil
		}
	}
	return "", nil, errors.New("no editor available. Set $EDITOR or install nano/vim")
}

func tempDir() string {
	if runtime.GOOS == "linux" {
		if info, err := os.Stat("/dev/shm"); err == nil && info.IsDir() {
			return "/dev/shm"
		}
	}
	return os.TempDir()
}

func sanitize(s string) string {
	if s == "" {
		return "note"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == '/':
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" {
		return "note"
	}
	if len(out) > 32 {
		return out[:32]
	}
	return out
}

// wipeAndRemove best-effort overwrites the file with zeros and unlinks it.
// On success or failure the file should not remain on disk.
func wipeAndRemove(path string) {
	if info, err := os.Stat(path); err == nil {
		if f, err := os.OpenFile(path, os.O_WRONLY, 0o600); err == nil {
			zero := make([]byte, 4096)
			remaining := info.Size()
			for remaining > 0 {
				n := int64(len(zero))
				if remaining < n {
					n = remaining
				}
				_, _ = f.Write(zero[:n])
				remaining -= n
			}
			_ = f.Sync()
			_ = f.Close()
		}
	}
	_ = os.Remove(path)
}
