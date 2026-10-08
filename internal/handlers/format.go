package handlers

import "database/sql"

// FormatTime renders a nullable timestamp as DD/MM/YYYY.
// Works for both drivers: the scan target is sql.NullTime, which
// database/sql fills from time.Time (Postgres) or parsed values.
func FormatTime(t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format("02/01/2006")
}
