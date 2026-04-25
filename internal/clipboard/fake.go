package clipboard

import "sync"

// Fake is an in-memory Clipboard suitable for tests of this package and
// of any package that wants to exercise Manager wiring without touching
// the host's real clipboard. Lives outside _test.go so importers can
// reach it. Not safe for production use.
type Fake struct {
	mu           sync.Mutex
	value        []byte
	supportPaste bool
	copies       int
	pasteErr     error
}

// NewFake returns a Fake with paste support enabled.
func NewFake() *Fake { return &Fake{supportPaste: true} }

func (f *Fake) Name() string { return "fake" }

func (f *Fake) Copy(data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.value = append([]byte(nil), data...)
	f.copies++
	return nil
}

func (f *Fake) Paste() ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pasteErr != nil {
		return nil, f.pasteErr
	}
	if !f.supportPaste {
		return nil, ErrPasteUnsupported
	}
	return append([]byte(nil), f.value...), nil
}

// Snapshot returns the current clipboard value.
func (f *Fake) Snapshot() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.value...)
}

// Copies returns how many times Copy has been invoked.
func (f *Fake) Copies() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copies
}

// SetExternal simulates the user copying something else into the clipboard.
func (f *Fake) SetExternal(v []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.value = append([]byte(nil), v...)
}

// SetPasteErr makes Paste return err (simulates a backend with no read tool).
func (f *Fake) SetPasteErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pasteErr = err
}
