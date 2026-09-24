package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestListAgentTasksPagination(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "paged history runtime")
	agentID := dbfx.Agent(t, "paged history agent", runtimeID)
	otherAgentID := dbfx.Agent(t, "other history agent", runtimeID)
	// Equal, sub-second timestamps exercise the tie breaker and lossless cursor.
	createdAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	visible := map[string]bool{}
	for i := 0; i < 205; i++ {
		id := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "completed", "created_at": createdAt})
		visible[id] = true
	}
	var parentID string
	for id := range visible {
		parentID = id
		break
	}
	dbfx.Task(t, otherAgentID, testutil.Cols{"runtime_id": runtimeID, "status": "completed", "created_at": createdAt})
	// These sort ahead of the visible history. Filtering after LIMIT would
	// produce an empty/short page and incorrectly make older work inaccessible.
	for _, status := range []string{"cancelled", "deferred"} {
		dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": status, "created_at": createdAt.Add(time.Second), "escalation_for_task_id": parentID})
	}
	startedFallback := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "cancelled", "created_at": createdAt, "started_at": createdAt, "escalation_for_task_id": parentID})
	visible[startedFallback] = true

	readPage := func(query string, wantStatus int) ([]AgentTaskResponse, string) {
		t.Helper()
		req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks"+query, nil), "id", agentID)
		var tasks []AgentTaskResponse
		response := testutil.Call(t, testHandler.ListAgentTasks, req).Want(wantStatus)
		if wantStatus == http.StatusOK {
			response.JSON(&tasks)
		}
		return tasks, response.Header().Get(HeaderAgentTasksNextCursor)
	}
	for _, query := range []string{"", "?limit=999999"} {
		tasks, cursor := readPage(query, http.StatusOK)
		if len(tasks) != 200 || cursor == "" {
			t.Fatalf("%s: got %d tasks, cursor %q", query, len(tasks), cursor)
		}
	}
	for _, query := range []string{"?limit=0", "?limit=-1", "?limit=abc", "?limit=999999999999999999999", "?before=bad", "?before=2026-01-01T00:00:00Z%7Cbad"} {
		readPage(query, http.StatusBadRequest)
	}

	seen := map[string]bool{}
	cursor := ""
	var previous AgentTaskResponse
	for page := 0; page < 100; page++ {
		tasks, next := readPage("?limit=7&before="+url.QueryEscape(cursor), http.StatusOK)
		if len(tasks) > 7 {
			t.Fatal("page exceeded limit")
		}
		for _, task := range tasks {
			if !visible[task.ID] || seen[task.ID] {
				t.Fatalf("unexpected or repeated task %s", task.ID)
			}
			if previous.ID != "" && previous.ID <= task.ID {
				t.Fatalf("tied timestamps not ordered by descending id: %s then %s", previous.ID, task.ID)
			}
			seen[task.ID] = true
			previous = task
		}
		if page == 0 {
			// Newer work must not shift subsequent pages or repeat old rows.
			dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "completed", "created_at": createdAt.Add(time.Minute)})
		}
		if next == "" {
			break
		}
		if next == cursor {
			t.Fatal("cursor did not advance")
		}
		cursor = next
	}
	if len(seen) != len(visible) {
		t.Fatalf("read %d of %d visible tasks", len(seen), len(visible))
	}

	// A cursor remains usable if its boundary row is deleted between requests.
	tasks, cursor := readPage("?limit=1", http.StatusOK)
	if _, err := testPool.Exec(context.Background(), "DELETE FROM agent_task_queue WHERE id = $1", tasks[0].ID); err != nil {
		t.Fatal(err)
	}
	tasks, _ = readPage("?limit=1&before="+url.QueryEscape(cursor), http.StatusOK)
	if len(tasks) != 1 {
		t.Fatal("deleted boundary lost the rest of history")
	}

	emptyID := dbfx.Agent(t, "empty history agent", runtimeID)
	req := withURLParam(newRequest(http.MethodGet, fmt.Sprintf("/api/agents/%s/tasks", emptyID), nil), "id", emptyID)
	response := testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK)
	if response.Body.String() != "[]\n" {
		t.Fatalf("empty history = %s", response.Body.String())
	}
}

func TestAgentActivityDurationUsesAllRuns(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "duration runtime")
	agentID := dbfx.Agent(t, "duration agent", runtimeID)
	now := time.Now().UTC().Truncate(time.Second)
	// More than one history page: duration must never be calculated from the
	// newest 200 rows or change when another page is opened.
	for i := 0; i < 201; i++ {
		duration := time.Minute
		if i == 0 {
			duration = 10 * time.Minute
		}
		dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "completed", "started_at": now.Add(-duration), "completed_at": now})
	}
	for _, cols := range []testutil.Cols{
		{"status": "completed", "started_at": now.Add(-32 * 24 * time.Hour), "completed_at": now.Add(-31 * 24 * time.Hour)},
		{"status": "cancelled", "completed_at": now},
		{"status": "failed", "started_at": now.Add(time.Second), "completed_at": now},
		{"status": "running", "started_at": now},
	} {
		cols["runtime_id"] = runtimeID
		dbfx.Task(t, agentID, cols)
	}
	req := withChatTestWorkspaceCtx(t, newRequest(http.MethodGet, "/api/agent-activity-30d", nil))
	var buckets []AgentActivityBucket
	testutil.Call(t, testHandler.GetWorkspaceAgentActivity30d, req).Want(http.StatusOK).JSON(&buckets)
	var count int32
	var duration float64
	for _, bucket := range buckets {
		if bucket.AgentID == agentID {
			count += bucket.DurationCount
			duration += bucket.DurationMs
		}
	}
	if count != 201 || duration != 210*60000 {
		t.Fatalf("duration/count = %v/%d, want %d/201", duration, count, 210*60000)
	}
}
