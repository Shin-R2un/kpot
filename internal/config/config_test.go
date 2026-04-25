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

func TestKeychainModeDefaultsAuto(t *testing.T) {
	cfg := Config{}
	if cfg.KeychainMode() != KeychainAuto {
		t.Fatalf("default = %q, want auto", cfg.KeychainMode())
	}
}

func TestKeychainModePassesThroughKnownValues(t *testing.T) {
	for _, v := range []string{KeychainAuto, KeychainAlways, KeychainNever} {
		cfg := Config{Keychain: v}
		if got := cfg.KeychainMode(); got != v {
			t.Errorf("KeychainMode(%q) = %q", v, got)
		}
	}
}

func TestKeychainModeUnknownFallsBackToAuto(t *testing.T) {
	// User-typoed values that slipped past the loader (loader rejects
	// them at parse time, but if we set the field directly we should
	// still be safe).
	cfg := Config{Keychain: "yes"}
	if cfg.KeychainMode() != KeychainAuto {
		t.Fatalf("unknown should fall back to auto, got %q", cfg.KeychainMode())
	}
}

func TestLoadFromAcceptsKeychainValues(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"auto", "always", "never"} {
		path := writeTOML(t, dir, `keychain = "`+v+`"`+"\n")
		cfg, err := LoadFrom(path)
		if err != nil {
			t.Fatalf("keychain=%q: %v", v, err)
		}
		if cfg.Keychain != v {
			t.Errorf("Keychain=%q, want %q", cfg.Keychain, v)
		}
	}
}

func TestLoadFromRejectsBadKeychain(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, `keychain = "sometimes"`+"\n")
	if _, err := LoadFrom(path); err == nil {
		t.Fatal("expected error for unknown keychain value")
	}
}

func TestLoadFromMalformed(t *testing.T) {
	dir := t.TempDir()
	path := writeTOML(t, dir, `editor = "unterminated`)
	if _, err := LoadFrom(path); err == nil {
		t.Fatal("expected parse error")
	}
}
