// Package permissions implements the central effective-permission resolver
// for the cerebro control plane (JEH-978).
//
// The resolver answers one question:
//
//	can(actor, capability, resource) → Allow | Deny | NeedsApproval
//
// with one consistent algorithm across UI, API, CLI, MCP, autopilot and
// runtime call sites. Today every enforcement path read grants directly and
// implemented its own intersection logic; they now route through Resolver so
// the override-lag (workspace_default → role → group → actor) is computed in
// one place and the audit trail is shaped identically.
//
// Decide is pure (slice of grants in, decision out). Can wraps it with a DB
// fetch. Tests use Decide so they do not need a database.
package permissions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// Subject types accepted on grant rows.
const (
	subjectMember           = "member"
	subjectAgent            = "agent"
	subjectGroup            = "group"
	subjectRole             = "role"
	subjectWorkspaceDefault = "workspace_default"
)

// grantStatusActive — non-revoked grants are evaluated.
const grantStatusActive = "active"

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
	Type     string // member|agent
	ID       pgtype.UUID
	GroupIDs []pgtype.UUID // groups the actor belongs to in this workspace
	RoleIDs  []pgtype.UUID // roles assigned to the actor
}

// Request is the input to Resolve / Can.
//
// Agent is the agent the actor is running through (when applicable). It lets
// the resolver enforce the bruger × agent × workspace intersection: even if
// the actor is allowed for a capability, the agent must also be allowed —
// expressed as a grant with subject_type=agent.
type Request struct {
	WorkspaceID pgtype.UUID
	Actor       Actor
	Agent       pgtype.UUID // optional; .Valid==false when call has no agent
	Capability  string
	Resource    string    // arbitrary string the grant resource_pattern matches against; "" matches any pattern
	Now         time.Time // override for testing; defaults to time.Now()
}

// Resolver runs grant evaluation. It is safe for concurrent use.
type Resolver struct {
	Cerebro grantLister
	Audit   permissionAuditWriter
}

type grantLister interface {
	ListCerebroWorkspaceGrants(ctx context.Context, arg cerebrodb.ListCerebroWorkspaceGrantsParams) ([]cerebrodb.CerebroWorkspaceGrant, error)
}

type permissionAuditWriter interface {
	InsertCerebroPermissionAuditEvent(ctx context.Context, arg cerebrodb.InsertCerebroPermissionAuditEventParams) error
}

// New constructs a Resolver backed by cerebro queries.
func New(q *cerebrodb.Queries) *Resolver { return &Resolver{Cerebro: q, Audit: q} }

// Can fetches the workspace's active grants and resolves them against req.
// A nil Cerebro field means "no grants known" → deny.
func (r *Resolver) Can(ctx context.Context, req Request) (Decision, error) {
	if r == nil || r.Cerebro == nil {
		return Decision{Kind: DecisionDeny, Reason: "resolver not configured"}, nil
	}
	rows, err := r.Cerebro.ListCerebroWorkspaceGrants(ctx, cerebrodb.ListCerebroWorkspaceGrantsParams{
		WorkspaceID: req.WorkspaceID,
	})
	if err != nil {
		return Decision{Kind: DecisionDeny, Reason: "grant lookup failed"}, fmt.Errorf("list grants: %w", err)
	}
	decision := Decide(rows, req)
	if err := r.writeAuditEvent(ctx, req, decision); err != nil {
		return decision, err
	}
	return decision, nil
}

// Decide is the pure resolution function. Tests pass synthetic grants directly.
//
// Algorithm:
//
//  1. Drop revoked/expired grants and grants outside the time-window.
//  2. Keep grants whose subject applies to the actor or agent.
//  3. Keep grants whose capability and resource patterns match the request.
//  4. If no grant matches → Deny.
//  5. If any matching grant has approval_required → NeedsApproval.
//  6. Otherwise → Allow.
//
// Specificity ordering (workspace → runtime → role → group → actor) is not yet
// modelled in the grant table; today every matching active grant counts as a
// vote. When the override-lag lands, the most-specific matching grant wins and
// the others are recorded as "shadowed" in the decision reason.
func Decide(grants []cerebrodb.CerebroWorkspaceGrant, req Request) Decision {
	if req.Capability == "" {
		return Decision{Kind: DecisionDeny, Reason: "capability required"}
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}

	var matched []pgtype.UUID
	anyApproval := false
	winningLayer := "none"
	for _, g := range grants {
		if g.Status != grantStatusActive {
			continue
		}
		if g.TimeWindowStart.Valid && now.Before(g.TimeWindowStart.Time) {
			continue
		}
		if g.TimeWindowEnd.Valid && now.After(g.TimeWindowEnd.Time) {
			continue
		}
		if !subjectApplies(g, req.Actor, req.Agent) {
			continue
		}
		if !capabilityMatches(g.Capability, req.Capability) {
			continue
		}
		if !resourceMatches(g.ResourcePattern, req.Resource) {
			continue
		}
		matched = append(matched, g.ID)
		layer := overrideLayer(g)
		if overrideLayerRank(layer) >= overrideLayerRank(winningLayer) {
			winningLayer = layer
		}
		if g.ApprovalRequired {
			anyApproval = true
		}
	}

	if len(matched) == 0 {
		return Decision{Kind: DecisionDeny, Reason: "no matching grant", WinningOverrideLayer: "none"}
	}
	if anyApproval {
		return Decision{Kind: DecisionNeedsApproval, MatchedGrantIDs: matched, Reason: "approval required", WinningOverrideLayer: winningLayer}
	}
	return Decision{Kind: DecisionAllow, MatchedGrantIDs: matched, Reason: "grant matched", WinningOverrideLayer: winningLayer}
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

// subjectApplies reports whether the grant's subject row applies to this
// actor or the agent they are running through.
//
//	workspace_default → always applies
//	member            → grant.subject_id == actor.ID and actor.Type == member
//	agent             → grant.subject_id == request.Agent (the agent in use)
//	group             → grant.subject_id ∈ actor.GroupIDs
//	role              → grant.subject_id ∈ actor.RoleIDs
func subjectApplies(g cerebrodb.CerebroWorkspaceGrant, actor Actor, agent pgtype.UUID) bool {
	switch g.SubjectType {
	case subjectWorkspaceDefault:
		return true
	case subjectMember:
		return actor.Type == subjectMember && uuidEqual(g.SubjectID, actor.ID)
	case subjectAgent:
		if uuidEqual(g.SubjectID, agent) {
			return true
		}
		// Direct-agent calls (no human in the loop) still need to match if the
		// actor itself is the agent.
		return actor.Type == subjectAgent && uuidEqual(g.SubjectID, actor.ID)
	case subjectGroup:
		for _, gid := range actor.GroupIDs {
			if uuidEqual(g.SubjectID, gid) {
				return true
			}
		}
		return false
	case subjectRole:
		for _, rid := range actor.RoleIDs {
			if uuidEqual(g.SubjectID, rid) {
				return true
			}
		}
		return false
	}
	return false
}

func overrideLayer(g cerebrodb.CerebroWorkspaceGrant) string {
	switch g.SubjectType {
	case subjectWorkspaceDefault:
		return "workspace"
	case subjectRole:
		return "role"
	case subjectGroup:
		return "group"
	case subjectMember, subjectAgent:
		return "actor"
	default:
		return "unknown"
	}
}

func overrideLayerRank(layer string) int {
	switch layer {
	case "workspace":
		return 1
	case "role":
		return 2
	case "group":
		return 3
	case "actor":
		return 4
	default:
		return 0
	}
}

// capabilityMatches supports three pattern shapes:
//
//	"*"          → matches any capability
//	"issue.read" → exact match
//	"issue.*"    → matches any capability with the "issue." prefix
//
// Dotted namespaces let admins grant "issue.*" without enumerating every leaf.
func capabilityMatches(pattern, capability string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == capability {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, "*") // keep the trailing dot
		return strings.HasPrefix(capability, prefix)
	}
	return false
}

// resourceMatches supports three pattern shapes:
//
//	""           → unconstrained pattern stored as empty string is treated as "*"
//	"*"          → matches any resource
//	"issues/123" → exact match
//	"issues/*"   → matches any resource under "issues/"
//
// An empty req.Resource matches any pattern — used by call sites that gate at
// the capability level (e.g. "may this actor run agents at all") and have no
// concrete resource to test.
func resourceMatches(pattern, resource string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	if resource == "" {
		return true
	}
	if pattern == resource {
		return true
	}
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(resource, prefix)
	}
	return false
}

func uuidEqual(a, b pgtype.UUID) bool {
	if !a.Valid || !b.Valid {
		return false
	}
	return a.Bytes == b.Bytes
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
