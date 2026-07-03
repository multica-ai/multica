// CEREBRO-PATCH(delegated-override-grant): FIR-2351 two new delegated
// override capabilities + the pure actor/target authorization rule behind
// them. See server/internal/handler/group_permissions_cerebro.go
// (cerebroRequireDelegatedOverridePolicy) for the write-path gate that uses
// this.
package toolpolicy

import "github.com/jackc/pgx/v5/pgtype"

// Capability tool-keys for the two delegated override permissions (FIR-2351,
// product decision in the issue thread 2026-07-03). Each is an ordinary
// tool_key resolved through the existing chain via Resolve (Base=Allow,
// tighten-only) — exactly like manage_credential_access (FIR-1479) and
// manage_connections (TECH-3513): granting one is just authoring an Allow row
// at Workspace/Group/User layer for this tool_key, no new storage.
const (
	// CapabilityManageGroupOverrides lets the holder author a User-layer
	// tool-policy row — general tool-policy OR a connection grant, both go
	// through the same /tool-policy write — for another member of a group
	// the holder themselves belongs to. It can never be used on the
	// holder's own row.
	CapabilityManageGroupOverrides = "manage_group_overrides"
	// CapabilityManageWorkspaceOverrides is the workspace-wide twin: the
	// holder may author a User-layer row for ANY other member in the
	// workspace, still never their own.
	CapabilityManageWorkspaceOverrides = "manage_workspace_overrides"
)

// CanAuthorDelegatedOverride reports whether actorID may author a User-layer
// tool-policy row that governs targetID's access, given which of the two
// delegated capabilities above the actor holds.
//
// Pure and I/O-free: callers resolve the capability grants (Resolve) and
// group memberships (GroupPermissions.ResolveGroupIDs) first and pass the
// results in. The rule, straight from the product decision on FIR-2351: a
// user may NEVER use a delegated override on their OWN access — the
// capability only ever reaches someone else's row, regardless of scope.
// hasWorkspaceScope reaches any other user in the workspace; hasGroupScope
// reaches only a user who shares at least one group with the actor.
func CanAuthorDelegatedOverride(actorID, targetID pgtype.UUID, hasWorkspaceScope, hasGroupScope bool, actorGroupIDs, targetGroupIDs []pgtype.UUID) bool {
	if !targetID.Valid || actorID == targetID {
		return false
	}
	if hasWorkspaceScope {
		return true
	}
	if !hasGroupScope {
		return false
	}
	for _, a := range actorGroupIDs {
		if !a.Valid {
			continue
		}
		for _, t := range targetGroupIDs {
			if a == t {
				return true
			}
		}
	}
	return false
}
