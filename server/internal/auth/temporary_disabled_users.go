package auth

import (
	"errors"
	"strings"
)

const TemporarilyDisabledUserError = "account disabled"

var ErrTemporarilyDisabledUser = errors.New(TemporarilyDisabledUserError)

// Temporary emergency denylist. Remove this once account suspension is
// persisted and enforced from the user model.
var temporarilyDisabledUserIDs = map[string]struct{}{
	"514492f7-b30f-4147-bd33-c0e8ce5d6d4f": {},
	"1d542296-17c6-484a-9914-dcee589be116": {},
}

var temporarilyDisabledUserEmails = map[string]struct{}{
	"pdzzer68@embassybase.com": {},
	"gtwtrox@mowan666.com":     {},
}

func IsTemporarilyDisabledUser(userID, email string) bool {
	return IsTemporarilyDisabledUserID(userID) || IsTemporarilyDisabledUserEmail(email)
}

func IsTemporarilyDisabledUserID(userID string) bool {
	userID = strings.ToLower(strings.TrimSpace(userID))
	if userID == "" {
		return false
	}
	_, ok := temporarilyDisabledUserIDs[userID]
	return ok
}

func IsTemporarilyDisabledUserEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}
	_, ok := temporarilyDisabledUserEmails[email]
	return ok
}
