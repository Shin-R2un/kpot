// Package clipboard provides a tiny cross-platform clipboard wrapper
// with a 30-second auto-clear Manager intended for short-lived secrets.
//
// The OS-specific Detect implementations live in detect_<goos>.go and
// shell out to standard tools (wl-copy / xclip / pbcopy / PowerShell).
// They are intentionally thin: parsing, threading and lifecycle live
// in this file so they can be exercised from tests via the Fake type.
package clipboard

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// ErrPasteUnsupported is returned by backends that can write but not read.
var ErrPasteUnsupported = errors.New("clipboard backend does not support paste")

// ErrUnavailable is returned by Detect when no backend tool is found.
var ErrUnavailable = errors.New("no clipboard tool available. Install xclip / wl-clipboard / pbcopy")

// DefaultClearAfter is how long a copied secret stays in the clipboard
// before the Manager attempts to wipe it.
const DefaultClearAfter = 30 * time.Second

// Clipboard is the minimal interface the Manager needs from a backend.
type Clipboard interface {
	Copy(data []byte) error
	Paste() ([]byte, error)
	Name() string
}

// Detect picks the best available system clipboard backend, or returns
// ErrUnavailable. The exact selection is OS-specific (see detect_*.go).
func Detect() (Clipboard, error) { return detect() }

// execClipboard is a generic shell-command-backed Clipboard. It is the
// implementation used by every OS detect helper.
type execClipboard struct {
	name      string
	copyCmd   func() *exec.Cmd
	pasteCmd  func() *exec.Cmd // nil ⇒ paste unsupported
}

func (c *execClipboard) Name() string { return c.name }

func (c *execClipboard) Copy(data []byte) error {
	cmd := c.copyCmd()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	_, writeErr := io.Copy(stdin, bytes.NewReader(data))
	closeErr := stdin.Close()
	waitErr := cmd.Wait()
	if writeErr != nil {
		return fmt.Errorf("clipboard write: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("clipboard close: %w", closeErr)
	}
	if waitErr != nil {
		return fmt.Errorf("clipboard tool %q failed: %w", c.name, waitErr)
	}
	return nil
}

func (c *execClipboard) Paste() ([]byte, error) {
	if c.pasteCmd == nil {
		return nil, ErrPasteUnsupported
	}
	out, err := c.pasteCmd().Output()
	if err != nil {
		return nil, fmt.Errorf("clipboard read via %q: %w", c.name, err)
	}
	return out, nil
}

// Manager owns a single pending auto-clear job. New copies cancel the
// prior pending job. Close synchronously clears any still-ours pending
// content so process exit doesn't leave secrets behind.
type Manager struct {
	cb       Clipboard
	clearTTL time.Duration

	mu      sync.Mutex
	current *clearJob
}

type clearJob struct {
	expected []byte
	done     chan struct{}
}

// NewManager wraps cb with a clear-after timer. ttl<=0 uses
// DefaultClearAfter. cb may be nil if the caller wants Copy to error.
func NewManager(cb Clipboard, ttl time.Duration) *Manager {
	if ttl <= 0 {
		ttl = DefaultClearAfter
	}
	return &Manager{cb: cb, clearTTL: ttl}
}

// Backend exposes the underlying Clipboard (mostly for diagnostics).
func (m *Manager) Backend() Clipboard { return m.cb }

// ClearAfter reports the configured TTL.
func (m *Manager) ClearAfter() time.Duration { return m.clearTTL }

// Copy puts data on the system clipboard and arms a goroutine to clear
// it after the configured TTL. Any prior pending clear is cancelled.
// Returns ErrUnavailable if no backend was wired.
func (m *Manager) Copy(data []byte) error {
	if m.cb == nil {
		return ErrUnavailable
	}
	if err := m.cb.Copy(data); err != nil {
		return err
	}

	m.mu.Lock()
	if m.current != nil {
		close(m.current.done)
		m.current = nil
	}
	expected := append([]byte(nil), data...) // own copy — caller may reuse / zero
	job := &clearJob{expected: expected, done: make(chan struct{})}
	m.current = job
	m.mu.Unlock()

	go m.run(job)
	return nil
}

func (m *Manager) run(job *clearJob) {
	t := time.NewTimer(m.clearTTL)
	defer t.Stop()
	select {
	case <-job.done:
		return
	case <-t.C:
		m.tryClear(job)
	}
}

// tryClear wipes the clipboard if our content is still there. If the
// backend can't read, it clears anyway: leaving a stale secret behind
// is worse than overwriting an unrelated unreadable value.
func (m *Manager) tryClear(job *clearJob) {
	m.mu.Lock()
	if m.current != job {
		m.mu.Unlock()
		return // a newer Copy raced past us
	}
	m.current = nil
	m.mu.Unlock()

	if cur, err := m.cb.Paste(); err == nil {
		if !bytes.Equal(bytes.TrimRight(cur, "\r\n"), bytes.TrimRight(job.expected, "\r\n")) {
			return // user copied something else; don't disturb it
		}
	}
	_ = m.cb.Copy(nil) // best-effort clear
}

// Close synchronously clears any still-ours pending secret. Safe to
// call multiple times.
func (m *Manager) Close() error {
	m.mu.Lock()
	job := m.current
	m.current = nil
	m.mu.Unlock()
	if job == nil {
		return nil
	}
	close(job.done)

	if m.cb == nil {
		return nil
	}
	if cur, err := m.cb.Paste(); err == nil {
		if !bytes.Equal(bytes.TrimRight(cur, "\r\n"), bytes.TrimRight(job.expected, "\r\n")) {
			return nil
		}
	}
	return m.cb.Copy(nil)
}
