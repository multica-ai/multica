package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

func setHandlerTestRuntimeCLIVersion(t *testing.T, version string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_runtime
		SET metadata = jsonb_set(COALESCE(metadata, '{}'::jsonb), '{cli_version}', to_jsonb($2::text), true)
		WHERE id = $1
	`, handlerTestRuntimeID(t), version); err != nil {
		t.Fatalf("set runtime cli_version: %v", err)
	}
}

func TestQuickCreateIssue_StoresExplicitFields(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	setHandlerTestRuntimeCLIVersion(t, "0.2.24")
	agentID := createHandlerTestAgent(t, "Quick Create Test Agent", nil)

	const dueDate = "2025-06-01T00:00:00Z"
	const projectID = "123e4567-e89b-12d3-a456-426614174000"

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/issues/quick-create", map[string]any{
		"agent_id":   agentID,
		"prompt":     "Create a follow-up issue",
		"priority":   " HIGH ",
		"due_date":   " " + dueDate + " ",
		"project_id": " " + projectID + " ",
	})
	testHandler.QuickCreateIssue(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("QuickCreateIssue: expected 202, got %d: %s", w.Code, w.Body.String())
	}

	var resp QuickCreateIssueResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var contextJSON []byte
	if err := testPool.QueryRow(ctx, `SELECT context FROM agent_task_queue WHERE id = $1`, resp.TaskID).Scan(&contextJSON); err != nil {
		t.Fatalf("load queued task context: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, resp.TaskID)
	})

	var qc service.QuickCreateContext
	if err := json.Unmarshal(contextJSON, &qc); err != nil {
		t.Fatalf("unmarshal quick-create context: %v", err)
	}
	if qc.Type != service.QuickCreateContextType {
		t.Fatalf("context type = %q, want %q", qc.Type, service.QuickCreateContextType)
	}
	if qc.Priority == nil || *qc.Priority != "high" {
		t.Fatalf("context priority = %v, want high", qc.Priority)
	}
	if qc.DueDate == nil || *qc.DueDate != dueDate {
		t.Fatalf("context due_date = %v, want %s", qc.DueDate, dueDate)
	}
	if qc.ProjectID == nil || *qc.ProjectID != projectID {
		t.Fatalf("context project_id = %v, want %s", qc.ProjectID, projectID)
	}
}

func TestQuickCreateIssue_RejectsInvalidOptionalFields(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	setHandlerTestRuntimeCLIVersion(t, "0.2.24")
	agentID := createHandlerTestAgent(t, "Quick Create Validation Agent", nil)

	tests := []struct {
		name string
		body map[string]any
		want string
	}{
		{
			name: "invalid priority",
			body: map[string]any{
				"agent_id": agentID,
				"prompt":   "Create something",
				"priority": "none",
			},
			want: "priority must be one of: urgent, high, medium, low",
		},
		{
			name: "invalid due date",
			body: map[string]any{
				"agent_id": agentID,
				"prompt":   "Create something",
				"due_date": "tomorrow",
			},
			want: "invalid due_date format, expected RFC3339",
		},
		{
			name: "invalid project id",
			body: map[string]any{
				"agent_id":   agentID,
				"prompt":     "Create something",
				"project_id": "not-a-uuid",
			},
			want: "invalid project_id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest(http.MethodPost, "/api/issues/quick-create", tc.body)
			testHandler.QuickCreateIssue(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("QuickCreateIssue: expected 400, got %d: %s", w.Code, w.Body.String())
			}
			if body := w.Body.String(); body == "" || !strings.Contains(body, tc.want) {
				t.Fatalf("response body %q does not contain %q", body, tc.want)
			}
		})
	}
}
