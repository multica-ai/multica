package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	AccountStatusActive    = "active"
	AccountStatusSuspended = "suspended"

	// AccountSuspendedCode is a stable API discriminator for clients (403).
	AccountSuspendedCode = "ACCOUNT_SUSPENDED"
	// AccountSuspendedMessage is the default human-readable error text.
	AccountSuspendedMessage = "this account has been suspended"
)

// ErrAccountSuspended is returned when a suspended user attempts login or token issuance.
var ErrAccountSuspended = errors.New(AccountSuspendedMessage)

// UserMayAuthenticate reports whether the user row is allowed to sign in or use API tokens.
// Only an explicit "active" status grants access; any unknown value (including empty) denies,
// so future statuses landing without an enforcement update fail closed.
func UserMayAuthenticate(u db.User) bool {
	return u.AccountStatus == AccountStatusActive
}

// SuspendedResponseJSON returns a compact JSON object for 403 suspended responses.
func SuspendedResponseJSON() []byte {
	b, err := json.Marshal(map[string]string{
		"error": AccountSuspendedMessage,
		"code":  AccountSuspendedCode,
	})
	if err != nil {
		return []byte(`{"error":"this account has been suspended","code":"ACCOUNT_SUSPENDED"}`)
	}
	return b
}

// WriteAccountSuspendedResponse writes HTTP 403 JSON with error + code fields.
func WriteAccountSuspendedResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write(SuspendedResponseJSON())
}
