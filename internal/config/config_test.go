package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTOML(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFromMissingFile(t *testing.T) {
	cfg, err := LoadFrom(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.Editor != "" || cfg.ClipboardClearSeconds != 0 {
		t.Fatalf("expected zero-value, got %+v", cfg)
	}
}

func TestLoadFromValid(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, `editor = "vim"
clipboard_clear_seconds = 60
`)
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Editor != "vim" {
		t.Errorf("Editor = %q, want vim", cfg.Editor)
	}
	if cfg.ClipboardClearSeconds != 60 {
		t.Errorf("ClipboardClearSeconds = %d, want 60", cfg.ClipboardClearSeconds)
	}
	if got := cfg.ClipboardTTL(); got != 60*time.Second {
		t.Errorf("ClipboardTTL = %v, want 60s", got)
	}
}

func TestClipboardTTLZeroMeansDefault(t *testing.T) {
	cfg := Config{}
	if cfg.ClipboardTTL() != 0 {
		t.Fatalf("zero-value should return 0 (caller picks default)")
	}
}

func TestLoadFromRejectsNegativeTTL(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, `clipboard_clear_seconds = -1
`)
	if _, err := LoadFrom(path); err == nil {
		t.Fatal("expected error for negative TTL")
	}
}

func TestLoadFromMalformed(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, `editor = "unterminated`)
	if _, err := LoadFrom(path); err == nil {
		t.Fatal("expected parse error")
	}
}
