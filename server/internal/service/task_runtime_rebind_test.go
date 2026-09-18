package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

type runtimeRebindTxStarter struct {
	pool        *pgxpool.Pool
	beforeBegin func()
}

func (s *runtimeRebindTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	if s.beforeBegin != nil {
		f := s.beforeBegin
		s.beforeBegin = nil
		f()
	}
	return s.pool.Begin(ctx)
}

func TestDirectChatRuntimeSelectionUsesLockedCarrier(t *testing.T) {
	fx, user := newPrincipalFixture(t)
	ctx := context.Background()
	runtimeA := fx.Runtime(t, "before rebind", nil)
	runtimeB := fx.Runtime(t, "after rebind", nil)
	agentID := fx.Agent(t, "chat carrier", runtimeA, nil)
	sessionID := fx.Insert(t, "chat_session", testutil.Cols{"workspace_id": fx.WorkspaceID, "agent_id": agentID, "creator_id": user})
	session, err := fx.q.GetChatSession(ctx, util.MustParseUUID(sessionID))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := fx.q.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewTaskService(fx.q, &runtimeRebindTxStarter{pool: sharedTestPool(t), beforeBegin: func() {
		if _, err := sharedTestPool(t).Exec(ctx, `UPDATE agent SET runtime_id=$2 WHERE id=$1`, agentID, runtimeB); err != nil {
			t.Fatal(err)
		}
	}}, nil, events.New())
	result, err := svc.SendDirectChatMessage(ctx, session, agent, util.MustParseUUID(user), "continue after rebind", nil, "member", util.MustParseUUID(user))
	if err != nil {
		t.Fatal(err)
	}
	if result.Task.RuntimeID != util.MustParseUUID(runtimeB) {
		t.Fatalf("stale runtime after rebind: got %s want %s", util.UUIDToString(result.Task.RuntimeID), runtimeB)
	}
	if _, err := fx.q.GetAgentTask(ctx, result.Task.ID); err != nil {
		t.Fatal(err)
	}
	// The snapshot is captured only after the carrier is reloaded under its lock.
	routing, err := ParseRuntimeRouting(result.Task.RuntimeRouting)
	if err != nil || routing == nil {
		t.Fatalf("snapshot: %v %v", routing, err)
	}
	route, err := routing.Route(agentID)
	if err != nil || route.RuntimeID != runtimeB {
		t.Fatalf("stale snapshot: %+v %v", route, err)
	}
}
