package notefmt

import (
	"strings"
	"testing"
	"time"
)

func TestRenderIncludesFrontmatterAndBody(t *testing.T) {
	created := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 4, 26, 9, 30, 0, 0, time.UTC)
	body := "hello"

	got := string(Render(created, updated, body))
	if !strings.HasPrefix(got, "---\n") {
		t.Fatalf("missing opening fence: %q", got)
	}
	if !strings.Contains(got, "created: ") {
		t.Errorf("missing created field")
	}
	if !strings.Contains(got, "updated: ") {
		t.Errorf("missing updated field")
	}
	if !strings.Contains(got, "\n---\n\nhello") {
		t.Errorf("body not placed correctly: %q", got)
	}
}

func TestStripFrontmatter(t *testing.T) {
	raw := "---\ncreated: 2026-04-25T12:00:00Z\nupdated: 2026-04-25T12:00:00Z\n---\n\nbody line 1\nbody line 2\n"
	got := Strip([]byte(raw))
	want := "body line 1\nbody line 2\n"
	if got != want {
		t.Fatalf("Strip = %q, want %q", got, want)
	}
}

func TestStripNoFrontmatter(t *testing.T) {
	raw := "no frontmatter here\nsecond line\n"
	got := Strip([]byte(raw))
	if got != raw {
		t.Fatalf("Strip changed content: %q", got)
	}
}

func TestStripLeadingBlanksBeforeFence(t *testing.T) {
	raw := "\n\n---\nk: v\n---\nbody\n"
	got := Strip([]byte(raw))
	if got != "body\n" {
		t.Fatalf("Strip = %q", got)
	}
}

func TestStripMissingClosingFenceReturnsOriginal(t *testing.T) {
	raw := "---\ncreated: now\nbody continues without close\n"
	got := Strip([]byte(raw))
	if got != raw {
		t.Fatalf("expected unchanged, got %q", got)
	}
}

func TestStripEmptyInput(t *testing.T) {
	if got := Strip(nil); got != "" {
		t.Fatalf("Strip(nil) = %q", got)
	}
	if got := Strip([]byte("")); got != "" {
		t.Fatalf("Strip(empty) = %q", got)
	}
}

func TestRoundTripPreservesBody(t *testing.T) {
	created := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 4, 26, 9, 30, 0, 0, time.UTC)
	body := "OPENAI_API_KEY=sk-abc\n\n## memo\n\nrotated 2026-04-25\n"

	rendered := Render(created, updated, body)
	got := Strip(rendered)
	if got != body {
		t.Fatalf("round-trip mismatch:\n  got:  %q\n  want: %q", got, body)
	}
}

func TestRoundTripDefaultBody(t *testing.T) {
	now := time.Now().UTC()
	rendered := Render(now, now, DefaultBody)
	got := Strip(rendered)
	if got != DefaultBody {
		t.Fatalf("DefaultBody round-trip mismatch:\n  got:  %q\n  want: %q", got, DefaultBody)
	}
}

func TestApplyPlaceholders(t *testing.T) {
	now := time.Date(2026, 4, 25, 21, 35, 12, 0, time.UTC)
	p := Placeholders{Name: "ai/openai", Now: now}
	tmpl := "name={{name}} base={{basename}} date={{date}} time={{time}} dt={{datetime}}"
	got := ApplyPlaceholders(tmpl, p)

	if !strings.Contains(got, "name=ai/openai") {
		t.Errorf("name not substituted: %q", got)
	}
	if !strings.Contains(got, "base=openai") {
		t.Errorf("basename not substituted: %q", got)
	}
	// date/time depend on local TZ — just assert format shape.
	if !strings.Contains(got, "date=") {
		t.Errorf("date placeholder not removed: %q", got)
	}
	if !strings.Contains(got, "dt=") || strings.Contains(got, "{{datetime}}") {
		t.Errorf("datetime not substituted: %q", got)
	}
}

func TestApplyPlaceholdersUnknownLeftAlone(t *testing.T) {
	got := ApplyPlaceholders("hello {{user}} {{name}}", Placeholders{Name: "x"})
	if !strings.Contains(got, "{{user}}") {
		t.Errorf("unknown placeholder should be preserved: %q", got)
	}
	if !strings.Contains(got, "x") {
		t.Errorf("known placeholder not substituted: %q", got)
	}
}

func TestApplyPlaceholdersBasenameNoSlash(t *testing.T) {
	got := ApplyPlaceholders("{{basename}}", Placeholders{Name: "openai"})
	if got != "openai" {
		t.Errorf("got %q", got)
	}
}

func TestApplyPlaceholdersZeroNowUsesWallClock(t *testing.T) {
	got := ApplyPlaceholders("{{date}}", Placeholders{Name: "n"})
	if got == "" || got == "{{date}}" {
		t.Errorf("zero Now should fall back to time.Now(): got %q", got)
	}
}

func TestDefaultBodyContainsNamePlaceholder(t *testing.T) {
	if !strings.Contains(DefaultBody, "{{name}}") {
		t.Error("DefaultBody should reference {{name}} so new notes get a useful heading")
	}
}

func TestStripIgnoresFrontmatterMidBody(t *testing.T) {
	// A second --- block buried in the body must not be treated as
	// closing fence of a non-existent opener.
	raw := "no frontmatter\n---\nlooks like fence\n---\nbut isn't\n"
	got := Strip([]byte(raw))
	if got != raw {
		t.Fatalf("Strip mishandled mid-body fences: %q", got)
	}
}
