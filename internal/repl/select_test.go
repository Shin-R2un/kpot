package repl

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestResolveByNumberPicksFromSelection(t *testing.T) {
	s := scriptedSession(t, filepath.Join(t.TempDir(), "v.kpot"))
	s.lastSelection = []string{"a", "b", "c"}

	got, err := s.resolveByNumberOrName("2")
	if err != nil {
		t.Fatal(err)
	}
	if got != "b" {
		t.Errorf("resolveByNumberOrName(\"2\") = %q, want %q", got, "b")
	}
}

func TestResolveByNameFallsThrough(t *testing.T) {
	s := scriptedSession(t, filepath.Join(t.TempDir(), "v.kpot"))
	s.lastSelection = []string{"x"}

	got, err := s.resolveByNumberOrName("AccountS/Foo")
	if err != nil {
		t.Fatal(err)
	}
	if got != "accounts/foo" {
		t.Errorf("got %q, want canonical 'accounts/foo'", got)
	}
}

func TestResolveByNumberOutOfRange(t *testing.T) {
	s := scriptedSession(t, filepath.Join(t.TempDir(), "v.kpot"))
	s.lastSelection = []string{"a"}

	_, err := s.resolveByNumberOrName("9")
	if !errors.Is(err, errSelectionOutOfRange) {
		t.Errorf("err = %v, want errSelectionOutOfRange", err)
	}
}

func TestResolveByNumberWithEmptySelection(t *testing.T) {
	s := scriptedSession(t, filepath.Join(t.TempDir(), "v.kpot"))
	// No find/recent run yet → numeric arg should refuse.
	_, err := s.resolveByNumberOrName("1")
	if !errors.Is(err, errSelectionEmpty) {
		t.Errorf("err = %v, want errSelectionEmpty", err)
	}
}

func TestResolveZeroFallsThroughToName(t *testing.T) {
	// strconv.Atoi("0") returns 0, but our predicate is n > 0 so 0 is
	// treated as a literal name (which NormalizeName then rejects:
	// digits are valid letters, so "0" would actually canonicalize
	// to "0"). The point of this test: numeric handling only fires
	// for positive integers; 0 is not a valid 1-indexed selection.
	s := scriptedSession(t, filepath.Join(t.TempDir(), "v.kpot"))
	s.lastSelection = []string{"a"}

	got, err := s.resolveByNumberOrName("0")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0" {
		t.Errorf("got %q, want literal '0'", got)
	}
}

func TestResolveNegativeFallsThroughToName(t *testing.T) {
	// "-1" parses as int but we only honour positives, so it must
	// fall through to NormalizeName, which rejects the leading '-'
	// segment shape... actually leading hyphen in name is allowed
	// (- is in the letter set). The test verifies the numeric path
	// doesn't panic / use it as an index.
	s := scriptedSession(t, filepath.Join(t.TempDir(), "v.kpot"))
	s.lastSelection = []string{"a", "b", "c"}

	got, err := s.resolveByNumberOrName("-1")
	if err != nil {
		t.Fatal(err)
	}
	if got == "c" {
		t.Errorf("negative interpreted as numeric reverse index: got %q", got)
	}
}

func TestSetSelectionCopiesInput(t *testing.T) {
	s := scriptedSession(t, filepath.Join(t.TempDir(), "v.kpot"))
	src := []string{"a", "b", "c"}
	s.setSelection(src)

	src[0] = "MUTATED"
	if s.lastSelection[0] != "a" {
		t.Errorf("setSelection didn't copy input; mutation leaked into lastSelection")
	}
}

func TestSetSelectionEmptyClears(t *testing.T) {
	s := scriptedSession(t, filepath.Join(t.TempDir(), "v.kpot"))
	s.lastSelection = []string{"a"}
	s.setSelection(nil)
	if len(s.lastSelection) != 0 {
		t.Errorf("setSelection(nil) didn't clear: %v", s.lastSelection)
	}
}
