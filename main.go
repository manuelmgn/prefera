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

	"proj_listas/internal/auth"
	"proj_listas/internal/db"
	"proj_listas/internal/handlers"
)

func main() {
	// Obter o caminho da base de dados desde variável de ambiente
	// Se nom existir, usa o caminho por defeito
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data/listas.db"
	}

	// Iniciar a conexom com a base de dados SQLite
	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Erro ao abrir a base de dados: %v", err)
	}
	defer database.Close()

	// Executar as migraçons (criar tabelas se nom existem)
	if err := db.Migrate(database); err != nil {
		log.Fatalf("Erro nas migraçons: %v", err)
	}

	// Carregar a base de dados de domínios permitidos para links
	domsPath := os.Getenv("DOMAINS_PATH")
	if domsPath == "" {
		domsPath = "./dominios.txt"
	}
	if err := handlers.LoadLinkDomains(domsPath); err != nil {
		log.Fatalf("Erro ao carregar dominios.txt: %v", err)
	}

	// Carregar os templates HTML desde o disco
	// (em desenvolvimento lê directamente; em Docker o caminho é /app/templates)
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
		log.Fatalf("Erro ao carregar templates: %v", err)
	}
	tmpl, err = tmpl.ParseGlob(tmplPath + "/partials/*.html")
	if err != nil {
		log.Fatalf("Erro ao carregar partials: %v", err)
	}

	// Criar o gestor de autenticaçom
	authManager := auth.NewManager(database)

	// Limpar sessons expiradas periodicamente (cada 6 horas)
	go func() {
		for {
			time.Sleep(6 * time.Hour)
			authManager.CleanExpiredSessions()
		}
	}()

	// Criar o gestor de rotas (handlers)
	h := handlers.New(database, tmpl, authManager)

	// Configurar o router chi
	r := chi.NewRouter()

	// Middleware global: logging, recuperaçom de panics e segurança
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(handlers.SecurityHeaders)

	// Servir ficheiros estáticos desde o disco
	staticPath := os.Getenv("STATIC_PATH")
	if staticPath == "" {
		staticPath = "static"
	}
	staticFS, err := fs.Sub(os.DirFS(staticPath), ".")
	if err != nil {
		log.Fatalf("Erro ao configurar ficheiros estáticos: %v", err)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Rotas públicas (sem autenticaçom)
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.LoginSubmit)

	// Redirecionar raiz sem autenticaçom para login
	// (o middleware dentro do grupo protegido trata disto,
	//  mas se alguém acede a / sem cookie, vai ao dashboard que redireciona)

	// Rotas protegidas (requerem autenticaçom)
	r.Group(func(r chi.Router) {
		r.Use(authManager.RequireAuth)

		// Página principal
		r.Get("/", h.Dashboard)
		r.Post("/logout", h.Logout)

		// As minhas listas
		r.Get("/my-lists", h.MyListsPage)
		r.Get("/my-lists/all", h.MyListsAllPage)
		r.Get("/my-lists/collectives", h.MyListsCollectivesPage)

		// Configuraçom do utilizador
		r.Get("/settings", h.SettingsPage)
		r.Post("/settings", h.SettingsSubmit)

		// Mudar palavra-chave
		r.Get("/password", h.PasswordChangePage)
		r.Post("/password", h.PasswordChangeSubmit)

		// Gestom de listas
		r.Get("/lists/new", h.ListCreate)
		r.Post("/lists", h.ListSave)
		r.Get("/lists/{id}", h.ListView)
		r.Get("/lists/{id}/edit", h.ListEdit)
		r.Post("/lists/{id}/update", h.ListUpdate)
		r.Post("/lists/{id}/delete", h.ListDelete)
		r.Post("/lists/{id}/reorder", h.ListReorder)
		r.Post("/lists/{id}/clone", h.ListClone)
		r.Post("/lists/{id}/items/{itemId}/details", h.ListUpdateItemDetails)

		// Listas colectivas
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

		// Upload de imagens (proxy IMGBB)
		r.Post("/api/upload-image", h.UploadImage)

		// Painel de administraçom
		r.Get("/admin", h.AdminPanel)
		r.Post("/admin/users", h.AdminCreateUser)
		r.Post("/admin/users/{id}/delete", h.AdminDeleteUser)
		r.Post("/admin/users/{id}/password", h.AdminChangePassword)

		// Modo Versus
		r.Post("/lists/{id}/versus", h.VersusStart)
		r.Get("/versus/{sid}", h.VersusPage)
		r.Get("/versus/{sid}/next", h.VersusNext)
		r.Post("/versus/{sid}/choose", h.VersusChoose)
		r.Post("/versus/{sid}/undo", h.VersusUndo)
		r.Get("/versus/{sid}/result", h.VersusResult)
		r.Post("/versus/{sid}/save", h.VersusSave)
	})

	// Iniciar o servidor na porta 7010
	port := ":7010"
	log.Printf("Servidor iniciado em http://localhost%s", port)
	if err := http.ListenAndServe(port, r); err != nil {
		log.Fatalf("Erro ao iniciar o servidor: %v", err)
	}
}

// formatDateStr converte uma string de data SQLite em formato DD/MM/YYYY.
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
