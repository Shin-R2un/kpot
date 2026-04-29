package serve

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/Shin-R2un/kpot/internal/fields"
	"github.com/Shin-R2un/kpot/internal/store"
)

// secretMask is what we render in body_redacted for secret-field values
// — fixed-width so the line layout doesn't change, but obviously not
// the real value.
const secretMask = "••••••••"

// matchResp mirrors store.Match for JSON output. Body field is
// intentionally absent from the list view — clients fetch the body
// via /api/notes/{name} when needed.
type matchResp struct {
	Name      string `json:"name"`
	NameMatch bool   `json:"name_match"`
	BodyMatch bool   `json:"body_match"`
	Snippet   string `json:"snippet,omitempty"`
}

type listResp struct {
	Matches []matchResp `json:"matches"`
}

type fieldResp struct {
	Key      string `json:"key"`
	IsSecret bool   `json:"is_secret"`
	Line     int    `json:"line"`
}

type detailResp struct {
	Name         string      `json:"name"`
	Fields       []fieldResp `json:"fields"`
	BodyRedacted string      `json:"body_redacted"`
	UpdatedAt    string      `json:"updated_at,omitempty"`
}

type fieldValueResp struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"is_secret"`
}

// handleNotes serves /api/notes — list-or-search. With ?q= it returns
// matches; without q (or empty q) it returns ALL note names so phone
// users can browse without typing.
func (s *Server) handleNotes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "read only", "")
		return
	}
	sess := s.resolveSession(w, r)
	if sess == nil {
		return
	}
	dv := sess.Vault()
	if dv == nil {
		writeError(w, http.StatusUnauthorized, "session locked", actionReauth)
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var out listResp
	if q == "" {
		// Browse-all: turn every note name into a name-only match.
		// Cheap enough for our scale (331 notes today, <1KB JSON).
		names := dv.Names()
		out.Matches = make([]matchResp, 0, len(names))
		for _, n := range names {
			out.Matches = append(out.Matches, matchResp{Name: n, NameMatch: true})
		}
	} else {
		ms := dv.Find(q)
		out.Matches = make([]matchResp, 0, len(ms))
		for _, m := range ms {
			out.Matches = append(out.Matches, matchResp{
				Name:      m.Name,
				NameMatch: m.NameMatch,
				BodyMatch: m.BodyMatch,
				Snippet:   m.Snippet,
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleNote dispatches /api/notes/{name} and its sub-paths.
// The mux routes everything under /api/notes/ here; we parse the
// remainder ourselves so paths can include URL-encoded slashes.
func (s *Server) handleNote(w http.ResponseWriter, r *http.Request) {
	sess := s.resolveSession(w, r)
	if sess == nil {
		return
	}
	dv := sess.Vault()
	if dv == nil {
		writeError(w, http.StatusUnauthorized, "session locked", actionReauth)
		return
	}

	// Strip the prefix and split. Path is already URL-decoded by
	// net/http for the *unescaped* form; we want the escaped form
	// so we use RawPath when present.
	raw := r.URL.RawPath
	if raw == "" {
		raw = r.URL.Path
	}
	rest := strings.TrimPrefix(raw, "/api/notes/")
	if rest == "" {
		writeError(w, http.StatusBadRequest, "note name required", "")
		return
	}

	// Decompose into <name> [/ <action> [/ <key>]].
	parts := strings.SplitN(rest, "/", 3)
	encName := parts[0]
	name, err := url.PathUnescape(encName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad note name encoding", "")
		return
	}
	canon, err := store.NormalizeName(name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error(), "")
		return
	}
	note, ok := dv.Get(canon)
	if !ok {
		writeError(w, http.StatusNotFound, "note not found", "")
		return
	}

	switch len(parts) {
	case 1:
		// /api/notes/{name} → detail
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "read only", "")
			return
		}
		s.writeDetail(w, canon, note.Body, note.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"))
	case 2, 3:
		action := parts[1]
		switch action {
		case "field":
			if len(parts) < 3 {
				writeError(w, http.StatusBadRequest, "field key required", "")
				return
			}
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "read only", "")
				return
			}
			key, err := url.PathUnescape(parts[2])
			if err != nil {
				writeError(w, http.StatusBadRequest, "bad field key encoding", "")
				return
			}
			s.writeFieldValue(w, note.Body, key)
		case "url":
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "read only", "")
				return
			}
			s.redirectURL(w, r, note.Body)
		default:
			writeError(w, http.StatusNotFound, "unknown sub-resource", "")
		}
	}
}

func (s *Server) writeDetail(w http.ResponseWriter, name, body, updatedAt string) {
	parsed := fields.Parse(body)
	out := detailResp{
		Name:         name,
		Fields:       make([]fieldResp, 0, len(parsed)),
		BodyRedacted: redactBody(body, parsed),
		UpdatedAt:    updatedAt,
	}
	for _, f := range parsed {
		out.Fields = append(out.Fields, fieldResp{
			Key:      f.Key,
			IsSecret: fields.IsSecretField(f.Key),
			Line:     f.Line,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) writeFieldValue(w http.ResponseWriter, body, key string) {
	v, ok := fields.Get(body, key)
	if !ok {
		writeError(w, http.StatusNotFound, "field not found", "")
		return
	}
	writeJSON(w, http.StatusOK, fieldValueResp{
		Key:      strings.ToLower(strings.TrimSpace(key)),
		Value:    v,
		IsSecret: fields.IsSecretField(key),
	})
}

func (s *Server) redirectURL(w http.ResponseWriter, r *http.Request, body string) {
	v, ok := fields.Get(body, "url")
	if !ok || v == "" {
		writeError(w, http.StatusNotFound, "no url field", "")
		return
	}
	// Only allow http(s) destinations to avoid javascript:/data: exfil.
	low := strings.ToLower(v)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		writeError(w, http.StatusBadRequest, "url field is not http(s)", "")
		return
	}
	http.Redirect(w, r, v, http.StatusFound)
}

// redactBody returns a copy of body with the value half of any secret
// field line replaced by secretMask. Done line-by-line so the line
// numbers in the parsed Field structs still apply.
func redactBody(body string, parsed []fields.Field) string {
	if len(parsed) == 0 {
		return body
	}
	lines := strings.Split(body, "\n")
	for _, f := range parsed {
		if !fields.IsSecretField(f.Key) {
			continue
		}
		if f.Line < 0 || f.Line >= len(lines) {
			continue
		}
		// Find the first colon and truncate to "<key>: " + mask.
		l := lines[f.Line]
		colon := strings.Index(l, ":")
		if colon < 0 {
			continue
		}
		// Preserve everything up to and including the colon + first
		// space (if present) so the line still reads naturally.
		prefix := l[:colon+1]
		if colon+1 < len(l) && l[colon+1] == ' ' {
			prefix = l[:colon+2]
		}
		lines[f.Line] = prefix + secretMask
	}
	return strings.Join(lines, "\n")
}

// ===== JSON helpers =====

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Defense-in-depth against any caching proxy on the path. The
	// SSH-tunnel deployment has none, but the loopback could be
	// proxied locally for debugging.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// writeError emits a JSON error body. action is optional and tells the
// frontend whether to bounce to login or locked view.
func writeError(w http.ResponseWriter, status int, msg, action string) {
	body := map[string]any{"error": msg}
	if action != "" {
		body["action"] = action
	}
	writeJSON(w, status, body)
}
