package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateIssue_DefaultAssigneeFallback(t *testing.T) {
	ptr := func(s string) *string { return &s }

	tests := []struct {
		name             string
		settings         map[string]string
		body             map[string]any
		wantAssigneeType *string
		wantAssigneeID   *string
	}{
		{
			name: "uses workspace default when request has no assignee",
			settings: map[string]string{
				"default_assignee_type": "agent",
				"default_assignee_id":   testAgentID,
			},
			body: map[string]any{
				"title": "Default assignee fallback issue",
			},
			wantAssigneeType: ptr("agent"),
			wantAssigneeID:   &testAgentID,
		},
		{
			name: "explicit request assignee wins over workspace default",
			settings: map[string]string{
				"default_assignee_type": "agent",
				"default_assignee_id":   testAgentID,
			},
			body: map[string]any{
				"title":         "Explicit assignee issue",
				"assignee_type": "member",
				"assignee_id":   testUserID,
			},
			wantAssigneeType: ptr("member"),
			wantAssigneeID:   &testUserID,
		},
		{
			name:     "empty workspace settings leaves issue unassigned",
			settings: map[string]string{},
			body: map[string]any{
				"title": "Unassigned issue",
			},
			wantAssigneeType: nil,
			wantAssigneeID:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setWorkspaceSettings(t, tt.settings)
			t.Cleanup(func() {
				setWorkspaceSettings(t, map[string]string{})
			})

			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, tt.body)
			testHandler.CreateIssue(w, req)
			if w.Code != http.StatusCreated {
				t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
			}

			var created IssueResponse
			if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
				t.Fatalf("decode CreateIssue response: %v", err)
			}
			t.Cleanup(func() {
				req := newRequest("DELETE", "/api/issues/"+created.ID, nil)
				req = withURLParam(req, "id", created.ID)
				testHandler.DeleteIssue(httptest.NewRecorder(), req)
			})

			assertOptionalString(t, "assignee_type", created.AssigneeType, tt.wantAssigneeType)
			assertOptionalString(t, "assignee_id", created.AssigneeID, tt.wantAssigneeID)
		})
	}
}

func setWorkspaceSettings(t *testing.T, settings map[string]string) {
	t.Helper()

	rawSettings, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal workspace settings: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE workspace
		SET settings = $1::jsonb
		WHERE id = $2
	`, string(rawSettings), testWorkspaceID); err != nil {
		t.Fatalf("update workspace settings: %v", err)
	}
}

func assertOptionalString(t *testing.T, field string, got, want *string) {
	t.Helper()

	if want == nil {
		if got != nil {
			t.Fatalf("expected %s to be nil, got %q", field, *got)
		}
		return
	}
	if got == nil {
		t.Fatalf("expected %s %q, got nil", field, *want)
	}
	if *got != *want {
		t.Fatalf("expected %s %q, got %q", field, *want, *got)
	}
}
