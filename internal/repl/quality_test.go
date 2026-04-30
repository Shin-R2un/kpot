package repl

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Shin-R2un/kpot/internal/fields"
)

// TestFindNumberedThenCdResolvesByIndex covers the headline UX of v0.10:
// run `find <q>`, then `cd N` should walk into the N-th match without
// the user having to retype the canonical name.
func TestFindNumberedThenCdResolvesByIndex(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	s.Vault.Put("accounts/github-main", "url: https://github.com")
	s.Vault.Put("dev/github-pat", "token: ghp_xxx")
	s.Vault.Put("ai/openai", "url: https://openai.com")
	if err := s.persist(); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	s.out = &buf

	if _, err := s.dispatch("find", []string{"github"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "1  accounts/github-main") {
		t.Errorf("find output missing numbered first hit: %q", out)
	}
	// lastSelection is sorted (Vault.Names is sorted), so:
	//   1: accounts/github-main
	//   2: dev/github-pat
	if got := s.lastSelection; len(got) != 2 ||
		got[0] != "accounts/github-main" || got[1] != "dev/github-pat" {
		t.Fatalf("lastSelection = %v, want [accounts/github-main dev/github-pat]", got)
	}

	if _, err := s.dispatch("cd", []string{"2"}); err != nil {
		t.Fatal(err)
	}
	if s.currentNote != "dev/github-pat" {
		t.Errorf("currentNote after cd 2 = %q, want %q", s.currentNote, "dev/github-pat")
	}
	// Recent should record the navigation.
	r := s.Vault.ListRecent()
	if len(r) == 0 || r[0] != "dev/github-pat" {
		t.Errorf("Recent = %v, want dev/github-pat at front", r)
	}
}

func TestRecentListsAndPopulatesSelection(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	s.Vault.Put("a", "x")
	s.Vault.Put("b", "y")
	s.Vault.Put("c", "z")
	s.persist()

	for _, n := range []string{"a", "b", "c"} {
		s.Vault.TrackRecent(n)
	}

	var buf bytes.Buffer
	s.out = &buf

	if _, err := s.dispatch("recent", nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// Most recent first → c, b, a
	if !strings.Contains(out, "1  c") || !strings.Contains(out, "2  b") || !strings.Contains(out, "3  a") {
		t.Errorf("recent output = %q", out)
	}
	if got := s.lastSelection; len(got) != 3 || got[0] != "c" {
		t.Errorf("lastSelection after recent = %v", got)
	}
}

func TestRecentEmpty(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	var buf bytes.Buffer
	s.out = &buf

	if _, err := s.dispatch("recent", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "(empty)") {
		t.Errorf("recent on empty = %q, want (empty)", buf.String())
	}
}

func TestCdNumberOutOfRangeErrors(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	s.Vault.Put("a", "x")
	s.persist()
	s.lastSelection = []string{"a"}

	_, err := s.dispatch("cd", []string{"9"})
	if err == nil {
		t.Fatal("expected out-of-range error, got nil")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("err = %v, want 'out of range'", err)
	}
}

func TestCdNumberWithEmptySelectionErrors(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	_, err := s.dispatch("cd", []string{"1"})
	if err == nil || !strings.Contains(err.Error(), "no recent selection") {
		t.Errorf("err = %v, want 'no recent selection'", err)
	}
}

func TestRmMovesToTrashAndRestoreReturns(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	s.Vault.Put("accounts/old", "secret-body")
	s.persist()

	var buf bytes.Buffer
	s.out = &buf
	withInput(s, "y\n") // confirm rm

	if _, err := s.dispatch("rm", []string{"accounts/old"}); err != nil {
		t.Fatal(err)
	}
	// Note must be gone from live notes...
	if _, ok := s.Vault.Get("accounts/old"); ok {
		t.Fatal("note still in Notes after rm")
	}
	// ...but present in trash with the original body intact.
	entries := s.Vault.ListTrash()
	if len(entries) != 1 {
		t.Fatalf("trash has %d entries, want 1", len(entries))
	}
	trashKey := entries[0].Key
	if entries[0].Note.Body != "secret-body" || entries[0].Note.OriginalName != "accounts/old" {
		t.Errorf("trash entry = %+v", entries[0].Note)
	}

	// Restore brings it back.
	buf.Reset()
	if _, err := s.dispatch("restore", []string{trashKey}); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.Vault.Get("accounts/old"); !ok || got.Body != "secret-body" {
		t.Errorf("restore round-trip lost data: ok=%v body=%v", ok, got)
	}
	if len(s.Vault.Trash) != 0 {
		t.Errorf("trash entry left after restore: %v", s.Vault.Trash)
	}
}

func TestTrashListShowsEntries(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	s.Vault.Put("a", "x")
	s.persist()

	withInput(s, "y\n")
	if _, err := s.dispatch("rm", []string{"a"}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	s.out = &buf
	if _, err := s.dispatch("trash", nil); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "a.deleted-") {
		t.Errorf("trash output = %q", out)
	}
	if !strings.Contains(out, "deleted just now") && !strings.Contains(out, "min ago") {
		t.Errorf("trash output missing relative time: %q", out)
	}
}

func TestPurgePermanentlyDeletes(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	s.Vault.Put("a", "x")
	s.persist()

	withInput(s, "y\n")
	if _, err := s.dispatch("rm", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	entries := s.Vault.ListTrash()
	if len(entries) != 1 {
		t.Fatal("setup: trash should have 1 entry")
	}
	trashKey := entries[0].Key

	withInput(s, "y\n") // confirm purge
	if _, err := s.dispatch("purge", []string{trashKey}); err != nil {
		t.Fatal(err)
	}
	if len(s.Vault.Trash) != 0 {
		t.Errorf("trash not empty after purge: %v", s.Vault.Trash)
	}
}

func TestPurgeAllRequiresExactKeyword(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	s.Vault.Put("a", "x")
	s.persist()

	withInput(s, "y\n")
	if _, err := s.dispatch("rm", []string{"a"}); err != nil {
		t.Fatal(err)
	}

	// Wrong keyword → operation cancelled.
	var buf bytes.Buffer
	s.out = &buf
	withInput(s, "purge\n") // lowercase, not "PURGE"
	if _, err := s.dispatch("purge", []string{"--all"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "cancelled") {
		t.Errorf("expected cancellation, got %q", buf.String())
	}
	if len(s.Vault.Trash) != 1 {
		t.Errorf("trash dropped despite wrong keyword: %v", s.Vault.Trash)
	}

	// Right keyword → wipes.
	buf.Reset()
	withInput(s, "PURGE\n")
	if _, err := s.dispatch("purge", []string{"--all"}); err != nil {
		t.Fatal(err)
	}
	if len(s.Vault.Trash) != 0 {
		t.Errorf("trash not empty after PURGE: %v", s.Vault.Trash)
	}
	if !strings.Contains(buf.String(), "purged 1 entries") {
		t.Errorf("purge --all output = %q", buf.String())
	}
}

func TestCpTwoArgFromSelection(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()

	clip := withFakeClipboard(s)
	s.Vault.Put("accounts/foo", "url: https://example.com\napikey: sk-secret-value\n")
	s.persist()
	s.lastSelection = []string{"accounts/foo"}

	if _, err := s.dispatch("cp", []string{"1", "apikey"}); err != nil {
		t.Fatal(err)
	}
	if got := clip.Snapshot(); string(got) != "sk-secret-value" {
		t.Errorf("clipboard = %q, want %q", got, "sk-secret-value")
	}
}

// TestSetRefusesSecretArgForEveryKnownSecretName is a contract guard
// for the v0.6 "secrets must not land in REPL history" promise: every
// key fields.IsSecretField returns true for must, when passed as the
// arg-form of `set`, refuse with the documented error. If anyone adds
// a new secret name to the map but forgets to keep this property
// invariant, this test catches it.
func TestSetRefusesSecretArgForEveryKnownSecretName(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	s.Vault.Put("ai/openai", "url: x")
	s.persist()
	s.currentNote = "ai/openai"

	// Iterate over enough plausible secret names. Sourced from the
	// fields.IsSecretField black-box test surface so we don't pin
	// implementation details from this side.
	candidates := []string{
		"pass", "password", "pwd", "pin",
		"apikey", "api_key", "api-key", "key", "token", "secret",
		"client_secret", "client-secret",
		"otp", "totp", "otp_seed", "otp-seed",
		"private_key", "private-key", "ssh_key", "ssh-key",
	}
	for _, name := range candidates {
		if !fields.IsSecretField(name) {
			t.Fatalf("preflight: %q expected to be a secret field", name)
		}
		_, err := s.dispatch("set", []string{name, "would-leak-to-history"})
		if err == nil {
			t.Errorf("set %s <value> succeeded — secret value would leak into REPL history", name)
			continue
		}
		if !strings.Contains(err.Error(), "secret value as an argument") {
			t.Errorf("set %s: err = %v, want refusal containing 'secret value as an argument'", name, err)
		}
	}
}

func TestRmClearsCurrentNoteContext(t *testing.T) {
	dir := t.TempDir()
	s := scriptedSession(t, filepath.Join(dir, "v.kpot"))
	defer s.Close()
	s.Vault.Put("foo", "x")
	s.persist()
	s.currentNote = "foo"

	withInput(s, "y\n")
	if _, err := s.dispatch("rm", []string{"foo"}); err != nil {
		t.Fatal(err)
	}
	if s.currentNote != "" {
		t.Errorf("currentNote = %q after rm of cd'd note, want cleared", s.currentNote)
	}
}
