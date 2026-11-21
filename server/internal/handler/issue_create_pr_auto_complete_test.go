package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestCreateIssuePRAutoComplete(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    *bool
		disabled bool
	}{
		{name: "default"},
		{name: "on", value: new(false)},
		{name: "off", value: new(true), disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"title": "PR auto-complete create " + tc.name, "status": "in_progress"}
			if tc.value != nil {
				body["pr_auto_complete_disabled"] = *tc.value
			}
			var result map[string]any
			testutil.Call(t, testHandler.CreateIssue, newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body)).Want(http.StatusCreated).JSON(&result)
			id := result["id"].(string)
			t.Cleanup(func() { deleteTestIssue(t, id) })
			dbfx.Cleanup(t, "DELETE FROM issue_pr_automation WHERE issue_id = $1", id)
			if result["pr_auto_complete_disabled"] != tc.disabled {
				t.Fatalf("create response setting = %v, want %v", result["pr_auto_complete_disabled"], tc.disabled)
			}
			disabled, err := testHandler.Queries.GetIssuePRAutoCompleteDisabled(context.Background(), parseUUID(id))
			if err != nil || disabled != tc.disabled {
				t.Fatalf("stored disabled = %v, err=%v, want %v", disabled, err, tc.disabled)
			}
			var count int
			dbfx.QueryRow(t, `SELECT count(*) FROM activity_log WHERE issue_id = $1 AND action = 'pr_auto_complete_changed' AND actor_type = 'member' AND actor_id = $2 AND details->>'disabled' = 'true'`, id, testUserID).Scan(&count)
			wantCount := 0
			if tc.disabled {
				wantCount = 1
				var actorType, actorID string
				dbfx.QueryRow(t, `SELECT updated_by_type, updated_by_id FROM issue_pr_automation WHERE issue_id = $1 AND workspace_id = $2`, id, testWorkspaceID).Scan(&actorType, &actorID)
				if actorType != "member" || actorID != testUserID {
					t.Fatalf("stored actor = %s:%s", actorType, actorID)
				}
			}
			if count != wantCount {
				t.Fatalf("setting activity count = %d, want %d", count, wantCount)
			}
			var current map[string]any
			req := withURLParam(newRequest("GET", "/api/issues/"+id, nil), "id", id)
			testutil.Call(t, testHandler.GetIssue, req).Want(http.StatusOK).JSON(&current)
			if _, present := current["pr_auto_complete_disabled"]; present || current["status"] != "in_progress" {
				t.Fatalf("ordinary issue read changed: %v", current)
			}
		})
	}
}

func TestCreateIssueRejectsMalformedPRAutoComplete(t *testing.T) {
	for _, value := range []any{"off", 1, []bool{true}} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			title := "invalid PR auto-complete " + fmt.Sprint(value)
			req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{"title": title, "pr_auto_complete_disabled": value})
			testutil.Call(t, testHandler.CreateIssue, req).Want(http.StatusBadRequest)
			if got := countIssuesWithTitle(t, title); got != 0 {
				t.Fatalf("created %d issues for malformed setting", got)
			}
		})
	}
}
