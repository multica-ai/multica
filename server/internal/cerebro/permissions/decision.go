// Package permissions defines the decision contract shared by the canonical
// tool-policy resolver and the approval inbox.
package permissions

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// DecisionKind is the resolver's verdict for one (actor, capability, resource).
type DecisionKind string

const (
	DecisionAllow         DecisionKind = "allow"
	DecisionDeny          DecisionKind = "deny"
	DecisionNeedsApproval DecisionKind = "needs_approval"
)

// Decision is the resolver output. Reason is a short human-readable explanation
// suitable for audit rows and error responses.
type Decision struct {
	Kind                 DecisionKind
	MatchedGrantIDs      []pgtype.UUID
	Reason               string
	WinningOverrideLayer string
}

// Allowed is a convenience for the most common call shape.
func (d Decision) Allowed() bool { return d.Kind == DecisionAllow }

// Actor identifies whose authority is being used.
type Actor struct {
	Type       string // member|agent
	ID         pgtype.UUID
	GroupIDs   []pgtype.UUID
	RoleIDs    []pgtype.UUID
	ProjectIDs []pgtype.UUID
}

// Request identifies the decision and approval context for one action.
type Request struct {
	WorkspaceID pgtype.UUID
	Actor       Actor
	Agent       pgtype.UUID
	Capability  string
	Resource    string
	Now         time.Time
}
