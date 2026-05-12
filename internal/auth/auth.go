package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"time"
)

// User representa um utilizador autenticado.
type User struct {
	ID                int
	Username          string
	DisplayName       string // Nome público (se vazio, usa Username)
	IsAdmin           bool
	DefaultPublic     bool   // Preferência: listas públicas por defeito?
	DefaultVersusMode string // Preferência: "rapido" ou "detalhado"
	ThemePreference   string // Preferência: "auto", "light" ou "dark"
}

// PublicName devolve o nome público do utilizador
func (u *User) PublicName() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

type contextKey string

const userContextKey contextKey = "user"

// Manager gere todas as operaçons de autenticaçom
type Manager struct {
	db *sql.DB
}

func NewManager(db *sql.DB) *Manager {
	return &Manager{db: db}
}

func (m *Manager) GetDB() *sql.DB {
	return m.db
}

// UserFromContext extrai o utilizador do contexto do pedido HTTP
func UserFromContext(ctx context.Context) *User {
	user, ok := ctx.Value(userContextKey).(*User)
	if !ok {
		return nil
	}
	return user
}

// RequireAuth verifica que o utilizador está autenticado
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

// CreateSession cria uma nova sessom (cookie de 30 dias)
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

	// Registar a última conexom
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

// DestroySession elimina a sessom actual
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

// Authenticate verifica as credenciais
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

// ChangePassword muda a palavra-chave
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

// UpdatePreferences actualiza as preferências do utilizador
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

// UpdateDisplayName actualiza o nome público do utilizador
func (m *Manager) UpdateDisplayName(userID int, displayName string) error {
	_, err := m.db.Exec("UPDATE users SET display_name = ? WHERE id = ?", displayName, userID)
	return err
}

// CleanExpiredSessions apaga sessons expiradas e tentativas de login antigas
func (m *Manager) CleanExpiredSessions() {
	m.db.Exec("DELETE FROM sessions WHERE expires_at < ?", time.Now())
	// Limpar tentativas de login com mais de 2 horas
	m.db.Exec("DELETE FROM login_attempts WHERE attempted_at < ?", time.Now().Add(-2*time.Hour))
}

// IsLoginBlocked verifica se um utilizador está bloqueado por demasiadas tentativas falhadas.
// Bloqueia durante 60 minutos após 3 tentativas falhadas.
func (m *Manager) IsLoginBlocked(username, ip string) (bool, int) {
	cutoff := time.Now().Add(-60 * time.Minute)
	var count int
	// Contar tentativas falhadas nos últimos 60 minutos para este username OU IP
	err := m.db.QueryRow(
		"SELECT COUNT(*) FROM login_attempts WHERE (username = ? OR ip_address = ?) AND attempted_at > ?",
		username, ip, cutoff,
	).Scan(&count)
	if err != nil {
		return false, 0
	}
	return count >= 3, count
}

// RecordFailedLogin regista uma tentativa de login falhada
func (m *Manager) RecordFailedLogin(username, ip string) {
	m.db.Exec(
		"INSERT INTO login_attempts (username, ip_address) VALUES (?, ?)",
		username, ip,
	)
}

// ClearLoginAttempts limpa as tentativas falhadas após um login com sucesso
func (m *Manager) ClearLoginAttempts(username, ip string) {
	m.db.Exec("DELETE FROM login_attempts WHERE username = ? OR ip_address = ?", username, ip)
}
