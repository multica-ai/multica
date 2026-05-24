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
	"github.com/multica-ai/multica/server/internal/cerebro/credentials"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
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
	idResource := "cerebro-credential:" + util.UUIDToString(req.CredentialID)
	if dec := m.checkResource(ctx, req, actor, action, idResource); dec.Allowed {
		return dec
	}
	if req.CredentialType == "" {
		return credentials.Deny("no id-scoped grant; no type fallback")
	}
	typeResource := "cerebro-credential-type:" + string(req.CredentialType)
	if dec := m.checkResource(ctx, req, actor, action, typeResource); dec.Allowed {
		return dec
	}
	return credentials.Deny("no matching credential grant (id or type)")
}

func (m *multicaCredentialPolicy) checkResource(ctx context.Context, req credentials.PolicyRequest, actor cerebropermissions.Actor, action, resource string) credentials.PolicyDecision {
	dec, err := m.resolver.Can(ctx, cerebropermissions.Request{
		WorkspaceID: req.WorkspaceID,
		Actor:       actor,
		Capability:  action,
		Resource:    resource,
	})
	if err != nil {
		return credentials.Deny("multica permission engine failed: " + err.Error())
	}
	switch dec.Kind {
	case cerebropermissions.DecisionAllow:
		return credentials.Allow("multica grant matched")
	case cerebropermissions.DecisionNeedsApproval:
		return credentials.Deny("approval required")
	default:
		reason := dec.Reason
		if reason == "" {
			reason = "multica deny"
		}
		return credentials.Deny(reason)
	}
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
func newCredentialsPolicy(cerebroQueries *cerebrodb.Queries, queries *db.Queries) credentials.PolicyChecker {
	if queries == nil {
		return credentials.DenyAllChecker
	}
	owner := credentials.NewOwnerPolicyChecker(&memberLookupFromQueries{q: queries})
	multica := credentials.PolicyChecker(nil)
	if cerebroQueries != nil {
		multica = &multicaCredentialPolicy{resolver: cerebropermissions.New(cerebroQueries)}
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
