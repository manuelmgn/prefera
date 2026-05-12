// Package models contém as estruturas de dados e consultas à base de dados.
package models

import (
	"database/sql"
	"time"
)

// User representa um utilizador na base de dados
type User struct {
	ID        int
	Username  string
	IsAdmin   bool
	CreatedAt time.Time
}

// GetUserByID obtém um utilizador pelo seu ID
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

// GetUserByUsername obtém um utilizador pelo seu nome de utilizador
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
