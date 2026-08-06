package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestTaskHasOnboardingKickoffInput covers the query the quick-actions
// eligibility gate uses to skip Mika's onboarding opening (MUL-5765): it must
// be true only when the task's user input row is the hidden kickoff, so an
// ordinary turn keeps its suggestion pass.
func TestTaskHasOnboardingKickoffInput(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})

	seedInput := func(kind string) string {
		var taskID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO chat_message (chat_session_id, role, content, message_kind, task_id)
			VALUES ($1, 'user', 'input', $2, gen_random_uuid())
			RETURNING task_id::text`, chatSessionID, kind).Scan(&taskID); err != nil {
			t.Fatalf("seed %s input: %v", kind, err)
		}
		return taskID
	}

	kickoffTaskID := seedInput("onboarding_kickoff")
	ordinaryTaskID := seedInput("message")

	got, err := q.TaskHasOnboardingKickoffInput(ctx, util.MustParseUUID(kickoffTaskID))
	if err != nil {
		t.Fatalf("TaskHasOnboardingKickoffInput(kickoff): %v", err)
	}
	if !got {
		t.Errorf("kickoff input task = false, want true")
	}

	got, err = q.TaskHasOnboardingKickoffInput(ctx, util.MustParseUUID(ordinaryTaskID))
	if err != nil {
		t.Fatalf("TaskHasOnboardingKickoffInput(ordinary): %v", err)
	}
	if got {
		t.Errorf("ordinary input task = true, want false")
	}
}

// TestWriteChatCompletionOutcomeStampsOnboardingOpening exercises the real
// completion write: the reply to a kickoff-input task must persist with
// message_kind 'onboarding_opening' (what makes the client render the starter
// cards — the kickoff row itself never reaches clients, MUL-5765), while an
// ordinary turn's reply stays kind 'message'.
func TestWriteChatCompletionOutcomeStampsOnboardingOpening(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})

	seedInput := func(kind string) string {
		var taskID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO chat_message (chat_session_id, role, content, message_kind, task_id)
			VALUES ($1, 'user', 'input', $2, gen_random_uuid())
			RETURNING task_id::text`, chatSessionID, kind).Scan(&taskID); err != nil {
			t.Fatalf("seed %s input: %v", kind, err)
		}
		return taskID
	}

	svc := &TaskService{Queries: q, TxStarter: pool}
	result, _ := json.Marshal(protocol.TaskCompletedPayload{Output: "Hi, I'm Mika."})

	complete := func(taskID string) db.ChatMessage {
		row, err := svc.writeChatCompletionOutcome(ctx, q, db.AgentTaskQueue{
			ID:            util.MustParseUUID(taskID),
			ChatSessionID: util.MustParseUUID(chatSessionID),
			AgentID:       util.MustParseUUID(agentID),
		}, result)
		if err != nil {
			t.Fatalf("writeChatCompletionOutcome: %v", err)
		}
		if row == nil {
			t.Fatalf("writeChatCompletionOutcome wrote no row")
		}
		return *row
	}

	opening := complete(seedInput("onboarding_kickoff"))
	if opening.MessageKind != protocol.ChatMessageKindOnboardingOpening {
		t.Errorf("kickoff reply kind = %q, want onboarding_opening", opening.MessageKind)
	}

	ordinary := complete(seedInput("message"))
	if ordinary.MessageKind == protocol.ChatMessageKindOnboardingOpening {
		t.Errorf("ordinary reply must not be stamped as the opening")
	}
}
