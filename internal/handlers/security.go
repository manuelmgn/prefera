package handlers

import (
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Security limits for user inputs
const (
	MaxListNameLen        = 150
	MaxItemNameLen        = 200
	MaxDescriptionLen     = 500
	MaxItemDescriptionLen = 600 // ~100 words
	MaxItemLinkLen        = 500
	MaxItemsPerList       = 100
	MaxUsernameLen        = 50
	MaxPasswordLen        = 128
)

// htmlTagRegex detects HTML/script tags
var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

// dangerousPatterns detects common injection attempts
var dangerousPatterns = regexp.MustCompile(`(?i)(javascript:|on\w+\s*=|<script|<iframe|<object|<embed|<form|data:text/html)`)

// SanitizeInput cleans a user input string:
// - Strips HTML tags
// - Removes dangerous patterns (javascript:, event handlers, etc.)
// - Enforces maximum length
// - Removes control characters (except whitespace)
func SanitizeInput(s string, maxLen int) string {
	// Limit length in runes (not bytes) for UTF-8 support
	if utf8.RuneCountInString(s) > maxLen {
		runes := []rune(s)
		s = string(runes[:maxLen])
	}

	// Strip HTML tags
	s = htmlTagRegex.ReplaceAllString(s, "")

	// Remove dangerous patterns
	s = dangerousPatterns.ReplaceAllString(s, "")

	// Remove control characters (keep space, tab, newline)
	var clean strings.Builder
	for _, r := range s {
		if r >= 32 || r == '\t' || r == '\n' || r == '\r' {
			clean.WriteRune(r)
		}
	}

	return strings.TrimSpace(clean.String())
}

// SanitizeItemDescription sanitizes an item description (max 100 words).
func SanitizeItemDescription(s string) string {
	s = SanitizeInput(s, MaxItemDescriptionLen)
	// Limit to 100 words
	words := strings.Fields(s)
	if len(words) > 100 {
		s = strings.Join(words[:100], " ")
	}
	return s
}

// SecurityHeaders adds security headers to all responses.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent the browser from MIME-sniffing the content type
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Clickjacking protection
		w.Header().Set("X-Frame-Options", "DENY")

		// Enable the browser's XSS filter
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Referrer policy: do not send referrer to third parties
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Content Security Policy: prevent embedding in iframes
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'unsafe-inline' 'unsafe-eval' https://cdn.tailwindcss.com https://cdn.jsdelivr.net; "+
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://cdn.jsdelivr.net; "+
				"font-src 'self' https://fonts.gstatic.com; "+
				"img-src 'self' data: https:; "+
				"connect-src 'self'")

		// Prevent caching of sensitive data
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")

		next.ServeHTTP(w, r)
	})
}
