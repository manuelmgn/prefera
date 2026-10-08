// Package server builds the HTTP router for the application.
// Templates, static files and the link-domain list are embedded
// into the binary so the same handler works in local development,
// Docker and Vercel serverless functions.
package server

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"time"

	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"proj_listas/internal/auth"
	"proj_listas/internal/handlers"
)

//go:embed templates
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

//go:embed dominios.txt
var defaultDomains []byte

// FuncMap exposes the template helper functions shared by the router.
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"linkType":        handlers.LinkType,
		"isImageURL":      handlers.IsImageURL,
		"allowedPatterns": handlers.AllowedPatternsJS,
		"formatDate":      FormatDateStr,
		"formatTime":      handlers.FormatTime,
	}
}

// FormatDateStr converts a SQLite date string to DD/MM/YYYY.
func FormatDateStr(s string) string {
	layouts := []string{
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("02/01/2006")
		}
	}
	return s
}

// NewRouter creates the full chi router for the application.
// The database must already be opened and migrated.
func NewRouter(database *sql.DB) (http.Handler, error) {
	// Load the allowed link domains (embedded by default, overridable via DOMAINS_PATH)
	if err := loadLinkDomains(); err != nil {
		return nil, err
	}

	// Parse the embedded HTML templates
	tmpl, err := template.New("").Funcs(FuncMap()).
		ParseFS(templatesFS, "templates/*.html", "templates/partials/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to load templates: %w", err)
	}

	// Authentication manager; delete expired sessions on each cold start
	// (serverless has no background goroutines)
	authManager := auth.NewManager(database)
	authManager.CleanExpiredSessions()

	// Route handlers
	h := handlers.New(database, tmpl, authManager)

	r := chi.NewRouter()

	// Global middleware: logging, panic recovery and security headers
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(handlers.SecurityHeaders)

	// Static files from the embedded filesystem
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("failed to configure static files: %w", err)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Public routes (no authentication required)
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.LoginSubmit)
	r.Get("/register", h.RegisterPage)
	r.Post("/register", h.RegisterSubmit)

	// Protected routes (require authentication)
	r.Group(func(r chi.Router) {
		r.Use(authManager.RequireAuth)

		// Main page
		r.Get("/", h.Dashboard)
		r.Post("/logout", h.Logout)

		// My lists
		r.Get("/my-lists", h.MyListsPage)
		r.Get("/my-lists/all", h.MyListsAllPage)
		r.Get("/my-lists/collectives", h.MyListsCollectivesPage)

		// User settings
		r.Get("/settings", h.SettingsPage)
		r.Post("/settings", h.SettingsSubmit)

		// Change password
		r.Get("/password", h.PasswordChangePage)
		r.Post("/password", h.PasswordChangeSubmit)

		// List management
		r.Get("/lists/new", h.ListCreate)
		r.Post("/lists", h.ListSave)
		r.Get("/lists/{id}", h.ListView)
		r.Get("/lists/{id}/edit", h.ListEdit)
		r.Post("/lists/{id}/update", h.ListUpdate)
		r.Post("/lists/{id}/delete", h.ListDelete)
		r.Post("/lists/{id}/reorder", h.ListReorder)
		r.Post("/lists/{id}/clone", h.ListClone)
		r.Post("/lists/{id}/items/{itemId}/details", h.ListUpdateItemDetails)

		// Collective lists
		r.Get("/collective/new", h.CollectiveCreate)
		r.Post("/collective", h.CollectiveSave)
		r.Get("/collective/join/{code}", h.CollectiveJoinDirect)
		r.Get("/collective/{id}", h.CollectiveView)
		r.Get("/collective/{id}/edit", h.CollectiveEditPage)
		r.Post("/collective/{id}/update", h.CollectiveUpdate)
		r.Post("/collective/{id}/reorder", h.CollectiveReorder)
		r.Post("/collective/{id}/versus", h.CollectiveVersusStart)
		r.Post("/collective/{id}/delete", h.CollectiveDelete)
		r.Post("/collective/{id}/delete-votes", h.CollectiveDeleteVotes)
		r.Post("/lists/{id}/convert-to-collective", h.CollectiveConvertFromList)
		r.Post("/collective/{id}/items/{itemId}/details", h.CollectiveUpdateItemDetails)

		// Image upload (IMGBB proxy)
		r.Post("/api/upload-image", h.UploadImage)

		// Administration panel
		r.Get("/admin", h.AdminPanel)
		r.Post("/admin/users", h.AdminCreateUser)
		r.Post("/admin/users/{id}/delete", h.AdminDeleteUser)
		r.Post("/admin/users/{id}/password", h.AdminChangePassword)

		// Versus mode
		r.Post("/lists/{id}/versus", h.VersusStart)
		r.Get("/versus/{sid}", h.VersusPage)
		r.Get("/versus/{sid}/next", h.VersusNext)
		r.Post("/versus/{sid}/choose", h.VersusChoose)
		r.Post("/versus/{sid}/undo", h.VersusUndo)
		r.Get("/versus/{sid}/result", h.VersusResult)
		r.Post("/versus/{sid}/save", h.VersusSave)
	})

	return r, nil
}

// loadLinkDomains loads the allowed link domains.
// If DOMAINS_PATH is set, reads from that file (e.g. a Docker volume mount);
// otherwise uses the embedded dominios.txt.
func loadLinkDomains() error {
	if path := os.Getenv("DOMAINS_PATH"); path != "" {
		return handlers.LoadLinkDomains(path)
	}
	return handlers.LoadLinkDomainsBytes(defaultDomains)
}
