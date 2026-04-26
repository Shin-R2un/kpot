package bundle

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/r2un/kpot/internal/crypto"
	"github.com/r2un/kpot/internal/store"
)

func sampleNotes(t *testing.T) map[string]*Note {
	t.Helper()
	now := time.Now().UTC()
	return map[string]*Note{
		"ai/openai":  {Body: "OPENAI_API_KEY=sk-zzz", CreatedAt: now, UpdatedAt: now},
		"server/fw0": {Body: "ssh user@fw0\nport 22", CreatedAt: now, UpdatedAt: now},
	}
}

func TestBuildOpenRoundTrip(t *testing.T) {
	notes := sampleNotes(t)
	pass := []byte("hunter2")

	blob, err := Build(notes, pass)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(blob, pass)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(notes) {
		t.Fatalf("got %d notes, want %d", len(got), len(notes))
	}
	for name, want := range notes {
		g, ok := got[name]
		if !ok {
			t.Fatalf("missing note %q", name)
		}
		if g.Body != want.Body {
			t.Errorf("note %q body mismatch", name)
		}
	}
}

func TestOpenWrongPassphrase(t *testing.T) {
	blob, err := Build(sampleNotes(t), []byte("right"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(blob, []byte("wrong")); !errors.Is(err, crypto.ErrAuthFailed) {
		t.Fatalf("wrong passphrase should authfail, got %v", err)
	}
}

func TestOpenTamperedBundle(t *testing.T) {
	blob, _ := Build(sampleNotes(t), []byte("p"))
	var hdr Header
	if err := json.Unmarshal(blob, &hdr); err != nil {
		t.Fatal(err)
	}
	// Downgrade Argon2id iterations — should fail AAD check.
	hdr.KDF.Params.Iterations = 1
	tampered, _ := json.MarshalIndent(&hdr, "", "  ")
	if _, err := Open(tampered, []byte("p")); !errors.Is(err, crypto.ErrAuthFailed) {
		t.Fatalf("KDF tamper should authfail, got %v", err)
	}
}

func TestBuildRejectsEmpty(t *testing.T) {
	if _, err := Build(map[string]*Note{}, []byte("p")); err == nil {
		t.Fatal("expected error for empty notes")
	}
	if _, err := Build(sampleNotes(t), nil); err == nil {
		t.Fatal("expected error for nil passphrase")
	}
}

func TestBuildSaltIsRandom(t *testing.T) {
	notes := sampleNotes(t)
	pass := []byte("same-passphrase")

	a, _ := Build(notes, pass)
	b, _ := Build(notes, pass)

	var ha, hb Header
	json.Unmarshal(a, &ha)
	json.Unmarshal(b, &hb)
	if ha.KDF.Salt == hb.KDF.Salt {
		t.Fatal("two builds with same passphrase produced identical salt")
	}
	if ha.WrapNonce == hb.WrapNonce {
		t.Fatal("two builds produced identical wrap_nonce")
	}
	// And the resulting blob must differ byte-for-byte.
	if bytes.Equal(a, b) {
		t.Fatal("bundle builds are deterministic — should be randomized")
	}
}

func TestBundleNotesContainNoPlaintextOnDisk(t *testing.T) {
	const marker = "VERY-SECRET-OPENAI-MARKER-12345"
	notes := map[string]*Note{
		"x": {Body: "OPENAI_API_KEY=" + marker, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	blob, _ := Build(notes, []byte("p"))
	if bytes.Contains(blob, []byte(marker)) {
		t.Fatal("bundle blob leaks plaintext marker")
	}
}

func TestFromStoreNotes(t *testing.T) {
	now := time.Now().UTC()
	src := map[string]*store.Note{
		"a": {Body: "1", CreatedAt: now, UpdatedAt: now},
		"b": {Body: "2", CreatedAt: now, UpdatedAt: now},
		"c": {Body: "3", CreatedAt: now, UpdatedAt: now},
	}

	out, err := FromStoreNotes(src, []string{"a", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d, want 2 notes", len(out))
	}
	if _, ok := out["b"]; ok {
		t.Error("unselected note 'b' should not be in output")
	}
	if out["a"].Body != "1" || out["c"].Body != "3" {
		t.Error("body mismatch in selected notes")
	}
}

func TestFromStoreNotesMissingFails(t *testing.T) {
	src := map[string]*store.Note{"a": {Body: "1"}}
	if _, err := FromStoreNotes(src, []string{"a", "missing"}); err == nil {
		t.Fatal("expected error for missing note")
	}
	if _, err := FromStoreNotes(src, nil); err == nil {
		t.Fatal("expected error for empty selection")
	}
}

func TestSortedNamesIsDeterministic(t *testing.T) {
	notes := map[string]*Note{"zebra": {}, "alpha": {}, "middle": {}}
	for i := 0; i < 5; i++ {
		got := SortedNames(notes)
		want := []string{"alpha", "middle", "zebra"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("sort iteration %d: got %v, want %v", i, got, want)
		}
	}
}

func TestOpenMalformedJSON(t *testing.T) {
	if _, err := Open([]byte("not json"), []byte("p")); err == nil {
		t.Fatal("expected error for non-JSON input")
	}
}

func TestOpenWrongFormat(t *testing.T) {
	hdr := Header{Format: "not-kpot", Version: 1}
	blob, _ := json.Marshal(&hdr)
	if _, err := Open(blob, []byte("p")); err == nil {
		t.Fatal("expected error for wrong format string")
	}
}
