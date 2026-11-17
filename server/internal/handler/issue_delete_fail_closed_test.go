package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestDeleteIssueFailsClosedWhenArchiveCannotWrite exercises DC-6 end to end.
// A temporary always-failing NOT VALID constraint makes the archive insert
// reject every new row without validating existing evidence. The delete must
// return an error, roll back its audit/delete transaction, and leave the issue.
func TestDeleteIssueFailsClosedWhenArchiveCannotWrite(t *testing.T) {
	ctx := context.Background()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "Fail closed deletion evidence",
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != 201 {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created issue: %v", err)
	}

	t.Cleanup(func() {
		if _, err := testPool.Exec(ctx, `ALTER TABLE issue_archive DROP CONSTRAINT IF EXISTS tmp_fail_closed`); err != nil {
			t.Errorf("drop temporary fail-closed constraint: %v", err)
		}
	})
	if _, err := testPool.Exec(ctx, `
		ALTER TABLE issue_archive
			ADD CONSTRAINT tmp_fail_closed CHECK (false) NOT VALID
	`); err != nil {
		t.Fatalf("add temporary fail-closed constraint: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/issues/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.DeleteIssue(w, req)
	if w.Code != 500 {
		t.Fatalf("DeleteIssue with failing archive: expected 500, got %d: %s", w.Code, w.Body.String())
	}

	issueCount := 0
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE id = $1`, created.ID).Scan(&issueCount); err != nil {
		t.Fatalf("reload issue after rollback: %v", err)
	}
	if issueCount != 1 {
		t.Fatalf("issue survived count = %d, want 1", issueCount)
	}
	var archiveCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue_archive WHERE issue_id = $1`, created.ID).Scan(&archiveCount); err != nil {
		t.Fatalf("load archive after rollback: %v", err)
	}
	if archiveCount != 0 {
		t.Fatalf("archive rows after rollback = %d, want 0", archiveCount)
	}
}
