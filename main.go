package main

import (
	"log"
	"net/http"
	"os"

	"proj_listas/internal/db"
	"proj_listas/internal/server"
)

func main() {
	// Database path: DB_PATH env var, or local default.
	// If LIBSQL_URL is set, db.Open connects to Turso instead.
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data/listas.db"
	}

	database, isPostgres, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("Erro ao abrir a base de dados: %v", err)
	}
	defer database.Close()

	// Run migrations (create tables if they don't exist)
	if err := db.Migrate(database, isPostgres); err != nil {
		log.Fatalf("Erro nas migraçons: %v", err)
	}

	handler, err := server.NewRouter(database)
	if err != nil {
		log.Fatalf("Erro ao configurar o router: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "7010"
	}
	log.Printf("Servidor iniciado em http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Erro ao iniciar o servidor: %v", err)
	}
}
