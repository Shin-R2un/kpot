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
		"openai":         {"openai", false},
		"OpenAI":         {"openai", false},
		"  openai  ":     {"openai", false},
		"ai/openai":      {"ai/openai", false},
		"server/fw0.tld": {"server/fw0.tld", false},
		"a-b_c.d":        {"a-b_c.d", false},
		"":               {"", true},
		"/leading":       {"", true},
		"trailing/":      {"", true},
		"a//b":           {"", true},
		"has space":      {"", true},
		"日本語":            {"", true},
		"a$b":            {"", true},
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
