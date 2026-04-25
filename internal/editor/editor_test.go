package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditWithStubEditor(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "stub-editor.sh")
	script := `#!/bin/sh
echo "edited content" > "$1"
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", stub)
	t.Setenv("VISUAL", "")

	got, err := Edit([]byte("initial"), "test/foo")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "edited content" {
		t.Fatalf("got %q", got)
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"":             "note",
		"openai":       "openai",
		"ai/openai":    "ai-openai",
		"with space":   "withspace",
		"a$b#c":        "abc",
		"こんにちは":        "note",
		"verylongnamethatdefinitelyexceedsthirtytwocharsmore": "verylongnamethatdefinitelyexceed",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
