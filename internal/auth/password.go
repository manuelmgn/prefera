// Package auth gere a autenticaçom de utilizadores:
// hashing de palavras-chave, sessons e middleware de protecçom.
package auth

import (
	"golang.org/x/crypto/bcrypt"
)

// HashPassword gera um hash bcrypt a partir de uma palavra-chave em texto plano.
// O custo 12 oferece boa segurança sem ser demasiado lento.
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword compara uma palavra-chave em texto plano com um hash bcrypt.
// Retorna true se coincidem, false se nom.
func CheckPassword(hash, plain string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
	return err == nil
}
