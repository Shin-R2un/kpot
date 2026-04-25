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

func TestPickEditorPrecedence(t *testing.T) {
	t.Setenv("EDITOR", "env-editor")
	t.Setenv("VISUAL", "visual-editor")

	t.Run("env wins when Default unset", func(t *testing.T) {
		Default = ""
		bin, _, err := pickEditor()
		if err != nil {
			t.Fatal(err)
		}
		if bin != "env-editor" {
			t.Errorf("bin = %q, want env-editor", bin)
		}
	})

	t.Run("Default beats env", func(t *testing.T) {
		Default = "config-editor --wait"
		t.Cleanup(func() { Default = "" })
		bin, args, err := pickEditor()
		if err != nil {
			t.Fatal(err)
		}
		if bin != "config-editor" {
			t.Errorf("bin = %q, want config-editor", bin)
		}
		if len(args) != 1 || args[0] != "--wait" {
			t.Errorf("args = %v, want [--wait]", args)
		}
	})

	t.Run("Default whitespace falls through to env", func(t *testing.T) {
		Default = "   "
		t.Cleanup(func() { Default = "" })
		bin, _, err := pickEditor()
		if err != nil {
			t.Fatal(err)
		}
		if bin != "env-editor" {
			t.Errorf("bin = %q, want env-editor", bin)
		}
	})
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"":           "note",
		"openai":     "openai",
		"ai/openai":  "ai-openai",
		"with space": "withspace",
		"a$b#c":      "abc",
		"こんにちは":      "note",
		"verylongnamethatdefinitelyexceedsthirtytwocharsmore": "verylongnamethatdefinitelyexceed",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
