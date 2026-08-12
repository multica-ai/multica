package admission

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func uid(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("bad uuid %q: %v", s, err)
	}
	return u
}

const (
	owner  = "11111111-1111-1111-1111-111111111111"
	member = "22222222-2222-2222-2222-222222222222"
)

func TestAgentInvocableByMember(t *testing.T) {
	privateAgent := db.Agent{OwnerID: uid(t, owner), PermissionMode: "private"}
	publicAgent := db.Agent{OwnerID: uid(t, owner), PermissionMode: "public_to"}
	wsTarget := []db.AgentInvocationTarget{{TargetType: "workspace"}}
	memberTarget := []db.AgentInvocationTarget{{TargetType: "member", TargetID: uid(t, member)}}

	if !AgentInvocableByMember(privateAgent, nil, owner, true) {
		t.Error("owner must be able to invoke their own private agent")
	}
	if AgentInvocableByMember(privateAgent, nil, owner, false) {
		t.Error("a non-member must invoke nothing, even an agent they own")
	}
	if AgentInvocableByMember(privateAgent, wsTarget, member, true) {
		t.Error("a private agent must not be invocable by a non-owner member")
	}
	if !AgentInvocableByMember(publicAgent, wsTarget, member, true) {
		t.Error("a public_to workspace-target agent is invocable by a current member")
	}
	if AgentInvocableByMember(publicAgent, wsTarget, member, false) {
		t.Error("a workspace target must NOT admit a non-member")
	}
	if !AgentInvocableByMember(publicAgent, memberTarget, member, true) {
		t.Error("a member target admits the matching member")
	}
	if AgentInvocableByMember(publicAgent, memberTarget, "33333333-3333-3333-3333-333333333333", true) {
		t.Error("a member target must not admit a different member")
	}
}

func TestAutopilotWriteByOwnership(t *testing.T) {
	byMember := func(role string) db.Member { return db.Member{Role: role, UserID: uid(t, member)} }
	ownedByMember := db.Autopilot{CreatedByType: "member", CreatedByID: uid(t, member)}
	ownedByOther := db.Autopilot{CreatedByType: "member", CreatedByID: uid(t, owner)}

	if !AutopilotWriteByOwnership(ownedByOther, byMember("admin")) {
		t.Error("admin may write any autopilot")
	}
	if !AutopilotWriteByOwnership(ownedByMember, byMember("member")) {
		t.Error("the creating member may write their own autopilot")
	}
	if AutopilotWriteByOwnership(ownedByOther, byMember("member")) {
		t.Error("a plain member may not write another member's autopilot")
	}
}
