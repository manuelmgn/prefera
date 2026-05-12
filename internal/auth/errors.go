package auth

import "errors"

// Authentication errors
var (
	// ErrWrongPassword indicates that the provided current password is incorrect.
	ErrWrongPassword = errors.New("wrong password")
)
