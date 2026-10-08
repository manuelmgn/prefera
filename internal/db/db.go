// Package db manages the database connection.
// Supports PostgreSQL (Vercel Postgres via DATABASE_URL), Turso/libsql
// over HTTP(S), and local SQLite (development/Docker).
package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	// PostgreSQL driver
	_ "github.com/lib/pq"

	// libsql driver: speaks the SQLite protocol over HTTP to Turso
	_ "github.com/tursodatabase/libsql-client-go/libsql"

	// Pure-Go SQLite driver (no CGO/gcc required) for local use
	_ "modernc.org/sqlite"
)

// Open opens a database connection and reports whether the backend
// is PostgreSQL (needed to pick the right DDL in Migrate).
// Priority: DATABASE_URL/POSTGRES_URL > LIBSQL_URL > local SQLite.
// If LIBSQL_URL is set, connects to a Turso/libsql database
// (authenticated with LIBSQL_AUTH_TOKEN if present).
// Otherwise opens or creates a local SQLite database at dbPath,
// enabling WAL mode and foreign key enforcement.
func Open(dbPath string) (*sql.DB, bool, error) {
	if pgURL := os.Getenv("DATABASE_URL"); pgURL != "" {
		return openPostgres(pgURL)
	}
	if pgURL := os.Getenv("POSTGRES_URL"); pgURL != "" {
		return openPostgres(pgURL)
	}
	if libsqlURL := os.Getenv("LIBSQL_URL"); libsqlURL != "" {
		database, err := openLibsql(libsqlURL, os.Getenv("LIBSQL_AUTH_TOKEN"))
		return database, false, err
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, false, fmt.Errorf("failed to create directory: %w", err)
	}

	// Open database with optimisation parameters
	// _pragma=journal_mode(wal) -> WAL mode, better for concurrent reads
	// _pragma=foreign_keys(1)   -> enable foreign key constraints
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(wal)&_pragma=foreign_keys(1)", dbPath)
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, false, fmt.Errorf("failed to open SQLite: %w", err)
	}

	// Verify the connection works
	if err := database.Ping(); err != nil {
		return nil, false, fmt.Errorf("failed to connect to SQLite: %w", err)
	}

	return database, false, nil
}

// openPostgres connects to a PostgreSQL database (e.g. Vercel Postgres).
func openPostgres(pgURL string) (*sql.DB, bool, error) {
	database, err := sql.Open("postgres", pgURL)
	if err != nil {
		return nil, true, fmt.Errorf("failed to open PostgreSQL connection: %w", err)
	}
	if err := database.Ping(); err != nil {
		return nil, true, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	return database, true, nil
}

// openLibsql connects to a Turso/libsql database over HTTPS.
// The URL may already carry an authToken query parameter; if not,
// the token is appended from the LIBSQL_AUTH_TOKEN env var.
func openLibsql(libsqlURL, authToken string) (*sql.DB, error) {
	u, err := url.Parse(libsqlURL)
	if err != nil {
		return nil, fmt.Errorf("invalid LIBSQL_URL: %w", err)
	}
	if u.Query().Get("authToken") == "" && authToken != "" {
		q := u.Query()
		q.Set("authToken", authToken)
		u.RawQuery = q.Encode()
	}

	database, err := sql.Open("libsql", u.String())
	if err != nil {
		return nil, fmt.Errorf("failed to open libsql connection: %w", err)
	}

	if err := database.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to libsql: %w", err)
	}

	return database, nil
}
