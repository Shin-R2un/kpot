// Package fields parses, extracts, and edits simple `key: value` rows
// inside a note body. It is intentionally not a Markdown parser — kpot
// notes are free-form text, and we only care about the small subset of
// `key: value` rows users want to address as fields (id, url, apikey,
// etc.).
//
// Lookup rules:
//   - Keys are normalized to lower-case for matching, but the original
//     case is preserved in the body when we update a value in place.
//   - Allowed key characters: [A-Za-z0-9_.-]. Anything else falls
//     through as plain prose and is ignored.
//   - A leading frontmatter block (between two `---` fences) is
//     skipped entirely so `created:` / `updated:` lines never leak
//     into the field list. The kpot REPL strips frontmatter before
//     persisting, so this is a belt-and-braces guard.
//
// Update rules:
//   - Set replaces an existing field's value in place when the key
//     already exists (preserving the original case and indentation).
//   - When the key does not yet exist, Set inserts a new line right
//     after the last contiguous block of field lines. If no field
//     lines exist, it inserts at the top of the body (after the
//     leading title heading, if there is one).
package fields

import (
	"regexp"
	"strings"
)

// Field is one `key: value` row recovered from a note body.
type Field struct {
	// Key is the lower-cased, canonical key used for lookups.
	Key string
	// OriginalKey is the key spelled exactly as it appears in the body.
	// Updates in place use this to avoid changing the user's preferred
	// casing or surrounding whitespace.
	OriginalKey string
	// Value is the trimmed string after the `:` separator.
	Value string
	// Line is the 0-indexed line number in the original body.
	Line int
}

// secretFieldNames is the canonical, lower-cased list of keys that
// must never be passed as a command argument (they would persist in
// shell history). The REPL `set` command refuses arg-form for these
// and prompts via TTY echo-off instead. Comparison is exact-match on
// the canonical key — the variants below cover the common spellings
// people actually use.
var secretFieldNames = map[string]struct{}{
	"pass":          {},
	"password":      {},
	"pwd":           {},
	"apikey":        {},
	"api_key":       {},
	"api-key":       {},
	"key":           {},
	"token":         {},
	"secret":        {},
	"client_secret": {},
	"client-secret": {},
}

// IsSecretField reports whether key is one of the well-known secret
// field names that should not be supplied as a command-line argument.
// Match is case-insensitive on the canonical key.
func IsSecretField(key string) bool {
	_, ok := secretFieldNames[strings.ToLower(strings.TrimSpace(key))]
	return ok
}

// keyPattern matches a `key: value` line where the key uses only the
// safe character set. We intentionally require the key to be at the
// start of the line (no indentation tolerated) so list-bullets like
// `- id:` or block-quoted lines don't accidentally register as
// fields. Users who want a leading bullet can drop it.
var keyPattern = regexp.MustCompile(`^([A-Za-z0-9_.\-]+)\s*:\s*(.*)$`)

// Parse walks body line by line and returns every recognised field
// in source order. Frontmatter is skipped. Lines inside a fenced code
// block (``` … ```) are also skipped so secrets pasted as code
// blocks don't show up as fields.
func Parse(body string) []Field {
	lines := strings.Split(body, "\n")
	out := make([]Field, 0, 8)

	i, end := skipFrontmatter(lines)

	inFence := false
	for ; i < end; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		// Skip headings, list bullets, blank lines fast.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		m := keyPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, Field{
			Key:         strings.ToLower(m[1]),
			OriginalKey: m[1],
			Value:       strings.TrimSpace(m[2]),
			Line:        i,
		})
	}
	return out
}

// Get returns the value of the field whose canonical key equals
// strings.ToLower(key). The second return is false if no such field
// exists in body.
func Get(body, key string) (string, bool) {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, f := range Parse(body) {
		if f.Key == k {
			return f.Value, true
		}
	}
	return "", false
}

// Names returns the canonical (lower-cased) keys of every field in
// body, in source order. Useful for completion and the `fields`
// command.
func Names(body string) []string {
	parsed := Parse(body)
	out := make([]string, 0, len(parsed))
	for _, f := range parsed {
		out = append(out, f.Key)
	}
	return out
}

// Set returns a new body with the field key set to value. If key
// already exists, the existing line is rewritten in place (case and
// whitespace before the colon preserved). If key does not exist, a
// new `key: value` line is appended directly after the existing
// field block (or, if there is none, after the title heading or at
// the top of the body).
//
// The key is normalized to lower-case for inserts so notes stay
// internally consistent. Existing keys with a different case are
// untouched on update.
func Set(body, key, value string) string {
	canonical := strings.ToLower(strings.TrimSpace(key))
	lines := strings.Split(body, "\n")

	for idx, f := range Parse(body) {
		if f.Key == canonical {
			// In-place update: reuse the original key text, only
			// replace the value tail. We rebuild the line manually
			// rather than using the regex because we want to
			// preserve any whitespace style around the colon.
			lines[f.Line] = formatLine(f.OriginalKey, value, lines[f.Line])
			_ = idx
			return strings.Join(lines, "\n")
		}
	}

	// No matching field — insert.
	insertAt := insertionPoint(lines)
	newLine := canonical + ": " + value
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:insertAt]...)
	out = append(out, newLine)
	out = append(out, lines[insertAt:]...)
	return strings.Join(out, "\n")
}

// Unset returns a new body with the line for field key removed
// entirely. If no such field exists, body is returned unchanged.
func Unset(body, key string) string {
	canonical := strings.ToLower(strings.TrimSpace(key))
	lines := strings.Split(body, "\n")
	for _, f := range Parse(body) {
		if f.Key == canonical {
			out := make([]string, 0, len(lines)-1)
			out = append(out, lines[:f.Line]...)
			out = append(out, lines[f.Line+1:]...)
			return strings.Join(out, "\n")
		}
	}
	return body
}

// formatLine rebuilds an updated `key: value` line, preserving the
// exact spacing/colon style the user originally used (e.g.
// "url:  https://…" vs. "url:https://…"). We extract the prefix up
// to and including the first colon from original, then add the new
// value with one space after the colon as a sane default if the
// original had no value at all.
func formatLine(originalKey, newValue, original string) string {
	colon := strings.Index(original, ":")
	if colon < 0 {
		// Should not happen — Parse only yields lines that match the
		// regex, which requires a colon — but degrade gracefully.
		return originalKey + ": " + newValue
	}
	prefix := original[:colon+1]
	// If the prefix didn't already end with whitespace, add a single
	// space so the result reads naturally.
	if !strings.HasSuffix(prefix, " ") {
		prefix += " "
	}
	return prefix + newValue
}

// insertionPoint chooses the line index at which a brand-new field
// line should be inserted. Strategy:
//
//  1. After the last existing field line, on the next line.
//  2. Failing that, after the title heading (`# …`) at the top, if
//     there is one.
//  3. Failing that, at the top of the body.
//
// We intentionally don't try to be cleverer than this — the user
// can always reorder fields by editing the note in $EDITOR.
func insertionPoint(lines []string) int {
	// 1. Last field line.
	parsed := Parse(strings.Join(lines, "\n"))
	if len(parsed) > 0 {
		return parsed[len(parsed)-1].Line + 1
	}
	// 2. After title heading.
	skipStart, _ := skipFrontmatter(lines)
	for i := skipStart; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			// Insert one blank line after the heading if there
			// isn't one already, so the output looks tidy.
			next := i + 1
			if next < len(lines) && strings.TrimSpace(lines[next]) == "" {
				return next + 1
			}
			return next
		}
	}
	return skipStart
}

// skipFrontmatter advances past a leading `--- … ---` block and
// returns the start and end (exclusive) of the body region we
// should consider. If there is no frontmatter, start is 0 and end
// is len(lines).
func skipFrontmatter(lines []string) (start, end int) {
	end = len(lines)
	i := 0
	for i < end && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= end || strings.TrimSpace(lines[i]) != "---" {
		return 0, end
	}
	j := i + 1
	for j < end && strings.TrimSpace(lines[j]) != "---" {
		j++
	}
	if j >= end {
		// Unterminated fence — let it be body, like notefmt.Strip.
		return 0, end
	}
	return j + 1, end
}
