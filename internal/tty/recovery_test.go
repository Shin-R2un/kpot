package tty

import (
	"strings"
	"testing"
)

func TestFormatSeedWords12(t *testing.T) {
	mn := "abandon ability able about above absent absorb abstract absurd abuse access accident"
	out := FormatSeedWords(mn)
	if !strings.Contains(out, " 1. abandon") {
		t.Errorf("missing first word index: %q", out)
	}
	if !strings.Contains(out, "12. accident") {
		t.Errorf("missing 12th word: %q", out)
	}
	// 12 words / 4 per row = 3 rows; row breaks should give 3 newlines.
	if got := strings.Count(out, "\n"); got != 3 {
		t.Errorf("expected 3 newlines, got %d (%q)", got, out)
	}
}

func TestFormatSeedWords24(t *testing.T) {
	words := strings.Fields("a b c d e f g h i j k l m n o p q r s t u v w x")
	out := FormatSeedWords(strings.Join(words, " "))
	if got := strings.Count(out, "\n"); got != 6 {
		t.Errorf("expected 6 newlines for 24 words, got %d", got)
	}
}

func TestFormatSeedWordsHandlesPartialRow(t *testing.T) {
	// 13 words → 3 full rows + 1 short row → 4 newlines
	words := strings.Fields("a a a a b b b b c c c c d")
	out := FormatSeedWords(strings.Join(words, " "))
	if got := strings.Count(out, "\n"); got != 4 {
		t.Errorf("expected 4 newlines for 13 words, got %d", got)
	}
}

func TestDisplayRecoveryRequiresTTY(t *testing.T) {
	// In `go test` stdin is typically not a TTY → this should fail
	// with ErrNoTTY without ever touching /dev/tty.
	err := DisplayRecoveryOnce("hdr", "body")
	if err != ErrNoTTY {
		t.Fatalf("expected ErrNoTTY in test environment, got %v", err)
	}
}
