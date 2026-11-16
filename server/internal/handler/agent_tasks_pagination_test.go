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

func TestListAgentTasksPageBoundaries(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "history boundaries runtime")
	for _, count := range []int{0, 1, 2, 3, 4, 6} {
		t.Run(fmt.Sprintf("%d_tasks_limit_3", count), func(t *testing.T) {
			agentID := dbfx.Agent(t, "history boundary agent", runtimeID)
			now := time.Now().UTC().Truncate(time.Microsecond)
			// Deliberately reverse UUID and time order: created_at must be the
			// primary sort key, not UUID order or insertion order.
			expected := make([]string, count)
			for i := 0; i < count; i++ {
				expected[i] = dbfx.Task(t, agentID, testutil.Cols{
					"id":         fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1),
					"runtime_id": runtimeID, "status": "completed",
					"created_at": now.Add(-time.Duration(i) * time.Microsecond),
				})
			}
			var got []string
			before := ""
			for page := 0; page < 3; page++ {
				req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks?limit=3&before="+url.QueryEscape(before), nil), "id", agentID)
				var tasks []AgentTaskResponse
				response := testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK).JSON(&tasks)
				for _, task := range tasks {
					got = append(got, task.ID)
				}
				before = response.Header().Get(HeaderAgentTasksNextCursor)
				wantMore := len(got) < count
				if (before != "") != wantMore {
					t.Fatalf("after %d/%d tasks: cursor=%q", len(got), count, before)
				}
				if !wantMore {
					break
				}
			}
			if len(got) != len(expected) {
				t.Fatalf("got %v, want %v", got, expected)
			}
			for i := range got {
				if got[i] != expected[i] {
					t.Fatalf("got %v, want timestamp order %v", got, expected)
				}
			}
			// A valid cursor older than every row is an empty, terminal page.
			before = transcriptCursor(now.Add(-time.Hour), parseUUID(agentID))
			req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks?before="+url.QueryEscape(before), nil), "id", agentID)
			response := testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK)
			if response.Body.String() != "[]\n" || response.Header().Get(HeaderAgentTasksNextCursor) != "" {
				t.Fatalf("past the end: %s, cursor=%q", response.Body.String(), response.Header().Get(HeaderAgentTasksNextCursor))
			}
		})
	}
}

func TestListAgentTasksCursorDoesNotGrantPrivateAccess(t *testing.T) {
	agentID, ownerID, memberID := privateAgentTestFixture(t)
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		dbfx.Task(t, agentID, testutil.Cols{"status": "completed", "completed_at": now, "created_at": now.Add(-time.Duration(i) * time.Minute)})
	}
	var first []AgentTaskResponse
	req := withURLParam(newRequestAs(ownerID, http.MethodGet, "/api/agents/"+agentID+"/tasks?limit=1", nil), "id", agentID)
	response := testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK).JSON(&first)
	cursor := response.Header().Get(HeaderAgentTasksNextCursor)
	if len(first) != 1 || cursor == "" {
		t.Fatal("expected a first page and cursor")
	}
	for _, userID := range []string{memberID, ownerID} {
		status := http.StatusForbidden
		if userID == ownerID {
			status = http.StatusOK
		}
		req := withURLParam(newRequestAs(userID, http.MethodGet, "/api/agents/"+agentID+"/tasks?limit=1&include_usage=true&before="+url.QueryEscape(cursor), nil), "id", agentID)
		response := testutil.Call(t, testHandler.ListAgentTasks, req).Want(status)
		if userID == memberID && response.Header().Get(HeaderAgentTasksNextCursor) != "" {
			t.Fatal("forbidden response leaked a continuation")
		}
		if userID == ownerID {
			var second []AgentTaskResponse
			response.JSON(&second)
			if len(second) != 1 || second[0].ID == first[0].ID {
				t.Fatalf("bad owner continuation: %+v", second)
			}
		}
	}
}

func TestListAgentTasksUsageAndAgentScopeAcrossPages(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "paged usage runtime")
	agentID := dbfx.Agent(t, "paged usage agent", runtimeID)
	otherID := dbfx.Agent(t, "other paged usage agent", runtimeID)
	now := time.Now().UTC()
	ids := make([]string, 3)
	for i := range ids {
		ids[i] = dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "status": "completed", "created_at": now.Add(-time.Duration(i) * time.Minute)})
		dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": ids[i], "provider": "openai", "model": "test-model", "input_tokens": i + 10})
	}
	otherTask := dbfx.Task(t, otherID, testutil.Cols{"runtime_id": runtimeID, "created_at": now.Add(-time.Minute)})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": otherTask, "provider": "openai", "model": "private-other-model", "input_tokens": 999})
	cursor := ""
	for i, id := range ids {
		req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks?limit=1&include_usage=true&before="+url.QueryEscape(cursor), nil), "id", agentID)
		var tasks []AgentTaskResponse
		response := testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK).JSON(&tasks)
		if len(tasks) != 1 || tasks[0].ID != id {
			t.Fatalf("page %d: %+v", i, tasks)
		}
		usage := tasks[0].Usage
		if len(usage) != 1 || usage[0].InputTokens != int64(i+10) || usage[0].Model != "test-model" {
			t.Fatalf("page %d usage: %+v", i, usage)
		}
		cursor = response.Header().Get(HeaderAgentTasksNextCursor)
	}
	if cursor != "" {
		t.Fatal("last usage page still has continuation")
	}
	// A cursor minted for a different agent is only a position, never a way
	// to read that agent's rows or its usage.
	cursor = transcriptCursor(now, parseUUID(ids[0]))
	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+otherID+"/tasks?limit=1&include_usage=true&before="+url.QueryEscape(cursor), nil), "id", otherID)
	var tasks []AgentTaskResponse
	testutil.Call(t, testHandler.ListAgentTasks, req).Want(http.StatusOK).JSON(&tasks)
	if len(tasks) != 1 || tasks[0].ID != otherTask || len(tasks[0].Usage) != 1 || tasks[0].Usage[0].Model != "private-other-model" {
		t.Fatalf("cross-agent cursor changed ownership scope: %+v", tasks)
	}
}
