package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func handleLogin(users *UserStore, sessions *SessionStore, authLog *AuthLog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		req.Username = strings.TrimSpace(req.Username)
		ip := remoteIP(r)

		if locked, until := authLog.LockedOut(req.Username); locked {
			minutesLeft := int(time.Until(until).Minutes()) + 1
			http.Error(w, fmt.Sprintf(`{"error":"too many failed attempts -- locked out for about %d more minute(s)"}`, minutesLeft), http.StatusTooManyRequests)
			return
		}

		if !users.Authenticate(req.Username, req.Password) {
			authLog.Record(req.Username, ip, false)
			// Deliberately generic -- doesn't distinguish "no such user" from "wrong password."
			http.Error(w, `{"error":"invalid username or password"}`, http.StatusUnauthorized)
			return
		}
		authLog.Record(req.Username, ip, true)
		token, err := sessions.Create(req.Username)
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
			// This is just the browser-side retention hint (matches the server's absolute cap) --
			// actual expiry is enforced server-side by SessionStore, which also applies the
			// shorter 5-minute idle timeout this cookie's own Expires can't express.
			Expires: time.Now().Add(absoluteTimeout),
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}
}

func handleWhoami(w http.ResponseWriter, r *http.Request, username string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"username": username})
}

func handleLogout(sessions *SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			sessions.Revoke(cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1})
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}
}
