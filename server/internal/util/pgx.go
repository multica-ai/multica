package util

// MULTICA-LOCAL: SQLite adapter utilities.
// With SQLite, UUIDs and timestamps are stored as TEXT strings.
// These helpers bridge the gap between nullable SQL types and Go pointers/strings.

import "database/sql"

// NullStringToPtr converts a sql.NullString to a *string.
func NullStringToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

// PtrToNullString converts a *string to a sql.NullString.
func PtrToNullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// StrToNullString converts a string to a sql.NullString.
// Returns an invalid NullString if the string is empty.
func StrToNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// NullStringToString converts a sql.NullString to a string (empty if null).
func NullStringToString(ns sql.NullString) string {
	if !ns.Valid {
		return ""
	}
	return ns.String
}

// NewUUID generates a new UUID v4 string.
func NewUUID() string {
	// Using google/uuid for generation.
	// Import is in the caller; we keep this package dependency-free.
	// This is a placeholder — actual UUID generation is in the caller.
	return ""
}
