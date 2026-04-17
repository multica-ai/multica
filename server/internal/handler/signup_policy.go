package handler

import (
	"errors"
	"os"
	"strings"
)

// errSignupDisabled is returned when a new user would be created but the
// self-hosted instance has been configured to block their registration.
var errSignupDisabled = errors.New("signup disabled")

// isSignupAllowed reports whether a new account may be created for email.
//
// Policy (see .env.example):
//   - ALLOWED_EMAILS (comma-separated) — explicit email whitelist. Matches
//     always allow signup, regardless of other settings.
//   - ALLOWED_EMAIL_DOMAINS (comma-separated) — domain whitelist. Matches
//     always allow signup, regardless of ALLOW_SIGNUP.
//   - ALLOW_SIGNUP (bool, default true) — global switch. When false and
//     neither whitelist matches, signup is denied.
//
// Existing users (returning sign-ins) are never affected: this helper only
// gates the creation of brand-new accounts.
func isSignupAllowed(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}

	if emailInList(email, os.Getenv("ALLOWED_EMAILS")) {
		return true
	}

	domain := ""
	if at := strings.LastIndex(email, "@"); at >= 0 && at < len(email)-1 {
		domain = email[at+1:]
	}
	if domain != "" && emailInList(domain, os.Getenv("ALLOWED_EMAIL_DOMAINS")) {
		return true
	}

	// If either whitelist is configured and we got here, no rule matched —
	// treat the configured whitelists as exhaustive and deny.
	if hasNonEmptyEntries(os.Getenv("ALLOWED_EMAILS")) ||
		hasNonEmptyEntries(os.Getenv("ALLOWED_EMAIL_DOMAINS")) {
		return false
	}

	return parseBoolEnv("ALLOW_SIGNUP", true)
}

// emailInList reports whether needle appears (case-insensitively, trimmed)
// in a comma-separated list.
func emailInList(needle, list string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return false
	}
	for _, raw := range strings.Split(list, ",") {
		entry := strings.ToLower(strings.TrimSpace(raw))
		if entry != "" && entry == needle {
			return true
		}
	}
	return false
}

func hasNonEmptyEntries(list string) bool {
	for _, raw := range strings.Split(list, ",") {
		if strings.TrimSpace(raw) != "" {
			return true
		}
	}
	return false
}

func parseBoolEnv(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "":
		return def
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
