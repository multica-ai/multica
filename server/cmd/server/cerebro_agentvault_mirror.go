// CEREBRO-PATCH(cerebro-agentvault-mirror-file): FIR-1739 Part B — the
// credential-grant → Agent Vault access connector's grant SOURCE.
//
// Lives in a cerebro-prefixed cmd/server file so the router's wiring stays a
// single marked line. This is the half that reads the AUTHORITATIVE model — the
// unified tool-policy chain — and resolves each granted credential UUID to its
// Agent Vault box name. The projection itself (writing the access table) lives
// in the agentvault package (mirror.go); this file only supplies the desired
// set. See docs/agents/permission-architecture.md §5.4.
//
// Deliberately NOT sourced from cerebro_credential_policy / the credentialpolicy
// package (handler agent_credential_grant_cerebro.go). That parallel authoring
// store is the documented FIR-1739 landmine (§5.4) — wiring it into enforcement
// is exactly the mistake this connector avoids. The only source here is the
// tool-policy chain.
package main

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/agentvault"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	// credentialBrokerAction is the tool-policy action whose explicit Allow on a
	// specific credential means "this agent may USE this secret". The broker is a
	// mediated reveal (the agent's traffic gets the value swapped in; it never
	// holds plaintext), so the reveal grant is the conservative, least-privilege
	// signal: only an agent already trusted to obtain the value gets a broker box.
	// A dedicated credential.use action is a possible future refinement; until it
	// exists, reveal is the authoritative signal and keys this projection.
	credentialBrokerAction = "credential.reveal"
	// credentialIDResourcePrefix is the id-scope resource pattern minted by
	// multicaCredentialPolicy.Check. Only id-scope grants map to a single box;
	// type-scope grants (cerebro-credential-type:<type>) name no single box and
	// are intentionally skipped (least-privilege — no fan-out to every box of a
	// type from a broker projection).
	credentialIDResourcePrefix = "cerebro-credential:"
)

// The chain-is-an-Allow-source gate (default OFF) is the same flag the credential
// Check keystone reads — flagCredentialChainGrant in cerebro_credentials_policy.go.
// While OFF this projection yields no boxes, so the mirror reconciles to
// deny-by-default. Reusing the one constant keeps the two gates from drifting.

// credentialGrantSubjectLister is the slice of the tool-policy Store the grant
// source needs: an agent's explicit settings at the agent layer.
type credentialGrantSubjectLister interface {
	ListForSubject(ctx context.Context, workspaceID pgtype.UUID, layer toolpolicy.Layer, subjectID pgtype.UUID) ([]toolpolicy.SubjectSetting, error)
}

// credentialNamer resolves a credential UUID to its Agent Vault box name and the
// per-actor feature-flag reader. *cerebrodb.Queries satisfies it.
type credentialNamer interface {
	GetCerebroCredential(ctx context.Context, id pgtype.UUID) (cerebrodb.CerebroCredential, error)
	GetCerebroFeatureFlag(ctx context.Context, arg cerebrodb.GetCerebroFeatureFlagParams) (bool, error)
}

// chainCredentialGrantSource implements agentvault.CredentialGrantSource by
// reading the unified tool-policy chain (agent layer) and mapping each explicit
// credential.reveal Allow on a specific credential to that credential's Agent
// Vault box name. Returns no boxes (not an error) when cerebro_credential_chain_grant
// is OFF — deny-by-default.
//
// v1 scope: only the agent's OWN-layer explicit Allow rows are mirrored. A
// credential granted via the group/workspace/runtime layers is NOT yet
// projected — that under-grants (the safe direction) rather than over-grants,
// and the per-agent tick is the primary authoring path for broker access.
// Folding the full chain resolution in is a tracked follow-up.
type chainCredentialGrantSource struct {
	policy credentialGrantSubjectLister
	creds  credentialNamer
}

// Compile-time guard: this satisfies the connector's source interface.
var _ agentvault.CredentialGrantSource = (*chainCredentialGrantSource)(nil)

func (s *chainCredentialGrantSource) GrantedBoxes(ctx context.Context, workspaceID, agentID string) ([]agentvault.BoxGrant, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, err
	}
	agID, err := util.ParseUUID(agentID)
	if err != nil {
		return nil, err
	}

	// Gate: the chain is only an Allow-source for credentials when the flag is on.
	// Off (or any lookup error) ⇒ no boxes ⇒ deny-by-default. Fail CLOSED.
	enabled, err := s.creds.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
		WorkspaceID: wsID,
		UserID:      pgtype.UUID{Valid: true}, // all-zero sentinel = workspace-level row
		FlagKey:     flagCredentialChainGrant,
	})
	if err != nil || !enabled {
		return nil, nil
	}

	settings, err := s.policy.ListForSubject(ctx, wsID, toolpolicy.LayerAgent, agID)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	var out []agentvault.BoxGrant
	for _, set := range settings {
		if set.ToolKey != credentialBrokerAction || set.Setting != toolpolicy.SettingAllow {
			continue
		}
		// FIR-1739 v1 (vault-level): the resource may name an Agent Vault vault
		// DIRECTLY (`agentvault-vault:<vault>`) — the authoring path picked in the
		// Permissions row, no cerebro_credential registry row needed. Projected
		// straight through; per-key within a vault is the FIR-2108 follow-up.
		if vault, isVault := agentvault.VaultFromResourcePattern(set.ResourcePattern); isVault {
			key := agentvault.VaultResourcePrefix + vault
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, agentvault.BoxGrant{Vault: vault, Role: "read-only"})
			continue
		}
		if !strings.HasPrefix(set.ResourcePattern, credentialIDResourcePrefix) {
			continue // type-scope or non-credential resource — not a single box
		}
		credID := strings.TrimPrefix(set.ResourcePattern, credentialIDResourcePrefix)
		if _, dup := seen[credID]; dup {
			continue
		}
		seen[credID] = struct{}{}

		box, ok, err := s.boxName(ctx, credID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue // credential deleted/renamed away — nothing to project
		}
		// read-only: the broker only needs to inject the value, never rotate or
		// administer the box. Least-privilege default.
		out = append(out, agentvault.BoxGrant{Vault: box, Role: "read-only"})
	}
	return out, nil
}

// boxName resolves a credential UUID to its Agent Vault box name (= the
// credential's name; one box = one credential — migration 9096). ok=false when
// the credential no longer exists, so a stale grant projects nothing rather than
// erroring the whole reconcile.
func (s *chainCredentialGrantSource) boxName(ctx context.Context, credID string) (string, bool, error) {
	id, err := util.ParseUUID(credID)
	if err != nil {
		return "", false, nil // malformed resource pattern — skip, don't fail
	}
	cred, err := s.creds.GetCerebroCredential(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	name := strings.TrimSpace(cred.Name)
	if name == "" {
		return "", false, nil
	}
	return name, true, nil
}
