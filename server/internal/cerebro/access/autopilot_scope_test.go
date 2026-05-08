package access

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func uuid(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[0] = b
	return u
}

func TestValidateScope(t *testing.T) {
	owner := uuid(1)
	group := uuid(2)
	none := pgtype.UUID{}

	cases := []struct {
		name        string
		scope       string
		ownerUserID pgtype.UUID
		groupID     pgtype.UUID
		want        error
	}{
		{"workspace ok", ScopeWorkspace, none, none, nil},
		{"workspace + owner rejected", ScopeWorkspace, owner, none, ErrWorkspaceExtraID},
		{"workspace + group rejected", ScopeWorkspace, none, group, ErrWorkspaceExtraID},
		{"personal ok", ScopePersonal, owner, none, nil},
		{"personal missing owner", ScopePersonal, none, none, ErrPersonalNeedsOwner},
		{"personal + group rejected", ScopePersonal, owner, group, ErrPersonalExtraGroup},
		{"group ok", ScopeGroup, none, group, nil},
		{"group missing group_id", ScopeGroup, none, none, ErrGroupNeedsGroupID},
		{"group + owner rejected", ScopeGroup, owner, group, ErrGroupExtraOwner},
		{"unknown scope", "personnel", none, none, ErrInvalidScope},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateScope(tc.scope, tc.ownerUserID, tc.groupID)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("expected nil, got %v", got)
				}
				return
			}
			if !errors.Is(got, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestCanSee(t *testing.T) {
	owner := uuid(1)
	other := uuid(2)
	group := uuid(3)

	personal := AutopilotView{Scope: ScopePersonal, OwnerUserID: owner}
	groupAuto := AutopilotView{Scope: ScopeGroup, GroupID: group}
	wsAuto := AutopilotView{Scope: ScopeWorkspace}

	if !CanSee(wsAuto, Viewer{UserID: other}) {
		t.Fatal("workspace scope: any user must see")
	}
	if !CanSee(personal, Viewer{UserID: owner}) {
		t.Fatal("personal: owner must see")
	}
	if CanSee(personal, Viewer{UserID: other}) {
		t.Fatal("personal: non-owner must NOT see")
	}
	if CanSee(personal, Viewer{UserID: other, IsAdmin: true}) {
		t.Fatal("personal: admin must NOT see (privacy)")
	}
	if !CanSee(groupAuto, Viewer{UserID: other, GroupIDs: []pgtype.UUID{group}}) {
		t.Fatal("group: member must see")
	}
	if CanSee(groupAuto, Viewer{UserID: other}) {
		t.Fatal("group: non-member must NOT see")
	}
	if !CanSee(groupAuto, Viewer{UserID: other, IsAdmin: true}) {
		t.Fatal("group: workspace admin must see")
	}
	// Empty/legacy scope — backfilled rows from before the migration.
	if !CanSee(AutopilotView{}, Viewer{UserID: other}) {
		t.Fatal("legacy empty scope: any user must see")
	}
}

func TestCanEdit(t *testing.T) {
	owner := uuid(1)
	other := uuid(2)
	group := uuid(3)

	wsAuto := AutopilotView{Scope: ScopeWorkspace}
	personal := AutopilotView{Scope: ScopePersonal, OwnerUserID: owner}
	groupAuto := AutopilotView{Scope: ScopeGroup, GroupID: group}

	if CanEdit(wsAuto, Viewer{UserID: other}) {
		t.Fatal("workspace: regular member must NOT edit")
	}
	if !CanEdit(wsAuto, Viewer{UserID: other, IsAdmin: true}) {
		t.Fatal("workspace: admin must edit")
	}
	if !CanEdit(personal, Viewer{UserID: owner}) {
		t.Fatal("personal: owner must edit")
	}
	if CanEdit(personal, Viewer{UserID: other, IsAdmin: true}) {
		t.Fatal("personal: admin must NOT edit (privacy)")
	}
	if !CanEdit(groupAuto, Viewer{UserID: other, GroupIDs: []pgtype.UUID{group}}) {
		t.Fatal("group: member must edit")
	}
	if !CanEdit(groupAuto, Viewer{UserID: other, IsAdmin: true}) {
		t.Fatal("group: admin must edit")
	}
}

func TestCanTrigger(t *testing.T) {
	owner := uuid(1)
	other := uuid(2)
	group := uuid(3)

	wsAuto := AutopilotView{Scope: ScopeWorkspace}
	personal := AutopilotView{Scope: ScopePersonal, OwnerUserID: owner}
	groupAuto := AutopilotView{Scope: ScopeGroup, GroupID: group}

	if !CanTrigger(wsAuto, Viewer{UserID: other}) {
		t.Fatal("workspace: any member must trigger")
	}
	if CanTrigger(personal, Viewer{UserID: other}) {
		t.Fatal("personal: non-owner must NOT trigger")
	}
	if !CanTrigger(personal, Viewer{UserID: owner}) {
		t.Fatal("personal: owner must trigger")
	}
	if !CanTrigger(groupAuto, Viewer{UserID: other, GroupIDs: []pgtype.UUID{group}}) {
		t.Fatal("group: member must trigger")
	}
	if CanTrigger(groupAuto, Viewer{UserID: other}) {
		t.Fatal("group: non-member must NOT trigger")
	}
}
