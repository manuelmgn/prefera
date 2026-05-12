package main

import (
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"prefera/internal/auth"
	"prefera/internal/db"
	"prefera/internal/handlers"
)

func main() {
	// Get the database path from environment variable
	// Fall back to default path if not set
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data/listas.db"
	}

	// Open SQLite database connection
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Run migrations (create tables if they don't exist)
	if err := db.Migrate(database); err != nil {
		log.Fatalf("Migrations failed: %v", err)
	}

	// Load the allowed link domains database
	domsPath := os.Getenv("DOMAINS_PATH")
	if domsPath == "" {
		domsPath = "./config/link_domains.txt"
	}
	if err := handlers.LoadLinkDomains(domsPath); err != nil {
		log.Fatalf("Failed to load link_domains.txt: %v", err)
	}

	// Load HTML templates from disk
	// (in development reads directly; in Docker the path is /app/templates)
	tmplPath := os.Getenv("TMPL_PATH")
	if tmplPath == "" {
		tmplPath = "templates"
	}

	funcMap := template.FuncMap{
		"linkType":        handlers.LinkType,
		"isImageURL":      handlers.IsImageURL,
		"allowedPatterns": handlers.AllowedPatternsJS,
		"formatDate":      formatDateStr,
	}
	tmpl, err := template.New("").Funcs(funcMap).ParseGlob(tmplPath + "/*.html")
	if err != nil {
		log.Fatalf("Failed to load templates: %v", err)
	}
	tmpl, err = tmpl.ParseGlob(tmplPath + "/partials/*.html")
	if err != nil {
		log.Fatalf("Failed to load partials: %v", err)
	}

	// Create the authentication manager
	authManager := auth.NewManager(database)

	// Clean up expired sessions periodically (every 6 hours)
	go func() {
		for {
			time.Sleep(6 * time.Hour)
			authManager.CleanExpiredSessions()
		}
	}()

	// Create the route handler
	h := handlers.New(database, tmpl, authManager)

	// Set up the chi router
	r := chi.NewRouter()

	// Global middleware: logging, panic recovery, and security headers
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(handlers.SecurityHeaders)

	// Serve static files from disk
	staticPath := os.Getenv("STATIC_PATH")
	if staticPath == "" {
		staticPath = "static"
	}
	staticFS, err := fs.Sub(os.DirFS(staticPath), ".")
	if err != nil {
		log.Fatalf("Failed to configure static files: %v", err)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Public routes (no authentication required)
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.LoginSubmit)

	// Redirect unauthenticated root access to login
	// (the middleware inside the protected group handles this,
	//  but if someone hits / without a cookie they go to the dashboard which redirects)

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

		// Admin panel
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

	// Start server on port 7010
	port := ":7010"
	log.Printf("Server started at http://localhost%s", port)
	if err := http.ListenAndServe(port, r); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// formatDateStr converts a SQLite date string to DD/MM/YYYY format.
func formatDateStr(s string) string {
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
