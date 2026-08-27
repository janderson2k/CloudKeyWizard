package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const (
	sessionCookieName = "fdtscout_session"
	// idleTimeout logs a session out after this long with no authenticated request -- refreshed on
	// every request that passes through requireAuth, not just on login. User-requested (2026-08-23):
	// "session tracking when logged in needs to be better... time out after 5 min of idle time."
	idleTimeout = 5 * time.Minute
	// absoluteTimeout is a hard ceiling regardless of activity -- defense in depth against a
	// session that's kept alive indefinitely by some automated/background traffic (e.g. a
	// dashboard tab left open polling metrics) never actually going idle. The cookie itself is
	// also capped at this so the browser doesn't hold onto a token long past any real use.
	absoluteTimeout = 24 * time.Hour
	// cleanupInterval is how often the in-memory session map is swept for anything expired --
	// pure hygiene (bounds memory for a console that's realistically used by a handful of admin
	// accounts), Validate() itself always enforces expiry correctly even between sweeps.
	cleanupInterval = time.Minute
)

type session struct {
	Username     string
	Created      time.Time
	LastActivity time.Time
}

func (s session) expired(now time.Time) bool {
	return now.Sub(s.LastActivity) > idleTimeout || now.Sub(s.Created) > absoluteTimeout
}

// SessionStore is deliberately in-memory only, not persisted -- a service restart (e.g. after a
// systemd unit update) simply logs everyone out rather than needing a signing key or a session
// table on disk. Given this console is meant for occasional admin use, not always-on sessions,
// that trade is worth the simplicity.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]session)}
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *SessionStore) Create(username string) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = session{Username: username, Created: now, LastActivity: now}
	return token, nil
}

// Validate checks a session token AND slides its idle timer forward -- every authenticated
// request (page load, API call) counts as activity, so a session genuinely being used stays alive
// past idleTimeout, but one left untouched (browser tab open, nothing clicked) expires on schedule.
func (s *SessionStore) Validate(token string) (string, bool) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[token]
	if !ok || sess.expired(now) {
		delete(s.sessions, token)
		return "", false
	}
	sess.LastActivity = now
	s.sessions[token] = sess
	return sess.Username, true
}

func (s *SessionStore) Revoke(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// RunCleanup periodically evicts expired sessions until stop is closed. Meant to be launched in
// its own goroutine -- purely a memory-hygiene sweep, Validate() already enforces expiry correctly
// for any session actually looked up between sweeps.
func (s *SessionStore) RunCleanup(stop <-chan struct{}) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			now := time.Now()
			s.mu.Lock()
			for token, sess := range s.sessions {
				if sess.expired(now) {
					delete(s.sessions, token)
				}
			}
			s.mu.Unlock()
		}
	}
}

// requireAuth wraps a handler so it 401s (JSON) or redirects to /login (page loads) unless a
// valid, non-idle session cookie is present.
func requireAuth(sessions *SessionStore, isAPI bool, next func(w http.ResponseWriter, r *http.Request, username string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			denyUnauthenticated(w, r, isAPI)
			return
		}
		username, ok := sessions.Validate(cookie.Value)
		if !ok {
			// Clear the stale cookie so the browser doesn't keep re-sending a dead token, and so a
			// page load (not just an API call) after idle-timeout visibly lands back on /login
			// rather than silently doing nothing.
			http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1})
			denyUnauthenticated(w, r, isAPI)
			return
		}
		next(w, r, username)
	}
}

func denyUnauthenticated(w http.ResponseWriter, r *http.Request, isAPI bool) {
	if isAPI {
		http.Error(w, `{"error":"session expired -- please log in again"}`, http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
