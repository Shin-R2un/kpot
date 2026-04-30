package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// StoreVersion bumps:
//   - v1 (kpot v0.1+): notes + template
//   - v2 (kpot v0.10+): adds recent[] and trash{}. v1 binaries reject
//     v2 vaults via the FromJSON `version > StoreVersion` guard.
const StoreVersion = 2

// ErrNotFound is returned by lookup-style methods (Delete, Trash,
// Restore, Purge) when the named note / trash entry doesn't exist.
// Sentinel so callers can errors.Is rather than string-match.
var ErrNotFound = errors.New("not found")

type Note struct {
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TrashedNote is a note that was removed via `rm` and can be brought
// back with `restore` until `purge` (or `purge --all`) wipes it. The
// original creation/update times are preserved so a restore round-trip
// is byte-identical with the pre-trash Note.
type TrashedNote struct {
	OriginalName string    `json:"original_name"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	DeletedAt    time.Time `json:"deleted_at"`
}

type DecryptedVault struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Template is the per-vault new-note template body. Empty means
	// "use the built-in default" — kept omitempty so existing v1 vaults
	// without this field round-trip cleanly.
	Template string           `json:"template,omitempty"`
	Notes    map[string]*Note `json:"notes"`
	// Recent holds canonical names of recently accessed notes,
	// most-recent first, capped at RecentMax. Populated by TrackRecent
	// from REPL cd/show/cp on success. omitempty so v2 vaults that
	// haven't accumulated history stay byte-equivalent to v1 on disk.
	Recent []string `json:"recent,omitempty"`
	// Trash holds soft-deleted notes keyed by `<original-name>.deleted-YYYYMMDD-HHMMSS`.
	// Populated by Trash, drained by Restore / Purge / PurgeAll. omitempty
	// so vaults with an empty trash don't carry the field on disk.
	Trash map[string]*TrashedNote `json:"trash,omitempty"`
}

func New() *DecryptedVault {
	now := time.Now().UTC()
	return &DecryptedVault{
		Version:   StoreVersion,
		CreatedAt: now,
		UpdatedAt: now,
		Notes:     map[string]*Note{},
	}
}

func FromJSON(b []byte) (*DecryptedVault, error) {
	v := &DecryptedVault{}
	if err := json.Unmarshal(b, v); err != nil {
		return nil, fmt.Errorf("decode vault contents: %w", err)
	}
	if v.Notes == nil {
		v.Notes = map[string]*Note{}
	}
	if v.Version > StoreVersion {
		return nil, fmt.Errorf("vault contents version v%d is newer than supported v%d", v.Version, StoreVersion)
	}
	return v, nil
}

func (v *DecryptedVault) ToJSON() ([]byte, error) {
	v.UpdatedAt = time.Now().UTC()
	return json.Marshal(v)
}

func (v *DecryptedVault) Names() []string {
	names := make([]string, 0, len(v.Notes))
	for k := range v.Notes {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func (v *DecryptedVault) Get(name string) (*Note, bool) {
	canon, err := NormalizeName(name)
	if err != nil {
		return nil, false
	}
	n, ok := v.Notes[canon]
	return n, ok
}

func (v *DecryptedVault) Put(name, body string) (*Note, error) {
	canon, err := NormalizeName(name)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if existing, ok := v.Notes[canon]; ok {
		existing.Body = body
		existing.UpdatedAt = now
		return existing, nil
	}
	n := &Note{Body: body, CreatedAt: now, UpdatedAt: now}
	v.Notes[canon] = n
	return n, nil
}

// Match describes a single hit from Find. NameMatch and BodyMatch are
// independent — a note may match both. Snippet, when populated, is the
// trimmed first body line containing the query (best-effort).
type Match struct {
	Name      string
	NameMatch bool
	BodyMatch bool
	Snippet   string
}

// Find returns notes whose name or body contains query (case-insensitive
// substring). Empty query returns no matches. Results are sorted by name.
func (v *DecryptedVault) Find(query string) []Match {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	out := make([]Match, 0)
	for _, name := range v.Names() {
		n := v.Notes[name]
		nameMatch := strings.Contains(name, q)
		body := n.Body
		bodyMatch := strings.Contains(strings.ToLower(body), q)
		if !nameMatch && !bodyMatch {
			continue
		}
		m := Match{Name: name, NameMatch: nameMatch, BodyMatch: bodyMatch}
		if bodyMatch {
			m.Snippet = firstMatchingLine(body, q)
		}
		out = append(out, m)
	}
	return out
}

func firstMatchingLine(body, lowerQuery string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(strings.ToLower(line), lowerQuery) {
			s := strings.TrimSpace(line)
			if len(s) > 120 {
				s = s[:117] + "..."
			}
			return s
		}
	}
	return ""
}

func (v *DecryptedVault) Delete(name string) error {
	canon, err := NormalizeName(name)
	if err != nil {
		return err
	}
	if _, ok := v.Notes[canon]; !ok {
		return fmt.Errorf("note %q: %w", canon, ErrNotFound)
	}
	delete(v.Notes, canon)
	return nil
}

// NormalizeName lowercases and validates a note name.
//
// Allowed:
//   - any Unicode letter (`unicode.IsLetter`) — covers Latin, CJK,
//     Cyrillic, Greek, etc., so users can write `password/のりお` or
//     `работа/email` natively.
//   - any Unicode decimal digit (`unicode.IsDigit`).
//   - the four ASCII punctuation marks `-`, `_`, `.`, `/`.
//
// Rejected:
//   - control characters, whitespace (space / tab / newline), and all
//     symbols / punctuation other than the four above. This blocks
//     emoji-only names, fullwidth slashes that look like '/' but
//     aren't, and shell-meaningful chars (`*`, `?`, `$`, etc.) that
//     would surprise users who pipe note names around.
//   - leading / trailing / doubled '/' (segment-shape rules).
//
// Length is capped at 128 RUNES, not bytes — Japanese is ~3 bytes per
// char in UTF-8, so a byte limit would let 42-char Japanese names
// through but reject 129-char ASCII names. Rune count is the
// user-facing measure.
//
// Case folding only meaningfully affects ASCII letters
// (`strings.ToLower` is a no-op for scripts without case like
// Hiragana/Katakana/Kanji), so case-insensitive lookup of `OPENAI`
// vs `openai` keeps working without affecting non-Latin names.
func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("note name is empty")
	}
	name = strings.ToLower(name)
	if utf8.RuneCountInString(name) > 128 {
		return "", errors.New("note name too long (max 128 runes)")
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "//") {
		return "", errors.New("invalid note name: bad use of '/'")
	}
	for _, r := range name {
		switch {
		case unicode.IsLetter(r):
		case unicode.IsDigit(r):
		case r == '-' || r == '_' || r == '.' || r == '/':
		default:
			return "", fmt.Errorf("invalid character in name: %q", r)
		}
	}
	return name, nil
}
