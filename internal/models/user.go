// Package models contains the data structures and database queries.
package models

import (
	"database/sql"
	"time"
)

// User represents a user in the database.
type User struct {
	ID        int
	Username  string
	IsAdmin   bool
	CreatedAt time.Time
}

// GetUserByID retrieves a user by their ID.
func GetUserByID(db *sql.DB, id int) (*User, error) {
	user := &User{}
	err := db.QueryRow(
		"SELECT id, username, is_admin, created_at FROM users WHERE id = ?", id,
	).Scan(&user.ID, &user.Username, &user.IsAdmin, &user.CreatedAt)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// GetUserByUsername retrieves a user by their username.
func GetUserByUsername(db *sql.DB, username string) (*User, error) {
	user := &User{}
	err := db.QueryRow(
		"SELECT id, username, is_admin, created_at FROM users WHERE username = ?", username,
	).Scan(&user.ID, &user.Username, &user.IsAdmin, &user.CreatedAt)
	if err != nil {
		return nil, err
	}
	return user, nil
}
