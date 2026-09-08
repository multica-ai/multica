package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestUpdateIssue_AgentStatusPublishesSourceTask proves the server half of the
// real `multica issue status` path. The CLI sends X-Agent-ID + X-Task-ID on its
// ordinary PUT /api/issues/{id}; after validating that pair, UpdateIssue must
// preserve the run id on issue:updated so the notification listener can select
// the exact question/delivery comment authored by that run.
func TestUpdateIssue_AgentStatusPublishesSourceTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "IssueStatusSourceTask", []byte("[]"))
	issueID := insertWorkflowTestIssue(t, "status source task", int(time.Now().UnixNano()%100000)+8_790_000)
	dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, issueID)
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	h := *testHandler
	h.Bus = events.New()
	var updated events.Event
	h.Bus.Subscribe(protocol.EventIssueUpdated, func(event events.Event) {
		updated = event
	})

	req := withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
		"status": "blocked",
	}), "id", issueID)
	req = testutil.WithHeaders(req, "X-Agent-ID", agentID, "X-Task-ID", taskID)
	testutil.Call(t, h.UpdateIssue, req).Want(http.StatusOK)

	if updated.ActorType != "agent" || updated.ActorID != agentID {
		t.Fatalf("issue:updated actor = %s/%s, want agent/%s", updated.ActorType, updated.ActorID, agentID)
	}
	payload, ok := updated.Payload.(map[string]any)
	if !ok {
		t.Fatalf("issue:updated payload = %#v", updated.Payload)
	}
	if got := payload["source_task_id"]; got != taskID {
		t.Fatalf("issue:updated source_task_id = %#v, want %q", got, taskID)
	}
}
