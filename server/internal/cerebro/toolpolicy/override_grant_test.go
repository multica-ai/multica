// CEREBRO-PATCH(delegated-override-grant): FIR-2351 tests for the pure
// actor/target authorization rule behind the two delegated override
// capabilities.
package toolpolicy

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func mustOverrideTestUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("scan uuid %q: %v", s, err)
	}
	return u
}

func TestCanAuthorDelegatedOverride_NeverSelf(t *testing.T) {
	actor := mustOverrideTestUUID(t, "11111111-1111-1111-1111-111111111111")
	// Even workspace-scope, even group-scope, even a shared group: self is
	// always rejected (Jesper, FIR-2351 — "en bruger skal ikke kunne
	// override sin egen adgang").
	if CanAuthorDelegatedOverride(actor, actor, true, true, []pgtype.UUID{actor}, []pgtype.UUID{actor}) {
		t.Fatalf("actor must never be able to override their own access")
	}
}

func TestCanAuthorDelegatedOverride_WorkspaceScopeReachesAnyone(t *testing.T) {
	actor := mustOverrideTestUUID(t, "11111111-1111-1111-1111-111111111111")
	target := mustOverrideTestUUID(t, "22222222-2222-2222-2222-222222222222")
	if !CanAuthorDelegatedOverride(actor, target, true, false, nil, nil) {
		t.Fatalf("workspace scope must reach any other user, even with no shared group")
	}
}

func TestCanAuthorDelegatedOverride_GroupScopeRequiresSharedGroup(t *testing.T) {
	actor := mustOverrideTestUUID(t, "11111111-1111-1111-1111-111111111111")
	target := mustOverrideTestUUID(t, "22222222-2222-2222-2222-222222222222")
	groupA := mustOverrideTestUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	groupB := mustOverrideTestUUID(t, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	if CanAuthorDelegatedOverride(actor, target, false, true, []pgtype.UUID{groupA}, []pgtype.UUID{groupB}) {
		t.Fatalf("group scope must not reach a target outside every group the actor is in")
	}
	if !CanAuthorDelegatedOverride(actor, target, false, true, []pgtype.UUID{groupA}, []pgtype.UUID{groupA}) {
		t.Fatalf("group scope must reach a target sharing a group with the actor")
	}
}

func TestCanAuthorDelegatedOverride_NoScopeNoAccess(t *testing.T) {
	actor := mustOverrideTestUUID(t, "11111111-1111-1111-1111-111111111111")
	target := mustOverrideTestUUID(t, "22222222-2222-2222-2222-222222222222")
	if CanAuthorDelegatedOverride(actor, target, false, false, nil, nil) {
		t.Fatalf("no delegated capability must never grant override authority")
	}
}

func TestCanAuthorDelegatedOverride_InvalidTargetRejected(t *testing.T) {
	actor := mustOverrideTestUUID(t, "11111111-1111-1111-1111-111111111111")
	if CanAuthorDelegatedOverride(actor, pgtype.UUID{}, true, true, nil, nil) {
		t.Fatalf("an unresolved/invalid target must never be authorized")
	}
}
