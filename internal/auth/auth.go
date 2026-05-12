package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"time"
)

// User represents an authenticated user.
type User struct {
	ID                int
	Username          string
	DisplayName       string // Public display name (falls back to Username if empty)
	IsAdmin           bool
	DefaultPublic     bool   // Preference: make lists public by default?
	DefaultVersusMode string // Preference: "rapido" or "detalhado"
	ThemePreference   string // Preference: "auto", "light" or "dark"
}

// PublicName returns the user's public display name.
func (u *User) PublicName() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

type contextKey string

const userContextKey contextKey = "user"

// Manager handles all authentication operations.
type Manager struct {
	db *sql.DB
}

func NewManager(db *sql.DB) *Manager {
	return &Manager{db: db}
}

func (m *Manager) GetDB() *sql.DB {
	return m.db
}

// UserFromContext extracts the authenticated user from the HTTP request context.
func UserFromContext(ctx context.Context) *User {
	user, ok := ctx.Value(userContextKey).(*User)
	if !ok {
		return nil
	}
	return user
}

// RequireAuth is middleware that verifies the user is authenticated.
func (m *Manager) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		var user User
		var defaultPublic int
		err = m.db.QueryRow(`
			SELECT u.id, u.username, u.display_name, u.is_admin, u.default_public,
			       u.default_versus_mode, u.theme_preference
			FROM sessions s
			JOIN users u ON s.user_id = u.id
			WHERE s.token = ? AND s.expires_at > ?
		`, cookie.Value, time.Now()).Scan(
			&user.ID, &user.Username, &user.DisplayName, &user.IsAdmin,
			&defaultPublic, &user.DefaultVersusMode, &user.ThemePreference,
		)

		if err != nil {
			http.SetCookie(w, &http.Cookie{
				Name: "session", Value: "", Path: "/",
				MaxAge: -1, HttpOnly: true,
			})
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		user.DefaultPublic = defaultPublic == 1

		ctx := context.WithValue(r.Context(), userContextKey, &user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CreateSession creates a new session cookie valid for 30 days.
func (m *Manager) CreateSession(w http.ResponseWriter, userID int) error {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return err
	}
	token := hex.EncodeToString(tokenBytes)

	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	_, err := m.db.Exec(
		"INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)",
		token, userID, expiresAt,
	)
	if err != nil {
		return err
	}

	// Record last login timestamp
	m.db.Exec("UPDATE users SET last_login_at = CURRENT_TIMESTAMP WHERE id = ?", userID)

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
	})

	return nil
}

// DestroySession deletes the current session cookie.
func (m *Manager) DestroySession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		m.db.Exec("DELETE FROM sessions WHERE token = ?", cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: "session", Value: "", Path: "/",
		MaxAge: -1, HttpOnly: true,
	})
}

// Authenticate verifies user credentials and returns the user on success.
func (m *Manager) Authenticate(username, password string) *User {
	var user User
	var passwordHash string
	var defaultPublic int

	err := m.db.QueryRow(
		"SELECT id, username, display_name, password_hash, is_admin, default_public, default_versus_mode, theme_preference FROM users WHERE username = ?",
		username,
	).Scan(&user.ID, &user.Username, &user.DisplayName, &passwordHash, &user.IsAdmin,
		&defaultPublic, &user.DefaultVersusMode, &user.ThemePreference)

	if err != nil {
		return nil
	}

	if !CheckPassword(passwordHash, password) {
		return nil
	}

	user.DefaultPublic = defaultPublic == 1
	return &user
}

// ChangePassword updates the user's password after verifying the current one.
func (m *Manager) ChangePassword(userID int, currentPassword, newPassword string) error {
	var passwordHash string
	err := m.db.QueryRow(
		"SELECT password_hash FROM users WHERE id = ?", userID,
	).Scan(&passwordHash)
	if err != nil {
		return err
	}

	if !CheckPassword(passwordHash, currentPassword) {
		return ErrWrongPassword
	}

	newHash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	_, err = m.db.Exec("UPDATE users SET password_hash = ? WHERE id = ?", newHash, userID)
	return err
}

// UpdatePreferences saves the user's display and behaviour preferences.
func (m *Manager) UpdatePreferences(userID int, defaultPublic bool, defaultVersusMode, themePreference string) error {
	dp := 0
	if defaultPublic {
		dp = 1
	}
	if defaultVersusMode != "detalhado" {
		defaultVersusMode = "rapido"
	}
	if themePreference != "light" && themePreference != "dark" {
		themePreference = "auto"
	}
	_, err := m.db.Exec(
		"UPDATE users SET default_public = ?, default_versus_mode = ?, theme_preference = ? WHERE id = ?",
		dp, defaultVersusMode, themePreference, userID,
	)
	return err
}

// UpdateDisplayName updates the user's public display name.
func (m *Manager) UpdateDisplayName(userID int, displayName string) error {
	_, err := m.db.Exec("UPDATE users SET display_name = ? WHERE id = ?", displayName, userID)
	return err
}

// CleanExpiredSessions deletes expired sessions and old failed login attempts.
func (m *Manager) CleanExpiredSessions() {
	m.db.Exec("DELETE FROM sessions WHERE expires_at < ?", time.Now())
	// Remove login attempts older than 2 hours
	m.db.Exec("DELETE FROM login_attempts WHERE attempted_at < ?", time.Now().Add(-2*time.Hour))
}

// IsLoginBlocked returns true if the user is blocked due to too many failed attempts.
// Blocks for 60 minutes after 3 failed attempts.
func (m *Manager) IsLoginBlocked(username, ip string) (bool, int) {
	cutoff := time.Now().Add(-60 * time.Minute)
	var count int
	// Count failed attempts in the last 60 minutes for this username OR IP
	err := m.db.QueryRow(
		"SELECT COUNT(*) FROM login_attempts WHERE (username = ? OR ip_address = ?) AND attempted_at > ?",
		username, ip, cutoff,
	).Scan(&count)
	if err != nil {
		return false, 0
	}
	return count >= 3, count
}

// RecordFailedLogin records a failed login attempt.
func (m *Manager) RecordFailedLogin(username, ip string) {
	m.db.Exec(
		"INSERT INTO login_attempts (username, ip_address) VALUES (?, ?)",
		username, ip,
	)
}

// ClearLoginAttempts clears failed attempts after a successful login.
func (m *Manager) ClearLoginAttempts(username, ip string) {
	m.db.Exec("DELETE FROM login_attempts WHERE username = ? OR ip_address = ?", username, ip)
}
