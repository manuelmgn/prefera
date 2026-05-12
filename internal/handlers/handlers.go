// Package handlers contains the HTTP controllers for the application.
package handlers

import (
	"bytes"
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"proj_listas/internal/auth"
)

// Handler holds the shared dependencies for all HTTP controllers.
type Handler struct {
	db   *sql.DB
	tmpl *template.Template
	auth *auth.Manager
}

// New creates a new Handler with all required dependencies.
func New(db *sql.DB, tmpl *template.Template, authMgr *auth.Manager) *Handler {
	return &Handler{
		db:   db,
		tmpl: tmpl,
		auth: authMgr,
	}
}

// renderPage renders a full page: first renders the content into a buffer,
// then injects it into the layout.
// This avoids issues with nested templates in Go.
func (h *Handler) renderPage(w http.ResponseWriter, contentTemplate string, data map[string]interface{}) {
	// 1. Render the page-specific content into a buffer
	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, contentTemplate, data); err != nil {
		log.Printf("Failed to render content '%s': %v", contentTemplate, err)
		http.Error(w, "Erro interno", http.StatusInternalServerError)
		return
	}

	// 2. Inject the rendered HTML into the layout data
	// template.HTML prevents Go from escaping the content HTML
	data["Content"] = template.HTML(buf.String())

	// 3. Render the full layout with the injected content
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		log.Printf("Failed to render layout: %v", err)
		http.Error(w, "Erro interno", http.StatusInternalServerError)
	}
}

// renderPartial renders only a partial template (no layout), for HTMX responses.
func (h *Handler) renderPartial(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("Failed to render partial '%s': %v", name, err)
		http.Error(w, "Erro interno", http.StatusInternalServerError)
	}
}

// renderFullPage renders a full page WITHOUT the layout (e.g. versus screen).
func (h *Handler) renderFullPage(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("Failed to render page '%s': %v", name, err)
		http.Error(w, "Erro interno", http.StatusInternalServerError)
	}
}

// LoginPage renders the login page.
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	h.renderFullPage(w, "login", map[string]interface{}{})
}

// LoginSubmit processes the login form with rate limiting.
func (h *Handler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	username := SanitizeInput(r.FormValue("username"), MaxUsernameLen)
	password := r.FormValue("password")
	clientIP := getClientIP(r)

	if len(password) > MaxPasswordLen {
		password = password[:MaxPasswordLen]
	}

	// Check if user is blocked due to too many failed attempts
	if blocked, _ := h.auth.IsLoginBlocked(username, clientIP); blocked {
		h.renderFullPage(w, "login", map[string]interface{}{
			"Error":   "Demasiadas tentativas falhadas. Tenta de novo em 60 minutos.",
			"Blocked": true,
		})
		return
	}

	user := h.auth.Authenticate(username, password)
	if user == nil {
		// Record failed attempt
		h.auth.RecordFailedLogin(username, clientIP)
		// Check if now blocked
		blocked, attempts := h.auth.IsLoginBlocked(username, clientIP)
		if blocked {
			h.renderFullPage(w, "login", map[string]interface{}{
				"Error":   "Demasiadas tentativas falhadas. Tenta de novo em 60 minutos.",
				"Blocked": true,
			})
		} else {
			remaining := 3 - attempts
			msg := "Utilizador ou palavra-chave incorrectos"
			if remaining <= 2 {
				msg = msg + " (" + strconv.Itoa(remaining) + " tentativa"
				if remaining != 1 {
					msg = msg + "s"
				}
				msg = msg + " restante"
				if remaining != 1 {
					msg = msg + "s"
				}
				msg = msg + ")"
			}
			h.renderFullPage(w, "login", map[string]interface{}{
				"Error": msg,
			})
		}
		return
	}

	// Successful login: clear previous failed attempts
	h.auth.ClearLoginAttempts(username, clientIP)

	if err := h.auth.CreateSession(w, user.ID); err != nil {
		http.Error(w, "Erro ao criar sessom", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// getClientIP returns the client's IP address, accounting for proxies.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (set by proxies/Docker)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// The first IP in the list is the original client
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fallback to remote address (strip the port)
	parts := strings.SplitN(r.RemoteAddr, ":", 2)
	return parts[0]
}

// Logout terminates the user's session.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	h.auth.DestroySession(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// SettingsPage renders the user settings page.
func (h *Handler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	h.renderPage(w, "settings", map[string]interface{}{
		"User":    user,
		"Title":   "Configuraçom",
		"Success": r.URL.Query().Get("success"),
	})
}

// SettingsSubmit processes the user preferences form.
func (h *Handler) SettingsSubmit(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	displayName := SanitizeInput(r.FormValue("display_name"), MaxListNameLen)
	defaultPublic := r.FormValue("default_public") != ""
	defaultVersusMode := r.FormValue("default_versus_mode")
	themePreference := r.FormValue("theme_preference")

	if err := h.auth.UpdateDisplayName(user.ID, displayName); err != nil {
		h.renderPage(w, "settings", map[string]interface{}{
			"User":  user,
			"Title": "Configuraçom",
			"Error": "Erro ao gardar o nome público",
		})
		return
	}

	if err := h.auth.UpdatePreferences(user.ID, defaultPublic, defaultVersusMode, themePreference); err != nil {
		h.renderPage(w, "settings", map[string]interface{}{
			"User":  user,
			"Title": "Configuraçom",
			"Error": "Erro ao gardar preferências",
		})
		return
	}

	http.Redirect(w, r, "/settings?success=1", http.StatusSeeOther)
}

// PasswordChangePage renders the password change form.
func (h *Handler) PasswordChangePage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	h.renderPage(w, "password_change", map[string]interface{}{
		"User":  user,
		"Title": "Mudar palavra-chave",
	})
}

// PasswordChangeSubmit processes the password change form.
func (h *Handler) PasswordChangeSubmit(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	current := r.FormValue("current_password")
	newPass := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")

	if len(current) > MaxPasswordLen || len(newPass) > MaxPasswordLen {
		h.renderPage(w, "password_change", map[string]interface{}{
			"User":  user,
			"Title": "Mudar palavra-chave",
			"Error": "Palavra-chave demasiado longa",
		})
		return
	}

	if newPass != confirm {
		h.renderPage(w, "password_change", map[string]interface{}{
			"User":  user,
			"Title": "Mudar palavra-chave",
			"Error": "As palavras-chave nom coincidem",
		})
		return
	}

	if len(newPass) < 6 {
		h.renderPage(w, "password_change", map[string]interface{}{
			"User":  user,
			"Title": "Mudar palavra-chave",
			"Error": "A nova palavra-chave deve ter ao menos 6 caracteres",
		})
		return
	}

	if err := h.auth.ChangePassword(user.ID, current, newPass); err != nil {
		msg := "Erro ao mudar a palavra-chave"
		if err == auth.ErrWrongPassword {
			msg = "Palavra-chave actual incorrecta"
		}
		h.renderPage(w, "password_change", map[string]interface{}{
			"User":  user,
			"Title": "Mudar palavra-chave",
			"Error": msg,
		})
		return
	}

	http.Redirect(w, r, "/settings?success=pw", http.StatusSeeOther)
}

// AdminPanel renders the administration panel.
func (h *Handler) AdminPanel(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if !user.IsAdmin {
		http.Error(w, "Sem permissom", http.StatusForbidden)
		return
	}

	// Fetch statistics for all non-admin users
	rows, err := h.db.Query(`
		SELECT u.id, u.username, u.display_name, u.is_admin, u.last_login_at, u.created_at,
		       COALESCE(SUM(CASE WHEN l.is_public = 1 THEN 1 ELSE 0 END), 0) as public_lists,
		       COALESCE(SUM(CASE WHEN l.is_public = 0 THEN 1 ELSE 0 END), 0) as private_lists,
		       COUNT(l.id) as total_lists
		FROM users u
		LEFT JOIN lists l ON u.id = l.user_id AND l.collective_source_id IS NULL
		WHERE u.is_admin = 0
		GROUP BY u.id
		ORDER BY u.username
	`)
	if err != nil {
		http.Error(w, "Erro ao obter dados", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type UserStats struct {
		ID           int
		Username     string
		DisplayName  string
		IsAdmin      bool
		LastLogin    sql.NullString
		CreatedAt    string
		PublicLists  int
		PrivateLists int
		TotalLists   int
	}

	var stats []UserStats
	for rows.Next() {
		var s UserStats
		if err := rows.Scan(&s.ID, &s.Username, &s.DisplayName, &s.IsAdmin, &s.LastLogin,
			&s.CreatedAt, &s.PublicLists, &s.PrivateLists, &s.TotalLists); err != nil {
			continue
		}
		stats = append(stats, s)
	}

	// Collective list statistics per user
	collectiveRows, err := h.db.Query(`
		SELECT u.id, u.username,
		       COALESCE(SUM(CASE WHEN cl.creator_id = u.id THEN 1 ELSE 0 END), 0) as created,
		       COALESCE(SUM(CASE WHEN cl.creator_id != u.id THEN 1 ELSE 0 END), 0) as participated
		FROM users u
		LEFT JOIN collective_participants cp ON u.id = cp.user_id
		LEFT JOIN collective_lists cl ON cp.collective_id = cl.id
		WHERE u.is_admin = 0
		GROUP BY u.id
		ORDER BY u.username
	`)

	type CollectiveStats struct {
		ID           int
		Username     string
		Created      int
		Participated int
		Total        int
	}

	var cStats []CollectiveStats
	if err == nil {
		defer collectiveRows.Close()
		for collectiveRows.Next() {
			var cs CollectiveStats
			if err := collectiveRows.Scan(&cs.ID, &cs.Username, &cs.Created, &cs.Participated); err != nil {
				continue
			}
			cs.Total = cs.Created + cs.Participated
			cStats = append(cStats, cs)
		}
	}

	h.renderPage(w, "admin", map[string]interface{}{
		"User":            user,
		"Title":           "Painel de administraçom",
		"UserStats":       stats,
		"CollectiveStats": cStats,
		"Success":         r.URL.Query().Get("success"),
		"Error":           r.URL.Query().Get("error"),
	})
}

// AdminCreateUser creates a new user account.
func (h *Handler) AdminCreateUser(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if !user.IsAdmin {
		http.Error(w, "Sem permissom", http.StatusForbidden)
		return
	}

	username := SanitizeInput(r.FormValue("username"), MaxUsernameLen)
	displayName := SanitizeInput(r.FormValue("display_name"), MaxListNameLen)
	password := r.FormValue("password")

	if username == "" || password == "" {
		http.Redirect(w, r, "/admin?error=Nome+de+utilizador+e+palavra-chave+som+obrigatórios", http.StatusSeeOther)
		return
	}

	if len(password) < 6 {
		http.Redirect(w, r, "/admin?error=A+palavra-chave+deve+ter+ao+menos+6+caracteres", http.StatusSeeOther)
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		http.Redirect(w, r, "/admin?error=Erro+ao+criar+utilizador", http.StatusSeeOther)
		return
	}

	_, err = h.db.Exec(
		"INSERT INTO users (username, password_hash, display_name) VALUES (?, ?, ?)",
		username, hash, displayName,
	)
	if err != nil {
		http.Redirect(w, r, "/admin?error=Utilizador+já+existe+ou+erro+na+base+de+dados", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin?success=Utilizador+criado", http.StatusSeeOther)
}

// AdminDeleteUser deletes a user and all their lists.
func (h *Handler) AdminDeleteUser(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if !user.IsAdmin {
		http.Error(w, "Sem permissom", http.StatusForbidden)
		return
	}

	targetID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	// Prevent deleting the admin's own account
	if targetID == user.ID {
		http.Redirect(w, r, "/admin?error=Nom+podes+apagar+a+tua+própria+conta", http.StatusSeeOther)
		return
	}

	// Verify the target exists and is not an admin
	var isAdmin int
	err = h.db.QueryRow("SELECT is_admin FROM users WHERE id = ?", targetID).Scan(&isAdmin)
	if err != nil {
		http.Redirect(w, r, "/admin?error=Utilizador+nom+encontrado", http.StatusSeeOther)
		return
	}
	if isAdmin == 1 {
		http.Redirect(w, r, "/admin?error=Nom+se+pode+apagar+um+administrador", http.StatusSeeOther)
		return
	}

	// Delete sessions, lists (items via CASCADE), and the user
	tx, err := h.db.Begin()
	if err != nil {
		http.Redirect(w, r, "/admin?error=Erro+interno", http.StatusSeeOther)
		return
	}
	defer tx.Rollback()

	tx.Exec("DELETE FROM sessions WHERE user_id = ?", targetID)
	tx.Exec("DELETE FROM lists WHERE user_id = ?", targetID)
	tx.Exec("DELETE FROM users WHERE id = ?", targetID)

	if err := tx.Commit(); err != nil {
		http.Redirect(w, r, "/admin?error=Erro+ao+apagar+utilizador", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin?success=Utilizador+apagado", http.StatusSeeOther)
}

// AdminChangePassword changes the password of any user.
func (h *Handler) AdminChangePassword(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if !user.IsAdmin {
		http.Error(w, "Sem permissom", http.StatusForbidden)
		return
	}

	targetID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	newPassword := r.FormValue("new_password")
	if len(newPassword) < 6 {
		http.Redirect(w, r, "/admin?error=A+palavra-chave+deve+ter+ao+menos+6+caracteres", http.StatusSeeOther)
		return
	}

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		http.Redirect(w, r, "/admin?error=Erro+ao+gerar+hash", http.StatusSeeOther)
		return
	}

	_, err = h.db.Exec("UPDATE users SET password_hash = ? WHERE id = ?", hash, targetID)
	if err != nil {
		http.Redirect(w, r, "/admin?error=Erro+ao+mudar+palavra-chave", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/admin?success=Palavra-chave+mudada", http.StatusSeeOther)
}
