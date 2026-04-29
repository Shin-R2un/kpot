package serve

import (
	"sync"
	"time"
)

// loginRateLimiter is a tiny in-memory throttle for /api/login. It
// counts failures in a sliding window and trips a cool-off when the
// threshold is exceeded.
//
// Default policy: 3 failures per 60s window → 30s lockout. Resets on
// any successful login.
//
// Keyed by remote-addr-string. In the SSH-tunnel use case every
// request comes from 127.0.0.1 so this is effectively a single
// counter, but the per-key shape lets us add IP-based protection
// later if the bind ever opens up.
type loginRateLimiter struct {
	mu      sync.Mutex
	state   map[string]*rlState
	window  time.Duration // failure-counting window
	thresh  int           // failures within window that trip the lockout
	cooloff time.Duration // lockout duration after threshold
	now     func() time.Time
}

type rlState struct {
	failures    []time.Time
	lockedUntil time.Time
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{
		state:   make(map[string]*rlState),
		window:  60 * time.Second,
		thresh:  3,
		cooloff: 30 * time.Second,
		now:     time.Now,
	}
}

// allow reports whether a login attempt for the given key should be
// processed right now. Returns false during a lockout period.
//
// allow does NOT record an attempt — call recordFailure or
// recordSuccess after attempting the unlock.
func (rl *loginRateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	st := rl.state[key]
	if st == nil {
		return true
	}
	if !st.lockedUntil.IsZero() && rl.now().Before(st.lockedUntil) {
		return false
	}
	return true
}

// recordFailure registers a failed login. If the failure count crosses
// the threshold within the window, the key enters a cool-off lockout.
func (rl *loginRateLimiter) recordFailure(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	st := rl.state[key]
	if st == nil {
		st = &rlState{}
		rl.state[key] = st
	}
	now := rl.now()
	// Drop failures outside the window.
	cutoff := now.Add(-rl.window)
	kept := st.failures[:0]
	for _, t := range st.failures {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	st.failures = append(kept, now)
	if len(st.failures) >= rl.thresh {
		st.lockedUntil = now.Add(rl.cooloff)
		// Clear the count so the lockout period is the only gate.
		st.failures = st.failures[:0]
	}
}

// recordSuccess clears any pending failure count and lockout for the
// key. Called after a successful login so a legit user who fat-fingered
// twice doesn't stay throttled.
func (rl *loginRateLimiter) recordSuccess(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.state, key)
}
