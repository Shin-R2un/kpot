package serve

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/Shin-R2un/kpot/internal/crypto"
	"github.com/Shin-R2un/kpot/internal/store"
)

// SessionState reports whether a session is currently usable.
type SessionState int

const (
	// StateActive means the session has an unlocked DEK and a live
	// DecryptedVault. API calls succeed.
	StateActive SessionState = iota
	// StateLocked means the idle timer fired; DEK was zeroed and
	// DecryptedVault was dropped. Cookie still recognised, but every
	// request except /api/login returns 401 reauth.
	StateLocked
)

// webSession is one cookie-bound session. Multiple sessions can exist
// concurrently (one per phone, plus one per laptop, etc.). All public
// access goes through the methods so the underlying mutex is honored.
type webSession struct {
	sid string

	mu         sync.RWMutex
	state      SessionState
	dek        []byte                // nil when locked
	dv         *store.DecryptedVault // nil when locked
	lastActive time.Time
	idleTimer  *time.Timer // non-nil only while Active
}

// State returns the current session state under a read lock.
func (s *webSession) State() SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Vault returns the live DecryptedVault if the session is active, or
// nil if it has been locked. Caller must NOT hold the returned pointer
// across an idle expiry — the slice/map underneath gets cleared under
// the session mutex when Lock fires. Callers do read-only ops anyway
// (search, get) which are safe under the RLock held during the request.
func (s *webSession) Vault() *store.DecryptedVault {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state != StateActive {
		return nil
	}
	return s.dv
}

// Touch bumps lastActive under a write lock and resets the idle timer.
// Called on every successful API request that consumed an active
// session. No-op if the session is locked (state will be checked by
// caller separately).
func (s *webSession) Touch(idle time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateActive {
		return
	}
	s.lastActive = time.Now()
	if s.idleTimer != nil && idle > 0 {
		s.idleTimer.Reset(idle)
	}
}

// Lock zeros the DEK and drops the DecryptedVault reference under a
// write lock. The session moves to StateLocked so subsequent /api/*
// requests get a 401-reauth response (re-login through the same cookie
// is supported via Activate).
//
// Safe to call multiple times; second and later calls are no-ops.
func (s *webSession) Lock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateLocked {
		return
	}
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	if s.dek != nil {
		crypto.Zero(s.dek)
		s.dek = nil
	}
	s.dv = nil
	s.state = StateLocked
}

// activate transitions the session into Active with a fresh DEK + DV.
// Used by both Create and re-auth. Caller holds the write lock.
func (s *webSession) activate(dek []byte, dv *store.DecryptedVault, idle time.Duration) {
	if s.dek != nil {
		crypto.Zero(s.dek)
	}
	s.dek = dek
	s.dv = dv
	s.state = StateActive
	s.lastActive = time.Now()
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	if idle > 0 {
		// time.AfterFunc fires the callback in its own goroutine,
		// so Lock acquires the session mutex normally.
		s.idleTimer = time.AfterFunc(idle, s.Lock)
	} else {
		s.idleTimer = nil
	}
}

// sessionStore manages all live sessions. Bound to a single vault
// (one daemon = one vault per plan).
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*webSession
	idle     time.Duration
}

func newSessionStore(idle time.Duration) *sessionStore {
	return &sessionStore{
		sessions: make(map[string]*webSession),
		idle:     idle,
	}
}

// newSID returns a 32-byte random session id encoded as hex (64 chars).
// Crypto/rand failure is treated as a hard error — without entropy we
// can't issue a safe cookie.
func newSID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Create issues a brand-new session bound to the given DEK + vault.
// Returns the session id (for the cookie). The DEK byte slice ownership
// transfers to the session — caller must NOT zero it after the call.
func (st *sessionStore) Create(dek []byte, dv *store.DecryptedVault) (string, error) {
	sid, err := newSID()
	if err != nil {
		return "", err
	}
	s := &webSession{sid: sid}
	s.mu.Lock()
	s.activate(dek, dv, st.idle)
	s.mu.Unlock()

	st.mu.Lock()
	st.sessions[sid] = s
	st.mu.Unlock()
	return sid, nil
}

// Activate re-arms an existing locked session with a fresh DEK + vault.
// Returns false if the sid is unknown (caller should issue a new
// session via Create instead). Cookie is preserved.
func (st *sessionStore) Activate(sid string, dek []byte, dv *store.DecryptedVault) bool {
	st.mu.Lock()
	s, ok := st.sessions[sid]
	st.mu.Unlock()
	if !ok {
		return false
	}
	s.mu.Lock()
	s.activate(dek, dv, st.idle)
	s.mu.Unlock()
	return true
}

// Lookup returns the session for sid, or nil if unknown. The caller
// inspects State() on the result to decide what to do.
func (st *sessionStore) Lookup(sid string) *webSession {
	if sid == "" {
		return nil
	}
	st.mu.Lock()
	s := st.sessions[sid]
	st.mu.Unlock()
	return s
}

// Destroy removes a session (logout). The DEK is zeroed; future
// requests with the same cookie are NoSession (not Locked) and get
// the {"action":"login"} response.
func (st *sessionStore) Destroy(sid string) {
	st.mu.Lock()
	s, ok := st.sessions[sid]
	if ok {
		delete(st.sessions, sid)
	}
	st.mu.Unlock()
	if ok {
		s.Lock()
	}
}

// Shutdown locks every session — used by the graceful-shutdown handler
// on SIGINT/SIGTERM so DEKs are zeroed before the process exits.
func (st *sessionStore) Shutdown() {
	st.mu.Lock()
	all := make([]*webSession, 0, len(st.sessions))
	for _, s := range st.sessions {
		all = append(all, s)
	}
	st.sessions = make(map[string]*webSession)
	st.mu.Unlock()
	for _, s := range all {
		s.Lock()
	}
}
