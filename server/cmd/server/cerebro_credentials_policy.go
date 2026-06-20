// CEREBRO-PATCH(cerebro-credentials-policy): JEH-1197 — net-new cerebro-only
// factory for the credential registry's PolicyChecker. Lives under
// server/cmd/server/ alongside cerebro_persona_mask.go so the router's
// CEREBRO-PATCH stays a single line. See docs/cerebro-patches.md.
//
// JEH-1197: credential governance policy wiring.
//
// Builds the production PolicyChecker for the credential registry:
//
//   1. OwnerPolicyChecker — workspace owners/admins always allow.
//   2. multicaCredentialPolicy — the unified Multica permission engine:
//      a deny-by-default grant floor (id scope first, type scope fallback)
//      layered with the tighten-only tool-policy cap chain. This is the
//      sole governance authority for credentials (FIR-1609); the legacy
//      Persona cut-over was removed once the chain became authoritative.
//
// Layered: first ALLOW wins. If neither layer allows, the chain returns
// the deny (or owner deny in dev when the engine is unconfigured), which
// the Service surfaces as a 403 with the reason on the wire and a deny
// row in cerebro_credential_audit with the same reason.
//
// Kept in a cerebro-prefixed file so the router's CEREBRO-PATCH stays
// a single line. Matches the cerebro_persona_mask.go pattern.

package main

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	// CEREBRO-PATCH(cerebro-credentials-approval-gate): FIR-2586 route a credential
	// needs-approval verdict through the shared approval inbox instead of a silent deny.
	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	"github.com/multica-ai/multica/server/internal/cerebro/credentials"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	cerebropermissions "github.com/multica-ai/multica/server/internal/cerebro/permissions"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/db/generated"
)

// memberLookupFromQueries adapts the upstream db.Queries to the
// credentials.MemberLookup interface — just GetMemberByUserAndWorkspace
// returning the role. Keeps the credentials package independent of the
// upstream db types.
type memberLookupFromQueries struct {
	q *db.Queries
}

func (m *memberLookupFromQueries) GetMemberRole(ctx context.Context, workspaceID, userID pgtype.UUID) (string, error) {
	if m == nil || m.q == nil {
		return "", errors.New("member lookup not wired")
	}
	row, err := m.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Not a member of this workspace — caller should deny.
			return "", nil
		}
		return "", err
	}
	return row.Role, nil
}

// CEREBRO-PATCH(multica-credential-policy): FIR-2130/FIR-1609 — the Multica
// permission engine is the sole credential PolicyChecker. It started life
// behind a Persona cut-over (MULTICA_PERMISSION_ENGINE); once the unified
// tool-policy chain became authoritative (FIR-1609 Phase 7) the Persona path
// and its feature flag were removed (Phase 8).
//
// multicaCredentialPolicy adapts the Multica permission engine to the
// credential PolicyChecker interface. It mirrors Persona's two resource scopes:
// exact credential first, credential-type fallback second.
type multicaCredentialPolicy struct {
	// resolver is the credential GRANT FLOOR. Credentials are deny-by-default:
	// a non-owner actor with no live grant gets nothing. The toolpolicy chain is
	// the opposite (default-allow, tighten-only), so the chain CANNOT be the sole
	// authority for a credential verdict without opening a default-allow hole on
	// reveal. The resolver therefore stays as the deny-by-default floor, and the
	// unified tool-policy chain (caps) is layered ON TOP as a tighten-only cap —
	// see Check. This is FIR-1609 Phase 7: credentials join the unified chain for
	// the System/runtime/user/group/condition CAP layers, while grant-state keeps
	// supplying the floor. (Honest framing: the resolver is retained, not removed;
	// the chain adds caps it cannot loosen.)
	resolver *cerebropermissions.Resolver
	// caps is the unified tool-policy chain, consulted with Base=Allow so it
	// contributes ONLY admin/System/runtime/user/group Deny|Ask rows that tighten
	// the grant floor. nil keeps pure grant-floor behaviour (no caps). It never
	// grants: a chain Allow row on a credential is a no-op because the floor, not
	// the chain, decides whether access exists at all.
	caps *toolpolicy.Store
	// agents maps an agent actor onto its runtime/owner so the chain can resolve
	// the agent + runtime + owner-user ceilings. nil (or a lookup error) fails
	// CLOSED for agent actors — the OPPOSITE of the gateway connection path, which
	// fails open; on the highest-sensitivity action (reveal/rotate) we never want
	// a missing-agent lookup to widen access.
	agents agentRuntimeLookup
	// CEREBRO-PATCH(cerebro-credentials-approval-gate): FIR-2586 — shared approval
	// seam. nil when CEREBRO_APPROVAL_GATE_ENABLED is off, so a needs-approval
	// verdict falls through to deny exactly as before; non-nil routes it to the
	// one /approvals inbox and blocks until a human decides.
	gate *permgate.Gate
}

// agentRuntimeLookup is the slim surface needed to map an agent actor onto its
// runtime and owner. *db.Queries satisfies it via GetAgent.
type agentRuntimeLookup interface {
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
}

func (m *multicaCredentialPolicy) Check(ctx context.Context, req credentials.PolicyRequest) credentials.PolicyDecision {
	if m == nil || m.resolver == nil {
		return credentials.Deny("multica permission engine not configured")
	}
	// CANONICAL CREDENTIAL TOOL-POLICY CONVENTION (FIR-1609 Phase 7). The cap layer
	// only tightens rows authored with these EXACT strings (resource_pattern is
	// matched by equality, not glob), so any admin-authoring surface that writes
	// credential cap rows MUST mint ToolKey/ResourcePattern identically or the cap
	// silently misses. Until such an authoring surface exists the caps are inert in
	// the SAFE direction (no row ⇒ Allow ⇒ no-op ⇒ grant floor decides).
	//   ToolKey         = "credential.<attach|read_redacted|reveal|rotate|revoke>"
	//   ResourcePattern = "cerebro-credential:<uuid>"   (id scope, most specific)
	//                     "cerebro-credential-type:<type>" (type scope, fallback)
	action := "credential." + string(req.Permission)
	idResource := "cerebro-credential:" + util.UUIDToString(req.CredentialID)
	typeResource := ""
	if req.CredentialType != "" {
		typeResource = "cerebro-credential-type:" + string(req.CredentialType)
	}

	// 1. GRANT FLOOR — deny-by-default, exact pre-existing id→type semantics.
	floor, askResource, floorReason, err := m.grantFloor(ctx, req, action, idResource, typeResource)
	if err != nil {
		return credentials.Deny("multica permission engine failed: " + err.Error())
	}

	// 2. ADMIN/SYSTEM CAPS — tighten-only. Base=Allow, so an unconfigured chain
	// adds nothing; Deny|Ask rows on this credential (id or type scope) tighten
	// the floor. Fails CLOSED on any resolve/lookup error.
	cap, capReason, err := m.adminCap(ctx, req, action, idResource, typeResource)
	if err != nil {
		return credentials.Deny("credential policy cap resolve failed: " + err.Error())
	}

	// 3. COMBINE — the cap can only tighten the floor, never loosen it, so the
	// result is never more permissive than the grant floor alone (no regression).
	final, reason := foldCredentialCap(floor, cap, floorReason, capReason)

	switch final {
	case toolpolicy.SettingAllow:
		return credentials.Allow(reason)
	case toolpolicy.SettingAsk:
		// An Ask reaches here only from an approval_required grant or a chain Ask
		// row — never from a missing grant (that is a hard Deny floor). Route it to
		// the shared inbox; with the gate off, fall through to deny exactly as the
		// pre-FIR-2586 behaviour did.
		if m.gate != nil {
			r := askResource
			if r == "" {
				r = idResource
			}
			return m.awaitApproval(ctx, req, action, r, reason)
		}
		return credentials.Deny("approval required but approval gate disabled")
	default:
		return credentials.Deny(reason)
	}
}

// foldCredentialCap applies the tighten-only admin/System cap to the grant floor.
// This is the security-critical core: MoreRestrictive guarantees the cap can only
// raise restrictiveness, so the credential verdict is NEVER looser than the grant
// floor alone. When the cap is the decider, attribution flows to the cap reason.
func foldCredentialCap(floor, cap toolpolicy.Setting, floorReason, capReason string) (toolpolicy.Setting, string) {
	final := toolpolicy.MoreRestrictive(floor, cap)
	if final == cap && cap != floor {
		return final, capReason
	}
	return final, floorReason
}

// grantFloor computes the deny-by-default credential verdict from grants alone,
// reproducing the prior id→type precedence exactly: an Allow at EITHER scope
// wins (id first), then an Ask at either scope (id first), else Deny. The
// returned askResource names the scope that asked (for the inbox row).
func (m *multicaCredentialPolicy) grantFloor(ctx context.Context, req credentials.PolicyRequest, action, idResource, typeResource string) (setting toolpolicy.Setting, askResource, reason string, err error) {
	actor := cerebropermissions.Actor{Type: req.ActorType, ID: req.ActorID}

	idDec, err := m.resolveScope(ctx, req, actor, action, idResource)
	if err != nil {
		return "", "", "", err
	}
	if idDec.Kind == cerebropermissions.DecisionAllow {
		return toolpolicy.SettingAllow, "", "multica grant matched", nil
	}

	var typeDec cerebropermissions.Decision
	if typeResource != "" {
		typeDec, err = m.resolveScope(ctx, req, actor, action, typeResource)
		if err != nil {
			return "", "", "", err
		}
		if typeDec.Kind == cerebropermissions.DecisionAllow {
			return toolpolicy.SettingAllow, "", "multica grant matched (type)", nil
		}
	}

	// No allow at either scope. An approval_required grant surfaces as Ask, id
	// first so the inbox row names the exact credential.
	if idDec.Kind == cerebropermissions.DecisionNeedsApproval {
		return toolpolicy.SettingAsk, idResource, idDec.Reason, nil
	}
	if typeResource != "" && typeDec.Kind == cerebropermissions.DecisionNeedsApproval {
		return toolpolicy.SettingAsk, typeResource, typeDec.Reason, nil
	}

	if typeResource == "" {
		return toolpolicy.SettingDeny, "", "no id-scoped grant; no type fallback", nil
	}
	return toolpolicy.SettingDeny, "", "no matching credential grant (id or type)", nil
}

// adminCap resolves the unified tool-policy chain for both credential scopes and
// returns the more-restrictive cap (Deny at EITHER scope caps — a per-credential
// id Deny is not weakened by a broader type grant). Base=Allow means no rows ⇒
// Allow ⇒ no cap. nil store ⇒ Allow. Any lookup/resolve error fails CLOSED.
func (m *multicaCredentialPolicy) adminCap(ctx context.Context, req credentials.PolicyRequest, action, idResource, typeResource string) (toolpolicy.Setting, string, error) {
	if m.caps == nil {
		return toolpolicy.SettingAllow, "", nil
	}
	base, err := m.actorQueryBase(ctx, req)
	if err != nil {
		return toolpolicy.SettingDeny, "", err
	}

	capID, reasonID, err := m.resolveCap(ctx, base, action, idResource)
	if err != nil {
		return toolpolicy.SettingDeny, "", err
	}
	cap, reason := capID, reasonID
	if typeResource != "" {
		capType, reasonType, err := m.resolveCap(ctx, base, action, typeResource)
		if err != nil {
			return toolpolicy.SettingDeny, "", err
		}
		if toolpolicy.MoreRestrictive(cap, capType) == capType && capType != cap {
			cap, reason = capType, reasonType
		}
	}
	return cap, reason, nil
}

// resolveCap runs one chain resolution with Base=Allow and validates the result
// is a concrete setting (fail CLOSED on anything unexpected).
func (m *multicaCredentialPolicy) resolveCap(ctx context.Context, base toolpolicy.Query, action, resource string) (toolpolicy.Setting, string, error) {
	q := base
	q.ToolKey = action
	q.ResourcePattern = resource
	q.Base = toolpolicy.SettingAllow
	// CEREBRO-PATCH(credential-action-condition): FIR-1609 — derive the action verb
	// (credential.reveal→"reveal"…) so an action-scoped Condition bites on credential
	// cap rows too. Without it ctx.Action is "" and an action-scoped Deny silently
	// drops; mirrors the repo gate. No conditioned credential rows exist yet, so this
	// is behaviour-preserving.
	q.RequestContext = toolpolicy.RequestContext{Action: toolpolicy.ActionOf(action)}
	eff, err := m.caps.Resolve(ctx, q)
	if err != nil {
		return toolpolicy.SettingDeny, "", err
	}
	switch eff.Setting {
	case toolpolicy.SettingAllow, toolpolicy.SettingAsk, toolpolicy.SettingDeny:
		return eff.Setting, eff.Reason, nil
	default:
		return toolpolicy.SettingDeny, "credential cap returned non-concrete setting", nil
	}
}

// actorQueryBase maps the credential actor onto the chain's subject ids. A member
// is its own user ceiling; an agent contributes its agent + runtime + owner-user
// ceilings (owner via GetAgent). IsSystem stays false: PolicyRequest carries no
// run origin, so the System Ask→Deny fail-safe cannot be set here without
// threading origin through the credentials Service — a separate change. This is
// safe because a missing grant is already a hard Deny floor (never reaches Ask),
// and the gate's await path fails closed on timeout, so no silent reveal results.
func (m *multicaCredentialPolicy) actorQueryBase(ctx context.Context, req credentials.PolicyRequest) (toolpolicy.Query, error) {
	q := toolpolicy.Query{WorkspaceID: req.WorkspaceID}
	switch req.ActorType {
	case "member":
		q.UserID = req.ActorID
		return q, nil
	case "agent":
		if m.agents == nil {
			return toolpolicy.Query{}, errors.New("agent lookup not configured")
		}
		agent, err := m.agents.GetAgent(ctx, req.ActorID)
		if err != nil {
			return toolpolicy.Query{}, err // fail closed
		}
		q.AgentID = req.ActorID
		q.RuntimeID = agent.RuntimeID
		q.UserID = agent.OwnerID
		return q, nil
	default:
		return toolpolicy.Query{}, errors.New("unknown credential actor type: " + req.ActorType)
	}
}

// resolveScope returns the raw grant decision for one (action, resource) pair.
func (m *multicaCredentialPolicy) resolveScope(ctx context.Context, req credentials.PolicyRequest, actor cerebropermissions.Actor, action, resource string) (cerebropermissions.Decision, error) {
	return m.resolver.Can(ctx, cerebropermissions.Request{
		WorkspaceID: req.WorkspaceID,
		Actor:       actor,
		Capability:  action,
		Resource:    resource,
	})
}

// awaitApproval raises a credential approval in the shared inbox and blocks until
// a human decides. Approve → Allow; reject/expire/timeout → Deny. A gate error
// fails closed (Deny), never a silent allow. (FIR-2586)
func (m *multicaCredentialPolicy) awaitApproval(ctx context.Context, req credentials.PolicyRequest, action, resource, reason string) credentials.PolicyDecision {
	greq := permgate.Request{
		Permission: cerebropermissions.Request{
			WorkspaceID: req.WorkspaceID,
			Actor:       cerebropermissions.Actor{Type: req.ActorType, ID: req.ActorID},
			Capability:  action,
			Resource:    resource,
		},
		RequesterType: req.ActorType,
		RequesterID:   req.ActorID,
		Surface:       approvals.SurfaceSystem,
		Context: map[string]any{
			"credential_id":   util.UUIDToString(req.CredentialID),
			"credential_type": string(req.CredentialType),
			"permission":      string(req.Permission),
			"resource":        resource,
		},
	}
	res, err := m.gate.GuardDecision(ctx, greq, cerebropermissions.Decision{
		Kind:   cerebropermissions.DecisionNeedsApproval,
		Reason: reason,
	})
	if err != nil {
		return credentials.Deny("approval gate error: " + err.Error())
	}
	if res.Outcome.Stops() {
		return credentials.Deny("approval " + string(res.Outcome))
	}
	return credentials.Allow("approved via inbox")
}

// newCredentialsPolicy builds the production chain. queries is the
// upstream db handle (needed for the owner check). Returns DenyAllChecker
// when queries is nil, which shouldn't happen in production but keeps
// startup safe for tests / partial wiring.
//
// The chain is ChainPolicyChecker(owner, multica): workspace owners/admins
// short-circuit to allow, everyone else goes through the Multica permission
// engine (deny-by-default grant floor + tighten-only tool-policy caps). When
// cerebroQueries is nil the multica layer is omitted and only the owner check
// fires — the deny-by-default behaviour from JEH-1197.
//
// gate is the shared approval seam (nil when CEREBRO_APPROVAL_GATE_ENABLED is
// off). When non-nil a credential needs-approval verdict lands in the one
// /approvals inbox and blocks until a human decides, instead of a silent deny.
func newCredentialsPolicy(cerebroQueries *cerebrodb.Queries, queries *db.Queries, gate *permgate.Gate) credentials.PolicyChecker {
	if queries == nil {
		return credentials.DenyAllChecker
	}
	owner := credentials.NewOwnerPolicyChecker(&memberLookupFromQueries{q: queries})
	multica := credentials.PolicyChecker(nil)
	if cerebroQueries != nil {
		multica = &multicaCredentialPolicy{
			resolver: cerebropermissions.New(cerebroQueries),
			// Unified tool-policy chain as the tighten-only cap layer (FIR-1609
			// Phase 7) — built from the same cerebro queries; agent→runtime/owner
			// mapping comes from the upstream queries (GetAgent).
			caps:   toolpolicy.NewStoreFromQueries(cerebroQueries),
			agents: queries,
			gate:   gate,
		}
	}
	return credentials.NewChainPolicyChecker(owner, multica)
}
