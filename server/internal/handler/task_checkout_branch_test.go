package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestRecordTaskCheckoutBranchBroadcastsRefreshEvent(t *testing.T) {
	ctx := context.Background()
	_, taskID := seedNULTask(t, "branch-recorded-agent")

	received := make(chan events.Event, 1)
	testHandler.Bus.Subscribe(protocol.EventTaskBranchRecorded, func(event events.Event) {
		if event.TaskID == taskID {
			received <- event
		}
	})

	w := httptest.NewRecorder()
	req := daemonTaskRequest(t, "/api/daemon/tasks/"+taskID+"/checkout-branch", taskID, map[string]any{
		"branch_name": "agent/mika/tes-1",
		"repo_url":    "https://github.com/homyee/openjob.git",
	})
	testHandler.RecordTaskCheckoutBranch(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("RecordTaskCheckoutBranch returned %d, want 204: %s", w.Code, w.Body.String())
	}

	var branch string
	if err := testPool.QueryRow(ctx,
		`SELECT branch_name FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&branch); err != nil {
		t.Fatalf("read branch_name: %v", err)
	}
	if branch != "agent/mika/tes-1" {
		t.Fatalf("branch_name = %q, want agent/mika/tes-1", branch)
	}

	select {
	case event := <-received:
		if event.WorkspaceID != testWorkspaceID {
			t.Fatalf("workspace_id = %q, want %q", event.WorkspaceID, testWorkspaceID)
		}
	case <-time.After(time.Second):
		t.Fatal("task:branch_recorded was not broadcast")
	}
}
