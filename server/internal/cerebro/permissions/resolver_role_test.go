package permissions

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// FIR-2130: roles are a first-class subject. A grant with subject_type=role
// applies to an actor only when the role is in the actor's assigned RoleIDs.

func TestDecide_RoleGrantAppliesWhenActorHasRole(t *testing.T) {
	roleAdmin := uuid(10)
	g := grant(subjectRole, roleAdmin, "issue.read", "*")

	got := Decide([]cerebrodb.CerebroWorkspaceGrant{g}, Request{
		Actor:      Actor{Type: subjectMember, ID: uuid(1), RoleIDs: []pgtype.UUID{roleAdmin}},
		Capability: "issue.read",
	})
	if !got.Allowed() {
		t.Fatalf("role grant should apply when actor holds the role, got %+v", got)
	}
	if got.WinningOverrideLayer != "role" {
		t.Fatalf("winning layer should be role, got %q", got.WinningOverrideLayer)
	}
}

func TestDecide_RoleGrantIgnoredWhenActorLacksRole(t *testing.T) {
	roleAdmin := uuid(10)
	other := uuid(11)
	g := grant(subjectRole, roleAdmin, "issue.read", "*")

	got := Decide([]cerebrodb.CerebroWorkspaceGrant{g}, Request{
		Actor:      Actor{Type: subjectMember, ID: uuid(1), RoleIDs: []pgtype.UUID{other}},
		Capability: "issue.read",
	})
	if got.Kind != DecisionDeny {
		t.Fatalf("role grant must not apply to an actor without the role, got %+v", got)
	}
}

// Override-chain coverage: the most specific matching layer wins
// (workspace < role < group < actor). When grants from several layers all
// match, the winning layer must be the most specific one.
func TestDecide_OverrideChainPicksMostSpecificLayer(t *testing.T) {
	member := uuid(1)
	role := uuid(10)
	group := uuid(20)

	cases := []struct {
		name   string
		grants []cerebrodb.CerebroWorkspaceGrant
		want   string
	}{
		{
			name:   "workspace only",
			grants: []cerebrodb.CerebroWorkspaceGrant{grant(subjectWorkspaceDefault, pgtype.UUID{}, "issue.read", "*")},
			want:   "workspace",
		},
		{
			name: "workspace + role → role wins",
			grants: []cerebrodb.CerebroWorkspaceGrant{
				grant(subjectWorkspaceDefault, pgtype.UUID{}, "issue.read", "*"),
				grant(subjectRole, role, "issue.read", "*"),
			},
			want: "role",
		},
		{
			name: "role + group → group wins",
			grants: []cerebrodb.CerebroWorkspaceGrant{
				grant(subjectRole, role, "issue.read", "*"),
				grant(subjectGroup, group, "issue.read", "*"),
			},
			want: "group",
		},
		{
			name: "group + actor → actor wins",
			grants: []cerebrodb.CerebroWorkspaceGrant{
				grant(subjectGroup, group, "issue.read", "*"),
				grant(subjectMember, member, "issue.read", "*"),
			},
			want: "actor",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.grants, Request{
				Actor: Actor{
					Type:     subjectMember,
					ID:       member,
					GroupIDs: []pgtype.UUID{group},
					RoleIDs:  []pgtype.UUID{role},
				},
				Capability: "issue.read",
			})
			if !got.Allowed() {
				t.Fatalf("expected allow, got %+v", got)
			}
			if got.WinningOverrideLayer != tc.want {
				t.Fatalf("winning layer = %q, want %q", got.WinningOverrideLayer, tc.want)
			}
		})
	}
}
