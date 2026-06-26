package agentvault

import (
	"context"
	"fmt"
	"strings"
)

// FIR-1739 Part B — the credential-grant → Agent Vault access connector.
//
// The unified tool-policy chain is the SINGLE place a credential permission is
// authored (docs/agents/permission-architecture.md §5.4): a `credential.reveal`
// Allow row on `cerebro-credential:<uuid>` says "this agent may use this
// secret". cerebro_agentvault_agent_access is NOT a second permission model —
// it is storage + runtime brokering (which box an agent's claim-time token may
// reach). This connector keeps the two aligned by PROJECTING the authoritative
// chain grants into the access table: the table becomes a derived view of the
// chain, never an independent authority.
//
// Deny-by-default + least-privilege: the projection is a full per-agent
// reconcile — every box the chain grants is written, every box it no longer
// grants is removed. An agent with no chain grants ends with no access rows, so
// the broker mints no token for it. The whole path is dormant until BOTH
// cerebro_agent_vault (broker, checked at claim) AND cerebro_credential_chain_grant
// (chain is an Allow-source, checked by the grant source) are on.
//
// Deliberately NOT wired: the parallel cerebro_credential_policy store
// (credentialpolicy package / agent_credential_grant_cerebro.go). That is the
// documented FIR-1739 landmine (§5.4) — a second authoring model for the same
// thing. This connector sources only from the tool-policy chain.

// BoxGrant is one Agent Vault box an agent is explicitly granted via the
// tool-policy chain, already resolved from a credential UUID to its box name,
// with the role to mirror it at.
type BoxGrant struct {
	// Vault is the Agent Vault box name (= cerebro_credential.name; one box =
	// one credential, keyed on the box name — migration 9096 comment).
	Vault string
	// Role is the per-vault Agent Vault role to write (read-only/member/admin).
	Role string
}

// VaultResourcePrefix is the tool-policy ResourcePattern prefix that names an
// Agent Vault vault DIRECTLY (FIR-1739 v1, vault-level): a `credential.reveal`
// Allow on `agentvault-vault:<vault>` grants the agent that whole vault. This is
// the authoring path that replaces the standalone access box — the vault is
// picked in the Permissions row itself, sourced live from Agent Vault. Unlike
// `cerebro-credential:<uuid>` it needs no cerebro_credential registry row (and
// no MULTICA_CREDENTIALS_KEY), because the secret already lives in Agent Vault.
//
// Per-KEY selection within a vault is a tracked follow-up (FIR-2108): Agent
// Vault scopes per vault today, so vault-level is the only granularity that can
// be truly enforced now.
const VaultResourcePrefix = "agentvault-vault:"

// VaultFromResourcePattern returns the vault name when pattern names an Agent
// Vault vault directly (`agentvault-vault:<vault>`), and ok=false otherwise.
// Pure and trims surrounding space so a blank vault never projects.
func VaultFromResourcePattern(pattern string) (vault string, ok bool) {
	if !strings.HasPrefix(pattern, VaultResourcePrefix) {
		return "", false
	}
	v := strings.TrimSpace(strings.TrimPrefix(pattern, VaultResourcePrefix))
	if v == "" {
		return "", false
	}
	return v, true
}

// CredentialGrantSource yields the credential boxes an agent is explicitly
// granted through the unified tool-policy chain. It returns an empty slice (not
// an error) when cerebro_credential_chain_grant is OFF for the workspace, so the
// reconcile projects "no boxes" — deny-by-default. Errors are returned so the
// caller fails the claim closed (spawns without broker env) rather than
// silently projecting a stale or partial grant set.
type CredentialGrantSource interface {
	GrantedBoxes(ctx context.Context, workspaceID, agentID string) ([]BoxGrant, error)
}

// accessReconciler is the slice of *Store the mirror needs: read the current
// projection, then add/update and delete to match the desired set.
type accessReconciler interface {
	ListForAgent(ctx context.Context, workspaceID, agentID string) ([]Access, error)
	SetAccess(ctx context.Context, workspaceID, agentID, vault, role string) error
	DeleteAccess(ctx context.Context, workspaceID, agentID, vault string) error
}

// Compile-time guard: the real Store satisfies the reconcile surface.
var _ accessReconciler = (*Store)(nil)

// reconcileAgentAccess projects the authoritative chain grants for one agent
// onto the per-agent access table. It SetAccess for every granted box (creating
// or correcting the role) and DeleteAccess for every box the agent currently
// holds that the chain no longer grants. The operation is scoped to a single
// agent, so it never touches another agent's rows.
//
// On any grant-source error the table is left UNTOUCHED and the error is
// returned — better a stale projection (the prior, already-admitted state) than
// a half-applied one that could drop a live grant mid-reconcile.
func reconcileAgentAccess(ctx context.Context, store accessReconciler, src CredentialGrantSource, workspaceID, agentID string) error {
	desired, err := src.GrantedBoxes(ctx, workspaceID, agentID)
	if err != nil {
		return fmt.Errorf("agentvault mirror: grant source: %w", err)
	}

	// Desired set keyed by box name → role. Last role wins on duplicate boxes
	// (multiple credential rows cannot share a name — unique (ws,type,name) —
	// but a box could be granted at two scopes; the table is unique per box).
	want := make(map[string]string, len(desired))
	for _, g := range desired {
		v := strings.TrimSpace(g.Vault)
		if v == "" {
			continue
		}
		role := strings.TrimSpace(g.Role)
		if role == "" {
			role = "read-only"
		}
		want[v] = role
	}

	current, err := store.ListForAgent(ctx, workspaceID, agentID)
	if err != nil {
		return fmt.Errorf("agentvault mirror: list current access: %w", err)
	}
	have := make(map[string]string, len(current))
	for _, a := range current {
		have[a.Vault] = a.Role
	}

	// Add or correct every desired box.
	for vault, role := range want {
		if existing, ok := have[vault]; ok && existing == role {
			continue // already correct — no write
		}
		if err := store.SetAccess(ctx, workspaceID, agentID, vault, role); err != nil {
			return fmt.Errorf("agentvault mirror: set access %q: %w", vault, err)
		}
	}

	// Remove every box the chain no longer grants.
	for vault := range have {
		if _, ok := want[vault]; ok {
			continue
		}
		if err := store.DeleteAccess(ctx, workspaceID, agentID, vault); err != nil {
			return fmt.Errorf("agentvault mirror: delete access %q: %w", vault, err)
		}
	}
	return nil
}
