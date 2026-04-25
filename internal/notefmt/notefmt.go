// Package notefmt renders notes for $EDITOR (with a YAML-style
// frontmatter showing created/updated timestamps) and strips that
// frontmatter back off when the user saves.
//
// Frontmatter is purely a display convenience — the JSON metadata in
// the vault remains the source of truth for timestamps. If the user
// edits the frontmatter, the edits are discarded; on the next open the
// frontmatter is regenerated from the freshly bumped JSON metadata.
package notefmt

import (
	"strings"
	"time"
)

// DefaultBody is the template seeded into a brand-new note. Designed
// for the most common entry types (web account, API key, secret memo).
// Placeholders ({{name}}, {{date}}, …) are expanded by ApplyPlaceholders
// the moment a new note is created — they do NOT live on in the saved
// body, so subsequent edits won't refresh them.
const DefaultBody = `# {{name}}

- id:
- url:
- password:
- api_key:

## memo

`

// Placeholders supplies the values ApplyPlaceholders substitutes into a
// raw template. Now is used for {{date}}/{{time}}/{{datetime}}; Name is
// the canonical note name and feeds both {{name}} and {{basename}}.
type Placeholders struct {
	Name string
	Now  time.Time
}

// SupportedPlaceholders is the user-visible documentation list. Order
// matters — it's printed in `help`/README in the listed order.
var SupportedPlaceholders = []string{
	"{{name}}",
	"{{basename}}",
	"{{date}}",
	"{{time}}",
	"{{datetime}}",
}

// ApplyPlaceholders substitutes every supported placeholder in tmpl
// using the values in p. Unknown {{tokens}} are left untouched so users
// can write literal mustaches without escaping. Substitution happens
// once, when a new note is created — it does not run on subsequent edits.
func ApplyPlaceholders(tmpl string, p Placeholders) string {
	now := p.Now
	if now.IsZero() {
		now = time.Now()
	}
	local := now.Local()
	rep := strings.NewReplacer(
		"{{name}}", p.Name,
		"{{basename}}", basename(p.Name),
		"{{date}}", local.Format("2006-01-02"),
		"{{time}}", local.Format("15:04"),
		"{{datetime}}", local.Format(time.RFC3339),
	)
	return rep.Replace(tmpl)
}

func basename(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// Render builds the bytes handed to $EDITOR: a frontmatter block with
// the supplied timestamps, a blank line, then the body. Timestamps are
// formatted in the local timezone so they're readable while editing.
func Render(created, updated time.Time, body string) []byte {
	var b strings.Builder
	b.Grow(len(body) + 96)
	b.WriteString("---\n")
	b.WriteString("created: ")
	b.WriteString(created.Local().Format(time.RFC3339))
	b.WriteString("\n")
	b.WriteString("updated: ")
	b.WriteString(updated.Local().Format(time.RFC3339))
	b.WriteString("\n---\n\n")
	b.WriteString(body)
	return []byte(b.String())
}

// Strip removes the leading frontmatter block (if any) and returns the
// remaining body with the leading blank line(s) trimmed. If the input
// has no recognizable frontmatter the entire input is returned as-is.
//
// A frontmatter block starts with a line containing only "---" (after
// optional leading blank lines) and ends at the next line containing
// only "---". Anything else is treated as body.
func Strip(raw []byte) string {
	lines := strings.Split(string(raw), "\n")

	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || strings.TrimSpace(lines[i]) != "---" {
		return string(raw)
	}

	j := i + 1
	for j < len(lines) && strings.TrimSpace(lines[j]) != "---" {
		j++
	}
	if j >= len(lines) {
		// no closing fence — leave the original alone rather than
		// silently swallowing the user's content.
		return string(raw)
	}

	body := strings.Join(lines[j+1:], "\n")
	return strings.TrimLeft(body, "\n")
}
