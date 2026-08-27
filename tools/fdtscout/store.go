package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// User is what's persisted to disk. PasswordHash is a bcrypt hash, never the plaintext -- the
// plaintext only ever exists in memory for the duration of a single request handler.
type User struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"passwordHash"`
	CreatedAt    time.Time `json:"createdAt"`
}

// PublicUser is what's ever sent back to the browser -- no hash, ever.
type PublicUser struct {
	Username  string     `json:"username"`
	CreatedAt time.Time  `json:"createdAt"`
	LastLogin *time.Time `json:"lastLogin,omitempty"`
}

// UserStore is a small file-backed account store. Not a database -- this app expects a handful
// of admin accounts, not thousands of users, so a mutex-guarded JSON file on disk (rewritten
// atomically on every change) is simpler and has fewer moving parts than embedding sqlite.
type UserStore struct {
	mu    sync.RWMutex
	users []User
}

func LoadUserStore() (*UserStore, error) {
	s := &UserStore{}
	data, err := os.ReadFile(UsersFile)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.users); err != nil {
		return nil, fmt.Errorf("users.json is corrupt: %w", err)
	}
	return s, nil
}

func (s *UserStore) save() error {
	data, err := json.MarshalIndent(s.users, "", "  ")
	if err != nil {
		return err
	}
	// Write to a temp file then rename -- rename is atomic on the same filesystem, so a crash or
	// power loss mid-write never leaves users.json half-written/corrupt.
	tmp := UsersFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, UsersFile)
}

func (s *UserStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users)
}

func (s *UserStore) List() []PublicUser {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PublicUser, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, PublicUser{Username: u.Username, CreatedAt: u.CreatedAt})
	}
	return out
}

func (s *UserStore) Authenticate(username, password string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.Username == username {
			return bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil
		}
	}
	// Still run bcrypt's compare against a dummy hash on a not-found username, so a login attempt
	// against a nonexistent account doesn't return measurably faster than one against a real
	// account with a wrong password (a basic username-enumeration timing guard).
	_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$7EqJtq98hPqEX7fNZaFWoOhi5L6bMdd/6z8n/vNbGx.2E7A5V6mnu"), []byte(password))
	return false
}

func (s *UserStore) AddOrReplace(username, password string) error {
	if username == "" || password == "" {
		return errors.New("username and password are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, u := range s.users {
		if u.Username == username {
			s.users[i].PasswordHash = string(hash)
			return s.save()
		}
	}
	s.users = append(s.users, User{Username: username, PasswordHash: string(hash), CreatedAt: time.Now().UTC()})
	return s.save()
}

func (s *UserStore) Remove(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.users) <= 1 {
		return errors.New("can't remove the last remaining account -- that would lock everyone out of this web console")
	}
	for i, u := range s.users {
		if u.Username == username {
			s.users = append(s.users[:i], s.users[i+1:]...)
			return s.save()
		}
	}
	return fmt.Errorf("no such user: %s", username)
}
