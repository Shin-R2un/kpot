package keychain

import "sync"

// Fake is an in-memory backend for tests. NOT for production use.
// Lives outside _test.go so importers (cmd, repl tests) can plug it
// in without depending on testing infrastructure.
type Fake struct {
	mu      sync.Mutex
	entries map[string][]byte
	avail   bool
}

// NewFake returns a Fake that reports Available() == true.
func NewFake() *Fake {
	return &Fake{entries: map[string][]byte{}, avail: true}
}

// SetAvailable lets tests simulate a backend that's offline (e.g.
// headless Linux without D-Bus).
func (f *Fake) SetAvailable(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.avail = v
}

func (f *Fake) Name() string { return "fake" }

func (f *Fake) Available() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.avail
}

func (f *Fake) Get(account string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.avail {
		return nil, ErrUnavailable
	}
	v, ok := f.entries[account]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), v...), nil
}

func (f *Fake) Set(account string, secret []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.avail {
		return ErrUnavailable
	}
	f.entries[account] = append([]byte(nil), secret...)
	return nil
}

func (f *Fake) Delete(account string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.avail {
		return ErrUnavailable
	}
	if _, ok := f.entries[account]; !ok {
		return ErrNotFound
	}
	delete(f.entries, account)
	return nil
}

// Count returns the number of stored entries (test helper).
func (f *Fake) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries)
}
