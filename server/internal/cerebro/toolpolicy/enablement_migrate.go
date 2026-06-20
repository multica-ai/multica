// Enablement fail-safe — "flyt adgang → eksplicitte Ja-rækker" (FIR-1609 Phase 6).
//
// enablement_audit.go answers the go/no-go question ("would flipping a gate to
// the chain lock anyone out?"). This file is the fix-up half: the "fejl-luk hvis
// ikke 1:1" migration the milestone calls for. Before a workspace flips a gate
// (repo_grants_enabled, cerebro_local_tool_policy, credentials, cerebro_policy_cel)
// from a legacy enforcing path to the chain, it pins today's access as EXPLICIT
// workspace-root Allow rows so the flip can never silently drop a grant.
//
// Why the workspace root layer: it is the base of the chain (chain.go) — it sets
// the starting point and never "caps", so an Allow row there is the most-open,
// least-surprising way to make an implicit Base=Allow durable and visible. It
// survives a future Base flip and an admin removing the legacy path, which is the
// whole point of the fail-safe.
//
// What it deliberately CANNOT do: loosen an intentional higher-layer Deny/Ask.
// Resolution is most-restrictive-wins (Resolve in chain.go), so a workspace-root
// Allow is capped by any Agent/User/Group/System Deny above it. Such a grant is
// surfaced in Blocked, never silently enabled — an intentional deny must be
// resolved by a human, not papered over by the migration.
package toolpolicy

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// AllowRowWriter is the narrow write surface the migration needs: upsert one
// explicit policy row. *Store satisfies it via Set. The interface keeps the
// migration unit-testable without a database, mirroring the Resolver seam the
// audit uses.
type AllowRowWriter interface {
	Set(ctx context.Context, p SetParams) (cerebrodb.UpsertCerebroToolPolicyRow, error)
}

// MigrationResult reports the outcome of the fail-safe run. Pinned and Blocked
// are disjoint: every at-risk grant lands in exactly one of them.
type MigrationResult struct {
	// Pinned is every at-risk legacy grant whose workspace-root Allow row CLEARED
	// the risk — the grant resolved to Allow on the re-audit. Today's access is
	// now explicit and durable for these.
	Pinned []LegacyGrant
	// Blocked is every grant still not resolving to Allow AFTER pinning — an
	// intentional higher-layer Deny/Ask the workspace root cannot override
	// (most-restrictive-wins). Non-empty Blocked is a hard no-go: enablement must
	// not proceed until a human resolves each one.
	Blocked []LockoutRisk
}

// Ready reports whether the migration cleared every lock-out risk — nothing is
// left Blocked. True is the go-signal to flip the gate. An empty grant set is
// trivially Ready.
func (r MigrationResult) Ready() bool { return len(r.Blocked) == 0 }

// MigrateAccessToAllowRows is the fail-safe ("fejl-luk hvis ikke 1:1") run before
// a workspace flips a gate to the chain.
//
// Step 1 (audit): resolve every legacy grant exactly as the live gate would
// (Base=Allow, action verb derived) via AuditEnablement. A grant that already
// resolves to Allow needs nothing — Base=Allow already preserves it.
//
// Step 2 (pin): for every at-risk grant, upsert an explicit workspace-root Allow
// row for that exact (tool_key, resource). This is idempotent (Set upserts on
// the row's natural key) and fail-closed (a write error aborts and is returned).
//
// Step 3 (re-audit): resolve the at-risk grants again now that the floor is
// pinned. Whatever still is not Allow is blocked by an intentional Deny/Ask above
// the workspace root and is returned in Blocked — never silently enabled.
//
// updatedBy is recorded on the written rows (the zero value leaves the column
// NULL). A nil writer or resolver is treated as "cannot prove safety": every
// grant is reported Blocked rather than assumed safe.
func MigrateAccessToAllowRows(ctx context.Context, w AllowRowWriter, resolver Resolver, grants []LegacyGrant, updatedBy pgtype.UUID) (MigrationResult, error) {
	if len(grants) == 0 {
		return MigrationResult{}, nil
	}
	if w == nil || resolver == nil {
		// Cannot write or cannot prove — fail closed: report everything as a
		// blocker so no caller mistakes "did nothing" for "safe to enable".
		risks, err := AuditEnablement(ctx, resolver, grants)
		if err != nil {
			return MigrationResult{}, err
		}
		if resolver == nil {
			// AuditEnablement returns no risks for a nil resolver; synthesize the
			// fail-closed set so Ready() is false.
			risks = risksFromGrants(grants, "fail-closed: no resolver to prove access is preserved")
		}
		return MigrationResult{Blocked: risks}, nil
	}

	// Step 1 — find the at-risk grants (the chain does not already Allow them).
	risks, err := AuditEnablement(ctx, resolver, grants)
	if err != nil {
		return MigrationResult{}, err
	}
	if len(risks) == 0 {
		return MigrationResult{}, nil // 1:1 already — nothing to pin.
	}

	// Step 2 — pin each at-risk grant as an explicit workspace-root Allow row.
	touched := make([]LegacyGrant, 0, len(risks))
	for _, r := range risks {
		g := r.Grant
		if _, err := w.Set(ctx, SetParams{
			WorkspaceID:     g.Query.WorkspaceID,
			ToolKey:         g.ToolKey,
			Layer:           LayerWorkspace,
			SubjectID:       g.Query.WorkspaceID, // workspace layer is keyed on the workspace itself
			ResourcePattern: g.Resource,
			Setting:         SettingAllow,
			UpdatedBy:       updatedBy,
		}); err != nil {
			return MigrationResult{}, err // fail closed — never swallow a write error
		}
		touched = append(touched, g)
	}

	// Step 3 — re-audit only the grants we touched; the rest were already Allow.
	blocked, err := AuditEnablement(ctx, resolver, touched)
	if err != nil {
		return MigrationResult{}, err
	}

	// Partition the touched grants: a pin that left the grant blocked did NOT
	// clear it (an intentional higher-layer deny), so it belongs only in Blocked.
	stillBlocked := make(map[string]bool, len(blocked))
	for _, b := range blocked {
		stillBlocked[grantKey(b.Grant)] = true
	}
	var pinned []LegacyGrant
	for _, g := range touched {
		if !stillBlocked[grantKey(g)] {
			pinned = append(pinned, g)
		}
	}
	return MigrationResult{Pinned: pinned, Blocked: blocked}, nil
}

// grantKey identifies a grant for set membership. A workspace can carry the same
// (tool_key, resource) for different subjects, so the subject ids are part of the
// key; two grants that resolve through the same layers collide intentionally.
func grantKey(g LegacyGrant) string {
	q := g.Query
	return g.ToolKey + "\x00" + g.Resource + "\x00" +
		uuidKey(q.WorkspaceID) + "\x00" + uuidKey(q.RuntimeID) + "\x00" +
		uuidKey(q.AgentID) + "\x00" + uuidKey(q.UserID) + "\x00" + uuidKey(q.SystemID)
}

// risksFromGrants turns a grant list into fail-closed risks with a shared reason.
// Used only on the nil-resolver path, where we cannot prove any grant is safe.
func risksFromGrants(grants []LegacyGrant, reason string) []LockoutRisk {
	risks := make([]LockoutRisk, 0, len(grants))
	for _, g := range grants {
		risks = append(risks, LockoutRisk{
			Grant:   g,
			Verdict: Effective{Setting: SettingDeny},
			Reason:  reason,
		})
	}
	return risks
}
