package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func contextReceiptFixture(t *testing.T, label ...string) (issueID, taskID string) {
	t.Helper()
	name := "context-receipt " + strings.Join(label, " ")
	runtimeID := dbfx.Runtime(t, name+" runtime")
	agentID := dbfx.Agent(t, name+" agent", runtimeID)
	issueID = dbfx.Issue(t, name+" issue")
	taskID = dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "running"})
	dbfx.Cleanup(t, `DELETE FROM task_context_receipt WHERE task_id = $1`, taskID)
	return issueID, taskID
}

func sampleContextReceipt() map[string]any {
	return map[string]any{
		"schema_version":  1,
		"runtime":         "kiro",
		"runtime_version": "2.34.0",
		"model":           "claude-opus",
		"prompt_bytes":    2640,
		"brief_bytes":     19022,
		"brief_delivery":  "file",
		"resumed_session": true,
		"mcp_servers":     []string{"github"},
		"sources": []map[string]any{
			{"kind": "runtime_brief", "bytes": 19022, "sha256": "cdc096ed0000", "delivery": "file", "path": "AGENTS.md", "load": "verified_other_version", "verified_version": "2.13.0"},
			{"kind": "skill", "name": "pr-review", "bytes": 19412, "sha256": "e07c21000000", "delivery": "file", "path": ".kiro/skills/pr-review/SKILL.md", "load": "unverified"},
		},
		// A field this server does not know must not reach the column.
		"content": "the full brief text",
	}
}

func reportContextReceipt(t *testing.T, taskID string, receipt any) *testutil.Response {
	t.Helper()
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/context-receipt",
		map[string]any{"receipt": receipt}), "taskId", taskID)
	return testutil.Call(t, testHandler.ReportTaskContextReceipt, req)
}

type contextReceiptResponse struct {
	Receipt    *contextReceipt `json:"receipt"`
	RecordedAt string          `json:"recorded_at"`
}

func getContextReceipt(t *testing.T, taskID, userID string) *testutil.Response {
	t.Helper()
	req := withURLParam(asUser(newRequest(http.MethodGet, "/api/tasks/"+taskID+"/context-receipt", nil), userID), "taskId", taskID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
	return testutil.Call(t, testHandler.GetTaskContextReceipt, req)
}

func TestTaskContextReceipt_ReportThenRead(t *testing.T) {
	issueID, taskID := contextReceiptFixture(t)

	// Before the daemon reports, the reader gets an explicit null.
	var empty contextReceiptResponse
	getContextReceipt(t, taskID, testUserID).Want(http.StatusOK).JSON(&empty)
	if empty.Receipt != nil {
		t.Fatalf("receipt before report = %+v, want null", empty.Receipt)
	}

	reportContextReceipt(t, taskID, sampleContextReceipt()).Want(http.StatusNoContent)

	var got contextReceiptResponse
	getContextReceipt(t, taskID, testUserID).Want(http.StatusOK).JSON(&got)
	if got.Receipt == nil || got.Receipt.Runtime != "kiro" || len(got.Receipt.Sources) != 2 || got.RecordedAt == "" {
		t.Fatalf("receipt = %+v recorded_at=%q", got.Receipt, got.RecordedAt)
	}
	if s := got.Receipt.Sources[0]; s.Load != "verified_other_version" || s.VerifiedVersion != "2.13.0" || s.Path != "AGENTS.md" {
		t.Fatalf("brief row = %+v", s)
	}

	var stored string
	if err := testPool.QueryRow(context.Background(),
		`SELECT receipt::text FROM task_context_receipt WHERE task_id = $1 AND issue_id = $2`, taskID, issueID,
	).Scan(&stored); err != nil {
		t.Fatalf("load stored receipt: %v", err)
	}
	if strings.Contains(stored, "the full brief text") {
		t.Fatalf("unknown field reached storage: %s", stored)
	}

	// A re-report (the fresh-session retry) replaces the receipt in place.
	retry := sampleContextReceipt()
	retry["resumed_session"] = false
	retry["fresh_retry"] = true
	reportContextReceipt(t, taskID, retry).Want(http.StatusNoContent)
	getContextReceipt(t, taskID, testUserID).Want(http.StatusOK).JSON(&got)
	if got.Receipt.ResumedSession || !got.Receipt.FreshRetry {
		t.Fatalf("retry receipt = %+v", got.Receipt)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM task_context_receipt WHERE task_id = $1`, taskID); n != 1 {
		t.Fatalf("rows after re-report = %d, want 1", n)
	}
}

func TestTaskContextReceipt_RejectsMalformed(t *testing.T) {
	_, taskID := contextReceiptFixture(t)

	noRuntime := sampleContextReceipt()
	delete(noRuntime, "runtime")
	reportContextReceipt(t, taskID, noRuntime).Want(http.StatusBadRequest)

	tooLong := sampleContextReceipt()
	tooLong["model"] = strings.Repeat("m", maxContextReceiptString+1)
	reportContextReceipt(t, taskID, tooLong).Want(http.StatusBadRequest)

	negative := sampleContextReceipt()
	negative["sources"] = []map[string]any{{"kind": "skill", "bytes": -1, "delivery": "file", "load": "unverified"}}
	reportContextReceipt(t, taskID, negative).Want(http.StatusBadRequest)

	reportContextReceipt(t, taskID, nil).Want(http.StatusBadRequest)

	if n := dbfx.Count(t, `SELECT count(*) FROM task_context_receipt WHERE task_id = $1`, taskID); n != 0 {
		t.Fatalf("rejected receipts stored %d rows", n)
	}
}

func TestTaskContextReceipt_OtherWorkspaceDaemonCannotReport(t *testing.T) {
	_, taskID := contextReceiptFixture(t)
	req := withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/context-receipt",
		map[string]any{"receipt": sampleContextReceipt()},
		"00000000-0000-0000-0000-000000000000", "attacker-daemon"), "taskId", taskID)
	testutil.Call(t, testHandler.ReportTaskContextReceipt, req).Want(http.StatusNotFound)
	if n := dbfx.Count(t, `SELECT count(*) FROM task_context_receipt WHERE task_id = $1`, taskID); n != 0 {
		t.Fatalf("foreign daemon stored %d rows", n)
	}
}

// The receipt is readable exactly where the run's transcript is: a member who
// cannot see the private project gets the same 404 as for a missing task.
func TestTaskContextReceipt_FollowsRunVisibility(t *testing.T) {
	f := newPrivateProjectFixture(t)
	agentID := seededHandlerTestAgent(t)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": f.secretIssue, "runtime_id": testRuntimeID})
	dbfx.Cleanup(t, `DELETE FROM task_context_receipt WHERE task_id = $1`, taskID)
	reportContextReceipt(t, taskID, sampleContextReceipt()).Want(http.StatusNoContent)

	getContextReceipt(t, taskID, f.outsider).Want(http.StatusNotFound)
	var got contextReceiptResponse
	getContextReceipt(t, taskID, f.insider).Want(http.StatusOK).JSON(&got)
	if got.Receipt == nil {
		t.Fatal("insider should read the receipt")
	}
}

func TestTaskContextReceipt_RemovedWithIssueAndTaskBatch(t *testing.T) {
	issueID, taskID := contextReceiptFixture(t)
	reportContextReceipt(t, taskID, sampleContextReceipt()).Want(http.StatusNoContent)
	if err := testHandler.Queries.DeleteIssue(context.Background(), db.DeleteIssueParams{
		ID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("delete issue: %v", err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM task_context_receipt WHERE task_id = $1`, taskID); n != 0 {
		t.Fatalf("receipt survived issue deletion (%d rows)", n)
	}

	_, batchTask := contextReceiptFixture(t, "batch")
	reportContextReceipt(t, batchTask, sampleContextReceipt()).Want(http.StatusNoContent)
	if err := testHandler.Queries.DeleteTaskBatch(context.Background(), []pgtype.UUID{parseUUID(batchTask)}); err != nil {
		t.Fatalf("delete task batch: %v", err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM task_context_receipt WHERE task_id = $1`, batchTask); n != 0 {
		t.Fatalf("receipt survived task batch deletion (%d rows)", n)
	}
}

// Encoded body sanity: the reader sees mcp_servers / sources as arrays even
// when the daemon omitted them.
func TestTaskContextReceipt_NormalizesEmptyLists(t *testing.T) {
	_, taskID := contextReceiptFixture(t)
	reportContextReceipt(t, taskID, map[string]any{"schema_version": 1, "runtime": "claude"}).Want(http.StatusNoContent)
	rec := getContextReceipt(t, taskID, testUserID).Want(http.StatusOK)
	var raw struct {
		Receipt map[string]json.RawMessage `json:"receipt"`
	}
	rec.JSON(&raw)
	if string(raw.Receipt["mcp_servers"]) != "[]" || string(raw.Receipt["sources"]) != "[]" {
		t.Fatalf("receipt lists = mcp %s sources %s", raw.Receipt["mcp_servers"], raw.Receipt["sources"])
	}
}
