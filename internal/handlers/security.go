package handlers

import (
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Limites de segurança para inputs
const (
	MaxListNameLen        = 150
	MaxItemNameLen        = 200
	MaxDescriptionLen     = 500
	MaxItemDescriptionLen = 600 // ~100 palavras
	MaxItemLinkLen        = 500
	MaxItemsPerList       = 100
	MaxUsernameLen        = 50
	MaxPasswordLen        = 128
)

// htmlTagRegex detecta tags HTML/script
var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

// dangerousPatterns detecta tentativas de injecçom comuns
var dangerousPatterns = regexp.MustCompile(`(?i)(javascript:|on\w+\s*=|<script|<iframe|<object|<embed|<form|data:text/html)`)

// SanitizeInput limpa uma string de entrada do utilizador:
// - Remove tags HTML
// - Elimina padrões perigosos (javascript:, event handlers, etc.)
// - Limita o tamanho
// - Elimina caracteres de controlo (excepto espaço)
func SanitizeInput(s string, maxLen int) string {
	// Limitar tamanho em runes (nom bytes) para suporte UTF-8
	if utf8.RuneCountInString(s) > maxLen {
		runes := []rune(s)
		s = string(runes[:maxLen])
	}

	// Remover tags HTML
	s = htmlTagRegex.ReplaceAllString(s, "")

	// Remover padrões perigosos
	s = dangerousPatterns.ReplaceAllString(s, "")

	// Remover caracteres de controlo (excepto espaço, tab, newline)
	var clean strings.Builder
	for _, r := range s {
		if r >= 32 || r == '\t' || r == '\n' || r == '\r' {
			clean.WriteRune(r)
		}
	}

	return strings.TrimSpace(clean.String())
}

// SanitizeItemDescription sanitiza uma descriçom de elemento (máx 100 palavras)
func SanitizeItemDescription(s string) string {
	s = SanitizeInput(s, MaxItemDescriptionLen)
	// Limitar a 100 palavras
	words := strings.Fields(s)
	if len(words) > 100 {
		s = strings.Join(words[:100], " ")
	}
	return s
}

// SecurityHeaders adiciona cabeçalhos de segurança a todas as respostas
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevenir que o navegador interprete conteúdo MIME incorrectamente
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Protecçom contra clickjacking
		w.Header().Set("X-Frame-Options", "DENY")

		// Activar filtro XSS do navegador
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Política de referência: nom enviar referrer a terceiros
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Nom permitir que a página apareça em iframes
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' 'unsafe-eval' https://cdn.tailwindcss.com https://cdn.jsdelivr.net; "+
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://cdn.jsdelivr.net; "+
				"font-src 'self' https://fonts.gstatic.com; "+
				"img-src 'self' data: https:; "+
				"connect-src 'self'")

		// Nom armazenar dados sensíveis em cache
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")

		next.ServeHTTP(w, r)
	})
}
