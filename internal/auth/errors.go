package auth

import "errors"

// Erros de autenticaçom
var (
	// ErrWrongPassword indica que a palavra-chave actual nom é correcta
	ErrWrongPassword = errors.New("palavra-chave incorrecta")
)
