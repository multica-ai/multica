package service

import (
	"context"
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRetryFailedChatTaskClonesLatestFailedTurnOnce(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var runtimeID, sessionID, installationID, bindingID, parentID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'manual retry') RETURNING id
	`, workspaceID, agentID, userID).Scan(&sessionID); err != nil {
		t.Fatalf("insert chat session: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, status, installer_user_id)
		VALUES ($1, $2, 'slack', '{}'::jsonb, 'active', $3) RETURNING id
	`, workspaceID, agentID, userID).Scan(&installationID); err != nil {
		t.Fatalf("insert channel installation: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO channel_chat_session_binding (
			chat_session_id, installation_id, channel_type, channel_chat_id, chat_type,
			last_message_id, last_thread_id, route_revision, config
		) VALUES ($1, $2, 'slack', 'C1', 'group', '171.001', '171.001', 2, '{"channel_id":"C1"}'::jsonb)
		RETURNING id
	`, sessionID, installationID).Scan(&bindingID); err != nil {
		t.Fatalf("insert channel binding: %v", err)
	}
	// A non-retryable reason with an exhausted budget: the automatic retry
	// would refuse it, but a member's explicit retry must not.
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, chat_session_id, status, priority, attempt,
			max_attempts, failure_reason, originator_user_id, accountable_user_id, originator_source
		) VALUES ($1, $2, $3, 'failed', 0, 3, 3, 'agent_error', $4, $4, 'direct_human')
		RETURNING id
	`, agentID, runtimeID, sessionID, userID).Scan(&parentID); err != nil {
		t.Fatalf("insert failed chat task: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_task_delivery (
			task_id, binding_id, installation_id, channel_type, channel_chat_id, chat_type,
			channel_message_id, channel_thread_id, route_revision, config
		) VALUES ($1, $2, $3, 'slack', 'C1', 'group', '171.001', '171.001', 2, '{"channel_id":"C1"}'::jsonb)
	`, parentID, bindingID, installationID); err != nil {
		t.Fatalf("insert parent delivery: %v", err)
	}

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	parent := util.MustParseUUID(parentID)
	actor := util.MustParseUUID(userID)

	child, err := svc.RetryFailedChatTask(ctx, parent, actor)
	if err != nil {
		t.Fatalf("RetryFailedChatTask: %v", err)
	}
	if child.Status != "queued" || child.ChatSessionID != util.MustParseUUID(sessionID) || child.ParentTaskID != parent {
		t.Fatalf("child = status %q session %v parent %v", child.Status, child.ChatSessionID, child.ParentTaskID)
	}
	delivery, err := q.GetChannelTaskDelivery(ctx, child.ID)
	if err != nil || util.UUIDToString(delivery.BindingID) != bindingID {
		t.Fatalf("child delivery = %+v, %v; want the parent's binding", delivery, err)
	}

	// A second click on the same card must not start a second run.
	if _, err := svc.RetryFailedChatTask(ctx, parent, actor); !errors.Is(err, ErrChatRetryStale) {
		t.Fatalf("duplicate retry err = %v, want ErrChatRetryStale", err)
	}
	var children int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id = $1`, parent).Scan(&children); err != nil {
		t.Fatalf("count children: %v", err)
	}
	if children != 1 {
		t.Fatalf("retry children = %d, want 1", children)
	}

	// A turn that did not fail is never retryable.
	if _, err := svc.RetryFailedChatTask(ctx, child.ID, actor); !errors.Is(err, ErrChatRetryStale) {
		t.Fatalf("retry of a queued turn err = %v, want ErrChatRetryStale", err)
	}
}
