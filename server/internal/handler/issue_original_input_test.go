package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestGetIssueQuickCreateOriginalInput locks the product invariant that the
// user's request remains available even when the quick-create agent wrote a
// semantically different description. The detail endpoint derives the raw
// request from the immutable origin task; list/event payloads stay unchanged.
func TestGetIssueQuickCreateOriginalInput(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "quick-create-original-input", nil)
	original := "调查 `command code` 的周限。\n不要改成 Claude Code。\n[@Eve](mention://agent/agent-1)"
	contextJSON, err := json.Marshal(service.QuickCreateContext{
		Type:        service.QuickCreateContextType,
		Prompt:      original,
		RequesterID: testUserID,
		WorkspaceID: testWorkspaceID,
	})
	if err != nil {
		t.Fatalf("marshal quick-create context: %v", err)
	}
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"status":     "completed",
		"context":    contextJSON,
	})
	issueID := dbfx.Issue(t, "Investigate Claude Code weekly limit", testutil.Cols{
		"description":  "Investigate whether the Claude Code weekly limit affects chat processing.",
		"creator_type": "agent",
		"creator_id":   agentID,
		"origin_type":  service.QuickCreateContextType,
		"origin_id":    taskID,
	})
	recorder := testutil.Call(t, testHandler.GetIssue,
		withURLParam(newRequest("GET", "/api/issues/"+issueID, nil), "id", issueID),
	).Want(http.StatusOK)
	var got IssueResponse
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode issue detail: %v", err)
	}
	if got.OriginalInput == nil || *got.OriginalInput != original {
		t.Fatalf("original_input = %v, want exact quick-create prompt %q", got.OriginalInput, original)
	}
	if got.Description == nil || *got.Description != "Investigate whether the Claude Code weekly limit affects chat processing." {
		t.Fatalf("description = %v, want generated summary to remain separate", got.Description)
	}
}

// A corrupt historical origin must not make the Issue itself unreadable. The
// detail response degrades by omitting original_input; GetIssue logs the fault.
func TestGetIssueQuickCreateOriginalInputDegrades(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "quick-create-original-input-corrupt", nil)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"status":     "completed",
		"context":    []byte(`{"type":"quick_create","prompt":"","workspace_id":"` + testWorkspaceID + `"}`),
	})
	issueID := dbfx.Issue(t, "Quick-create issue with corrupt provenance", testutil.Cols{
		"creator_type": "agent",
		"creator_id":   agentID,
		"origin_type":  service.QuickCreateContextType,
		"origin_id":    taskID,
	})

	recorder := testutil.Call(t, testHandler.GetIssue,
		withURLParam(newRequest("GET", "/api/issues/"+issueID, nil), "id", issueID),
	).Want(http.StatusOK)
	var got IssueResponse
	if err := json.NewDecoder(recorder.Body).Decode(&got); err != nil {
		t.Fatalf("decode issue detail: %v", err)
	}
	if got.OriginalInput != nil {
		t.Fatalf("original_input = %q, want omitted for corrupt origin", *got.OriginalInput)
	}
}

func TestCreateIssueRejectsInvalidQuickCreateOrigins(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	creatorID := createHandlerTestAgent(t, "quick-create-origin-validation", nil)
	otherAgentID := createHandlerTestAgent(t, "quick-create-origin-other-agent", nil)
	validContext := func(workspaceID, prompt string) []byte {
		t.Helper()
		payload, err := json.Marshal(service.QuickCreateContext{
			Type: service.QuickCreateContextType, Prompt: prompt,
			RequesterID: testUserID, WorkspaceID: workspaceID,
		})
		if err != nil {
			t.Fatalf("marshal quick-create context: %v", err)
		}
		return payload
	}
	newTask := func(agentID string, taskContext []byte) string {
		t.Helper()
		return dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": handlerTestRuntimeID(t), "status": "running", "context": taskContext,
		})
	}
	actingTaskID := newTask(creatorID, validContext(testWorkspaceID, "acting task"))

	tests := []struct {
		name         string
		originTaskID string
	}{
		{name: "missing task", originTaskID: "00000000-0000-4000-8000-000000000001"},
		{name: "wrong creator", originTaskID: newTask(otherAgentID, validContext(testWorkspaceID, "other agent"))},
		{name: "wrong context type", originTaskID: newTask(creatorID, []byte(`{"type":"issue","prompt":"x","workspace_id":"`+testWorkspaceID+`"}`))},
		{name: "wrong context workspace", originTaskID: newTask(creatorID, validContext("00000000-0000-4000-8000-000000000002", "wrong workspace"))},
		{name: "empty prompt", originTaskID: newTask(creatorID, validContext(testWorkspaceID, ""))},
		{name: "malformed context", originTaskID: newTask(creatorID, []byte(`[]`))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title := "Reject invalid quick-create origin: " + tt.name
			recorder := createQuickCreateIssue(t, creatorID, actingTaskID, tt.originTaskID, title)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("CreateIssue: got %d (%s), want 400", recorder.Code, recorder.Body.String())
			}
			var count int
			if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title).Scan(&count); err != nil {
				t.Fatalf("count rejected issues: %v", err)
			}
			if count != 0 {
				t.Fatalf("persisted %d issues for rejected origin, want 0", count)
			}
		})
	}
}

func TestCreateIssueRejectsConcurrentQuickCreateOriginReuse(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "quick-create-origin-concurrency", nil)
	contextJSON, err := json.Marshal(service.QuickCreateContext{
		Type: service.QuickCreateContextType, Prompt: "one origin, one issue",
		RequesterID: testUserID, WorkspaceID: testWorkspaceID,
	})
	if err != nil {
		t.Fatalf("marshal quick-create context: %v", err)
	}
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t), "status": "running", "context": contextJSON,
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1 AND origin_type = $2 AND origin_id = $3`, testWorkspaceID, service.QuickCreateContextType, taskID)
	})

	start := make(chan struct{})
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			recorder := createQuickCreateIssue(t, agentID, taskID, taskID, "Concurrent quick-create origin "+string(rune('A'+i)))
			codes <- recorder.Code
		}(i)
	}
	close(start)
	wg.Wait()
	close(codes)

	gotCodes := make([]int, 0, 2)
	for code := range codes {
		gotCodes = append(gotCodes, code)
	}
	sort.Ints(gotCodes)
	wantCodes := []int{http.StatusCreated, http.StatusBadRequest}
	if len(gotCodes) != len(wantCodes) || gotCodes[0] != wantCodes[0] || gotCodes[1] != wantCodes[1] {
		t.Fatalf("concurrent status codes = %v, want %v", gotCodes, wantCodes)
	}

	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM issue WHERE workspace_id = $1 AND origin_type = $2 AND origin_id = $3`, testWorkspaceID, service.QuickCreateContextType, taskID).Scan(&count); err != nil {
		t.Fatalf("count issues by origin: %v", err)
	}
	if count != 1 {
		t.Fatalf("issues for one quick-create origin = %d, want 1", count)
	}
}

func createQuickCreateIssue(t *testing.T, agentID, actingTaskID, originTaskID, title string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": title, "origin_type": service.QuickCreateContextType, "origin_id": originTaskID,
	})
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", actingTaskID)
	testHandler.CreateIssue(recorder, req)
	return recorder
}
