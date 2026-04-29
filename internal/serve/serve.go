// Package serve hosts the kpot WebUI. One daemon per vault, bound to
// 127.0.0.1, accessed from a phone via SSH tunnel + VPN.
//
// Architecture and threat model are documented in docs/serve.md and
// /home/shin/.claude/plans/kpot-webui-url-id-ssh-vpn-vpn-fw0-ssh-we-distributed-charm.md.
package serve

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Shin-R2un/kpot/internal/config"
	"github.com/Shin-R2un/kpot/internal/crypto"
	"github.com/Shin-R2un/kpot/internal/keychain"
	"github.com/Shin-R2un/kpot/internal/store"
	"github.com/Shin-R2un/kpot/internal/vault"
)

// Options configure a Run() call. Zero values are sensible defaults
// where applicable.
type Options struct {
	// VaultPath is the resolved path to the .kpot file. Caller is
	// responsible for v0.7+ name resolution.
	VaultPath string

	// Port is the TCP port to listen on. 0 means use 8765.
	Port int

	// Idle is the per-session idle timeout. Zero means use 30 min.
	// Negative disables idle locking entirely.
	Idle time.Duration

	// NoCache forces the daemon to skip the OS keychain even if a
	// DEK is cached. Every web visit then requires a passphrase via
	// the login form.
	NoCache bool

	// Cfg supplies keychain mode, idle defaults, etc.
	Cfg config.Config
}

// Server holds runtime state shared across handlers.
type Server struct {
	vaultPath   string
	idleTimeout time.Duration
	sessions    *sessionStore
	rateLimit   *loginRateLimiter

	// Bootstrap state: populated when the keychain holds a DEK at
	// startup AND NoCache wasn't requested. Cookieless requests then
	// auto-create a session using this DV, so the user doesn't have
	// to type the passphrase on the phone for the first visit.
	//
	// Idle expiry on auto-created sessions still fires, so re-auth
	// after inactivity remains a passphrase prompt — bootstrap is
	// not consulted again after the initial mint.
	bootstrapDV  *store.DecryptedVault
	bootstrapDEK []byte
}

// Run starts the WebUI server and blocks until SIGINT/SIGTERM.
func Run(opts Options) error {
	if opts.VaultPath == "" {
		return errors.New("serve: VaultPath required")
	}
	port := opts.Port
	if port == 0 {
		port = 8765
	}
	idle := opts.Idle
	if idle == 0 {
		idle = 30 * time.Minute
	}
	if idle < 0 {
		idle = 0 // 0 = disabled inside session
	}

	s := &Server{
		vaultPath:   opts.VaultPath,
		idleTimeout: idle,
		sessions:    newSessionStore(idle),
		rateLimit:   newLoginRateLimiter(),
	}

	// Bootstrap from keychain if allowed. Failure is non-fatal — we
	// just fall back to passphrase-only mode and tell the user.
	if !opts.NoCache && opts.Cfg.KeychainMode() != config.KeychainNever {
		if err := s.tryBootstrap(); err != nil {
			fmt.Fprintf(os.Stderr,
				"serve: keychain bootstrap unavailable (%v); login form required\n", err)
		} else {
			fmt.Fprintln(os.Stderr,
				"serve: keychain bootstrap active — phone first visit auto-unlocks")
		}
	}

	mux := s.mux()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		// Allow long-running phone connections (Keep-Alive friendly).
		IdleTimeout: 60 * time.Second,
	}

	fmt.Fprintf(os.Stderr,
		"kpot serve: %s — http://localhost:%d/  (Ctrl-C to stop)\n",
		opts.VaultPath, port)
	fmt.Fprintf(os.Stderr,
		"  SSH tunnel from your phone:\n    ssh -L %d:127.0.0.1:%d user@<this host>\n",
		port, port)

	// Catch SIGINT/SIGTERM for graceful shutdown.
	idleConnsClosed := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		fmt.Fprintln(os.Stderr, "\nserve: shutting down…")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		s.sessions.Shutdown()
		if s.bootstrapDEK != nil {
			crypto.Zero(s.bootstrapDEK)
			s.bootstrapDEK = nil
		}
		s.bootstrapDV = nil
		close(idleConnsClosed)
	}()

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	<-idleConnsClosed
	return nil
}

// mux builds the http.ServeMux. Exposed as a method so tests can wrap
// it in httptest.NewServer without spinning up the real listener.
func (s *Server) mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/notes", s.handleNotes)
	mux.HandleFunc("/api/notes/", s.handleNote)
	mux.Handle("/static/", staticHandler())
	mux.HandleFunc("/", indexHandler())
	return mux
}

// newServerForTests constructs a Server without starting the listener
// or attempting keychain bootstrap. Test code controls bootstrap by
// setting bootstrapDV directly. Production code goes through Run.
func newServerForTests(vaultPath string, idle time.Duration) *Server {
	return &Server{
		vaultPath:   vaultPath,
		idleTimeout: idle,
		sessions:    newSessionStore(idle),
		rateLimit:   newLoginRateLimiter(),
	}
}

// tryBootstrap attempts to unlock the vault using a DEK cached in the
// OS keychain. On success the DV and DEK are stashed in the Server so
// cookieless requests can auto-mint a session. Errors are returned —
// caller decides whether to log and continue or abort.
func (s *Server) tryBootstrap() error {
	kc := keychain.Default()
	if !kc.Available() {
		return errors.New("no OS keychain backend on this system")
	}
	account := keychain.CanonicalAccount(s.vaultPath)
	dek, err := kc.Get(account)
	if err != nil {
		if errors.Is(err, keychain.ErrNotFound) {
			return errors.New("keychain has no DEK for this vault")
		}
		return fmt.Errorf("keychain get: %w", err)
	}
	if len(dek) == 0 {
		return errors.New("keychain returned empty DEK")
	}
	plaintext, _, err := vault.OpenWithKey(s.vaultPath, dek)
	if err != nil {
		// DEK no longer matches the vault (passphrase rotated since
		// last cache). Don't keep the stale key around.
		crypto.Zero(dek)
		return fmt.Errorf("vault open with cached key: %w", err)
	}
	defer crypto.Zero(plaintext)
	dv, err := store.FromJSON(plaintext)
	if err != nil {
		crypto.Zero(dek)
		return fmt.Errorf("decode vault payload: %w", err)
	}
	s.bootstrapDV = dv
	s.bootstrapDEK = dek
	return nil
}

// resolveSession returns the session this request should use. If the
// request has no cookie and the bootstrap is available, mints a new
// session and sets the cookie inline. Returns nil if the request must
// be 401'd (caller has already written the response in that case).
//
// Used by every /api/notes* handler in place of requireSession when
// bootstrap-shortcut behavior is desired. requireSession is still used
// by login/logout/status.
func (s *Server) resolveSession(w http.ResponseWriter, r *http.Request) *webSession {
	c, err := r.Cookie(cookieName)
	if err == nil && c.Value != "" {
		sess := s.sessions.Lookup(c.Value)
		if sess != nil {
			if sess.State() == StateLocked {
				writeError(w, http.StatusUnauthorized, "session locked", actionReauth)
				return nil
			}
			sess.Touch(s.idleTimeout)
			return sess
		}
		// Cookie present but server forgot the sid (process restart)
		// → fall through to bootstrap or 401.
	}
	if s.bootstrapDV != nil {
		sid, err := s.sessions.Create(nil, s.bootstrapDV)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "session create", "")
			return nil
		}
		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    sid,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
		return s.sessions.Lookup(sid)
	}
	writeError(w, http.StatusUnauthorized, "login required", actionLogin)
	return nil
}
