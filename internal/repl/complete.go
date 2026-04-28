package repl

import "strings"

// commandNames is the list of all REPL commands offered to TAB
// completion. Order is the user-visible order in `help`.
var commandNames = []string{
	"ls",
	"note",
	"cd",
	"pwd",
	"show",
	"read",
	"fields",
	"cp",
	"copy",
	"set",
	"unset",
	"find",
	"rm",
	"template",
	"passphrase",
	"export",
	"import",
	"bundle",
	"import-bundle",
	"help",
	"exit",
	"quit",
}

// templateSubcommands are the second-token completions when the first
// token is "template". An empty entry represents "(no subcommand)" and
// is filtered out of the candidate list.
var templateSubcommands = []string{"show", "reset"}

// noteNameCommands names the commands whose first argument is a note
// name. `bundle` accepts MULTIPLE note-name arguments, so we still
// complete on every position past the first token (the wordComplete
// logic doesn't currently distinguish between commands that take 1
// vs N note args — for `read` etc the user just won't get useful
// candidates for trailing positions, which is harmless).
var noteNameCommands = map[string]bool{
	"note":   true,
	"read":   true,
	"copy":   true,
	"rm":     true,
	"bundle": true,
	"cd":     true,
}

// fieldOrNoteCommands name the commands whose first argument can be
// either a field of the current note OR a note name (note name wins
// at runtime). The completer offers the union: current-note field
// names first, then note names.
var fieldOrNoteCommands = map[string]bool{
	"show": true,
	"cp":   true,
}

// fieldOnlyCommands name the commands whose first argument is always
// a field of the current note (set / unset). Completion only offers
// field names — nothing else makes sense in those positions.
var fieldOnlyCommands = map[string]bool{
	"set":   true,
	"unset": true,
}

// nameLister returns the live set of completable note names. Captured
// by closure so adds/deletes during the session show up immediately.
type nameLister func() []string

// fieldLister returns the field-key list for the current note, or
// nil when no current note is set. Captured by closure so cd / set /
// unset all see the up-to-date field set without explicit refresh.
type fieldLister func() []string

// wordComplete implements peterh/liner's WordCompleter contract:
// given a line and a cursor position it returns the prefix to keep,
// the candidate completions for the word at the cursor, and the
// suffix to keep. The returned candidates already include any required
// trailing space (for commands) but no quoting.
func wordComplete(line string, pos int, names nameLister, currentFields fieldLister) (head string, completions []string, tail string) {
	if pos > len(line) {
		pos = len(line)
	}
	left := line[:pos]
	tail = line[pos:]

	// Find the start of the word at the cursor.
	wordStart := strings.LastIndexAny(left, " \t") + 1
	head = left[:wordStart]
	wordPrefix := left[wordStart:]

	// Are we still on the first token? (no whitespace before the cursor)
	firstToken := strings.IndexAny(strings.TrimRight(head, " \t"), " \t") < 0 && strings.TrimSpace(head) == ""

	if firstToken {
		for _, c := range commandNames {
			if strings.HasPrefix(c, wordPrefix) {
				completions = append(completions, c+" ")
			}
		}
		return head, completions, tail
	}

	// Subsequent token: look at the first word to decide what to complete.
	cmd := strings.TrimSpace(strings.SplitN(head, " ", 2)[0])
	if cmd == "template" {
		// Only complete the first arg of `template`. Anything past that
		// is unknown territory and stays free-form.
		if strings.Count(strings.TrimSpace(head), " ") > 0 {
			return head, nil, tail
		}
		for _, sub := range templateSubcommands {
			if strings.HasPrefix(sub, wordPrefix) {
				completions = append(completions, sub)
			}
		}
		return head, completions, tail
	}

	// Commands that take a field name in arg position: field-only
	// (set / unset) get just the current note's field keys; field-or-
	// note commands (show / cp) get fields first then note names so
	// the most contextually relevant options surface at the top.
	if fieldOnlyCommands[cmd] {
		if currentFields != nil {
			for _, f := range currentFields() {
				if strings.HasPrefix(f, wordPrefix) {
					completions = append(completions, f)
				}
			}
		}
		return head, completions, tail
	}
	if fieldOrNoteCommands[cmd] {
		seen := make(map[string]bool, 8)
		if currentFields != nil {
			for _, f := range currentFields() {
				if strings.HasPrefix(f, wordPrefix) {
					completions = append(completions, f)
					seen[f] = true
				}
			}
		}
		if names != nil {
			for _, n := range names() {
				if strings.HasPrefix(n, wordPrefix) && !seen[n] {
					completions = append(completions, n)
				}
			}
		}
		return head, completions, tail
	}

	if !noteNameCommands[cmd] {
		return head, nil, tail
	}
	if names == nil {
		return head, nil, tail
	}
	for _, n := range names() {
		if strings.HasPrefix(n, wordPrefix) {
			completions = append(completions, n)
		}
	}
	return head, completions, tail
}
