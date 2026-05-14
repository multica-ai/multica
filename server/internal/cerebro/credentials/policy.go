package credentials

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// Permission names the five governance actions a credential supports
// (JEH-1197). The names match the verbs the issue uses and are mirrored
// into the audit-row "reason" payload when a check denies.
type Permission string

const (
	PermAttach       Permission = "attach"
	PermReadRedacted Permission = "read_redacted"
	PermReveal       Permission = "reveal"
	PermRotate       Permission = "rotate"
	PermRevoke       Permission = "revoke"
)

// String lets Permission satisfy fmt.Stringer without ceremony.
func (p Permission) String() string { return string(p) }

// PolicyDecision is the structured answer returned by a PolicyChecker.
// Allowed=true short-circuits the chain; Allowed=false with Reason
// surfaces to the audit row and to the 403 response body.
type PolicyDecision struct {
	Allowed    bool
	Reason     string
	DecisionID string
}

// Allow is the canned positive answer with an attached reason — useful
// for the workspace-owner short-circuit so callers can tell *why* the
// allow fired when reading audit rows.
func Allow(reason string) PolicyDecision { return PolicyDecision{Allowed: true, Reason: reason} }

// Deny is the canned negative answer with an explanation. Reasons are
// human-readable strings — they end up in the audit log and in the API
// response, so keep them informative but not leaky.
func Deny(reason string) PolicyDecision { return PolicyDecision{Allowed: false, Reason: reason} }

// ErrPolicyDenied is returned by Service operations when the policy
// denies an action. Handlers map it to 403 Forbidden. The accompanying
// PolicyDecision is carried via PolicyDeniedError so handlers can
// surface the reason without exposing internals.
var ErrPolicyDenied = errors.New("policy denied this action")

// PolicyDeniedError wraps ErrPolicyDenied with the structured decision.
// errors.Is(err, ErrPolicyDenied) still matches; handlers can also do
// errors.As to extract the reason for the response body.
type PolicyDeniedError struct {
	Permission Permission
	Decision   PolicyDecision
}

func (e *PolicyDeniedError) Error() string {
	if e == nil {
		return ErrPolicyDenied.Error()
	}
	reason := e.Decision.Reason
	if reason == "" {
		reason = "denied"
	}
	return "policy denied " + string(e.Permission) + ": " + reason
}

func (e *PolicyDeniedError) Unwrap() error { return ErrPolicyDenied }

// PolicyRequest describes one access check. Fields are populated by the
// Service from the loaded credential row — the checker never has to
// re-fetch the credential.
type PolicyRequest struct {
	WorkspaceID    pgtype.UUID
	CredentialID   pgtype.UUID
	CredentialType Type
	Permission     Permission
	ActorType      string
	ActorID        pgtype.UUID
}

// PolicyChecker decides whether an actor may perform a permission on a
// credential. Implementations:
//
//   - OwnerPolicyChecker: workspace owners/admins always allow.
//   - PersonaPolicyChecker: defers to the persona service.
//   - ChainPolicyChecker: composes checkers left-to-right; first allow
//     wins, otherwise the last decision is returned.
//
// The Service is wired with a ChainPolicyChecker(owner, persona) in
// production. When persona is unconfigured the chain still works — only
// the owner check fires, giving the deny-by-default behaviour described
// in JEH-1197.
type PolicyChecker interface {
	Check(ctx context.Context, req PolicyRequest) PolicyDecision
}

// PolicyCheckerFunc adapts a plain function to the PolicyChecker
// interface — convenient for tests.
type PolicyCheckerFunc func(ctx context.Context, req PolicyRequest) PolicyDecision

func (f PolicyCheckerFunc) Check(ctx context.Context, req PolicyRequest) PolicyDecision {
	return f(ctx, req)
}

// DenyAllChecker is the safe default the Service uses when no checker
// has been wired. Deny-by-default per the issue acceptance criteria.
var DenyAllChecker PolicyChecker = PolicyCheckerFunc(func(_ context.Context, _ PolicyRequest) PolicyDecision {
	return Deny("no policy checker configured")
})

// AllowAllChecker is a convenience for tests that exercise the
// success path without setting up persona. NOT used in production.
var AllowAllChecker PolicyChecker = PolicyCheckerFunc(func(_ context.Context, _ PolicyRequest) PolicyDecision {
	return Allow("allow-all (test)")
})

// ChainPolicyChecker evaluates a list of checkers in order. The first
// Allowed=true decision is returned immediately. If all deny, the LAST
// deny is returned (so the most-specific reason reaches the audit log).
// Empty chain falls back to DenyAllChecker.
type ChainPolicyChecker struct {
	Checkers []PolicyChecker
}

// NewChainPolicyChecker constructs a chain. Nil entries are skipped.
func NewChainPolicyChecker(checkers ...PolicyChecker) *ChainPolicyChecker {
	cleaned := make([]PolicyChecker, 0, len(checkers))
	for _, c := range checkers {
		if c != nil {
			cleaned = append(cleaned, c)
		}
	}
	return &ChainPolicyChecker{Checkers: cleaned}
}

func (c *ChainPolicyChecker) Check(ctx context.Context, req PolicyRequest) PolicyDecision {
	if c == nil || len(c.Checkers) == 0 {
		return DenyAllChecker.Check(ctx, req)
	}
	var last PolicyDecision
	for _, ch := range c.Checkers {
		dec := ch.Check(ctx, req)
		if dec.Allowed {
			return dec
		}
		last = dec
	}
	if last.Reason == "" {
		last.Reason = "deny-by-default"
	}
	return last
}

// MemberLookup is the slim surface OwnerPolicyChecker needs from the
// upstream queries. Defined here so the package does not pull in the
// full db.Queries type — keeping the package's exported surface narrow.
type MemberLookup interface {
	GetMemberRole(ctx context.Context, workspaceID, userID pgtype.UUID) (string, error)
}

// OwnerPolicyChecker grants every permission to workspace owners and
// admins. JEH-1197 acceptance: "workspace-owner får alle permissions".
//
// Only fires for member actors — agent tokens are not owners/admins
// regardless of who spawned them, so they fall through to the next
// checker (persona) to get a structured grant.
type OwnerPolicyChecker struct {
	Members MemberLookup
	// AdminRoles is the set of roles that count as "workspace owner"
	// for credential governance. Defaults to {"owner", "admin"} when
	// nil/empty.
	AdminRoles []string
}

// NewOwnerPolicyChecker constructs a checker against the upstream
// member table. Roles default to owner+admin to match the other
// workspace-management endpoints.
func NewOwnerPolicyChecker(lookup MemberLookup) *OwnerPolicyChecker {
	return &OwnerPolicyChecker{Members: lookup, AdminRoles: []string{"owner", "admin"}}
}

func (o *OwnerPolicyChecker) Check(ctx context.Context, req PolicyRequest) PolicyDecision {
	if o == nil || o.Members == nil {
		return Deny("owner lookup not configured")
	}
	if req.ActorType != "member" {
		return Deny("owner check only applies to members")
	}
	if !req.ActorID.Valid {
		return Deny("owner check needs a valid actor id")
	}
	role, err := o.Members.GetMemberRole(ctx, req.WorkspaceID, req.ActorID)
	if err != nil {
		return Deny("owner check: " + err.Error())
	}
	roles := o.AdminRoles
	if len(roles) == 0 {
		roles = []string{"owner", "admin"}
	}
	for _, r := range roles {
		if strings.EqualFold(role, r) {
			return Allow("workspace " + role)
		}
	}
	return Deny("member role " + role + " is not owner/admin")
}

// PersonaChecker is the persona-backed implementation. The actual
// persona SDK wiring lives in the cmd/server package (cerebro_credentials_policy.go)
// so this package keeps zero dependencies on the SDK. The checker here
// is an interface adapter — production code constructs it with the SDK
// adapter, tests use a stub.
type PersonaChecker interface {
	// CheckCredential asks "may actor act on resource". resource is the
	// canonical persona resource string — implementations should expect
	// at least the two forms used here:
	//
	//   cerebro-credential:<credential-uuid>
	//   cerebro-credential-type:<credential-type>
	//
	// On error (transport, decode, etc.) the implementation must return
	// Allowed=false with a non-empty Reason so the chain can still log
	// the deny.
	CheckCredential(ctx context.Context, action, resource, actorID string) PolicyDecision
}

// PersonaPolicyChecker is a PolicyChecker backed by a PersonaChecker.
// It performs the id-scoped lookup first, then falls back to the type
// scope if the id-scoped check denied. This matches the issue's
// "per credential-id OG pr. credential-type (type-level fallback)"
// requirement.
type PersonaPolicyChecker struct {
	Backend PersonaChecker
}

// NewPersonaPolicyChecker wraps a PersonaChecker.
func NewPersonaPolicyChecker(backend PersonaChecker) *PersonaPolicyChecker {
	return &PersonaPolicyChecker{Backend: backend}
}

func (p *PersonaPolicyChecker) Check(ctx context.Context, req PolicyRequest) PolicyDecision {
	if p == nil || p.Backend == nil {
		return Deny("persona checker not configured")
	}
	actor := util.UUIDToString(req.ActorID)
	action := "credential." + string(req.Permission)

	idResource := "cerebro-credential:" + util.UUIDToString(req.CredentialID)
	if dec := p.Backend.CheckCredential(ctx, action, idResource, actor); dec.Allowed {
		return dec
	}
	if req.CredentialType == "" {
		return Deny("no id-scoped grant; no type fallback")
	}
	typeResource := "cerebro-credential-type:" + string(req.CredentialType)
	if dec := p.Backend.CheckCredential(ctx, action, typeResource, actor); dec.Allowed {
		return dec
	}
	return Deny("no matching credential grant (id or type)")
}
