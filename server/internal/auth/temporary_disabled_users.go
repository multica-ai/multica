package auth

import (
	"errors"
	"strings"
)

// TemporarilyDisabledUserError is the error body for a suspended account,
// kept so clients that already handle it keep working.
const TemporarilyDisabledUserError = "account disabled"

var ErrTemporarilyDisabledUser = errors.New(TemporarilyDisabledUserError)

// Emergency email denylist, checked where an address can sign in or sign up
// before it has an account. The accounts it covered are suspended in the
// database (migration 760); delete this once every address is confirmed to
// belong to one of them.
var temporarilyDisabledUserEmails = map[string]struct{}{
	"pdzzer68@embassybase.com": {},
	"gtwtrox@mowan666.com":     {},
}

func IsTemporarilyDisabledUserEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}
	_, ok := temporarilyDisabledUserEmails[email]
	return ok
}
