package clipboard

import (
	"errors"
	"testing"
	"time"
)

func TestManagerCopyAndAutoClear(t *testing.T) {
	f := NewFake()
	m := NewManager(f, 30*time.Millisecond)

	if err := m.Copy([]byte("secret")); err != nil {
		t.Fatal(err)
	}
	if string(f.Snapshot()) != "secret" {
		t.Fatalf("after Copy: %q", string(f.Snapshot()))
	}

	time.Sleep(80 * time.Millisecond)
	if string(f.Snapshot()) != "" {
		t.Fatalf("auto-clear failed: %q", string(f.Snapshot()))
	}
}

func TestManagerSkipsClearWhenContentChanged(t *testing.T) {
	f := NewFake()
	m := NewManager(f, 30*time.Millisecond)

	if err := m.Copy([]byte("secret")); err != nil {
		t.Fatal(err)
	}
	f.SetExternal([]byte("user-typed-something-else"))

	time.Sleep(80 * time.Millisecond)
	if string(f.Snapshot()) != "user-typed-something-else" {
		t.Fatalf("user content was wiped: %q", string(f.Snapshot()))
	}
}

func TestManagerCopyCancelsPriorJob(t *testing.T) {
	f := NewFake()
	m := NewManager(f, 30*time.Millisecond)

	if err := m.Copy([]byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := m.Copy([]byte("second")); err != nil {
		t.Fatal(err)
	}

	time.Sleep(80 * time.Millisecond)
	// only the second copy should clear; clipboard should be empty.
	if string(f.Snapshot()) != "" {
		t.Fatalf("expected cleared clipboard, got %q", string(f.Snapshot()))
	}
	// Three Copy calls total: "first", "second", and the final "" wipe.
	if got := f.Copies(); got != 3 {
		t.Fatalf("expected 3 Copy calls (incl. wipe), got %d", got)
	}
}

func TestManagerCloseClearsImmediately(t *testing.T) {
	f := NewFake()
	m := NewManager(f, 5*time.Second)

	if err := m.Copy([]byte("secret")); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if string(f.Snapshot()) != "" {
		t.Fatalf("Close should clear: %q", string(f.Snapshot()))
	}
}

func TestManagerCloseSkipsWhenContentChanged(t *testing.T) {
	f := NewFake()
	m := NewManager(f, 5*time.Second)

	if err := m.Copy([]byte("secret")); err != nil {
		t.Fatal(err)
	}
	f.SetExternal([]byte("other"))
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if string(f.Snapshot()) != "other" {
		t.Fatalf("Close wiped user content: %q", string(f.Snapshot()))
	}
}

func TestManagerClearsBlindlyWhenPasteUnsupported(t *testing.T) {
	f := NewFake()
	f.SetPasteErr(ErrPasteUnsupported)
	m := NewManager(f, 30*time.Millisecond)

	if err := m.Copy([]byte("secret")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if string(f.Snapshot()) != "" {
		t.Fatalf("expected blind clear, got %q", string(f.Snapshot()))
	}
}

func TestManagerNilBackend(t *testing.T) {
	m := NewManager(nil, 0)
	if err := m.Copy([]byte("x")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close on nil backend: %v", err)
	}
}
