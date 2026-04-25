package repl

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r2un/kpot/internal/store"
	"github.com/r2un/kpot/internal/vault"
)

// scriptedSession runs a sequence of commands against the dispatcher
// without going through ReadString. This lets us exercise the command
// surface without a TTY.
func scriptedSession(t *testing.T, path string) *Session {
	t.Helper()
	pass := []byte("p")
	v := store.New()
	pt, _ := v.ToJSON()
	key, hdr, err := vault.Create(path, pass, pt)
	if err != nil {
		t.Fatal(err)
	}
	return NewSession(path, v, key, hdr)
}

func TestDispatchLs(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("ls", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "(empty)") {
		t.Fatalf("ls output = %q", buf.String())
	}
}

func TestDispatchExit(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	stop, err := s.dispatch("exit", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !stop {
		t.Fatal("expected stop=true")
	}
}

func TestDispatchUnknown(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	if _, err := s.dispatch("nosuchcmd", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestReadAndPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.kpot")
	s := scriptedSession(t, path)
	defer s.Close()

	if _, err := s.Vault.Put("ai/openai", "OPENAI_API_KEY=sk-test"); err != nil {
		t.Fatal(err)
	}
	if err := s.persist(); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	s.out = &buf
	if err := s.read("ai/openai"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "sk-test") {
		t.Fatalf("read output = %q", buf.String())
	}
}
