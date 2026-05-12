package db

import (
	"database/sql"
	"fmt"
	"log"

	"golang.org/x/crypto/bcrypt"
)

// Migrate runs the database migrations.
// Creates all required tables if they don't exist
// and inserts the default admin user.
func Migrate(database *sql.DB) error {
	// Full application schema
	schema := `
	-- Users table
	-- Stores all application users
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		display_name TEXT NOT NULL DEFAULT '',
		is_admin INTEGER NOT NULL DEFAULT 0,
		default_public INTEGER NOT NULL DEFAULT 1,
		default_versus_mode TEXT NOT NULL DEFAULT 'rapido',
		theme_preference TEXT NOT NULL DEFAULT 'auto',
		last_login_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Sessions table
	-- Each session is a random token associated with a user
	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id),
		expires_at DATETIME NOT NULL
	);

	-- Lists table
	-- Each list belongs to a user and can be public or private
	CREATE TABLE IF NOT EXISTS lists (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL REFERENCES users(id),
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		is_public INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- List items, with a numeric position for ordering
	CREATE TABLE IF NOT EXISTS list_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		list_id INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		link TEXT NOT NULL DEFAULT '',
		position INTEGER NOT NULL
	);

	-- Versus session (Swiss tournament state)
	-- Stores the progress of an ongoing tournament
	CREATE TABLE IF NOT EXISTS versus_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		list_id INTEGER NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
		mode TEXT NOT NULL CHECK(mode IN ('rapido','detalhado')),
		total_comparisons INTEGER NOT NULL,
		completed_comparisons INTEGER NOT NULL DEFAULT 0,
		is_round_robin INTEGER NOT NULL DEFAULT 0,
		current_round INTEGER NOT NULL DEFAULT 1,
		finished INTEGER NOT NULL DEFAULT 0
	);

	-- Individual match results
	-- winner_id is NULL if the match has not been played yet
	CREATE TABLE IF NOT EXISTS versus_matches (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id INTEGER NOT NULL REFERENCES versus_sessions(id) ON DELETE CASCADE,
		round INTEGER NOT NULL,
		item_a_id INTEGER NOT NULL REFERENCES list_items(id),
		item_b_id INTEGER NOT NULL REFERENCES list_items(id),
		winner_id INTEGER REFERENCES list_items(id),
		match_order INTEGER NOT NULL
	);

	-- Cumulative standings for each item in the tournament
	CREATE TABLE IF NOT EXISTS versus_standings (
		session_id INTEGER NOT NULL REFERENCES versus_sessions(id) ON DELETE CASCADE,
		item_id INTEGER NOT NULL REFERENCES list_items(id),
		wins INTEGER NOT NULL DEFAULT 0,
		losses INTEGER NOT NULL DEFAULT 0,
		buchholz REAL NOT NULL DEFAULT 0,
		PRIMARY KEY (session_id, item_id)
	);

	-- Collective lists: defines a shared set of items
	CREATE TABLE IF NOT EXISTS collective_lists (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		creator_id INTEGER NOT NULL REFERENCES users(id),
		name TEXT NOT NULL,
		description TEXT DEFAULT '',
		share_code TEXT UNIQUE NOT NULL,
		is_public INTEGER NOT NULL DEFAULT 1,
		vote_permission TEXT NOT NULL DEFAULT 'all',
		hide_items INTEGER NOT NULL DEFAULT 0,
		is_active INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- Failed login attempts (rate limiting)
	CREATE TABLE IF NOT EXISTS login_attempts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL,
		ip_address TEXT NOT NULL DEFAULT '',
		attempted_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_login_attempts_username ON login_attempts(username, attempted_at);

	-- Canonical items of a collective list
	CREATE TABLE IF NOT EXISTS collective_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		collective_id INTEGER NOT NULL REFERENCES collective_lists(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		link TEXT NOT NULL DEFAULT '',
		position INTEGER NOT NULL
	);

	-- Participants of a collective list
	CREATE TABLE IF NOT EXISTS collective_participants (
		collective_id INTEGER NOT NULL REFERENCES collective_lists(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL REFERENCES users(id),
		joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (collective_id, user_id)
	);

	-- Individual ranking per participant (one row per item per user)
	CREATE TABLE IF NOT EXISTS collective_rankings (
		collective_id INTEGER NOT NULL REFERENCES collective_lists(id) ON DELETE CASCADE,
		user_id INTEGER NOT NULL REFERENCES users(id),
		item_id INTEGER NOT NULL REFERENCES collective_items(id) ON DELETE CASCADE,
		position INTEGER NOT NULL,
		PRIMARY KEY (collective_id, user_id, item_id)
	);
	`

	// Execute the schema
	if _, err := database.Exec(schema); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	// Incremental migrations: add new columns if they don't exist
	// (SQLite doesn't support IF NOT EXISTS in ALTER TABLE, so we ignore errors)
	database.Exec("ALTER TABLE users ADD COLUMN default_public INTEGER NOT NULL DEFAULT 1")
	database.Exec("ALTER TABLE users ADD COLUMN default_versus_mode TEXT NOT NULL DEFAULT 'rapido'")
	database.Exec("ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT ''")
	database.Exec("ALTER TABLE users ADD COLUMN theme_preference TEXT NOT NULL DEFAULT 'auto'")
	database.Exec("ALTER TABLE users ADD COLUMN last_login_at DATETIME")
	database.Exec("ALTER TABLE lists ADD COLUMN collective_source_id INTEGER REFERENCES collective_lists(id)")
	database.Exec("ALTER TABLE collective_lists ADD COLUMN is_public INTEGER NOT NULL DEFAULT 1")
	database.Exec("ALTER TABLE collective_lists ADD COLUMN vote_permission TEXT NOT NULL DEFAULT 'all'")
	database.Exec("ALTER TABLE collective_lists ADD COLUMN hide_items INTEGER NOT NULL DEFAULT 0")
	database.Exec("ALTER TABLE list_items ADD COLUMN description TEXT NOT NULL DEFAULT ''")
	database.Exec("ALTER TABLE list_items ADD COLUMN link TEXT NOT NULL DEFAULT ''")
	database.Exec("ALTER TABLE list_items ADD COLUMN image TEXT NOT NULL DEFAULT ''")
	database.Exec("ALTER TABLE collective_items ADD COLUMN description TEXT NOT NULL DEFAULT ''")
	database.Exec("ALTER TABLE collective_items ADD COLUMN link TEXT NOT NULL DEFAULT ''")
	database.Exec("ALTER TABLE collective_items ADD COLUMN image TEXT NOT NULL DEFAULT ''")

	// Failed login attempts table (rate limiting)
	database.Exec(`CREATE TABLE IF NOT EXISTS login_attempts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL,
		ip_address TEXT NOT NULL DEFAULT '',
		attempted_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	database.Exec("CREATE INDEX IF NOT EXISTS idx_login_attempts_username ON login_attempts(username, attempted_at)")

	// Seed the admin user if it doesn't exist
	if err := seedAdmin(database); err != nil {
		return fmt.Errorf("failed to create admin user: %w", err)
	}

	log.Println("Migrations completed successfully")
	return nil
}

// seedAdmin creates the default administrator account.
// The password is bcrypt-hashed before being stored.
func seedAdmin(database *sql.DB) error {
	// Check if admin already exists
	var count int
	err := database.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", "listadmin").Scan(&count)
	if err != nil {
		return err
	}

	// Nothing to do if admin already exists
	if count > 0 {
		return nil
	}

	// Generate bcrypt hash for the password
	// Cost 12 is a good balance between security and speed
	hash, err := bcrypt.GenerateFromPassword([]byte("Kv8$mTnR3xPq#2Lw"), 12)
	if err != nil {
		return fmt.Errorf("failed to generate hash: %w", err)
	}

	// Insert admin user into the database
	_, err = database.Exec(
		"INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, 1)",
		"listadmin", string(hash),
	)
	if err != nil {
		return fmt.Errorf("failed to insert admin user: %w", err)
	}

	log.Println("Admin user created: listadmin")
	return nil
}
