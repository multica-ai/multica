package handler

// agent_archive_cancel_broadcast_test.go — archiving an agent cancels every
// run it has in flight, and something outside this process is showing those
// runs to a user.
//
// The rows are flipped by the same query the agent-level "cancel all tasks"
// button uses, so the task is over either way. What differs is whether anyone
// is told. ArchiveAgent used to run the query and drop the returned rows, on
// the reasoning that the agent:archived event invalidates every web client's
// active-task list, which made per-task events redundant. That was true of the
// web client and of nothing else: a channel adapter has a bubble on a user's
// screen, and task:cancelled is the only event that ever arrives for a
// cancelled chat run — cancellation publishes no chat:done and no task:failed,
// and the daemon's own completion arrives to find the row already cancelled,
// where CompleteAgentTask's `status = 'running'` guard matches nothing and the
// answer is discarded silently.

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestArchivingAnAgentTellsItsChatRunsSubscribersTheRunIsOver pins the event
// the channel adapters need, and the field they route it by.
//
// The loss on the other side of this is not an invalidation a client can
// refetch its way out of. WeCom paints a bubble when the question arrives and
// waits for an ending; five minutes with no ending and its guard replaces the
// spinner with "还在处理，完成后我再单独回复你。" — a written promise of a
// separate reply — and schedules nothing after it. With no task:cancelled and
// no chat:done, that promise is the last thing the asker ever hears about
// their question.
func TestArchivingAnAgentTellsItsChatRunsSubscribersTheRunIsOver(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "archive-cancel-broadcast-agent", nil)
	sessionID := insertChatSessionAs(t, agentID, testUserID)
	taskID := insertPendingChatTask(t, agentID, sessionID, "running")

	var mu sync.Mutex
	var seen []events.Event
	testHandler.Bus.Subscribe(protocol.EventTaskCancelled, func(e events.Event) {
		// The bus is shared with every other test in this package, so only this
		// run's own cancellation counts.
		if e.TaskID != taskID {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, e)
	})

	req := newRequest(http.MethodPost, "/api/agents/"+agentID+"/archive", nil)
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.ArchiveAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ArchiveAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if got := taskStatus(t, taskID); got != "cancelled" {
		t.Fatalf("task status after archive = %q, want %q", got, "cancelled")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 {
		t.Fatalf("archiving the agent cancelled its running chat task and published %d "+
			"task:cancelled event(s) for it, want 1 — a cancelled chat run publishes nothing "+
			"else, and the daemon's own completion arrives to a row that is no longer 'running' "+
			"and is dropped before it broadcasts, so the bubble a channel is showing the asker "+
			"has no ending left to close it: five minutes on they are promised a separate reply "+
			"and never told anything again", len(seen))
	}
	if seen[0].ChatSessionID != sessionID {
		t.Errorf("the cancellation carries chat_session_id %q, want %q — it is how a channel "+
			"adapter finds the round this run belongs to", seen[0].ChatSessionID, sessionID)
	}
	payload, ok := seen[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("the cancellation payload is %T, want map[string]any", seen[0].Payload)
	}
	if payload["task_id"] != taskID {
		t.Errorf("the cancellation payload names task %v, want %q", payload["task_id"], taskID)
	}
}
