package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Preflight independent-review BLOCK items (MUL-4809). Each test reproduces the
// exact production symptom and pins the fix.

// #1 — archive-with-migration must keep status_id and the legacy `status`
// projection in agreement. Migrating a custom in_progress status onto the
// built-in In Review (same Category, different token) previously left status_id
// on In Review while `status` stayed "in_progress": new clients saw In Review,
// old readers saw In Progress — a broken double-write during rollout.
func TestArchiveMigrationKeepsLegacyTokenConsistent(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	custom, code, body := createStatus(t, map[string]any{
		"name": "Custom WIP", "category": "in_progress", "icon": "in_progress", "color": "warning",
	})
	if code != http.StatusCreated {
		t.Fatalf("create custom status: %d %s", code, body)
	}
	issue, code, body := createIssueWithStatusFields(t, map[string]any{
		"title": "archive migrate to in_review", "status_id": custom.ID, "priority": "none",
	})
	if code != http.StatusCreated {
		t.Fatalf("create issue: %d %s", code, body)
	}
	t.Cleanup(func() { deleteTestIssue(t, issue.ID) })

	inReviewID := statusIDForSystemKey(t, "in_review")
	// Archive the custom status, migrating its issues onto the built-in In Review.
	if code, msg := deleteStatus(t, custom.ID, inReviewID); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("archive+migrate: %d %s", code, msg)
	}

	status, statusID := readIssueStatusColumns(t, issue.ID)
	if statusID != inReviewID {
		t.Fatalf("status_id = %q, want migrated In Review %q", statusID, inReviewID)
	}
	// The regression: status_id moved but the legacy token stayed "in_progress".
	if status != "in_review" {
		t.Fatalf("legacy status = %q, want in_review to match status_id (double-write must stay consistent)", status)
	}
}

// #2 — a workspace that upgraded before the one-shot backfill has issues with
// status_id IS NULL. The open_only status filters must still find them via the
// legacy token, and only a BUILT-IN selection may claim such a row.
func TestListOpenIssuesMatchesLegacyNullStatusID(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	todoID := getStatusCatalog(t, false).CategoryDefaults["todo"]
	if todoID == "" {
		t.Fatal("todo default missing")
	}
	custom, code, body := createStatus(t, map[string]any{
		"name": "Custom Todo Lane", "category": "todo", "icon": "todo", "color": "info",
	})
	if code != http.StatusCreated {
		t.Fatalf("create custom status: %d %s", code, body)
	}
	t.Cleanup(func() { deleteStatus(t, custom.ID, "") })

	id := createTestIssue(t, "legacy null-status_id open row", "todo", "none")
	t.Cleanup(func() { deleteTestIssue(t, id) })
	// Simulate a pre-catalog row: legacy token only, no status_id.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET status_id = NULL WHERE id = $1`, id); err != nil {
		t.Fatalf("null out status_id: %v", err)
	}

	has := func(query string) bool {
		issues, code, msg := listIssuesForReadside(t, query)
		if code != http.StatusOK {
			t.Fatalf("list %q: %d %s", query, code, msg)
		}
		for _, i := range issues {
			if i.ID == id {
				return true
			}
		}
		return false
	}

	// Built-in Todo id → the legacy "todo" row must appear (was hidden before).
	if !has("open_only=true&status_ids=" + todoID) {
		t.Fatal("open_only status_ids=<built-in todo> hid the legacy status_id=NULL issue (data-loss regression)")
	}
	// status_category=todo → also appears.
	if !has("open_only=true&status_category=todo") {
		t.Fatal("open_only status_category=todo hid the legacy status_id=NULL issue")
	}
	// A CUSTOM status id must NOT claim a legacy NULL row.
	if has("open_only=true&status_ids=" + custom.ID) {
		t.Fatal("a custom status id wrongly matched a legacy status_id=NULL row")
	}
}

// #3 — the internal Category-alias transition (failed-task reset, PR merge,
// stuck-issue sweep) must resolve to the workspace's CURRENT default status for
// the Category, not the built-in by system_key. A workspace with a custom Todo
// default was silently sent to the built-in Todo, bypassing the configured
// workflow.
func TestUpdateIssueStatusResolvesWorkspaceCategoryDefault(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	custom, code, body := createStatus(t, map[string]any{
		"name": "Backlog Triage", "category": "todo", "icon": "todo", "color": "info", "is_default": true,
	})
	if code != http.StatusCreated {
		t.Fatalf("create custom default: %d %s", code, body)
	}
	// Restore the built-in as default afterward so the shared workspace is clean.
	builtinTodo := statusIDForSystemKey(t, "todo")
	t.Cleanup(func() {
		patchStatus(t, builtinTodo, map[string]any{"is_default": true})
		deleteStatus(t, custom.ID, "")
	})

	id := createTestIssue(t, "auto-reset to custom todo default", "in_progress", "none")
	t.Cleanup(func() { deleteTestIssue(t, id) })

	// This is exactly what task.go / runtime_sweeper.go / github.go call.
	if _, err := testHandler.Queries.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{
		ID:          parseUUID(id),
		Status:      "todo",
		WorkspaceID: parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("UpdateIssueStatus: %v", err)
	}
	status, statusID := readIssueStatusColumns(t, id)
	if statusID != custom.ID {
		t.Fatalf("status_id = %q, want the workspace custom Todo default %q (not the built-in)", statusID, custom.ID)
	}
	if status != "todo" {
		t.Fatalf("legacy status = %q, want todo", status)
	}

	// A legacy alias still resolves to its built-in, not a Category default.
	if _, err := testHandler.Queries.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{
		ID:          parseUUID(id),
		Status:      "in_review",
		WorkspaceID: parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("UpdateIssueStatus in_review: %v", err)
	}
	status, statusID = readIssueStatusColumns(t, id)
	if statusID != statusIDForSystemKey(t, "in_review") || status != "in_review" {
		t.Fatalf("in_review resolved to status_id=%q status=%q, want the built-in In Review", statusID, status)
	}
}

// #4 — the batch issue:updated event must carry the resolved status_id /
// status_detail, like single UpdateIssue. Moving between two custom statuses in
// one Category leaves the legacy token unchanged, so without hydration the
// client keeps rendering the old status name/icon/color: a cache dirty read.
func TestBatchUpdateEventCarriesStatusDetail(t *testing.T) {
	ensureTestWorkspaceStatuses(t)
	a, code, body := createStatus(t, map[string]any{
		"name": "Batch Lane A", "category": "in_progress", "icon": "in_progress", "color": "warning",
	})
	if code != http.StatusCreated {
		t.Fatalf("create A: %d %s", code, body)
	}
	t.Cleanup(func() { deleteStatus(t, a.ID, "") })
	b, code, body := createStatus(t, map[string]any{
		"name": "Batch Lane B", "category": "in_progress", "icon": "in_review", "color": "info",
	})
	if code != http.StatusCreated {
		t.Fatalf("create B: %d %s", code, body)
	}
	t.Cleanup(func() { deleteStatus(t, b.ID, "") })

	issue, code, body := createIssueWithStatusFields(t, map[string]any{
		"title": "batch same-category custom move", "status_id": a.ID, "priority": "none",
	})
	if code != http.StatusCreated {
		t.Fatalf("create issue: %d %s", code, body)
	}
	t.Cleanup(func() { deleteTestIssue(t, issue.ID) })

	captured := make(chan IssueResponse, 4)
	testHandler.Bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		if resp, ok := payload["issue"].(IssueResponse); ok && resp.ID == issue.ID {
			select {
			case captured <- resp:
			default:
			}
		}
	})

	// Batch-move A -> B (same Category, so the legacy token does not change).
	w := httptest.NewRecorder()
	testHandler.BatchUpdateIssues(w, newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issue.ID},
		"updates":   map[string]any{"status_id": b.ID},
	}))
	if w.Code != http.StatusOK {
		t.Fatalf("batch update: %d %s", w.Code, w.Body.String())
	}

	select {
	case resp := <-captured:
		if resp.StatusID == nil || *resp.StatusID != b.ID {
			t.Fatalf("event status_id = %v, want %s", resp.StatusID, b.ID)
		}
		if resp.StatusDetail == nil || resp.StatusDetail.Name != "Batch Lane B" {
			t.Fatalf("event status_detail = %v, want Batch Lane B (unhydrated event = client cache dirty read)", resp.StatusDetail)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no issue:updated event captured for the batch move")
	}
}
