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
	// cerebroQueries reads the cerebro_policy_cel feature flag so a CEL Expr
	// condition on a credential cap row evaluates (FIR-1609). nil leaves Query.Eval
	// unset, so an Expr condition is undecidable and fails closed by effect — the
	// same default-OFF behaviour as the daemon and registry gates.
	cerebroQueries *cerebrodb.Queries
}

// policyCELEvaluator returns the shared CEL evaluator when the default-OFF
// cerebro_policy_cel flag is on for this workspace, else nil — the credential-gate
// twin of the daemon/registry gates, so a credential cap row's Expr condition
// evaluates identically everywhere. A lookup miss or DB error resolves to OFF.
func (m *multicaCredentialPolicy) policyCELEvaluator(ctx context.Context, workspaceID pgtype.UUID) toolpolicy.ExprEvaluator {
	if m.cerebroQueries == nil {
		return nil
	}
	enabled, err := m.cerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     toolpolicy.FlagPolicyCEL,
	})
	if err != nil || !enabled {
		return nil
	}
	eval, err := toolpolicy.SharedCELEvaluator()
	if err != nil {
		return nil
	}
	return eval.Eval
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

	// 2. CHAIN SIGNAL — one resolution of the unified tool-policy chain per scope,
	// reporting BOTH roles the chain plays for credentials:
	//   - cap:   the more-restrictive Deny|Ask it imposes (tighten-only, Base=Allow,
	//            so no rows ⇒ Allow ⇒ no cap). This is the pre-FIR-1609-Phase-7-keystone
	//            behaviour and is ALWAYS active.
	//   - grant: an EXPLICIT Allow row (DecidedBy set) granting access at either
	//            scope — the keystone that lets the chain be an Allow-source so the
	//            old grant table can be retired. Gated behind cerebro_credential_chain_grant
	//            (default OFF) and never counts a no-row Base=Allow result, so it can
	//            NEVER open a default-allow hole on reveal. Fails CLOSED on any error.
	cap, capReason, granted, grantReason, err := m.chainCredentialSignal(ctx, req, action, idResource, typeResource)
	if err != nil {
		return credentials.Deny("credential policy chain resolve failed: " + err.Error())
	}

	// 3. COMBINE — union the grant authorities (grant floor OR explicit chain Allow),
	// then apply the tighten-only cap. The cap can only tighten, never loosen, so the
	// result is never looser than both the floor and an explicit per-credential Deny.
	final, reason := foldCredentialVerdict(floor, floorReason, granted, grantReason, cap, capReason)

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

// foldCredentialVerdict combines the two grant authorities and the tighten-only
// cap into the final credential verdict (FIR-1609 Phase 7 keystone).
//
// Step 1 — UNION the grant authorities: access is granted if the deny-by-default
// grant floor allows OR an explicit tool-policy Allow row grants it. An explicit
// chain Allow only ever RAISES the floor toward Allow; it can never tighten (that
// is the cap's job) and never fabricates a grant from a no-row default (the caller
// passes granted=false in that case), so deny-by-default is preserved.
//
// Step 2 — apply the tighten-only cap exactly as before, so a per-credential admin
// Deny (or Ask) still overrides even a freshly granted credential. Security
// invariant: the result is never looser than min(floor-or-grant) AND never looser
// than the cap — identical to foldCredentialCap once the grant is folded in.
func foldCredentialVerdict(floor toolpolicy.Setting, floorReason string, granted bool, grantReason string, cap toolpolicy.Setting, capReason string) (toolpolicy.Setting, string) {
	base, baseReason := floor, floorReason
	// An explicit chain Allow grants only when the floor did not already allow —
	// it lifts an Ask/Deny floor to Allow. It never loosens the cap below.
	if granted && base != toolpolicy.SettingAllow {
		base, baseReason = toolpolicy.SettingAllow, grantReason
	}
	return foldCredentialCap(base, cap, baseReason, capReason)
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

// flagCredentialChainGrant is the workspace feature-flag key (default OFF) that
// lets an EXPLICIT tool-policy Allow row grant credential access — the FIR-1609
// Phase 7 keystone that makes the unified chain an Allow-source so the legacy
// grant table can eventually be retired. While OFF, the chain stays a pure
// tighten-only cap and credential governance is byte-for-byte the prior behaviour.
const flagCredentialChainGrant = "cerebro_credential_chain_grant"

// chainCredentialSignal resolves the unified tool-policy chain once per credential
// scope (id, then type fallback) and reports BOTH roles the chain plays:
//
//   - cap   — the more-restrictive Deny|Ask the chain imposes (Deny at EITHER scope
//     caps; Base=Allow ⇒ no rows ⇒ Allow ⇒ no cap). ALWAYS active; identical to the
//     prior adminCap.
//   - grant — true iff an EXPLICIT Allow row (eff.DecidedBy != "", i.e. a real
//     authored layer, never the no-row Base default) allows at either scope, AND the
//     cerebro_credential_chain_grant flag is on for this workspace. A no-row Base=Allow
//     result is NOT a grant, so this can never open a default-allow hole on reveal.
//
// nil store ⇒ Allow cap, no grant (pure grant-floor behaviour). Any lookup/resolve
// error fails CLOSED (cap=Deny, grant=false).
func (m *multicaCredentialPolicy) chainCredentialSignal(ctx context.Context, req credentials.PolicyRequest, action, idResource, typeResource string) (cap toolpolicy.Setting, capReason string, grant bool, grantReason string, err error) {
	if m.caps == nil {
		return toolpolicy.SettingAllow, "", false, "", nil
	}
	base, err := m.actorQueryBase(ctx, req)
	if err != nil {
		return toolpolicy.SettingDeny, "", false, "", err
	}
	grantEnabled := m.credentialChainGrantEnabled(ctx, base.WorkspaceID)

	cap = toolpolicy.SettingAllow
	// id scope is most specific and named first for attribution.
	resources := []string{idResource}
	if typeResource != "" {
		resources = append(resources, typeResource)
	}
	for _, resource := range resources {
		eff, rerr := m.resolveChainEffective(ctx, base, action, resource)
		if rerr != nil {
			return toolpolicy.SettingDeny, "", false, "", rerr
		}
		switch {
		case eff.Setting == toolpolicy.SettingAllow && eff.DecidedBy != "":
			// EXPLICIT Allow row — an Allow-source grant (keystone). Only honoured
			// when the flag is on; id scope wins attribution (set once).
			if grantEnabled && !grant {
				grant, grantReason = true, "granted by tool-policy: "+eff.Reason
			}
		case eff.Setting == toolpolicy.SettingDeny || eff.Setting == toolpolicy.SettingAsk:
			// Tighten-only cap — Deny at either scope caps (more-restrictive wins).
			if toolpolicy.MoreRestrictive(cap, eff.Setting) == eff.Setting && eff.Setting != cap {
				cap, capReason = eff.Setting, eff.Reason
			}
		}
	}
	return cap, capReason, grant, grantReason, nil
}

// resolveChainEffective runs one chain resolution with Base=Allow and validates the
// result is a concrete setting (fail CLOSED on anything unexpected). It returns the
// full Effective so the caller can distinguish an EXPLICIT Allow row (DecidedBy set)
// from the no-row Base=Allow default (DecidedBy empty) — the difference between a
// real grant and a hole.
func (m *multicaCredentialPolicy) resolveChainEffective(ctx context.Context, base toolpolicy.Query, action, resource string) (toolpolicy.Effective, error) {
	q := base
	q.ToolKey = action
	q.ResourcePattern = resource
	q.Base = toolpolicy.SettingAllow
	// CEREBRO-PATCH(credential-action-condition): FIR-1609 — derive the action verb
	// (credential.reveal→"reveal"…) so an action-scoped Condition bites on credential
	// rows too. Without it ctx.Action is "" and an action-scoped Deny silently drops;
	// mirrors the repo gate.
	q.RequestContext = toolpolicy.RequestContext{Action: toolpolicy.ActionOf(action)}
	// Inject the CEL evaluator (flag-gated) so an Expr condition on a credential row
	// evaluates rather than failing closed undecided — FIR-1609, twin of the
	// daemon/registry gates. nil (flag off) keeps the prior fail-closed behaviour.
	q.Eval = m.policyCELEvaluator(ctx, q.WorkspaceID)
	eff, err := m.caps.Resolve(ctx, q)
	if err != nil {
		return toolpolicy.Effective{}, err
	}
	switch eff.Setting {
	case toolpolicy.SettingAllow, toolpolicy.SettingAsk, toolpolicy.SettingDeny:
		return eff, nil
	default:
		return toolpolicy.Effective{Setting: toolpolicy.SettingDeny, Reason: "credential chain returned non-concrete setting"}, nil
	}
}

// credentialChainGrantEnabled reads the default-OFF cerebro_credential_chain_grant
// flag for the workspace. A lookup miss or DB error resolves to OFF — fail safe, so
// the chain stays a pure cap and the grant table remains the sole Allow-source.
func (m *multicaCredentialPolicy) credentialChainGrantEnabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	if m.cerebroQueries == nil {
		return false
	}
	enabled, err := m.cerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: workspaceID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     flagCredentialChainGrant,
	})
	if err != nil {
		return false
	}
	return enabled
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
			caps:           toolpolicy.NewStoreFromQueries(cerebroQueries),
			agents:         queries,
			gate:           gate,
			cerebroQueries: cerebroQueries,
		}
	}
	return credentials.NewChainPolicyChecker(owner, multica)
}
