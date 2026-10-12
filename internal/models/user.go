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
		"SELECT id, username, is_admin, created_at FROM users WHERE id = $1", id,
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
		"SELECT id, username, is_admin, created_at FROM users WHERE username = $1", username,
	).Scan(&user.ID, &user.Username, &user.IsAdmin, &user.CreatedAt)
	if err != nil {
		return nil, err
	}
	return user, nil
}

// UserSearchResult is a lightweight user record for autocomplete searches.
type UserSearchResult struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// SearchUsers finds users whose username or display name matches the query.
// The excludeID user (usually the searcher) is omitted. Case-insensitive.
func SearchUsers(db *sql.DB, query string, excludeID, limit int) ([]UserSearchResult, error) {
	rows, err := db.Query(`
		SELECT id, username, display_name FROM users
		WHERE id != $1 AND (LOWER(username) LIKE '%' || LOWER($2) || '%'
		    OR LOWER(display_name) LIKE '%' || LOWER($2) || '%')
		ORDER BY username
		LIMIT $3
	`, excludeID, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []UserSearchResult
	for rows.Next() {
		var u UserSearchResult
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName); err != nil {
			return nil, err
		}
		results = append(results, u)
	}
	return results, nil
}
