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
//   2. PersonaPolicyChecker — defers to persona; checks id-scoped grant
//      first (cerebro-credential:<uuid>), falls back to type-scoped
//      grant (cerebro-credential-type:<type>).
//
// Layered: first ALLOW wins. If neither layer allows, the chain returns
// the persona deny (or owner deny in dev when persona is unconfigured),
// which the Service surfaces as a 403 with the persona reason on the
// wire and a deny row in cerebro_credential_audit with the same reason.
//
// Kept in a cerebro-prefixed file so the router's CEREBRO-PATCH stays
// a single line. Matches the cerebro_persona_mask.go pattern.

package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	persona "github.com/hvejsel/firtal-persona/sdk/go"
	// CEREBRO-PATCH(cerebro-credentials-approval-gate): FIR-2586 route a credential
	// needs-approval verdict through the shared approval inbox instead of a silent deny.
	"github.com/multica-ai/multica/server/internal/cerebro/approvals"
	"github.com/multica-ai/multica/server/internal/cerebro/credentials"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/permgate"
	cerebropermissions "github.com/multica-ai/multica/server/internal/cerebro/permissions"
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

// personaBackend wraps the persona SDK's CheckResource so it conforms
// to credentials.PersonaChecker. Constructed only when persona env vars
// are present.
type personaBackend struct {
	client *persona.Client
	log    *slog.Logger
}

func (p *personaBackend) CheckCredential(ctx context.Context, action, resource, actorID string) credentials.PolicyDecision {
	if p == nil || p.client == nil {
		return credentials.Deny("persona client not configured")
	}
	// resource is canonical "<kind>:<id-or-type>"; persona's CheckResource
	// takes the kind and id separately, so we split on the first colon.
	kind, id, ok := strings.Cut(resource, ":")
	if !ok {
		return credentials.Deny("malformed credential resource: " + resource)
	}
	res, err := p.client.CheckResource(ctx, actorID, action, kind, id, nil)
	if err != nil {
		if p.log != nil {
			p.log.Warn("credentials policy: persona check failed",
				"err", err, "kind", kind, "id", id, "action", action, "actor_id", actorID)
		}
		return credentials.Deny("persona unreachable")
	}
	if res == nil {
		return credentials.Deny("persona returned nil result")
	}
	if !res.Allowed {
		reason := res.Reason
		if reason == "" {
			reason = "persona deny"
		}
		return credentials.PolicyDecision{Allowed: false, Reason: reason, DecisionID: res.DecisionID}
	}
	return credentials.PolicyDecision{Allowed: true, Reason: res.Reason, DecisionID: res.DecisionID}
}

// CEREBRO-PATCH(multica-credential-policy): FIR-2130 cut-over plumbing — new
// Multica permission engine implements the credential PolicyChecker interface
// alongside the existing Persona backend, feature-flagged via
// MULTICA_PERMISSION_ENGINE so we can run parallel/shadow before flipping.
//
// multicaCredentialPolicy adapts the new Multica permission engine to the
// credential PolicyChecker interface. It mirrors Persona's two resource scopes:
// exact credential first, credential-type fallback second.
type multicaCredentialPolicy struct {
	resolver *cerebropermissions.Resolver
	// CEREBRO-PATCH(cerebro-credentials-approval-gate): FIR-2586 — shared approval
	// seam. nil when CEREBRO_APPROVAL_GATE_ENABLED is off, so a needs-approval
	// verdict falls through to deny exactly as before; non-nil routes it to the
	// one /approvals inbox and blocks until a human decides.
	gate *permgate.Gate
}

func (m *multicaCredentialPolicy) Check(ctx context.Context, req credentials.PolicyRequest) credentials.PolicyDecision {
	if m == nil || m.resolver == nil {
		return credentials.Deny("multica permission engine not configured")
	}
	action := "credential." + string(req.Permission)
	actor := cerebropermissions.Actor{
		Type: req.ActorType,
		ID:   req.ActorID,
	}

	// Resolve the most-specific scope first, then the type fallback. A matching
	// allow at either scope wins immediately and never raises an approval.
	idResource := "cerebro-credential:" + util.UUIDToString(req.CredentialID)
	idDec, err := m.resolveScope(ctx, req, actor, action, idResource)
	if err != nil {
		return credentials.Deny("multica permission engine failed: " + err.Error())
	}
	if idDec.Kind == cerebropermissions.DecisionAllow {
		return credentials.Allow("multica grant matched")
	}

	var typeDec cerebropermissions.Decision
	var typeResource string
	if req.CredentialType != "" {
		typeResource = "cerebro-credential-type:" + string(req.CredentialType)
		typeDec, err = m.resolveScope(ctx, req, actor, action, typeResource)
		if err != nil {
			return credentials.Deny("multica permission engine failed: " + err.Error())
		}
		if typeDec.Kind == cerebropermissions.DecisionAllow {
			return credentials.Allow("multica grant matched (type)")
		}
	}

	// Neither scope allows. If either asks for approval and the shared gate is
	// wired, raise ONE inbox request (most-specific scope first) and await the
	// human decision — the same seam agent tool calls and repo checkout use
	// (FIR-2586). Flag off (gate==nil) keeps the prior deny-by-default behaviour.
	if m.gate != nil {
		if askResource, askReason, ok := firstCredentialAsk(idDec, idResource, typeDec, typeResource); ok {
			return m.awaitApproval(ctx, req, action, askResource, askReason)
		}
	}

	if req.CredentialType == "" {
		return credentials.Deny("no id-scoped grant; no type fallback")
	}
	return credentials.Deny("no matching credential grant (id or type)")
}

// resolveScope returns the raw engine decision for one (action, resource) pair.
func (m *multicaCredentialPolicy) resolveScope(ctx context.Context, req credentials.PolicyRequest, actor cerebropermissions.Actor, action, resource string) (cerebropermissions.Decision, error) {
	return m.resolver.Can(ctx, cerebropermissions.Request{
		WorkspaceID: req.WorkspaceID,
		Actor:       actor,
		Capability:  action,
		Resource:    resource,
	})
}

// firstCredentialAsk picks the scope whose verdict is needs-approval, preferring
// the most-specific (id) scope so the inbox row names the exact credential.
func firstCredentialAsk(idDec cerebropermissions.Decision, idResource string, typeDec cerebropermissions.Decision, typeResource string) (string, string, bool) {
	if idDec.Kind == cerebropermissions.DecisionNeedsApproval {
		return idResource, idDec.Reason, true
	}
	if typeResource != "" && typeDec.Kind == cerebropermissions.DecisionNeedsApproval {
		return typeResource, typeDec.Reason, true
	}
	return "", "", false
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
// Persona is optional: when MULTICA_PERSONA_URL / MULTICA_PERSONA_TOKEN
// are unset, only the owner check fires. This matches the issue's
// deny-by-default behaviour — non-owners get nothing until persona is
// configured and grants are issued.
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
		multica = &multicaCredentialPolicy{resolver: cerebropermissions.New(cerebroQueries), gate: gate}
	}

	url := strings.TrimSpace(os.Getenv("MULTICA_PERSONA_URL"))
	token := strings.TrimSpace(os.Getenv("MULTICA_PERSONA_TOKEN"))
	if url == "" || token == "" {
		return credentials.NewChainPolicyChecker(owner, multica)
	}
	client, err := persona.New(persona.Config{Endpoint: url, Token: token})
	var personaChk credentials.PolicyChecker
	if err != nil {
		slog.Default().Warn("credentials policy: persona client init failed, falling back to owner-only",
			"err", err)
	} else {
		personaChk = credentials.NewPersonaPolicyChecker(&personaBackend{client: client, log: slog.Default()})
	}
	cutover := credentials.NewCutoverPolicyChecker(
		personaChk,
		multica,
		credentials.PermissionEngineMode(os.Getenv("MULTICA_PERMISSION_ENGINE")),
		envInt("MULTICA_PERMISSION_PARALLEL_SAMPLE", 100),
		slog.Default(),
	)
	return credentials.NewChainPolicyChecker(owner, cutover)
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		slog.Default().Warn("invalid integer env var, using fallback", "key", key, "value", raw, "fallback", fallback)
		return fallback
	}
	return n
}
