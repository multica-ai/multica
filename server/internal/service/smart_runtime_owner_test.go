package service

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestSmartRuntimeDeliveryRequiresOriginatorOwnership(t *testing.T) {
	workspaceID := util.MustParseUUID("00000000-0000-0000-0000-000000000001")
	originatorID := util.MustParseUUID("00000000-0000-0000-0000-000000000002")
	otherMemberID := util.MustParseUUID("00000000-0000-0000-0000-000000000003")
	runtimeID := util.MustParseUUID("00000000-0000-0000-0000-000000000004")
	agentID := util.MustParseUUID("00000000-0000-0000-0000-000000000005")
	agent := db.Agent{ID: agentID, WorkspaceID: workspaceID, OwnerID: otherMemberID}
	task := db.AgentTaskQueue{AgentID: agentID, RuntimeID: runtimeID, OriginatorUserID: originatorID}
	for _, tc := range []struct {
		name       string
		owner      pgtype.UUID
		visibility string
		allow      bool
	}{
		{name: "originator's private machine", owner: originatorID, visibility: "private", allow: true},
		{name: "template owner's private machine", owner: otherMemberID, visibility: "private"},
		{name: "workspace machine owned by another member", owner: otherMemberID, visibility: "workspace"},
		{name: "ownerless workspace machine", visibility: "workspace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := db.AgentRuntime{
				ID: runtimeID, WorkspaceID: workspaceID, OwnerID: tc.owner, Visibility: tc.visibility,
				SharedWith: []pgtype.UUID{originatorID},
			}
			if got := PrivateRuntimePermitsAgent(agent, task, runtime); got != tc.allow {
				t.Fatalf("delivery allowed = %v, want %v", got, tc.allow)
			}
		})
	}
}
