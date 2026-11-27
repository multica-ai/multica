package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// AccessNotAllowedError is the client-facing message for a caller whose
// account email is outside ACCESS_ALLOWED_EMAILS.
const AccessNotAllowedError = "this account is not allowed to access this instance"

var ErrAccessNotAllowed = errors.New(AccessNotAllowedError)

// UserEmailLookup resolves a user ID to its account email. It returns
// ("", nil) when the user does not exist, which denies access.
type UserEmailLookup func(ctx context.Context, userID string) (string, error)

// AccessAllowlist restricts every authenticated request to accounts whose
// email matches an exact address ("alice@example.com") or a domain wildcard
// ("*@example.com"). It is configured once at boot from ACCESS_ALLOWED_EMAILS.
type AccessAllowlist struct {
	emails  map[string]struct{}
	domains map[string]struct{}
	lookup  UserEmailLookup

	// Account emails are never updated and the list only changes on restart,
	// so a user's decision is fixed for the life of the process.
	decisions sync.Map // userID -> bool
}

// ParseAccessAllowlist parses a comma-separated list. An empty list returns
// nil, which allows everyone. Any malformed entry is an error so a typo
// cannot silently lock everyone out or leave the instance open.
func ParseAccessAllowlist(raw string) (*AccessAllowlist, error) {
	a := &AccessAllowlist{emails: map[string]struct{}{}, domains: map[string]struct{}{}}
	for _, part := range strings.Split(raw, ",") {
		entry := strings.ToLower(strings.TrimSpace(part))
		if entry == "" {
			continue
		}
		local, domain, ok := strings.Cut(entry, "@")
		if !ok || local == "" || domain == "" || strings.ContainsAny(domain, "@*") ||
			(local != "*" && strings.Contains(local, "*")) {
			return nil, fmt.Errorf("invalid entry %q: use name@example.com or *@example.com", strings.TrimSpace(part))
		}
		if local == "*" {
			a.domains[domain] = struct{}{}
		} else {
			a.emails[entry] = struct{}{}
		}
	}
	if len(a.emails) == 0 && len(a.domains) == 0 {
		return nil, nil
	}
	return a, nil
}

// AllowsEmail reports whether email matches the list. A nil list allows all.
func (a *AccessAllowlist) AllowsEmail(email string) bool {
	if a == nil {
		return true
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}
	if _, ok := a.emails[email]; ok {
		return true
	}
	if at := strings.LastIndex(email, "@"); at > 0 {
		_, ok := a.domains[email[at+1:]]
		return ok
	}
	return false
}

func (a *AccessAllowlist) checkUser(ctx context.Context, userID, email string) error {
	if a == nil {
		return nil
	}
	if email != "" {
		if a.AllowsEmail(email) {
			return nil
		}
		return ErrAccessNotAllowed
	}
	userID = strings.ToLower(strings.TrimSpace(userID))
	if userID == "" {
		return ErrAccessNotAllowed
	}
	if allowed, ok := a.decisions.Load(userID); ok {
		if allowed.(bool) {
			return nil
		}
		return ErrAccessNotAllowed
	}
	if a.lookup == nil {
		return ErrAccessNotAllowed
	}
	resolved, err := a.lookup(ctx, userID)
	if err != nil {
		return fmt.Errorf("resolve user email for access allowlist: %w", err)
	}
	allowed := a.AllowsEmail(resolved)
	a.decisions.Store(userID, allowed)
	if allowed {
		return nil
	}
	return ErrAccessNotAllowed
}

var activeAccessAllowlist atomic.Pointer[AccessAllowlist]

// SetAccessAllowlist installs the process-wide allowlist. lookup resolves
// emails for credentials that carry only a user ID (PATs, task tokens, daemon
// tokens). Passing a nil list disables the gate.
func SetAccessAllowlist(a *AccessAllowlist, lookup UserEmailLookup) {
	if a != nil {
		a.lookup = lookup
	}
	activeAccessAllowlist.Store(a)
}

// IsEmailAccessAllowed reports whether an email may sign in or use the API.
func IsEmailAccessAllowed(email string) bool {
	return activeAccessAllowlist.Load().AllowsEmail(email)
}

// CheckUserAccess returns nil when the user may call the API,
// ErrAccessNotAllowed when the allowlist excludes them, or a lookup error.
// email is used directly when the credential carries it (session JWTs);
// otherwise the email is resolved from userID.
func CheckUserAccess(ctx context.Context, userID, email string) error {
	return activeAccessAllowlist.Load().checkUser(ctx, userID, email)
}
