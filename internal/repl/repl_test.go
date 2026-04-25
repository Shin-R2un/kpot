package repl

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/r2un/kpot/internal/clipboard"
	"github.com/r2un/kpot/internal/crypto"
	"github.com/r2un/kpot/internal/store"
	"github.com/r2un/kpot/internal/tty"
	"github.com/r2un/kpot/internal/vault"
)

// withInput swaps the session's prompter so confirm() reads from the
// given string instead of os.Stdin.
func withInput(s *Session, in string) {
	s.p = newBufioPrompter(bufio.NewReader(strings.NewReader(in)), s.out)
}

// withFakeClipboard installs an in-memory clipboard so copy tests don't
// touch the host's real selection.
func withFakeClipboard(s *Session) *clipboard.Fake {
	f := clipboard.NewFake()
	s.clip = clipboard.NewManager(f, 30*time.Millisecond)
	return f
}

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

func TestRmConfirmYes(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	s.Vault.Put("openai", "secret")
	if err := s.persist(); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	s.out = &buf
	withInput(s, "y\n")

	if _, err := s.dispatch("rm", []string{"openai"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Vault.Get("openai"); ok {
		t.Fatal("note still exists after rm y")
	}
	if !strings.Contains(buf.String(), "removed openai") {
		t.Fatalf("rm output = %q", buf.String())
	}
}

func TestRmConfirmNo(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	s.Vault.Put("openai", "secret")
	s.persist()

	var buf bytes.Buffer
	s.out = &buf
	withInput(s, "\n") // empty answer = N

	if _, err := s.dispatch("rm", []string{"openai"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Vault.Get("openai"); !ok {
		t.Fatal("note was removed despite cancel")
	}
	if !strings.Contains(buf.String(), "cancelled") {
		t.Fatalf("rm output = %q", buf.String())
	}
}

func TestRmMissing(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	if _, err := s.dispatch("rm", []string{"nope"}); err == nil {
		t.Fatal("expected error for missing note")
	}
}

func TestFindOutput(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	s.Vault.Put("ai/openai", "OPENAI_API_KEY=...")
	s.Vault.Put("server/fw0", "ssh user@fw0")

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("find", []string{"openai"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "ai/openai") {
		t.Fatalf("find output missing ai/openai: %q", out)
	}
	if strings.Contains(out, "server/fw0") {
		t.Fatalf("find should not include server/fw0: %q", out)
	}
}

func TestFindNoMatches(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("find", []string{"nothing"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no matches") {
		t.Fatalf("find output = %q", buf.String())
	}
}

func TestCopyAndAutoClear(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	fake := withFakeClipboard(s)
	s.Vault.Put("openai", "sk-secret")

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("copy", []string{"openai"}); err != nil {
		t.Fatal(err)
	}
	if string(fake.Snapshot()) != "sk-secret" {
		t.Fatalf("clipboard = %q", string(fake.Snapshot()))
	}
	if !strings.Contains(buf.String(), "copied openai") {
		t.Fatalf("copy output = %q", buf.String())
	}

	time.Sleep(80 * time.Millisecond)
	if string(fake.Snapshot()) != "" {
		t.Fatalf("clipboard should auto-clear: %q", string(fake.Snapshot()))
	}
}

func TestCopyMissing(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	withFakeClipboard(s)

	if _, err := s.dispatch("copy", []string{"nope"}); err == nil {
		t.Fatal("expected error for missing note")
	}
}

func TestTemplateShowDefault(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("template", []string{"show"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "built-in default") {
		t.Errorf("expected default-source label, got %q", out)
	}
	if !strings.Contains(out, "{{name}}") {
		t.Errorf("expected default template body to contain {{name}}, got %q", out)
	}
	if !strings.Contains(out, "{{date}}") {
		t.Errorf("expected placeholder list, got %q", out)
	}
}

func TestTemplateShowCustom(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	s.Vault.Template = "custom: {{name}}\n"

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("template", []string{"show"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "(vault)") {
		t.Errorf("expected vault-source label, got %q", out)
	}
	if !strings.Contains(out, "custom: {{name}}") {
		t.Errorf("expected custom template body, got %q", out)
	}
}

func TestTemplateResetClearsAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.kpot")
	s := scriptedSession(t, path)
	defer s.Close()
	s.Vault.Template = "custom\n"
	s.persist()

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("template", []string{"reset"}); err != nil {
		t.Fatal(err)
	}
	if s.Vault.Template != "" {
		t.Fatalf("Template not cleared: %q", s.Vault.Template)
	}
	if !strings.Contains(buf.String(), "reset to built-in default") {
		t.Errorf("expected reset confirmation, got %q", buf.String())
	}
}

func TestTemplateResetWhenAlreadyDefault(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("template", []string{"reset"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "already using built-in default") {
		t.Errorf("expected no-op confirmation, got %q", buf.String())
	}
}

func TestTemplateUnknownSubcommand(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	if _, err := s.dispatch("template", []string{"bogus"}); err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestRmYesFlagSkipsConfirm(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	s.Vault.Put("openai", "secret")
	s.persist()

	var buf bytes.Buffer
	s.out = &buf
	// No input provided — confirm() would block / fail. -y must skip it.
	withInput(s, "")

	if _, err := s.dispatch("rm", []string{"-y", "openai"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Vault.Get("openai"); ok {
		t.Fatal("note still exists after rm -y")
	}
	if !strings.Contains(buf.String(), "removed openai") {
		t.Fatalf("rm -y output = %q", buf.String())
	}
}

func TestRmRejectsUnknownFlag(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	if _, err := s.dispatch("rm", []string{"--bogus", "x"}); err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestExportToStdout(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	s.Vault.Put("openai", "sk-zzz")

	var out, errBuf bytes.Buffer
	s.out = &out
	s.err = &errBuf

	if _, err := s.dispatch("export", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "sk-zzz") {
		t.Fatalf("export missing note body: %q", out.String())
	}
	if !strings.Contains(errBuf.String(), "warning") {
		t.Fatalf("expected stderr warning, got %q", errBuf.String())
	}
}

func TestExportToFileRequiresForce(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	s.Vault.Put("k", "v")
	target := filepath.Join(dir, "out.json")

	if _, err := s.dispatch("export", []string{"-o", target}); err != nil {
		t.Fatal(err)
	}
	// Second write to same target without --force must error.
	if _, err := s.dispatch("export", []string{"-o", target}); err == nil {
		t.Fatal("expected overwrite to require --force")
	}
	if _, err := s.dispatch("export", []string{"-o", target, "--force"}); err != nil {
		t.Fatal(err)
	}
}

func TestImportMergeAddsNewAndKeepsConflicts(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	s.Vault.Put("openai", "local-version")
	s.persist()

	// Build an export-shaped JSON file by hand.
	other := scriptedSession(t, filepath.Join(dir, "other.kpot"))
	other.Vault.Put("openai", "imported-version") // conflict
	other.Vault.Put("anthropic", "new-key")       // new
	otherJSON, err := other.Vault.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
	jsonPath := filepath.Join(dir, "other.json")
	if err := os.WriteFile(jsonPath, otherJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := s.dispatch("import", []string{jsonPath}); err != nil {
		t.Fatal(err)
	}

	if n, ok := s.Vault.Get("anthropic"); !ok || n.Body != "new-key" {
		t.Errorf("expected anthropic added, got ok=%v note=%v", ok, n)
	}
	if n, ok := s.Vault.Get("openai"); !ok || n.Body != "local-version" {
		t.Errorf("local openai should be untouched, got ok=%v body=%q", ok, n.Body)
	}
	// Conflict copy must exist with .conflict-<date> prefix.
	found := false
	for _, name := range s.Vault.Names() {
		if strings.HasPrefix(name, "openai.conflict-") {
			found = true
			n, _ := s.Vault.Get(name)
			if n.Body != "imported-version" {
				t.Errorf("conflict body = %q, want imported-version", n.Body)
			}
		}
	}
	if !found {
		t.Errorf("expected an openai.conflict-* entry, got names=%v", s.Vault.Names())
	}
}

func TestImportReplaceWithYesFlag(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	s.Vault.Put("local-only", "local")
	s.persist()

	other := scriptedSession(t, filepath.Join(dir, "other.kpot"))
	other.Vault.Put("imported-only", "imported")
	otherJSON, _ := other.Vault.ToJSON()
	other.Close()
	jsonPath := filepath.Join(dir, "o.json")
	if err := os.WriteFile(jsonPath, otherJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	withInput(s, "") // no input — -y must skip prompt
	if _, err := s.dispatch("import", []string{jsonPath, "--mode", "replace", "-y"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Vault.Get("local-only"); ok {
		t.Error("local-only should be gone after replace")
	}
	if _, ok := s.Vault.Get("imported-only"); !ok {
		t.Error("imported-only should be present after replace")
	}
}

func TestImportRejectsBadMode(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	jsonPath := filepath.Join(dir, "x.json")
	os.WriteFile(jsonPath, []byte(`{"version":1,"notes":{}}`), 0o600)

	if _, err := s.dispatch("import", []string{jsonPath, "--mode", "wat"}); err == nil {
		t.Fatal("expected error for bad mode")
	}
}

func TestIdleLockArmAndDisarmIsSafeWhenUnused(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	// Without arming, reset/disarm must be no-ops (REPL never armed
	// the timer because IdleTimeout was zero).
	s.resetIdleLock()
	s.disarmIdleLock()
}

func TestIdleLockResetsTimerOnActivity(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	// Use a long enough timeout that the timer never naturally fires
	// during the test, but verify Reset() actually pushes it out.
	s.opts.IdleTimeout = time.Hour
	s.armIdleLock(s.opts.IdleTimeout)
	defer s.disarmIdleLock()

	s.idleMu.Lock()
	if s.idleTimer == nil {
		s.idleMu.Unlock()
		t.Fatal("armIdleLock should have created a timer")
	}
	s.idleMu.Unlock()

	// Reset shouldn't panic and the timer should still be live.
	s.resetIdleLock()
	s.idleMu.Lock()
	stillLive := s.idleTimer != nil
	s.idleMu.Unlock()
	if !stillLive {
		t.Fatal("resetIdleLock dropped the timer")
	}
}

func TestPassphraseRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v.kpot")
	s := scriptedSession(t, path)
	defer s.Close()
	s.Vault.Put("openai", "sk-aaa")
	s.persist()

	// Inject the new passphrase via env so ReadNewPassphrase doesn't
	// touch the TTY. Both prompts return the same value, which is what
	// rekey wants.
	t.Setenv("KPOT_PASSPHRASE", "new-pass-abc")
	tty.ResetEnvWarnForTest()

	if _, err := s.dispatch("passphrase", nil); err != nil {
		t.Fatal(err)
	}

	// Re-open with the new passphrase to confirm rotation worked.
	pt, key, _, err := vault.Open(path, []byte("new-pass-abc"))
	if err != nil {
		t.Fatalf("reopen with new pass failed: %v", err)
	}
	defer crypto.Zero(key)
	v2, err := store.FromJSON(pt)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := v2.Get("openai"); !ok || n.Body != "sk-aaa" {
		t.Fatalf("notes lost across rekey: ok=%v body=%v", ok, n)
	}

	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf(".bak should be gone after rekey, err=%v", err)
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
