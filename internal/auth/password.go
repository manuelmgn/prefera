// Package auth handles user authentication:
// password hashing, session management, and auth middleware.
package auth

import (
	"golang.org/x/crypto/bcrypt"
)

// HashPassword generates a bcrypt hash from a plain-text password.
// Cost 12 provides good security without being too slow.
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword compares a plain-text password against a bcrypt hash.
// Returns true if they match, false otherwise.
func CheckPassword(hash, plain string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
	return err == nil
}
