package fields

import (
	"strings"
	"testing"
)

const sample = `# ai/openai

id: shin@example.com
url: https://platform.openai.com
apikey: sk-xxxx
pass: hunter2
token: abcdef

## memo
OpenClaw用
`

func TestParseExtractsAllFields(t *testing.T) {
	got := Parse(sample)
	want := []string{"id", "url", "apikey", "pass", "token"}
	if len(got) != len(want) {
		t.Fatalf("Parse: got %d fields, want %d (got=%+v)", len(got), len(want), got)
	}
	for i, f := range got {
		if f.Key != want[i] {
			t.Errorf("Parse[%d].Key = %q, want %q", i, f.Key, want[i])
		}
	}
}

func TestParseSkipsFrontmatter(t *testing.T) {
	body := `---
created: 2026-04-28T10:00:00Z
updated: 2026-04-28T10:00:00Z
---

# n

url: https://example.com
`
	got := Parse(body)
	if len(got) != 1 || got[0].Key != "url" {
		t.Fatalf("frontmatter not skipped: got=%+v", got)
	}
}

func TestParseSkipsCodeFences(t *testing.T) {
	body := "# n\n\nurl: https://example.com\n\n```\nfake: not-a-field\n```\n"
	got := Parse(body)
	if len(got) != 1 || got[0].Key != "url" {
		t.Fatalf("code fence not skipped: got=%+v", got)
	}
}

func TestParseIgnoresListBullets(t *testing.T) {
	// `- id: x` should NOT register as a field — kpot's default
	// template uses bullets and we don't want the legacy template
	// bodies to suddenly yield "fields".
	body := "# n\n\n- id: shin\n- url: https://example.com\n"
	got := Parse(body)
	if len(got) != 0 {
		t.Fatalf("list bullets parsed as fields: got=%+v", got)
	}
}

func TestGet(t *testing.T) {
	if v, ok := Get(sample, "url"); !ok || v != "https://platform.openai.com" {
		t.Errorf("Get(url) = %q,%v want https://platform.openai.com,true", v, ok)
	}
	if v, ok := Get(sample, "URL"); !ok || v != "https://platform.openai.com" {
		t.Errorf("Get(URL) (case-insensitive) = %q,%v want https://platform.openai.com,true", v, ok)
	}
	if _, ok := Get(sample, "nonexistent"); ok {
		t.Errorf("Get(nonexistent) returned ok=true")
	}
}

func TestSetUpdatesInPlace(t *testing.T) {
	out := Set(sample, "url", "https://api.openai.com")
	v, ok := Get(out, "url")
	if !ok || v != "https://api.openai.com" {
		t.Errorf("after Set: Get(url) = %q,%v", v, ok)
	}
	// Other fields untouched.
	if v, _ := Get(out, "apikey"); v != "sk-xxxx" {
		t.Errorf("Set leaked into apikey: %q", v)
	}
	// Field count unchanged.
	if got := len(Parse(out)); got != 5 {
		t.Errorf("Set on existing field changed field count: %d (want 5)", got)
	}
}

func TestSetInsertsAfterFieldBlock(t *testing.T) {
	out := Set(sample, "newfield", "newvalue")
	v, ok := Get(out, "newfield")
	if !ok || v != "newvalue" {
		t.Fatalf("Set(newfield) not retrievable: %q,%v", v, ok)
	}
	// The new line should land after `token: abcdef` and before
	// the blank line preceding `## memo`.
	lines := strings.Split(out, "\n")
	tokenLine := -1
	memoLine := -1
	newLine := -1
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "token:"):
			tokenLine = i
		case strings.HasPrefix(l, "## memo"):
			memoLine = i
		case strings.HasPrefix(l, "newfield:"):
			newLine = i
		}
	}
	if newLine < 0 || tokenLine < 0 || memoLine < 0 {
		t.Fatalf("expected to find token/memo/newfield lines (got token=%d memo=%d new=%d)", tokenLine, memoLine, newLine)
	}
	if !(tokenLine < newLine && newLine < memoLine) {
		t.Errorf("newfield landed at line %d; want between token (%d) and memo (%d)", newLine, tokenLine, memoLine)
	}
}

func TestSetInsertsAfterTitleWhenNoFields(t *testing.T) {
	body := "# n\n\n## memo\nbody only\n"
	out := Set(body, "url", "https://example.com")
	v, ok := Get(out, "url")
	if !ok || v != "https://example.com" {
		t.Fatalf("Set on body-without-fields failed: %q,%v", v, ok)
	}
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], "# n") {
		t.Errorf("title moved (line 0=%q)", lines[0])
	}
}

func TestSetPreservesKeyCase(t *testing.T) {
	body := "# n\n\nURL: https://old.example.com\n"
	out := Set(body, "url", "https://new.example.com")
	if !strings.Contains(out, "URL: https://new.example.com") {
		t.Errorf("expected key case preserved (URL), got: %q", out)
	}
	if strings.Contains(out, "url: https://new.example.com") {
		t.Errorf("Set lower-cased the existing key: %q", out)
	}
}

func TestUnsetRemovesLine(t *testing.T) {
	out := Unset(sample, "apikey")
	if _, ok := Get(out, "apikey"); ok {
		t.Errorf("Unset(apikey): still findable in result")
	}
	// Other fields intact.
	if _, ok := Get(out, "url"); !ok {
		t.Errorf("Unset(apikey) clobbered url")
	}
	if got := len(Parse(out)); got != 4 {
		t.Errorf("Unset: got %d fields, want 4", got)
	}
}

func TestUnsetMissingFieldNoOp(t *testing.T) {
	out := Unset(sample, "nope")
	if out != sample {
		t.Errorf("Unset(nonexistent) modified body")
	}
}

func TestNamesOrder(t *testing.T) {
	got := Names(sample)
	want := []string{"id", "url", "apikey", "pass", "token"}
	if len(got) != len(want) {
		t.Fatalf("Names: got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Names[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestIsSecretField(t *testing.T) {
	cases := map[string]bool{
		"pass":          true,
		"PASSWORD":      true,
		"pwd":           true,
		"apikey":        true,
		"api_key":       true,
		"api-key":       true,
		"key":           true,
		"token":         true,
		"secret":        true,
		"client_secret": true,
		"client-secret": true,
		"url":           false,
		"id":            false,
		"email":         false,
		"":              false,
	}
	for k, want := range cases {
		if got := IsSecretField(k); got != want {
			t.Errorf("IsSecretField(%q) = %v, want %v", k, got, want)
		}
	}
}
