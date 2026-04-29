package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// withTempVaultDir installs a fake home directory for the duration of
// t and returns the resolved <home>/.kpot path. Both HOME (Unix) and
// USERPROFILE (Windows) are pinned because Go's os.UserHomeDir
// consults different env vars per platform — pinning only HOME passes
// on Linux/macOS and silently leaks the runner's real home on Windows.
func withTempVaultDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	return filepath.Join(dir, ".kpot")
}

func TestResolveVault_EmptyArgAndNoDefault(t *testing.T) {
	_, err := ResolveVault("", Config{})
	if err == nil {
		t.Fatal("expected error when no arg and no default_vault")
	}
	if !strings.Contains(err.Error(), "no vault specified") {
		t.Errorf("error message = %q, want substring 'no vault specified'", err.Error())
	}
}

func TestResolveVault_EmptyArgWithDefault(t *testing.T) {
	dir := withTempVaultDir(t)
	got, err := ResolveVault("", Config{DefaultVault: "personal"})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "personal.kpot")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveVault_AbsolutePath(t *testing.T) {
	got, err := ResolveVault("/abs/path/to/v.kpot", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "/abs/path/to/v.kpot" {
		t.Errorf("got %q, want passthrough", got)
	}
}

func TestResolveVault_PathLikeRelative(t *testing.T) {
	// Anything containing / passes through — no .kpot appended,
	// no vault_dir applied. The user told us where to look.
	got, err := ResolveVault("../neighbour/v.kpot", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "../neighbour/v.kpot" {
		t.Errorf("got %q, want passthrough", got)
	}
}

func TestResolveVault_PathLikePreservedEvenWithoutSuffix(t *testing.T) {
	// `dir/vault` (no .kpot) is still treated as a path: user gave
	// us a directory hint, don't append .kpot or vault-dir prefix.
	got, err := ResolveVault("dir/vault", Config{VaultDir: "/tmp/kp"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "dir/vault" {
		t.Errorf("got %q, want passthrough", got)
	}
}

func TestResolveVault_AppendsKpotSuffix(t *testing.T) {
	withTempVaultDir(t)
	got, err := ResolveVault("personal", Config{})
	if err != nil {
		t.Fatal(err)
	}
	// filepath.Base() so the assertion holds on Windows (\) too.
	if filepath.Base(got) != "personal.kpot" {
		t.Errorf("got %q, want basename 'personal.kpot'", got)
	}
}

func TestResolveVault_PrefersCWDFileWhenSuffixExplicit(t *testing.T) {
	// User typed `kpot personal.kpot` (suffix explicit) and there's
	// a matching file in CWD → use it. This is the legacy
	// `cd /repo && kpot vault.kpot` workflow.
	withTempVaultDir(t)
	cwd, err := os.MkdirTemp("", "kpot-cwd-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cwd)
	fake := filepath.Join(cwd, "personal.kpot")
	if err := os.WriteFile(fake, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	defer os.Chdir(prev)
	os.Chdir(cwd)

	got, err := ResolveVault("personal.kpot", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "personal.kpot" {
		t.Errorf("got %q, want CWD file 'personal.kpot'", got)
	}
}

func TestResolveVault_BareNameSkipsCWDPhishingDefense(t *testing.T) {
	// Critical security regression test:
	// User typed `kpot personal` (BARE name) — must NOT pick up a
	// `personal.kpot` shipped by an unrelated repo in the user's
	// CWD. That would let a cloned project shadow the real vault.
	dir := withTempVaultDir(t)
	cwd, err := os.MkdirTemp("", "kpot-malicious-cwd-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(cwd)
	bait := filepath.Join(cwd, "personal.kpot")
	if err := os.WriteFile(bait, []byte("attacker-controlled"), 0o600); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	defer os.Chdir(prev)
	os.Chdir(cwd)

	got, err := ResolveVault("personal", Config{})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "personal.kpot")
	if got != want {
		t.Errorf("bare-name resolution leaked into CWD: got %q, want %q", got, want)
	}
}

func TestResolveVault_ExpandsTildeInCLIArg(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	got, err := ResolveVault("~/vaults/work.kpot", Config{})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "vaults/work.kpot")
	if got != want {
		t.Errorf("got %q, want %q (tilde-expanded path-like arg)", got, want)
	}
}

func TestResolveVault_FallsBackToVaultDir(t *testing.T) {
	dir := withTempVaultDir(t)
	got, err := ResolveVault("work", Config{})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "work.kpot")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveVault_ExplicitVaultDir(t *testing.T) {
	custom := t.TempDir()
	got, err := ResolveVault("alt", Config{VaultDir: custom})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(custom, "alt.kpot")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveVault_KpotSuffixNotDoubled(t *testing.T) {
	withTempVaultDir(t)
	// `kpot vault.kpot` (bare name with .kpot suffix already) must
	// NOT become `vault.kpot.kpot`.
	got, err := ResolveVault("vault.kpot", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(got, ".kpot.kpot") {
		t.Errorf("double suffix in %q", got)
	}
}

func TestResolveVault_TrimsWhitespace(t *testing.T) {
	withTempVaultDir(t)
	got, err := ResolveVault("  personal  ", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "personal.kpot" {
		t.Errorf("got %q, want basename 'personal.kpot'", got)
	}
}

// --- LoadFrom: ~ expansion in vault_dir ---

func TestLoadFrom_ExpandsTildeInVaultDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`vault_dir = "~/secrets"`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "secrets")
	if cfg.VaultDir != want {
		t.Errorf("vault_dir = %q, want %q", cfg.VaultDir, want)
	}
}

func TestLoadFrom_AbsoluteVaultDirPassesThrough(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	cfgPath := filepath.Join(dir, "config.toml")
	// Use a Windows-friendly absolute path: starting with /
	// works on Unix, and on Windows it stays the same string
	// because expandHome only acts on `~/` prefixes.
	if err := os.WriteFile(cfgPath, []byte(`vault_dir = "/srv/kpot"`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VaultDir != "/srv/kpot" {
		t.Errorf("vault_dir = %q, want /srv/kpot", cfg.VaultDir)
	}
}

// --- EnsureVaultDir ---

func TestEnsureVaultDirCreatesParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics don't apply on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "sub", "vault.kpot")
	if err := EnsureVaultDir(target); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(target)
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		t.Fatalf("parent dir not created: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("permissions = %o, want 0700 (owner-only)", got)
	}
}

func TestEnsureVaultDirTightensExistingDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics don't apply on Windows")
	}
	// Simulate a pre-existing ~/.kpot/ created by some other tool
	// with a permissive mode (0o755). EnsureVaultDir must clamp it
	// back to 0o700 so other users on the box can't enumerate
	// vault filenames.
	dir := t.TempDir()
	parent := filepath.Join(dir, "kp")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	// Force the actual mode bits even if umask masked some.
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureVaultDir(filepath.Join(parent, "v.kpot")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("permissions after EnsureVaultDir = %o, want 0700 (clamped)", got)
	}
}
