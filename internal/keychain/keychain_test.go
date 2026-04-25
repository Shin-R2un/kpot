package keychain

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// --- abstraction tests via Fake ---

func TestFakeRoundTrip(t *testing.T) {
	f := NewFake()
	if got, err := f.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on empty = (%v, %v)", got, err)
	}
	secret := []byte{1, 2, 3, 4, 5}
	if err := f.Set("a", secret); err != nil {
		t.Fatal(err)
	}
	got, err := f.Get("a")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, secret) {
		t.Fatalf("Get returned %v, want %v", got, secret)
	}
	// Mutating returned bytes must not affect stored value.
	got[0] = 99
	again, _ := f.Get("a")
	if again[0] != 1 {
		t.Fatal("Get returned aliased bytes; caller mutation bled into store")
	}
}

func TestFakeOverwrite(t *testing.T) {
	f := NewFake()
	f.Set("a", []byte("first"))
	f.Set("a", []byte("second"))
	got, _ := f.Get("a")
	if string(got) != "second" {
		t.Fatalf("got %q after overwrite, want second", got)
	}
}

func TestFakeDelete(t *testing.T) {
	f := NewFake()
	f.Set("a", []byte("v"))
	if err := f.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Get("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := f.Delete("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete on missing = %v, want ErrNotFound", err)
	}
}

func TestFakeUnavailable(t *testing.T) {
	f := NewFake()
	f.SetAvailable(false)
	if f.Available() {
		t.Fatal("Available reported true after SetAvailable(false)")
	}
	if _, err := f.Get("a"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Get when unavailable = %v, want ErrUnavailable", err)
	}
	if err := f.Set("a", []byte("v")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Set when unavailable = %v, want ErrUnavailable", err)
	}
}

// --- helpers ---

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := bytes.Repeat([]byte{0xAB, 0xCD}, 16)
	enc := EncodeSecret(in)
	out, err := DecodeSecret(enc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(in, out) {
		t.Fatalf("round-trip mismatch")
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := DecodeSecret("not-hex!"); err == nil {
		t.Fatal("expected decode error for non-hex input")
	}
}

func TestCanonicalAccountResolves(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.kpot")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.kpot")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported here: %v", err)
	}
	a := CanonicalAccount(target)
	b := CanonicalAccount(link)
	if a != b {
		t.Fatalf("symlink and target resolved to different accounts:\n  target=%q\n  link  =%q", a, b)
	}
}

func TestCanonicalAccountHandlesMissingFile(t *testing.T) {
	// CanonicalAccount must not error on a file that doesn't exist
	// (caller is e.g. about to `init` it). It should fall back to the
	// absolute path.
	got := CanonicalAccount("/tmp/definitely-not-here-12345.kpot")
	if got == "" {
		t.Fatal("expected non-empty fallback")
	}
}

// --- platform integration test (gated) ---

// TestRealBackend exercises the OS-native backend when explicitly
// opted in via KPOT_TEST_KEYCHAIN=1. CI without a keychain available
// (no D-Bus session, no Touch ID dialog handling, etc.) skips it.
func TestRealBackend(t *testing.T) {
	if os.Getenv("KPOT_TEST_KEYCHAIN") != "1" {
		t.Skip("set KPOT_TEST_KEYCHAIN=1 to exercise the OS backend")
	}
	kc := Default()
	if !kc.Available() {
		t.Skipf("backend %s not available on this host", kc.Name())
	}

	account := "kpot-test-" + filepath.Base(t.TempDir())
	t.Cleanup(func() { _ = kc.Delete(account) })

	secret := []byte("integration-test-secret-bytes-32B!!")[:32]
	if err := kc.Set(account, secret); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := kc.Get(account)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, secret) {
		t.Fatalf("round-trip mismatch")
	}
	if err := kc.Delete(account); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := kc.Get(account); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
}
