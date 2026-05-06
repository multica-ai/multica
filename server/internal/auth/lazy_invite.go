package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// LazyInviteRule maps an email domain to a workspace whose membership the
// user is auto-granted on signup or on first login with zero memberships.
// The rule is trusted only after slug-to-UUID resolution at startup; at
// runtime we never look up by slug again.
type LazyInviteRule struct {
	Domain        string // lowercase, no leading "@"
	WorkspaceID   pgtype.UUID
	WorkspaceSlug string // kept for log readability
}

// LazyInviteRules is the parsed env-var allowlist. Empty means feature off.
type LazyInviteRules []LazyInviteRule

// SlugResolver is the subset of *db.Queries we need for parsing. Defining
// it as an interface lets the unit tests fake it without standing up Postgres.
type SlugResolver interface {
	GetWorkspaceBySlug(ctx context.Context, slug string) (db.Workspace, error)
}

// emailDomain returns the lowercase domain part of an email, or "" if the
// email is malformed (no "@" or empty domain).
func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}

// Match returns the first rule whose Domain matches the email's domain,
// case-insensitive, exact (no subdomain matching).
func (r LazyInviteRules) Match(email string) (LazyInviteRule, bool) {
	d := emailDomain(email)
	if d == "" {
		return LazyInviteRule{}, false
	}
	for _, rule := range r {
		if rule.Domain == d {
			return rule, true
		}
	}
	return LazyInviteRule{}, false
}

// IsAllowedDomain answers the signup-gate question: should signup be
// permitted on the basis of a lazy-invite rule, regardless of global
// signup config?
func (r LazyInviteRules) IsAllowedDomain(email string) bool {
	_, ok := r.Match(email)
	return ok
}

// ParseLazyInviteRules reads the LAZY_INVITE_RULES spec, validates structure,
// rejects duplicate domains, and resolves each slug to a workspace UUID.
// Returns an empty LazyInviteRules when spec is empty (feature off).
//
// Any error here is fatal at server startup — operators must fix their
// config before users hit the auth flow.
func ParseLazyInviteRules(ctx context.Context, spec string, resolver SlugResolver) (LazyInviteRules, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return LazyInviteRules{}, nil
	}

	pairs := strings.Split(spec, ",")
	rules := make(LazyInviteRules, 0, len(pairs))
	seen := make(map[string]string, len(pairs)) // domain -> slug, to detect dups

	for i, raw := range pairs {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		idx := strings.Index(p, ":")
		if idx < 0 {
			return nil, fmt.Errorf("LAZY_INVITE_RULES[%d]: %q must be <domain>:<slug>", i, p)
		}
		domain := strings.ToLower(strings.TrimSpace(p[:idx]))
		slug := strings.TrimSpace(p[idx+1:])
		if domain == "" {
			return nil, fmt.Errorf("LAZY_INVITE_RULES[%d]: empty domain in %q", i, p)
		}
		if slug == "" {
			return nil, fmt.Errorf("LAZY_INVITE_RULES[%d]: empty slug in %q", i, p)
		}
		if !strings.Contains(domain, ".") {
			return nil, fmt.Errorf("LAZY_INVITE_RULES[%d]: domain %q must contain a dot", i, domain)
		}
		if prev, dup := seen[domain]; dup {
			return nil, fmt.Errorf("LAZY_INVITE_RULES: duplicate domain %q (previously mapped to %q, now to %q)", domain, prev, slug)
		}
		ws, err := resolver.GetWorkspaceBySlug(ctx, slug)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil, fmt.Errorf("LAZY_INVITE_RULES: unknown workspace slug %q for domain %q", slug, domain)
			}
			return nil, fmt.Errorf("LAZY_INVITE_RULES: resolving slug %q: %w", slug, err)
		}
		seen[domain] = slug
		rules = append(rules, LazyInviteRule{
			Domain:        domain,
			WorkspaceID:   ws.ID,
			WorkspaceSlug: slug,
		})
	}
	return rules, nil
}
