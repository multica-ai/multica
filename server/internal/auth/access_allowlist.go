package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	// AccessNotAllowedError is the client-facing message for a caller whose
	// account email is outside ACCESS_ALLOWED_EMAILS.
	AccessNotAllowedError = "this account is not allowed to access this instance"
	// AccessNotAllowedCode is the machine-readable `code` sent with that
	// message. Clients end the session on it rather than retrying, since the
	// credential is valid but will keep being refused.
	AccessNotAllowedCode = "access_not_allowed"
	// AccessNotAllowedBody is the JSON error body carrying both.
	AccessNotAllowedBody = `{"error":"` + AccessNotAllowedError + `","code":"` + AccessNotAllowedCode + `"}`
)

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
// cannot silently lock everyone out or leave the instance open. lookup
// resolves emails for credentials that carry only a user ID (PATs, task
// tokens, daemon tokens); without one those credentials are denied.
func ParseAccessAllowlist(raw string, lookup UserEmailLookup) (*AccessAllowlist, error) {
	a := &AccessAllowlist{emails: map[string]struct{}{}, domains: map[string]struct{}{}, lookup: lookup}
	for _, part := range strings.Split(raw, ",") {
		entry := strings.ToLower(strings.TrimSpace(part))
		if entry == "" {
			continue
		}
		local, domain, ok := strings.Cut(entry, "@")
		if !ok || !validAllowlistLocal(entry, local) || !validAllowlistDomain(domain) {
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

// validAllowlistLocal accepts "*" or a plain address local part. The whole
// entry must parse as a bare address and come back unchanged, which rejects
// display names, quoting, comments and embedded spaces.
func validAllowlistLocal(entry, local string) bool {
	if local == "*" {
		return true
	}
	if local == "" || strings.Contains(local, "*") {
		return false
	}
	addr, err := mail.ParseAddress(entry)
	return err == nil && addr.Address == entry
}

// validAllowlistDomain accepts a lowercase DNS name: dot-separated labels of
// letters, digits and inner hyphens. Internationalized domains must be given
// in their ASCII (xn--) form, which is what sign-in providers report.
func validAllowlistDomain(domain string) bool {
	if domain == "" || len(domain) > 253 {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
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

// SetAccessAllowlist installs the process-wide allowlist. Passing a nil list
// disables the gate.
func SetAccessAllowlist(a *AccessAllowlist) {
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
