package repl

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sampleNote is the canonical body used by most context tests. It
// includes a title heading, a contiguous field block, and a `## memo`
// section so we exercise both field parsing and `set` insertion.
const sampleNote = `# ai/openai

id: shin@example.com
url: https://platform.openai.com
apikey: sk-xxxx
pass: hunter2

## memo
OpenClaw用
`

func seedContextVault(t *testing.T, s *Session) {
	t.Helper()
	if _, err := s.Vault.Put("ai/openai", sampleNote); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Vault.Put("ai/claude", "# ai/claude\n\napikey: ant-xxxx\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Vault.Put("ai/gemini", "# ai/gemini\n\nkey: gem-xxxx\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Vault.Put("server/fw0", "# server/fw0\n\nip: 10.0.0.1\n"); err != nil {
		t.Fatal(err)
	}
	if err := s.persist(); err != nil {
		t.Fatal(err)
	}
}

// --- cd / pwd / prompt ---

func TestCdSetsCurrentNoteOnExactMatch(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	if _, err := s.dispatch("cd", []string{"ai/openai"}); err != nil {
		t.Fatal(err)
	}
	if s.currentNote != "ai/openai" {
		t.Errorf("currentNote = %q, want ai/openai", s.currentNote)
	}
	// Prompt reflects context.
	if got := s.prompt(); !strings.Contains(got, "ai/openai") {
		t.Errorf("prompt = %q, want substring 'ai/openai'", got)
	}
}

func TestPwdReportsCurrentNote(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("pwd", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "/" {
		t.Errorf("pwd at root = %q, want /", got)
	}

	buf.Reset()
	s.dispatch("cd", []string{"ai/openai"})
	s.dispatch("pwd", nil)
	if got := strings.TrimSpace(buf.String()); got != "/ai/openai" {
		t.Errorf("pwd in context = %q, want /ai/openai", got)
	}
}

func TestCdParentClearsContext(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	s.dispatch("cd", []string{"ai/openai"})
	if _, err := s.dispatch("cd", []string{".."}); err != nil {
		t.Fatal(err)
	}
	if s.currentNote != "" {
		t.Errorf("currentNote after cd .. = %q, want empty", s.currentNote)
	}
}

func TestCdRootClearsContext(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	s.dispatch("cd", []string{"ai/openai"})
	if _, err := s.dispatch("cd", []string{"/"}); err != nil {
		t.Fatal(err)
	}
	if s.currentNote != "" {
		t.Errorf("currentNote after cd / = %q, want empty", s.currentNote)
	}
}

func TestCdGroupShowsCandidatesAndDoesNotChangeContext(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("cd", []string{"ai"}); err != nil {
		t.Fatal(err)
	}
	if s.currentNote != "" {
		t.Errorf("group cd should not set context; got %q", s.currentNote)
	}
	out := buf.String()
	if !strings.Contains(out, "is a group") {
		t.Errorf("expected 'is a group' message, got %q", out)
	}
	for _, want := range []string{"ai/openai", "ai/claude", "ai/gemini"} {
		if !strings.Contains(out, want) {
			t.Errorf("group output missing candidate %q: %q", want, out)
		}
	}
}

func TestCdMissingNoteReturnsError(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	_, err := s.dispatch("cd", []string{"unknown"})
	if err == nil {
		t.Fatalf("expected error for cd unknown")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %v, want 'not found'", err)
	}
}

// --- show / read ---

func TestShowCurrentNoteWithoutArg(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	s.dispatch("cd", []string{"ai/openai"})
	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("show", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "OpenClaw用") {
		t.Errorf("show without arg should print body; got %q", buf.String())
	}
}

func TestShowFieldOfCurrentNote(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	s.dispatch("cd", []string{"ai/openai"})
	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("show", []string{"url"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "https://platform.openai.com" {
		t.Errorf("show url = %q, want url value", got)
	}
}

func TestShowNoteByName(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("show", []string{"ai/openai"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "apikey: sk-xxxx") {
		t.Errorf("show ai/openai missing body; got %q", buf.String())
	}
}

func TestReadIsAliasOfShow(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("read", []string{"ai/openai"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "OpenClaw用") {
		t.Errorf("read alias missing body; got %q", buf.String())
	}
}

func TestShowMissingFieldErrors(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	s.dispatch("cd", []string{"ai/openai"})
	_, err := s.dispatch("show", []string{"nonexistent"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected field-not-found error, got %v", err)
	}
}

func TestShowWithoutContextRequiresArg(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	_, err := s.dispatch("show", nil)
	if err == nil || !strings.Contains(err.Error(), "no current note") {
		t.Errorf("expected no-current-note error, got %v", err)
	}
}

// --- fields ---

func TestFieldsListsFieldKeys(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	s.dispatch("cd", []string{"ai/openai"})
	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("fields", nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, k := range []string{"id", "url", "apikey", "pass"} {
		if !strings.Contains(out, k) {
			t.Errorf("fields output missing %q: %q", k, out)
		}
	}
}

func TestFieldsWithoutCurrentNoteShowsHint(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("fields", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No current note") {
		t.Errorf("fields without context = %q, want hint", buf.String())
	}
}

// --- cp ---

func TestCpFieldOfCurrentNote(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	clip := withFakeClipboard(s)
	s.dispatch("cd", []string{"ai/openai"})

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("cp", []string{"apikey"}); err != nil {
		t.Fatal(err)
	}
	if got := string(clip.Snapshot()); got != "sk-xxxx" {
		t.Errorf("clipboard = %q, want sk-xxxx", got)
	}
	if !strings.Contains(buf.String(), `field "apikey"`) {
		t.Errorf("cp announce should mention field; got %q", buf.String())
	}
}

func TestCpCurrentNoteWithoutArg(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	clip := withFakeClipboard(s)
	s.dispatch("cd", []string{"ai/openai"})

	if _, err := s.dispatch("cp", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(clip.Snapshot()), "OpenClaw用") {
		t.Errorf("cp without arg should copy whole body; got %q", string(clip.Snapshot()))
	}
}

func TestCpNoteByName(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	clip := withFakeClipboard(s)
	if _, err := s.dispatch("cp", []string{"ai/openai"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(clip.Snapshot()), "apikey: sk-xxxx") {
		t.Errorf("cp <note> should copy body; got %q", string(clip.Snapshot()))
	}
}

// --- set / unset ---

func TestSetUpdatesPlainField(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	s.dispatch("cd", []string{"ai/openai"})
	if _, err := s.dispatch("set", []string{"url", "https://api.openai.com"}); err != nil {
		t.Fatal(err)
	}
	n, _ := s.Vault.Get("ai/openai")
	if !strings.Contains(n.Body, "url: https://api.openai.com") {
		t.Errorf("body did not get updated url: %q", n.Body)
	}
}

func TestSetRefusesSecretAsArgument(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	s.dispatch("cd", []string{"ai/openai"})
	_, err := s.dispatch("set", []string{"pass", "hunter2"})
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("expected refusal for secret-as-arg, got %v", err)
	}
}

func TestSetSecretViaPromptUpdates(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	s.dispatch("cd", []string{"ai/openai"})

	// Non-TTY path: promptForFieldValue uses the session prompter,
	// so feeding input via withInput is enough.
	withInput(s, "newsecret\n")
	if _, err := s.dispatch("set", []string{"apikey"}); err != nil {
		t.Fatal(err)
	}
	n, _ := s.Vault.Get("ai/openai")
	if !strings.Contains(n.Body, "apikey: newsecret") {
		t.Errorf("apikey not updated via prompt: %q", n.Body)
	}
}

func TestSetInsertsMissingField(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	s.dispatch("cd", []string{"ai/openai"})
	if _, err := s.dispatch("set", []string{"newfield", "newvalue"}); err != nil {
		t.Fatal(err)
	}
	n, _ := s.Vault.Get("ai/openai")
	if !strings.Contains(n.Body, "newfield: newvalue") {
		t.Errorf("newfield not inserted: %q", n.Body)
	}
}

func TestUnsetRemovesField(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	s.dispatch("cd", []string{"ai/openai"})
	if _, err := s.dispatch("unset", []string{"apikey"}); err != nil {
		t.Fatal(err)
	}
	n, _ := s.Vault.Get("ai/openai")
	if strings.Contains(n.Body, "apikey:") {
		t.Errorf("apikey line not removed: %q", n.Body)
	}
}

func TestUnsetMissingFieldErrors(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	s.dispatch("cd", []string{"ai/openai"})
	_, err := s.dispatch("unset", []string{"nonexistent"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

// --- vault state coherence ---

// TestSetPreservesCreatedAt locks in store.Put's invariant that
// updating an existing note's body via `set` does not bump its
// CreatedAt timestamp. If store.Put's behavior ever changes,
// silent CreatedAt drift would be a real footgun for users
// relying on `created:` for record-keeping.
func TestSetPreservesCreatedAt(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	original, _ := s.Vault.Get("ai/openai")
	createdBefore := original.CreatedAt
	updatedBefore := original.UpdatedAt

	// Sleep enough for UpdatedAt to differ measurably even on
	// systems with coarse clock resolution.
	time.Sleep(2 * time.Millisecond)

	s.dispatch("cd", []string{"ai/openai"})
	if _, err := s.dispatch("set", []string{"url", "https://api.openai.com"}); err != nil {
		t.Fatal(err)
	}

	after, _ := s.Vault.Get("ai/openai")
	if !after.CreatedAt.Equal(createdBefore) {
		t.Errorf("CreatedAt drifted: before=%v after=%v", createdBefore, after.CreatedAt)
	}
	if !after.UpdatedAt.After(updatedBefore) {
		t.Errorf("UpdatedAt did not advance: before=%v after=%v", updatedBefore, after.UpdatedAt)
	}
}

// TestShowAfterNoteVanishedClearsContext exercises the "note
// disappeared between cd and the next command" branch in show.
// The same branch exists in cp/set/unset/fields — covering one
// is enough to prove the pattern works; if it ever breaks the
// in-memory test would catch it.
func TestShowAfterNoteVanishedClearsContext(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	s.dispatch("cd", []string{"ai/openai"})
	if s.currentNote != "ai/openai" {
		t.Fatalf("setup: cd did not set context")
	}

	// Simulate an external (or concurrent) deletion: bypass the
	// REPL command and remove the note directly. This is what
	// would happen in a future split-vault world or via another
	// process modifying the file underneath us.
	if err := s.Vault.Delete("ai/openai"); err != nil {
		t.Fatalf("setup: Delete returned error: %v", err)
	}

	_, err := s.dispatch("show", nil)
	if err == nil {
		t.Fatal("expected error from show after note vanished, got nil")
	}
	if !strings.Contains(err.Error(), "vanished") {
		t.Errorf("error = %v, want substring 'vanished'", err)
	}
	if s.currentNote != "" {
		t.Errorf("context not cleared after vanish: currentNote=%q", s.currentNote)
	}
}

// --- backward compat ---

func TestCopyWithExplicitNameStillWorks(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	seedContextVault(t, s)

	clip := withFakeClipboard(s)
	if _, err := s.dispatch("copy", []string{"ai/openai"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(clip.Snapshot()), "apikey: sk-xxxx") {
		t.Errorf("legacy copy broken: %q", string(clip.Snapshot()))
	}
}
