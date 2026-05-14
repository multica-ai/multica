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
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	persona "github.com/hvejsel/firtal-persona/sdk/go"
	"github.com/multica-ai/multica/server/internal/cerebro/credentials"
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

// newCredentialsPolicy builds the production chain. queries is the
// upstream db handle (needed for the owner check). Returns DenyAllChecker
// when queries is nil, which shouldn't happen in production but keeps
// startup safe for tests / partial wiring.
//
// Persona is optional: when MULTICA_PERSONA_URL / MULTICA_PERSONA_TOKEN
// are unset, only the owner check fires. This matches the issue's
// deny-by-default behaviour — non-owners get nothing until persona is
// configured and grants are issued.
func newCredentialsPolicy(queries *db.Queries) credentials.PolicyChecker {
	if queries == nil {
		return credentials.DenyAllChecker
	}
	owner := credentials.NewOwnerPolicyChecker(&memberLookupFromQueries{q: queries})

	url := strings.TrimSpace(os.Getenv("MULTICA_PERSONA_URL"))
	token := strings.TrimSpace(os.Getenv("MULTICA_PERSONA_TOKEN"))
	if url == "" || token == "" {
		// No persona configured. Owner-only chain — non-owners always
		// deny, which is the deny-by-default position from JEH-1197.
		return credentials.NewChainPolicyChecker(owner)
	}
	client, err := persona.New(persona.Config{Endpoint: url, Token: token})
	if err != nil {
		slog.Default().Warn("credentials policy: persona client init failed, falling back to owner-only",
			"err", err)
		return credentials.NewChainPolicyChecker(owner)
	}
	personaChk := credentials.NewPersonaPolicyChecker(&personaBackend{client: client, log: slog.Default()})
	return credentials.NewChainPolicyChecker(owner, personaChk)
}
