// Package db gere a conexom com a base de dados SQLite.
// Usa o driver pure-Go (sem CGO) para simplificar a compilaçom.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	// Driver SQLite pure-Go (nom precisa de CGO/gcc)
	_ "modernc.org/sqlite"
)

// Open abre ou cria a base de dados SQLite no caminho indicado.
// Activa o modo WAL (Write-Ahead Logging) para melhor rendimento
// e activa as chaves foráneas (foreign keys).
func Open(dbPath string) (*sql.DB, error) {
	// Criar o directório se nom existir
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("erro ao criar directório: %w", err)
	}

	// Abrir a base de dados com parâmetros de optimizaçom
	// _pragma=journal_mode(wal) -> modo WAL, melhor para leituras concorrentes
	// _pragma=foreign_keys(1)   -> activar chaves foráneas
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)", dbPath)
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir SQLite: %w", err)
	}

	// Verificar que a conexom funciona
	if err := database.Ping(); err != nil {
		return nil, fmt.Errorf("erro ao conectar com SQLite: %w", err)
	}

	return database, nil
}
