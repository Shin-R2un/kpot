package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/Shin-R2un/kpot/internal/crypto"
	"github.com/Shin-R2un/kpot/internal/store"
	"github.com/Shin-R2un/kpot/internal/vault"
)

// cookieName is the session cookie. Picking a kpot_-prefixed name
// avoids collisions on multi-app shared 127.0.0.1 (the user might be
// SSH-tunneling other tools to the same loopback over time).
const cookieName = "kpot_sid"

// reauthAction values returned in 401 bodies — the frontend uses
// these to decide which view to bounce to.
const (
	actionLogin  = "login"  // no session at all
	actionReauth = "reauth" // cookie exists but session is locked
)

// loginRequest is the JSON shape POST /api/login expects.
// Passphrase is *not* zeroed in the JSON layer (Go strings can't be
// safely zeroed); we cast to []byte once in handleLogin and Zero
// THAT copy explicitly.
type loginRequest struct {
	Passphrase string `json:"passphrase"`
}

type statusResponse struct {
	State          string `json:"state"`
	IdleRemainingS int    `json:"idle_remaining_s,omitempty"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	// Strip the source-port from RemoteAddr so attempts from the same
	// client (different ephemeral ports per connection) accumulate
	// against the same bucket.
	rlKey := r.RemoteAddr
	if h, _, err := net.SplitHostPort(rlKey); err == nil {
		rlKey = h
	}
	if !s.rateLimit.allow(rlKey) {
		writeError(w, http.StatusTooManyRequests,
			"too many failed attempts; wait a moment", "")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body", "")
		return
	}
	var req loginRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json", "")
		return
	}
	if req.Passphrase == "" {
		writeError(w, http.StatusBadRequest, "passphrase required", "")
		return
	}

	// Convert the passphrase to a []byte so we can crypto.Zero it after
	// vault.Open consumes it. Note: Go strings are immutable so the
	// original req.Passphrase string still lives in memory until GC.
	// CLAUDE.md acknowledges this Go memzero limitation; we wipe what
	// we can.
	pass := []byte(req.Passphrase)
	defer crypto.Zero(pass)

	plaintext, key, _, err := vault.Open(s.vaultPath, pass)
	if err != nil {
		s.rateLimit.recordFailure(rlKey)
		// Don't leak whether it's "wrong passphrase" vs "vault corrupt"
		// vs "file missing" — same 401 either way.
		writeError(w, http.StatusUnauthorized, "wrong passphrase", actionLogin)
		return
	}
	defer crypto.Zero(plaintext)

	dv, err := store.FromJSON(plaintext)
	if err != nil {
		// Successful decryption but malformed payload — unusual.
		// Treat as auth failure for the user's POV.
		crypto.Zero(key)
		s.rateLimit.recordFailure(rlKey)
		writeError(w, http.StatusInternalServerError, "vault payload corrupt", "")
		return
	}

	s.rateLimit.recordSuccess(rlKey)

	// Re-use the existing session cookie if the request brought one and
	// the sid is known (Locked → Active transition). Otherwise mint a
	// brand new cookie.
	var sid string
	if c, err := r.Cookie(cookieName); err == nil {
		if s.sessions.Activate(c.Value, key, dv) {
			sid = c.Value
		}
	}
	if sid == "" {
		sid, err = s.sessions.Create(key, dv)
		if err != nil {
			writeError(w, http.StatusInternalServerError,
				"session create", "")
			return
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		// No Secure flag: kpot serve is plain HTTP. The cookie's
		// privacy hinges on the loopback / VPN / SSH-tunnel boundary,
		// not on TLS. Adding Secure here would prevent the cookie from
		// sticking under plain HTTP (cookiejars follow RFC 6265 and
		// refuse to persist Secure cookies over non-https), which
		// would defeat the daemon entirely.
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	if c, err := r.Cookie(cookieName); err == nil {
		s.sessions.Destroy(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   cookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	resp := statusResponse{State: "none"}
	if c, err := r.Cookie(cookieName); err == nil {
		if sess := s.sessions.Lookup(c.Value); sess != nil {
			switch sess.State() {
			case StateActive:
				resp.State = "active"
				if s.idleTimeout > 0 {
					resp.IdleRemainingS = idleRemaining(sess, s.idleTimeout)
				}
			case StateLocked:
				resp.State = "locked"
			}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func idleRemaining(s *webSession, idle time.Duration) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state != StateActive {
		return 0
	}
	rem := int((idle - time.Since(s.lastActive)).Seconds())
	if rem < 0 {
		rem = 0
	}
	return rem
}

// requireSession resolves the cookie to a live (active) session. On
// failure it writes the appropriate 401 and returns nil; the caller
// returns immediately.
func (s *Server) requireSession(w http.ResponseWriter, r *http.Request) *webSession {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		writeError(w, http.StatusUnauthorized, "login required", actionLogin)
		return nil
	}
	sess := s.sessions.Lookup(c.Value)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, "session expired", actionLogin)
		return nil
	}
	if sess.State() != StateActive {
		writeError(w, http.StatusUnauthorized, "session locked", actionReauth)
		return nil
	}
	sess.Touch(s.idleTimeout)
	return sess
}

// errInvalidVault is exposed for tests so they can match the precise
// failure mode of vault.Open without depending on its internals.
var errInvalidVault = errors.New("invalid vault")

// fmtBytes formats a byte count for log output without allocating in
// the hot path; not currently used but kept for future logging hooks.
func fmtBytes(n int) string { return fmt.Sprintf("%d", n) }
