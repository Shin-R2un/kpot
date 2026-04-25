//go:build !darwin && !linux && !windows

package keychain

// noopBackend is the fallback for OSes we don't support yet (BSDs,
// Plan 9, etc.). Always reports unavailable; never stores anything.
// Callers should treat ErrUnavailable as "caching off, fall back to
// passphrase prompt every time".
type noopBackend struct{}

func defaultBackend() Backend { return &noopBackend{} }

func (*noopBackend) Name() string               { return "none" }
func (*noopBackend) Available() bool            { return false }
func (*noopBackend) Get(string) ([]byte, error) { return nil, ErrUnavailable }
func (*noopBackend) Set(string, []byte) error   { return ErrUnavailable }
func (*noopBackend) Delete(string) error        { return ErrUnavailable }
