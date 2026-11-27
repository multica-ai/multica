package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The delivery gate's private-runtime boundary matrix for smart agents: the
// agent template owner's own machine and the originator's machine deliver,
// shared-with still delivers, and everything else is the runtime-access
// denial the handler settles before daemon delivery. Non-private runtimes
// carry no owner boundary at this gate.
func TestSmartRuntimeDeliveryRequiresOriginatorOwnership(t *testing.T) {
	ctx := context.Background()
	pool := sharedTestPool(t)
	queries := db.New(pool)
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
		{name: "template owner's private machine", owner: otherMemberID, visibility: "private", allow: true},
		{name: "unrelated member's private machine", owner: util.MustParseUUID("00000000-0000-0000-0000-000000000006"), visibility: "private"},
		{name: "workspace machine owned by another member", owner: otherMemberID, visibility: "workspace", allow: true},
		{name: "ownerless workspace machine", visibility: "workspace", allow: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := db.AgentRuntime{
				ID: runtimeID, WorkspaceID: workspaceID, OwnerID: tc.owner, Visibility: tc.visibility,
				SharedWith: []pgtype.UUID{originatorID},
			}
			got, err := PrivateRuntimePermitsAgent(ctx, queries, agent, task, runtime)
			if err != nil {
				t.Fatalf("permit check: %v", err)
			}
			if got != tc.allow {
				t.Fatalf("delivery allowed = %v, want %v", got, tc.allow)
			}
		})
	}
}
