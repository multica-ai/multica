// Package permissions implements the central effective-permission resolver
// for the cerebro control plane (JEH-978).
//
// The resolver answers one question:
//
//	can(actor, capability, resource) → Allow | Deny | NeedsApproval
//
// with one consistent algorithm across UI, API, CLI, MCP, autopilot and
// runtime call sites, and writes one audit row per evaluation.
//
// Grant retirement (FIR-1512). The resolver used to read the
// cerebro_workspace_grant table at request time. Every surface that could
// author a grant — the agent-facing grant tools, the `multica grant` CLI, and
// the operator grant control plane — was removed in Spor 1 (#1688) and #1768,
// so the table sat empty and write-dead in production. With no grant able to
// exist, the resolver is now deny-by-default for every request: it resolves
// against an empty grant set and returns Deny. This is byte-for-byte the
// verdict the old algorithm produced against the empty table — only the
// synchronous DB read (and the now-dropped table) are gone.
//
// The resolver remains the security FLOOR that every call site layers its own
// authority on top of: the credentials policy adds an upstream owner-allow
// check (deny-by-default for non-owners), and the approval gate layers the
// tighten-only tool-policy chain. Removing the floor itself is a separate,
// larger decision (engine-flip option B) and is intentionally NOT done here.
package permissions

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	googleuuid "github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
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

// Actor identifies whose authority is being used. For an autopilot run, the
// Actor is the autopilot's owner (member), not the agent — autopilot-owner
// substitution lives at the call site.
type Actor struct {
	Type       string // member|agent
	ID         pgtype.UUID
	GroupIDs   []pgtype.UUID // groups the actor belongs to in this workspace
	RoleIDs    []pgtype.UUID // roles assigned to the actor
	ProjectIDs []pgtype.UUID // projects the current task belongs to (FIR-2512)
}

// Request is the input to Can.
//
// Agent is the agent the actor is running through (when applicable). It is
// recorded on the audit row so the bruger × agent × workspace context is
// preserved even though the resolver no longer evaluates grants.
type Request struct {
	WorkspaceID pgtype.UUID
	Actor       Actor
	Agent       pgtype.UUID // optional; .Valid==false when call has no agent
	Capability  string
	Resource    string    // arbitrary string naming the resource under evaluation
	Now         time.Time // override for testing; defaults to time.Now()
}

// Resolver runs permission evaluation. It is safe for concurrent use.
type Resolver struct {
	Audit permissionAuditWriter
}

type permissionAuditWriter interface {
	InsertCerebroPermissionAuditEvent(ctx context.Context, arg cerebrodb.InsertCerebroPermissionAuditEventParams) error
}

// New constructs a Resolver backed by cerebro queries (used only for the
// permission-audit write — the grant table it once read is retired).
func New(q *cerebrodb.Queries) *Resolver { return &Resolver{Audit: q} }

// Can resolves req against an empty grant set and writes the audit event.
//
// The cerebro_workspace_grant table was retired in FIR-1512 (no surface can
// author a grant), so every request resolves to deny-by-default — identical to
// the verdict the resolver produced against the already-empty table. Each call
// site supplies its own allow authority on top of this floor (owner-allow for
// credentials, the tool-policy chain for the approval gate).
func (r *Resolver) Can(ctx context.Context, req Request) (Decision, error) {
	if r == nil {
		return Decision{Kind: DecisionDeny, Reason: "resolver not configured"}, nil
	}
	decision := denyByDefault(req)
	if err := r.writeAuditEvent(ctx, req, decision); err != nil {
		return decision, err
	}
	return decision, nil
}

// denyByDefault is the resolver's only possible verdict now that grants are
// retired: an empty grant set matches nothing, so every request is denied. It
// reproduces exactly what the prior Decide algorithm returned for an empty
// slice (capability still required; no matching grant otherwise).
func denyByDefault(req Request) Decision {
	if req.Capability == "" {
		return Decision{Kind: DecisionDeny, Reason: "capability required"}
	}
	return Decision{Kind: DecisionDeny, Reason: "no matching grant", WinningOverrideLayer: "none"}
}

func (r *Resolver) writeAuditEvent(ctx context.Context, req Request, decision Decision) error {
	if r == nil || r.Audit == nil {
		return nil
	}
	details, err := json.Marshal(map[string]any{
		"subject_type":           req.Actor.Type,
		"subject_id":             uuidString(req.Actor.ID),
		"agent_id":               uuidString(req.Agent),
		"group_ids":              uuidStrings(req.Actor.GroupIDs),
		"role_ids":               uuidStrings(req.Actor.RoleIDs),
		"capability":             req.Capability,
		"scope":                  req.Resource,
		"decision":               string(decision.Kind),
		"winning_override_layer": decision.WinningOverrideLayer,
		"matched_grant_ids":      uuidStrings(decision.MatchedGrantIDs),
		"reason":                 decision.Reason,
	})
	if err != nil {
		return fmt.Errorf("marshal permission audit event: %w", err)
	}
	if err := r.Audit.InsertCerebroPermissionAuditEvent(ctx, cerebrodb.InsertCerebroPermissionAuditEventParams{
		WorkspaceID: req.WorkspaceID,
		ActorType:   pgtype.Text{String: req.Actor.Type, Valid: req.Actor.Type != ""},
		ActorID:     req.Actor.ID,
		Details:     details,
	}); err != nil {
		return fmt.Errorf("write permission audit event: %w", err)
	}
	return nil
}

func uuidString(v pgtype.UUID) string {
	if !v.Valid {
		return ""
	}
	id, err := googleuuid.FromBytes(v.Bytes[:])
	if err != nil {
		return ""
	}
	return id.String()
}

func uuidStrings(values []pgtype.UUID) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if id := uuidString(value); id != "" {
			out = append(out, id)
		}
	}
	return out
}
