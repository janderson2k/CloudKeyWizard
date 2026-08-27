package main

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

const (
	authLogFile        = DataDir + "/auth-log.json"
	maxAuthLogEntries  = 500
	maxFailedAttempts  = 5
	lockoutDuration    = 15 * time.Minute
	failWindowDuration = 15 * time.Minute // consecutive-failure counting resets if the gap between failures exceeds this
)

type LoginAttempt struct {
	Username string    `json:"username"`
	Time     time.Time `json:"time"`
	Success  bool      `json:"success"`
	RemoteIP string    `json:"remoteIp"`
}

// AuthLog tracks login history (for the Users tab -- last login, failed attempts) and enforces a
// simple per-username lockout after repeated failures. User-requested (2026-08-23): "Add logging
// here, ability to see when users last logged in/out/failed login attempts" + "Add lockout/rate-
// limiting too" on a console that had neither before.
type AuthLog struct {
	mu          sync.Mutex
	attempts    []LoginAttempt
	failCounts  map[string]int
	lastFailAt  map[string]time.Time
	lockedUntil map[string]time.Time
}

func NewAuthLog() *AuthLog {
	log := &AuthLog{
		failCounts:  map[string]int{},
		lastFailAt:  map[string]time.Time{},
		lockedUntil: map[string]time.Time{},
	}
	log.loadFromDisk()
	return log
}

func (a *AuthLog) loadFromDisk() {
	data, err := os.ReadFile(authLogFile)
	if err != nil {
		return
	}
	var attempts []LoginAttempt
	if json.Unmarshal(data, &attempts) == nil {
		a.mu.Lock()
		a.attempts = attempts
		a.mu.Unlock()
	}
}

func (a *AuthLog) persist() {
	a.mu.Lock()
	data, err := json.Marshal(a.attempts)
	a.mu.Unlock()
	if err != nil {
		return
	}
	tmp := authLogFile + ".tmp"
	if os.WriteFile(tmp, data, 0600) == nil {
		os.Rename(tmp, authLogFile)
	}
}

// LockedOut reports whether username is currently locked out, and if so, when the lockout expires.
func (a *AuthLog) LockedOut(username string) (bool, time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	until, ok := a.lockedUntil[username]
	if !ok || time.Now().After(until) {
		return false, time.Time{}
	}
	return true, until
}

// Record logs one login attempt and updates the lockout state. Call this for every login attempt,
// success or failure, before deciding whether to actually authenticate -- LockedOut should be
// checked first so a locked-out account never even reaches password comparison.
func (a *AuthLog) Record(username, remoteIP string, success bool) {
	a.mu.Lock()
	a.attempts = append(a.attempts, LoginAttempt{Username: username, Time: time.Now(), Success: success, RemoteIP: remoteIP})
	if len(a.attempts) > maxAuthLogEntries {
		a.attempts = a.attempts[len(a.attempts)-maxAuthLogEntries:]
	}

	if success {
		delete(a.failCounts, username)
		delete(a.lastFailAt, username)
		delete(a.lockedUntil, username)
	} else {
		now := time.Now()
		if last, ok := a.lastFailAt[username]; ok && now.Sub(last) > failWindowDuration {
			a.failCounts[username] = 0 // gap was long enough that this is a fresh run, not a continued attack
		}
		a.failCounts[username]++
		a.lastFailAt[username] = now
		if a.failCounts[username] >= maxFailedAttempts {
			if _, alreadyLocked := a.lockedUntil[username]; !alreadyLocked {
				cfg := LoadPushbulletConfig()
				if cfg.Enabled && cfg.AlertLockout {
					notifyLockout(username)
				}
			}
			a.lockedUntil[username] = now.Add(lockoutDuration)
		}
	}
	a.mu.Unlock()
	go a.persist() // off the request path -- login shouldn't wait on a disk write
}

// History returns the most recent attempts, newest first, capped to limit (0 = all).
func (a *AuthLog) History(limit int) []LoginAttempt {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]LoginAttempt, len(a.attempts))
	copy(out, a.attempts)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// LastLoginByUser returns each username's most recent successful login time.
func (a *AuthLog) LastLoginByUser() map[string]time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]time.Time{}
	for _, att := range a.attempts {
		if att.Success {
			if existing, ok := out[att.Username]; !ok || att.Time.After(existing) {
				out[att.Username] = att.Time
			}
		}
	}
	return out
}
