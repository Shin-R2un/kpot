package store

import (
	"bytes"
	"testing"
)

func TestPutGetDelete(t *testing.T) {
	v := New()
	if _, err := v.Put("ai/openai", "key=sk-..."); err != nil {
		t.Fatal(err)
	}
	n, ok := v.Get("ai/openai")
	if !ok {
		t.Fatal("expected note to exist")
	}
	if n.Body != "key=sk-..." {
		t.Fatalf("body = %q", n.Body)
	}
	if err := v.Delete("ai/openai"); err != nil {
		t.Fatal(err)
	}
	if _, ok := v.Get("ai/openai"); ok {
		t.Fatal("expected note to be gone")
	}
}

func TestNormalizeName(t *testing.T) {
	cases := map[string]struct {
		want    string
		wantErr bool
	}{
		// ASCII (legacy contract — must keep working)
		"openai":         {"openai", false},
		"OpenAI":         {"openai", false},
		"  openai  ":     {"openai", false},
		"ai/openai":      {"ai/openai", false},
		"server/fw0.tld": {"server/fw0.tld", false},
		"a-b_c.d":        {"a-b_c.d", false},

		// rejected shapes (unchanged)
		"":          {"", true},
		"/leading":  {"", true},
		"trailing/": {"", true},
		"a//b":      {"", true},
		"has space": {"", true}, // whitespace inside still rejected
		"a$b":       {"", true}, // shell-meaningful chars still rejected

		// non-ASCII letters (now allowed — Japanese / Cyrillic / Greek)
		"日本語":          {"日本語", false},
		"password/のりお": {"password/のりお", false},
		"работа/почта": {"работа/почта", false},
		"alpha/βeta":   {"alpha/βeta", false},
		"login/ω":      {"login/ω", false},

		// emoji / pure symbols still rejected (not Letter/Digit)
		"login/🔑": {"", true},
		"a★b":     {"", true},

		// fullwidth slash that looks like '/' but isn't — reject so
		// hierarchy semantics aren't quietly broken
		"a／b": {"", true},
	}
	for in, c := range cases {
		got, err := NormalizeName(in)
		if c.wantErr {
			if err == nil {
				t.Errorf("NormalizeName(%q) expected error, got %q", in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeName(%q) error: %v", in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeName(%q) = %q, want %q", in, got, c.want)
		}
	}
}

func TestRoundTripJSON(t *testing.T) {
	v := New()
	v.Put("ai/openai", "first")
	v.Put("server/fw0", "ssh ...")

	b, err := v.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	v2, err := FromJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	n1, _ := v2.Get("ai/openai")
	n2, _ := v2.Get("server/fw0")
	if n1.Body != "first" || n2.Body != "ssh ..." {
		t.Fatalf("round-trip mismatch: %+v %+v", n1, n2)
	}
}

// TestToJSONStampsCurrentVersion guards the v1 → v2 auto-upgrade
// contract: any save by a v0.10+ binary writes payload version=2,
// regardless of the version the vault was loaded under. Without this,
// a v1 vault round-tripped through v0.10 would still claim version=1,
// and a downgraded v0.9 binary would happily open it — drop the new
// recent/trash fields silently — and ship secrets out of restorability.
func TestToJSONStampsCurrentVersion(t *testing.T) {
	// Synthesize a vault that *was* a v1 (version=1, no recent/trash).
	v := New()
	v.Version = 1
	v.Put("a", "x")

	b, err := v.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	if v.Version != StoreVersion {
		t.Errorf("in-memory Version = %d, want %d after ToJSON", v.Version, StoreVersion)
	}

	// Re-load and confirm the persisted payload reads back at the
	// current version, not the load-time version.
	v2, err := FromJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if v2.Version != StoreVersion {
		t.Errorf("decoded Version = %d, want %d", v2.Version, StoreVersion)
	}
}

// TestFromJSONRejectsNewerVersion locks the symmetric guard: a
// hypothetical v3 vault is rejected by this binary so a future
// breaking format change can't be silently downgraded.
func TestFromJSONRejectsNewerVersion(t *testing.T) {
	payload := []byte(`{"version":99,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z","notes":{}}`)
	_, err := FromJSON(payload)
	if err == nil {
		t.Fatal("FromJSON accepted version=99, expected version-mismatch error")
	}
}

func TestNamesSorted(t *testing.T) {
	v := New()
	v.Put("zebra", "z")
	v.Put("alpha", "a")
	v.Put("middle", "m")
	got := v.Names()
	want := []string{"alpha", "middle", "zebra"}
	if len(got) != len(want) {
		t.Fatalf("len = %d", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Names()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFind(t *testing.T) {
	v := New()
	v.Put("ai/openai", "OPENAI_API_KEY=sk-zzz")
	v.Put("ai/anthropic", "key=ant-yyy")
	v.Put("server/fw0", "ssh user@fw0\nopenai is mentioned in body")
	v.Put("misc/notes", "nothing relevant")

	cases := []struct {
		query     string
		wantNames []string
	}{
		{"openai", []string{"ai/openai", "server/fw0"}},
		{"OPENAI", []string{"ai/openai", "server/fw0"}},             // case-insensitive
		{"ssh", []string{"server/fw0"}},                             // body only
		{"ai", []string{"ai/anthropic", "ai/openai", "server/fw0"}}, // hits "ai" via name and via "openai" in body
		{"none-here", nil},
		{"   ", nil},
		{"", nil},
	}
	for _, c := range cases {
		got := v.Find(c.query)
		gotNames := make([]string, len(got))
		for i, m := range got {
			gotNames[i] = m.Name
		}
		if len(gotNames) != len(c.wantNames) {
			t.Errorf("Find(%q) names = %v, want %v", c.query, gotNames, c.wantNames)
			continue
		}
		for i := range c.wantNames {
			if gotNames[i] != c.wantNames[i] {
				t.Errorf("Find(%q)[%d] = %q, want %q", c.query, i, gotNames[i], c.wantNames[i])
			}
		}
	}
}

func TestFindFlags(t *testing.T) {
	v := New()
	v.Put("openai", "no body match here at all")
	v.Put("server", "openai mentioned in body")

	matches := v.Find("openai")
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	byName := map[string]Match{}
	for _, m := range matches {
		byName[m.Name] = m
	}
	if !byName["openai"].NameMatch || byName["openai"].BodyMatch {
		t.Errorf("openai: NameMatch=%v BodyMatch=%v", byName["openai"].NameMatch, byName["openai"].BodyMatch)
	}
	if byName["server"].NameMatch || !byName["server"].BodyMatch {
		t.Errorf("server: NameMatch=%v BodyMatch=%v", byName["server"].NameMatch, byName["server"].BodyMatch)
	}
	if byName["server"].Snippet == "" {
		t.Error("server snippet should be populated for body match")
	}
}

func TestTemplateRoundTrip(t *testing.T) {
	v := New()
	v.Template = "# {{name}}\nmy custom template\n"
	v.Put("k", "body")

	b, err := v.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("template")) {
		t.Fatalf("template field missing from JSON: %s", b)
	}

	v2, err := FromJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if v2.Template != v.Template {
		t.Fatalf("template = %q, want %q", v2.Template, v.Template)
	}
}

func TestTemplateOmittedWhenEmpty(t *testing.T) {
	v := New()
	b, err := v.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("\"template\"")) {
		t.Fatalf("empty template should be omitted from JSON: %s", b)
	}
}

func TestPutOverwrites(t *testing.T) {
	v := New()
	v.Put("k", "first")
	v.Put("k", "second")
	n, _ := v.Get("k")
	if n.Body != "second" {
		t.Fatalf("body = %q", n.Body)
	}
	if !bytes.Equal([]byte(n.Body), []byte("second")) {
		t.Fatal("body not updated")
	}
}
