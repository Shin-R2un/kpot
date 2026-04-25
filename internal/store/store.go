package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

const StoreVersion = 1

type Note struct {
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DecryptedVault struct {
	Version   int              `json:"version"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	// Template is the per-vault new-note template body. Empty means
	// "use the built-in default" — kept omitempty so existing v1 vaults
	// without this field round-trip cleanly.
	Template string           `json:"template,omitempty"`
	Notes    map[string]*Note `json:"notes"`
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
		return fmt.Errorf("note %q not found", canon)
	}
	delete(v.Notes, canon)
	return nil
}

// NormalizeName lowercases and validates a note name.
// Allowed: ASCII letters, digits, '-', '_', '.', '/'. Length 1..128.
// '/' enables hierarchical names like "ai/openai".
func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("note name is empty")
	}
	name = strings.ToLower(name)
	if len(name) > 128 {
		return "", errors.New("note name too long (max 128 chars)")
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "//") {
		return "", errors.New("invalid note name: bad use of '/'")
	}
	for _, r := range name {
		if r > unicode.MaxASCII {
			return "", fmt.Errorf("invalid character in name: %q", r)
		}
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == '/':
		default:
			return "", fmt.Errorf("invalid character in name: %q", r)
		}
	}
	return name, nil
}
