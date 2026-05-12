// Package db manages the SQLite database connection.
// Uses the pure-Go driver (no CGO) to simplify compilation.
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	// Pure-Go SQLite driver (no CGO/gcc required)
	_ "modernc.org/sqlite"
)

// Open opens or creates the SQLite database at the given path.
// Enables WAL mode (Write-Ahead Logging) for better performance
// and enables foreign key enforcement.
func Open(dbPath string) (*sql.DB, error) {
	// Create directory if it doesn't exist
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Open database with optimisation parameters
	// _pragma=journal_mode(wal) -> WAL mode, better for concurrent reads
	// _pragma=foreign_keys(1)   -> enable foreign key constraints
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)", dbPath)
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite: %w", err)
	}

	// Verify the connection works
	if err := database.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to SQLite: %w", err)
	}

	return database, nil
}
