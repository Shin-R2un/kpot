package repl

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/Shin-R2un/kpot/internal/store"
)

// errSelectionEmpty signals that the user typed a numeric arg (like
// `cd 1`) without any prior find/recent that would populate the
// selection list. Surfaced with a helpful message at the dispatch
// site.
var errSelectionEmpty = errors.New("no recent selection — run 'find' or 'recent' first")

// errSelectionOutOfRange signals that the user typed a numeric arg
// that's beyond the current selection list (e.g. `cd 99` when only
// 3 matches exist).
var errSelectionOutOfRange = errors.New("selection index out of range")

// setSelection replaces the session's "last selection" list — the
// numeric pool consulted by `cd N` / `show N` / `cp N field`. Called
// by find / recent after they print results. Other read-only commands
// (ls, pwd, fields, trash) intentionally do NOT touch this so the
// numeric arg space stays predictable: the last query result is
// always referenceable, regardless of intervening output.
func (s *Session) setSelection(names []string) {
	if len(names) == 0 {
		s.lastSelection = nil
		return
	}
	cp := make([]string, len(names))
	copy(cp, names)
	s.lastSelection = cp
}

// resolveByNumberOrName turns a `cd 1` / `cd accounts/foo` argument
// into a canonical note name. It does NOT verify that the resulting
// name exists in the vault — caller invokes Vault.Get afterward (most
// already do, since the lookup also handles the "current note vanished"
// race). Returns:
//
//   - canonical name (numeric path) — when arg is a positive integer
//     in [1, len(lastSelection)]. The store.NormalizeName check is
//     skipped here because lastSelection entries are already canonical
//     (find / recent only emit canonical names).
//   - canonical name (literal path) — when arg is anything else. We
//     run NormalizeName so callers don't have to.
//   - error — empty arg, empty selection on numeric arg, out-of-range
//     numeric arg, or NormalizeName rejection on literal arg.
//
// Numeric wins on ambiguity: if a literal note "1" exists AND
// lastSelection has 1+ entry, the numeric interpretation is used. This
// is documented in the manual; users who really mean a literal "1"
// note can rename it.
func (s *Session) resolveByNumberOrName(arg string) (string, error) {
	if arg == "" {
		return "", errors.New("empty argument")
	}
	if n, err := strconv.Atoi(arg); err == nil && n > 0 {
		if len(s.lastSelection) == 0 {
			return "", errSelectionEmpty
		}
		if n > len(s.lastSelection) {
			return "", fmt.Errorf("%w: %d > %d", errSelectionOutOfRange, n, len(s.lastSelection))
		}
		return s.lastSelection[n-1], nil
	}
	return store.NormalizeName(arg)
}
