package handlers

import (
	"bufio"
	"encoding/json"
	"html/template"
	"os"
	"regexp"
	"strings"
)

// imageExtRegex detects URLs ending with a known image extension
var imageExtRegex = regexp.MustCompile(`(?i)\.(jpg|jpeg|png|gif|webp|svg|bmp|avif)(\?.*)?$`)

type linkSection struct {
	Name     string
	Patterns []string
}

type linkDomainDB struct {
	sections []linkSection
}

// globalLinkDB is the global instance loaded at startup
var globalLinkDB *linkDomainDB

// LoadLinkDomains parses the domains file and initialises the global database.
// File format:
//
//	# comment
//	[section_name]
//	domain.com/
//	other.org/
func LoadLinkDomains(path string) error {
	db, err := parseLinkDomains(path)
	if err != nil {
		return err
	}
	globalLinkDB = db
	return nil
}

func parseLinkDomains(path string) (*linkDomainDB, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var db linkDomainDB
	var current *linkSection

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			db.sections = append(db.sections, linkSection{Name: line[1 : len(line)-1]})
			current = &db.sections[len(db.sections)-1]
			continue
		}
		if current != nil {
			current.Patterns = append(current.Patterns, line)
		}
	}
	return &db, scanner.Err()
}

// ValidateItemLink validates a URL against the domain database.
// Does NOT accept image extensions — use ValidateImageURL for images.
// Returns the cleaned link if valid, or "" if invalid/empty.
func ValidateItemLink(link string) string {
	link = strings.TrimSpace(link)
	if link == "" {
		return ""
	}
	if !strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "http://") {
		return ""
	}
	if globalLinkDB == nil {
		return ""
	}
	lower := strings.ToLower(link)
	for _, sec := range globalLinkDB.sections {
		for _, p := range sec.Patterns {
			if strings.Contains(lower, strings.ToLower(p)) {
				return link
			}
		}
	}
	return ""
}

// ValidateImageURL validates that a URL points to an image.
// Accepts URLs with a known image extension or URLs from i.ibb.co (IMGBB).
// Returns the cleaned URL or "" if invalid.
func ValidateImageURL(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return ""
	}
	lower := strings.ToLower(url)
	// Accept IMGBB URLs (integrated upload service)
	if strings.Contains(lower, "i.ibb.co/") || strings.Contains(lower, "ibb.co/") {
		return url
	}
	// Accept URLs with a known image extension
	if imageExtRegex.MatchString(url) {
		return url
	}
	return ""
}

// LinkType returns the section name for a link (e.g. "wiki", "videos", "social").
// Returns "" if no section matches.
func LinkType(link string) string {
	if link == "" || globalLinkDB == nil {
		return ""
	}
	lower := strings.ToLower(link)
	for _, sec := range globalLinkDB.sections {
		for _, p := range sec.Patterns {
			if strings.Contains(lower, strings.ToLower(p)) {
				return sec.Name
			}
		}
	}
	return ""
}

// IsImageURL returns true if the URL has a known image extension.
// Used in templates to decide whether to display as a background in Versus mode.
func IsImageURL(link string) bool {
	return imageExtRegex.MatchString(link)
}

// AllowedPatternsJS returns a JSON array of all allowed patterns,
// marked as safe for direct injection into JavaScript.
func AllowedPatternsJS() template.JS {
	if globalLinkDB == nil {
		return template.JS("[]")
	}
	var patterns []string
	for _, sec := range globalLinkDB.sections {
		patterns = append(patterns, sec.Patterns...)
	}
	b, _ := json.Marshal(patterns)
	return template.JS(b)
}
