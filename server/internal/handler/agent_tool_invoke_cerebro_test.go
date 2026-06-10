package handler

// CEREBRO-PATCH(invoke-handler-test): TECH-3226 — regression coverage for
// InvokeAgentTool: 403 vs 422 routing, callerUserID/cascadeUserID assignment
// for user-token and task-token paths.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

// mockToolExecutor captures Invoke calls for assertion and returns configured results.
type mockToolExecutor struct {
	err    error
	result string

	capturedAgentID     pgtype.UUID
	capturedWorkspaceID pgtype.UUID
	capturedUserID      pgtype.UUID
	capturedCascadeUID  pgtype.UUID
	capturedToolName    string
}

func (m *mockToolExecutor) Invoke(_ context.Context, agentID, workspaceID, userID, cascadeUserID pgtype.UUID, toolName string, _ map[string]any) (string, error) {
	m.capturedAgentID = agentID
	m.capturedWorkspaceID = workspaceID
	m.capturedUserID = userID
	m.capturedCascadeUID = cascadeUserID
	m.capturedToolName = toolName
	return m.result, m.err
}

// withInvokeParams adds both "id" and "name" chi URL params to the request in one context,
// avoiding the issue where sequential withURLParam calls overwrite each other.
func withInvokeParams(req *http.Request, agentID, toolName string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", agentID)
	rctx.URLParams.Add("name", toolName)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestInvokeAgentTool_ErrToolNotPermitted_Returns403(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "invoke-test-perm", []byte(`{}`))

	mock := &mockToolExecutor{err: ErrToolNotPermitted}
	prev := testHandler.ToolExecutor
	testHandler.ToolExecutor = mock
	t.Cleanup(func() { testHandler.ToolExecutor = prev })

	w := httptest.NewRecorder()
	req := withInvokeParams(
		newRequest(http.MethodPost, "/api/agents/"+agentID+"/tools/add_comment/invoke", map[string]any{"args": map[string]any{}}),
		agentID, "add_comment",
	)
	testHandler.InvokeAgentTool(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInvokeAgentTool_ToolError_Returns422(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "invoke-test-toolfail", []byte(`{}`))

	mock := &mockToolExecutor{err: errors.New("issue not found")}
	prev := testHandler.ToolExecutor
	testHandler.ToolExecutor = mock
	t.Cleanup(func() { testHandler.ToolExecutor = prev })

	w := httptest.NewRecorder()
	req := withInvokeParams(
		newRequest(http.MethodPost, "/api/agents/"+agentID+"/tools/add_comment/invoke", map[string]any{"args": map[string]any{}}),
		agentID, "add_comment",
	)
	testHandler.InvokeAgentTool(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInvokeAgentTool_Success_Returns200WithResult(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "invoke-test-ok", []byte(`{}`))

	mock := &mockToolExecutor{result: `{"id":"abc"}`}
	prev := testHandler.ToolExecutor
	testHandler.ToolExecutor = mock
	t.Cleanup(func() { testHandler.ToolExecutor = prev })

	w := httptest.NewRecorder()
	req := withInvokeParams(
		newRequest(http.MethodPost, "/api/agents/"+agentID+"/tools/add_comment/invoke", map[string]any{"args": map[string]any{}}),
		agentID, "add_comment",
	)
	testHandler.InvokeAgentTool(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["result"] != `{"id":"abc"}` {
		t.Fatalf("result mismatch: %v", body)
	}
}

// TestInvokeAgentTool_UserToken_CallerIDAndCascadeIDFromXUserID verifies that
// user token calls set callerUserID (authorship) and cascadeUserID (permissions)
// both to X-User-ID.
func TestInvokeAgentTool_UserToken_CallerIDAndCascadeIDFromXUserID(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "invoke-test-userid", []byte(`{}`))

	mock := &mockToolExecutor{result: "ok"}
	prev := testHandler.ToolExecutor
	testHandler.ToolExecutor = mock
	t.Cleanup(func() { testHandler.ToolExecutor = prev })

	w := httptest.NewRecorder()
	// newRequest already sets X-User-ID = testUserID; no X-Actor-Source header = user token path
	req := withInvokeParams(
		newRequest(http.MethodPost, "/api/agents/"+agentID+"/tools/add_comment/invoke", nil),
		agentID, "add_comment",
	)
	testHandler.InvokeAgentTool(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	expectedUID, err := util.ParseUUID(testUserID)
	if err != nil {
		t.Fatalf("parse testUserID: %v", err)
	}
	if mock.capturedUserID != expectedUID {
		t.Errorf("callerUserID: got %v, want %v", mock.capturedUserID, expectedUID)
	}
	if mock.capturedCascadeUID != expectedUID {
		t.Errorf("cascadeUserID: got %v, want %v", mock.capturedCascadeUID, expectedUID)
	}
}

// TestInvokeAgentTool_TaskToken_ZeroCallerUserID_OriginalUserIDForCascade
// verifies that task-token calls leave callerUserID zero (agent authorship)
// and set cascadeUserID to task.OriginalUserID.
func TestInvokeAgentTool_TaskToken_ZeroCallerUserID_OriginalUserIDForCascade(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "invoke-test-tasktoken", []byte(`{}`))
	// createHandlerTestDelegatedTaskForAgent seeds a task with original_user_id = testUserID
	taskID := createHandlerTestDelegatedTaskForAgent(t, agentID, testUserID)

	mock := &mockToolExecutor{result: "ok"}
	prev := testHandler.ToolExecutor
	testHandler.ToolExecutor = mock
	t.Cleanup(func() { testHandler.ToolExecutor = prev })

	w := httptest.NewRecorder()
	req := withInvokeParams(
		newRequest(http.MethodPost, "/api/agents/"+agentID+"/tools/add_comment/invoke", nil),
		agentID, "add_comment",
	)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)

	testHandler.InvokeAgentTool(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var zero pgtype.UUID
	if mock.capturedUserID != zero {
		t.Errorf("callerUserID should be zero for task token, got %v", mock.capturedUserID)
	}
	expectedCascadeUID, err := util.ParseUUID(testUserID)
	if err != nil {
		t.Fatalf("parse testUserID: %v", err)
	}
	if mock.capturedCascadeUID != expectedCascadeUID {
		t.Errorf("cascadeUserID: got %v, want originalUserID %v", mock.capturedCascadeUID, expectedCascadeUID)
	}
}
