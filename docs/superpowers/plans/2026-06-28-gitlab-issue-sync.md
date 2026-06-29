# GitLab Issue Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Sync GitLab issues (labeled "agent") and comments bidirectionally with Multica, and show a GitLab issue badge + assignee in the issue detail sidebar.

**Architecture:** New `gitlab_issue` table links GitLab issues to Multica issues; `comment.gitlab_note_id` prevents echo loops. The existing webhook endpoint routes two new event types (`Issue Hook`, `Note Hook`). Multica comment creation triggers a background GitLab API post. A new `GET /api/issues/:id/gitlab-issue` endpoint feeds a read-only frontend badge.

**Tech Stack:** Go (Chi, sqlc, pgx), TypeScript (React, TanStack Query, Zod), PostgreSQL

## Global Constraints

- Migration number 129 (128 was the last: `128_gitlab_integration`)
- `creator_type` in `issue` and `author_type` in `comment` must be `'member'` or `'agent'` (DB CHECK constraint)
- `IssueService.Create` is the only correct way to create issues from webhook handlers (enforces dup guard, broadcasts, analytics)
- Webhook handler always returns 204 regardless of processing errors
- No Multica → GitLab issue creation (out of scope)
- `gitlab_note_id` on `comment` uses the DB unique index as a safety net; the handler also checks it explicitly before creating a comment
- Token post-back is fire-and-forget in a goroutine; `gitlab_note_id` is set after GitLab API responds
- Frontend parses API responses with TanStack Query; 404 from `GET /api/issues/:id/gitlab-issue` is treated as "no linked issue" (returns `null`)

---

### Task 1: DB Migration + SQL Queries + sqlc Regeneration

**Files:**
- Create: `server/migrations/129_gitlab_issue_sync.up.sql`
- Create: `server/migrations/129_gitlab_issue_sync.down.sql`
- Modify: `server/pkg/db/queries/gitlab.sql` (add 4 queries)
- Modify: `server/pkg/db/queries/comment.sql` (add 2 queries)
- Modify: `server/pkg/db/queries/issue.sql` (add 1 query)
- Generated: `server/pkg/db/generated/` (via `make sqlc`)

**Interfaces:**
- Produces: `db.GitlabIssue` struct, `db.Queries.GetGitLabIssueByProjectAndIID`, `db.Queries.GetGitLabIssueByIssueID`, `db.Queries.InsertGitLabIssue`, `db.Queries.UpdateGitLabIssueAssignee`, `db.Queries.GetCommentByGitLabNoteID`, `db.Queries.SetCommentGitLabNoteID`, `db.Queries.UpdateIssueDescription`

- [ ] **Step 1: Write the up migration**

```sql
-- server/migrations/129_gitlab_issue_sync.up.sql
CREATE TABLE gitlab_issue (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    connection_id        UUID NOT NULL REFERENCES gitlab_connection(id) ON DELETE CASCADE,
    project_path         TEXT NOT NULL,
    gl_issue_iid         INTEGER NOT NULL,
    gl_project_id        BIGINT NOT NULL,
    issue_id             UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    gl_assignee_username TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, project_path, gl_issue_iid)
);

CREATE INDEX idx_gitlab_issue_workspace ON gitlab_issue(workspace_id);
CREATE INDEX idx_gitlab_issue_issue ON gitlab_issue(issue_id);

ALTER TABLE comment ADD COLUMN gitlab_note_id BIGINT;
CREATE UNIQUE INDEX idx_comment_gitlab_note ON comment(gitlab_note_id) WHERE gitlab_note_id IS NOT NULL;
```

- [ ] **Step 2: Write the down migration**

```sql
-- server/migrations/129_gitlab_issue_sync.down.sql
DROP TABLE IF EXISTS gitlab_issue;
DROP INDEX IF EXISTS idx_comment_gitlab_note;
ALTER TABLE comment DROP COLUMN IF EXISTS gitlab_note_id;
```

- [ ] **Step 3: Add gitlab_issue queries to `server/pkg/db/queries/gitlab.sql`**

Append at the end of the file:

```sql
-- name: GetGitLabIssueByProjectAndIID :one
SELECT * FROM gitlab_issue
WHERE workspace_id = $1 AND project_path = $2 AND gl_issue_iid = $3;

-- name: GetGitLabIssueByIssueID :one
SELECT * FROM gitlab_issue WHERE issue_id = $1;

-- name: InsertGitLabIssue :one
INSERT INTO gitlab_issue (
    workspace_id, connection_id, project_path, gl_issue_iid,
    gl_project_id, issue_id, gl_assignee_username
) VALUES (
    $1, $2, $3, $4, $5, $6, sqlc.narg('gl_assignee_username')
)
ON CONFLICT (workspace_id, project_path, gl_issue_iid) DO NOTHING
RETURNING *;

-- name: UpdateGitLabIssueAssignee :exec
UPDATE gitlab_issue SET gl_assignee_username = sqlc.narg('gl_assignee_username'), updated_at = now()
WHERE id = $1;
```

- [ ] **Step 4: Add comment queries to `server/pkg/db/queries/comment.sql`**

Append at the end of the file:

```sql
-- name: GetCommentByGitLabNoteID :one
SELECT * FROM comment WHERE gitlab_note_id = $1;

-- name: SetCommentGitLabNoteID :exec
UPDATE comment SET gitlab_note_id = $2 WHERE id = $1;
```

- [ ] **Step 5: Add issue description update query to `server/pkg/db/queries/issue.sql`**

Append after the `UpdateIssueStatus` query (around line 113):

```sql
-- name: UpdateIssueDescription :exec
UPDATE issue SET description = $2, updated_at = now()
WHERE id = $1;
```

- [ ] **Step 6: Run sqlc**

```bash
cd /Users/alex/paral/multica && make sqlc
```

Expected: exits 0, regenerates `server/pkg/db/generated/*.go`

- [ ] **Step 7: Verify Go compiles**

```bash
cd /Users/alex/paral/multica && go build ./server/...
```

Expected: exits 0

- [ ] **Step 8: Commit**

```bash
git add server/migrations/129_gitlab_issue_sync.up.sql \
        server/migrations/129_gitlab_issue_sync.down.sql \
        server/pkg/db/queries/gitlab.sql \
        server/pkg/db/queries/comment.sql \
        server/pkg/db/queries/issue.sql \
        server/pkg/db/generated/
git commit -m "feat(gitlab): add gitlab_issue table, gitlab_note_id on comment, new sqlc queries"
```

---

### Task 2: Issue Hook Handler + Webhook Routing Update

**Files:**
- Modify: `server/internal/handler/gitlab.go`
- Modify: `server/internal/handler/gitlab_test.go`

**Interfaces:**
- Consumes: `db.Queries.GetGitLabIssueByProjectAndIID`, `db.Queries.InsertGitLabIssue`, `db.Queries.UpdateGitLabIssueAssignee`, `db.Queries.UpdateIssueDescription`, `db.Queries.UpdateIssueStatus`, `db.Queries.DeleteIssue`, `db.Queries.GetIssue`, `db.Queries.ListMembers`, `service.IssueService.Create`, `h.advanceIssueToDone`
- Produces: `h.handleGitLabIssueEvent(ctx, body []byte)` — internal webhook handler

- [ ] **Step 1: Write failing test stubs** in `server/internal/handler/gitlab_test.go`

Add to the end of the existing file:

```go
// TestHandleGitLabIssueEvent_LabelAdd tests that an Issue Hook with the
// "agent" label on a new issue creates a Multica issue and gitlab_issue row.
func TestHandleGitLabIssueEvent_LabelAdd(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	// Create a GitLab connection for the test workspace.
	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-issue-add",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: create connection: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	payload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 10, "title": "Sync me", "description": "desc", "action": "open"},
		"project": {"id": 99, "path_with_namespace": "testorg-issue-add/repo", "namespace": "testorg-issue-add"},
		"labels": [{"title": "agent"}],
		"assignees": []
	}`
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(payload))
	req.Header.Set("X-Gitlab-Token", "s")
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	w := httptest.NewRecorder()
	testHandler.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	// Verify gitlab_issue row was created.
	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID,
		ProjectPath: "testorg-issue-add/repo",
		GlIssueIid:  10,
	})
	if err != nil {
		t.Fatalf("gitlab_issue not created: %v", err)
	}
	issue, err := testHandler.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		t.Fatalf("multica issue not created: %v", err)
	}
	if issue.Title != "Sync me" {
		t.Errorf("title: got %q, want %q", issue.Title, "Sync me")
	}
}

// TestHandleGitLabIssueEvent_LabelRemove tests that removing the "agent" label
// deletes the linked Multica issue and gitlab_issue row.
func TestHandleGitLabIssueEvent_LabelRemove(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-issue-remove",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Seed: create issue+row via the add path.
	addPayload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 11, "title": "Remove me", "description": "", "action": "open"},
		"project": {"id": 100, "path_with_namespace": "testorg-issue-remove/repo", "namespace": "testorg-issue-remove"},
		"labels": [{"title": "agent"}],
		"assignees": []
	}`
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	addReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(addPayload))
	addReq.Header.Set("X-Gitlab-Token", "s")
	addReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), addReq)

	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-issue-remove/repo", GlIssueIid: 11,
	})
	if err != nil {
		t.Fatalf("seed: gitlab_issue not found: %v", err)
	}
	issueID := row.IssueID

	// Remove label.
	removePayload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 11, "title": "Remove me", "description": "", "action": "update"},
		"project": {"id": 100, "path_with_namespace": "testorg-issue-remove/repo", "namespace": "testorg-issue-remove"},
		"labels": [],
		"assignees": []
	}`
	removeReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(removePayload))
	removeReq.Header.Set("X-Gitlab-Token", "s")
	removeReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), removeReq)

	// Multica issue should be deleted.
	if _, err := testHandler.Queries.GetIssue(ctx, issueID); err == nil {
		t.Error("multica issue should have been deleted")
	}
}

// TestHandleGitLabIssueEvent_Close tests that action=close marks the issue Done.
func TestHandleGitLabIssueEvent_Close(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-issue-close",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Seed.
	addPayload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 12, "title": "Close me", "description": "", "action": "open"},
		"project": {"id": 101, "path_with_namespace": "testorg-issue-close/repo", "namespace": "testorg-issue-close"},
		"labels": [{"title": "agent"}],
		"assignees": []
	}`
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	addReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(addPayload))
	addReq.Header.Set("X-Gitlab-Token", "s")
	addReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), addReq)

	row, _ := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-issue-close/repo", GlIssueIid: 12,
	})

	// Close.
	closePayload := `{
		"object_kind": "issue",
		"object_attributes": {"iid": 12, "title": "Close me", "description": "", "action": "close"},
		"project": {"id": 101, "path_with_namespace": "testorg-issue-close/repo", "namespace": "testorg-issue-close"},
		"labels": [{"title": "agent"}],
		"assignees": []
	}`
	closeReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(closePayload))
	closeReq.Header.Set("X-Gitlab-Token", "s")
	closeReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), closeReq)

	issue, err := testHandler.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	if issue.Status != "done" {
		t.Errorf("status: got %q, want %q", issue.Status, "done")
	}
}

// TestHandleGitLabIssueEvent_Reopen tests that action=reopen marks the issue In Progress.
func TestHandleGitLabIssueEvent_Reopen(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-issue-reopen",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Seed + close.
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	for _, payload := range []string{
		`{"object_kind":"issue","object_attributes":{"iid":13,"title":"Reopen me","description":"","action":"open"},"project":{"id":102,"path_with_namespace":"testorg-issue-reopen/repo","namespace":"testorg-issue-reopen"},"labels":[{"title":"agent"}],"assignees":[]}`,
		`{"object_kind":"issue","object_attributes":{"iid":13,"title":"Reopen me","description":"","action":"close"},"project":{"id":102,"path_with_namespace":"testorg-issue-reopen/repo","namespace":"testorg-issue-reopen"},"labels":[{"title":"agent"}],"assignees":[]}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(payload))
		req.Header.Set("X-Gitlab-Token", "s")
		req.Header.Set("X-Gitlab-Event", "Issue Hook")
		testHandler.HandleGitLabWebhook(httptest.NewRecorder(), req)
	}

	row, _ := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-issue-reopen/repo", GlIssueIid: 13,
	})

	// Reopen.
	reopenPayload := `{"object_kind":"issue","object_attributes":{"iid":13,"title":"Reopen me","description":"","action":"reopen"},"project":{"id":102,"path_with_namespace":"testorg-issue-reopen/repo","namespace":"testorg-issue-reopen"},"labels":[{"title":"agent"}],"assignees":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(reopenPayload))
	req.Header.Set("X-Gitlab-Token", "s")
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), req)

	issue, _ := testHandler.Queries.GetIssue(ctx, row.IssueID)
	if issue.Status != "in_progress" {
		t.Errorf("status: got %q, want %q", issue.Status, "in_progress")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/alex/paral/multica && go test ./server/internal/handler/ -run "TestHandleGitLabIssueEvent" -v 2>&1 | tail -20
```

Expected: compile error (handleGitLabIssueEvent not defined yet) OR test failures

- [ ] **Step 3: Add payload structs and `handleGitLabIssueEvent` to `server/internal/handler/gitlab.go`**

Add the following after the existing `gitlabMRPayload` struct (around line 202):

```go
// gitlabIssuePayload is the subset of GitLab's Issue Hook webhook we consume.
type gitlabIssuePayload struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		IID         int32  `json:"iid"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Action      string `json:"action"`
	} `json:"object_attributes"`
	Project struct {
		ID                int64  `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		Namespace         string `json:"namespace"`
	} `json:"project"`
	Labels []struct {
		Title string `json:"title"`
	} `json:"labels"`
	Assignees []struct {
		Username string `json:"username"`
	} `json:"assignees"`
}

// containsLabel reports whether labels slice contains a label with the given title.
func containsLabel(labels []struct{ Title string `json:"title"` }, title string) bool {
	for _, l := range labels {
		if l.Title == title {
			return true
		}
	}
	return false
}

func (h *Handler) handleGitLabIssueEvent(ctx context.Context, body []byte) {
	var p gitlabIssuePayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Error("gitlab: failed to parse issue payload", "err", err)
		return
	}

	namespace := p.Project.Namespace
	projectPath := p.Project.PathWithNamespace
	action := p.ObjectAttributes.Action

	conn, err := h.resolveGitLabConnectionByNamespace(ctx, namespace)
	if err != nil {
		slog.Warn("gitlab: no connection for namespace", "namespace", namespace)
		return
	}
	workspaceID := uuidToString(conn.WorkspaceID)

	hasAgent := containsLabel(p.Labels, "agent")

	assigneeUsername := ""
	if len(p.Assignees) > 0 {
		assigneeUsername = p.Assignees[0].Username
	}

	// Look up existing gitlab_issue row.
	row, rowErr := h.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: conn.WorkspaceID,
		ProjectPath: projectPath,
		GlIssueIid:  p.ObjectAttributes.IID,
	})
	rowExists := rowErr == nil

	// Agent label removed while row exists → delete Multica issue (cascade deletes row).
	if !hasAgent && rowExists {
		if err := h.Queries.DeleteIssue(ctx, db.DeleteIssueParams{
			ID:          row.IssueID,
			WorkspaceID: conn.WorkspaceID,
		}); err != nil {
			slog.Error("gitlab: failed to delete issue on label removal", "err", err)
			return
		}
		h.publish(protocol.EventIssueDeleted, workspaceID, "system", "", map[string]any{
			"issue_id": uuidToString(row.IssueID),
		})
		return
	}

	if hasAgent {
		if !rowExists && (action == "open" || action == "update") {
			// Create Multica issue.
			creatorID, ok := h.gitlabCreatorID(ctx, conn)
			if !ok {
				slog.Error("gitlab: no creator available, skipping issue creation", "workspace", workspaceID)
				return
			}

			res, err := h.IssueService.Create(ctx, service.IssueCreateParams{
				WorkspaceID: conn.WorkspaceID,
				Title:       p.ObjectAttributes.Title,
				Description: pgtype.Text{String: p.ObjectAttributes.Description, Valid: p.ObjectAttributes.Description != ""},
				Status:      "todo",
				Priority:    "none",
				CreatorType: "member",
				CreatorID:   creatorID,
				AllowDuplicate: true,
			}, service.IssueCreateOpts{})
			if err != nil {
				slog.Error("gitlab: failed to create issue", "err", err)
				return
			}

			glRow, err := h.Queries.InsertGitLabIssue(ctx, db.InsertGitLabIssueParams{
				WorkspaceID:       conn.WorkspaceID,
				ConnectionID:      conn.ID,
				ProjectPath:       projectPath,
				GlIssueIid:        p.ObjectAttributes.IID,
				GlProjectId:       p.Project.ID,
				IssueID:           res.Issue.ID,
				GlAssigneeUsername: pgtype.Text{String: assigneeUsername, Valid: assigneeUsername != ""},
			})
			if err != nil {
				slog.Error("gitlab: failed to insert gitlab_issue row", "err", err)
				return
			}
			row = glRow
			rowExists = true

		} else if rowExists {
			// Sync description.
			if err := h.Queries.UpdateIssueDescription(ctx, db.UpdateIssueDescriptionParams{
				ID:          row.IssueID,
				Description: pgtype.Text{String: p.ObjectAttributes.Description, Valid: p.ObjectAttributes.Description != ""},
			}); err != nil {
				slog.Warn("gitlab: failed to sync description", "err", err)
			}
			// Sync assignee.
			if err := h.Queries.UpdateGitLabIssueAssignee(ctx, db.UpdateGitLabIssueAssigneeParams{
				ID:                 row.ID,
				GlAssigneeUsername: pgtype.Text{String: assigneeUsername, Valid: assigneeUsername != ""},
			}); err != nil {
				slog.Warn("gitlab: failed to sync assignee", "err", err)
			}
		}
	}

	// Status transitions — applied after the create/sync block.
	if rowExists {
		issue, err := h.Queries.GetIssue(ctx, row.IssueID)
		if err != nil {
			slog.Warn("gitlab: issue not found for status transition", "issue_id", uuidToString(row.IssueID))
			return
		}
		switch action {
		case "close":
			h.advanceIssueToDone(ctx, issue, workspaceID, "gitlab_issue_closed")
		case "reopen":
			updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
				ID:          issue.ID,
				Status:      "in_progress",
				WorkspaceID: issue.WorkspaceID,
			})
			if err != nil {
				slog.Warn("gitlab: failed to reopen issue", "err", err)
				return
			}
			prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
			h.publish(protocol.EventIssueUpdated, workspaceID, "system", "", map[string]any{
				"issue":          issueToResponse(updated, prefix),
				"status_changed": true,
				"prev_status":    issue.Status,
				"source":         "gitlab_issue_reopened",
			})
		}
	}
}

// gitlabCreatorID returns the user UUID to use as creator for webhook-triggered
// issue/comment creation. Prefers the connection's connected_by_id; falls back
// to the first workspace member.
func (h *Handler) gitlabCreatorID(ctx context.Context, conn db.GitlabConnection) (pgtype.UUID, bool) {
	if conn.ConnectedByID.Valid {
		return conn.ConnectedByID, true
	}
	members, err := h.Queries.ListMembers(ctx, conn.WorkspaceID)
	if err != nil || len(members) == 0 {
		return pgtype.UUID{}, false
	}
	return members[0].UserID, true
}
```

- [ ] **Step 4: Update `HandleGitLabWebhook` to route Issue Hook events**

In `HandleGitLabWebhook` (around line 169), replace:

```go
	if r.Header.Get("X-Gitlab-Event") != "Merge Request Hook" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.handleGitLabMergeRequestEvent(r.Context(), body)
```

With:

```go
	switch r.Header.Get("X-Gitlab-Event") {
	case "Merge Request Hook":
		h.handleGitLabMergeRequestEvent(r.Context(), body)
	case "Issue Hook":
		h.handleGitLabIssueEvent(r.Context(), body)
	}
```

- [ ] **Step 5: Add missing imports to `gitlab.go`**

Ensure the import block includes `service` and `pgtype`:

```go
import (
    // existing imports ...
    "github.com/multica-ai/multica/server/internal/service"
    "github.com/jackc/pgx/v5/pgtype"
)
```

(Both are likely already imported; verify and add only missing ones.)

- [ ] **Step 6: Run the failing tests**

```bash
cd /Users/alex/paral/multica && go test ./server/internal/handler/ -run "TestHandleGitLabIssueEvent" -v 2>&1 | tail -30
```

Expected: PASS for all four tests (requires DB)

- [ ] **Step 7: Run the full handler test suite to catch regressions**

```bash
cd /Users/alex/paral/multica && go test ./server/internal/handler/ -count=1 2>&1 | tail -20
```

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add server/internal/handler/gitlab.go server/internal/handler/gitlab_test.go
git commit -m "feat(gitlab): handle Issue Hook — label sync, status transitions, issue lifecycle"
```

---

### Task 3: Note Hook Handler (GitLab → Multica Comments)

**Files:**
- Modify: `server/internal/handler/gitlab.go`
- Modify: `server/internal/handler/gitlab_test.go`

**Interfaces:**
- Consumes: `db.Queries.GetGitLabIssueByProjectAndIID`, `db.Queries.GetCommentByGitLabNoteID`, `db.Queries.CreateComment`, `db.Queries.SetCommentGitLabNoteID`, `h.gitlabCreatorID`
- Produces: `h.handleGitLabNoteEvent(ctx, body []byte)` — internal webhook handler

- [ ] **Step 1: Write failing tests** in `server/internal/handler/gitlab_test.go`

Append at the end:

```go
// TestHandleGitLabNoteEvent_CreatesComment tests that a Note Hook creates a
// Multica comment prefixed with the GitLab username.
func TestHandleGitLabNoteEvent_CreatesComment(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-note-create",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Create an issue via Issue Hook first.
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	issuePayload := `{"object_kind":"issue","object_attributes":{"iid":20,"title":"Note target","description":"","action":"open"},"project":{"id":200,"path_with_namespace":"testorg-note-create/repo","namespace":"testorg-note-create"},"labels":[{"title":"agent"}],"assignees":[]}`
	issueReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(issuePayload))
	issueReq.Header.Set("X-Gitlab-Token", "s")
	issueReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), issueReq)

	// Fire Note Hook.
	notePayload := `{
		"object_kind": "note",
		"object_attributes": {
			"notable_type": "Issue",
			"system": false,
			"id": 777,
			"note": "Hello from GitLab"
		},
		"project": {"path_with_namespace": "testorg-note-create/repo", "namespace": "testorg-note-create"},
		"issue": {"iid": 20},
		"user": {"username": "gitlabuser"}
	}`
	noteReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(notePayload))
	noteReq.Header.Set("X-Gitlab-Token", "s")
	noteReq.Header.Set("X-Gitlab-Event", "Note Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), noteReq)

	// Verify comment exists with gitlab_note_id set.
	comment, err := testHandler.Queries.GetCommentByGitLabNoteID(ctx, pgtype.Int8{Int64: 777, Valid: true})
	if err != nil {
		t.Fatalf("comment with gitlab_note_id not found: %v", err)
	}
	if !strings.Contains(comment.Content, "**gitlabuser** (GitLab)") {
		t.Errorf("content attribution missing: %q", comment.Content)
	}
	if !strings.Contains(comment.Content, "Hello from GitLab") {
		t.Errorf("comment body missing: %q", comment.Content)
	}
}

// TestHandleGitLabNoteEvent_DuplicateSkipped tests that a Note Hook with an
// already-seen gitlab_note_id is silently skipped (echo loop prevention).
func TestHandleGitLabNoteEvent_DuplicateSkipped(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-note-dup",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Create issue.
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	issuePayload := `{"object_kind":"issue","object_attributes":{"iid":21,"title":"Dup note target","description":"","action":"open"},"project":{"id":201,"path_with_namespace":"testorg-note-dup/repo","namespace":"testorg-note-dup"},"labels":[{"title":"agent"}],"assignees":[]}`
	issueReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(issuePayload))
	issueReq.Header.Set("X-Gitlab-Token", "s")
	issueReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), issueReq)

	notePayload := `{"object_kind":"note","object_attributes":{"notable_type":"Issue","system":false,"id":888,"note":"Once"},"project":{"path_with_namespace":"testorg-note-dup/repo","namespace":"testorg-note-dup"},"issue":{"iid":21},"user":{"username":"u"}}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(notePayload))
		req.Header.Set("X-Gitlab-Token", "s")
		req.Header.Set("X-Gitlab-Event", "Note Hook")
		testHandler.HandleGitLabWebhook(httptest.NewRecorder(), req)
	}

	// Only one comment should exist for note_id 888.
	// The unique index enforces this; verify via direct query.
	var count int
	err = testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE gitlab_note_id = 888`).Scan(&count)
	if err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 comment for note 888, got %d", count)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/alex/paral/multica && go test ./server/internal/handler/ -run "TestHandleGitLabNoteEvent" -v 2>&1 | tail -10
```

Expected: compile error or test failures

- [ ] **Step 3: Add `gitlabNotePayload` struct and `handleGitLabNoteEvent` to `server/internal/handler/gitlab.go`**

Add after the `gitlabIssuePayload` struct:

```go
// gitlabNotePayload is the subset of GitLab's Note Hook webhook we consume.
type gitlabNotePayload struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		NotableType string `json:"notable_type"`
		System      bool   `json:"system"`
		ID          int64  `json:"id"`
		Note        string `json:"note"`
	} `json:"object_attributes"`
	Project struct {
		PathWithNamespace string `json:"path_with_namespace"`
		Namespace         string `json:"namespace"`
	} `json:"project"`
	Issue struct {
		IID int32 `json:"iid"`
	} `json:"issue"`
	User struct {
		Username string `json:"username"`
	} `json:"user"`
}

func (h *Handler) handleGitLabNoteEvent(ctx context.Context, body []byte) {
	var p gitlabNotePayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Error("gitlab: failed to parse note payload", "err", err)
		return
	}

	// Only handle issue comments; skip system notes.
	if p.ObjectAttributes.NotableType != "Issue" || p.ObjectAttributes.System {
		return
	}

	namespace := p.Project.Namespace
	projectPath := p.Project.PathWithNamespace

	conn, err := h.resolveGitLabConnectionByNamespace(ctx, namespace)
	if err != nil {
		slog.Warn("gitlab: no connection for namespace", "namespace", namespace)
		return
	}

	// Find the linked gitlab_issue row.
	row, err := h.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: conn.WorkspaceID,
		ProjectPath: projectPath,
		GlIssueIid:  p.Issue.IID,
	})
	if err != nil {
		// Issue not synced — skip.
		return
	}

	noteID := pgtype.Int8{Int64: p.ObjectAttributes.ID, Valid: true}

	// Echo prevention: skip if this note_id already exists.
	if _, err := h.Queries.GetCommentByGitLabNoteID(ctx, noteID); err == nil {
		return
	}

	// Build attributed content.
	content := "**" + p.User.Username + "** (GitLab):\n" + p.ObjectAttributes.Note

	// Resolve creator for author fields.
	authorID, ok := h.gitlabCreatorID(ctx, conn)
	if !ok {
		slog.Error("gitlab: no author available for note comment", "workspace", uuidToString(conn.WorkspaceID))
		return
	}

	issue, err := h.Queries.GetIssue(ctx, row.IssueID)
	if err != nil {
		slog.Warn("gitlab: issue not found for note", "issue_id", uuidToString(row.IssueID))
		return
	}

	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "member",
		AuthorID:    authorID,
		Content:     content,
		Type:        "comment",
	})
	if err != nil {
		slog.Error("gitlab: failed to create comment from note", "err", err)
		return
	}

	// Store the note_id for echo loop prevention.
	if err := h.Queries.SetCommentGitLabNoteID(ctx, db.SetCommentGitLabNoteIDParams{
		ID:           comment.ID,
		GithubNoteId: noteID,
	}); err != nil {
		slog.Warn("gitlab: failed to set gitlab_note_id on comment", "err", err)
	}
}
```

**Note on the SetCommentGitLabNoteID params:** After running `make sqlc` in Task 1, check the generated struct name for the `gitlab_note_id` field. sqlc generates field names from column names. The field will be `GlNoteId` or `GitlabNoteId` — match the generated struct. Similarly, the `GetCommentByGitLabNoteID` param type will match the generated `pgtype.Int8`. Adjust field names to match the generated code.

- [ ] **Step 4: Update `HandleGitLabWebhook` to route Note Hook**

The switch added in Task 2 already has the structure. Add:

```go
	switch r.Header.Get("X-Gitlab-Event") {
	case "Merge Request Hook":
		h.handleGitLabMergeRequestEvent(r.Context(), body)
	case "Issue Hook":
		h.handleGitLabIssueEvent(r.Context(), body)
	case "Note Hook":
		h.handleGitLabNoteEvent(r.Context(), body)
	}
```

- [ ] **Step 5: Run note hook tests**

```bash
cd /Users/alex/paral/multica && go test ./server/internal/handler/ -run "TestHandleGitLabNoteEvent" -v 2>&1 | tail -20
```

Expected: PASS

- [ ] **Step 6: Run full handler suite**

```bash
cd /Users/alex/paral/multica && go test ./server/internal/handler/ -count=1 2>&1 | tail -5
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add server/internal/handler/gitlab.go server/internal/handler/gitlab_test.go
git commit -m "feat(gitlab): handle Note Hook — GitLab→Multica comment sync with echo prevention"
```

---

### Task 4: Multica → GitLab Comment Posting

**Files:**
- Modify: `server/internal/handler/gitlab.go` (add `postCommentToGitLab`)
- Modify: `server/internal/handler/comment.go` (call helper after comment saved)
- Modify: `server/internal/handler/gitlab_test.go` (echo loop test)

**Interfaces:**
- Consumes: `db.Queries.GetGitLabIssueByIssueID`, `db.Queries.GetGitLabConnectionByID`, `db.Queries.SetCommentGitLabNoteID`, `h.GitLabBox`
- Produces: `h.postCommentToGitLab(ctx, comment db.Comment, issue db.Issue)` — fire-and-forget, called from `CreateComment`

- [ ] **Step 1: Write the echo loop test** in `server/internal/handler/gitlab_test.go`

Append:

```go
// TestPostCommentToGitLab_EchoLoopPrevention tests that a comment posted from
// Multica to GitLab gets a gitlab_note_id, and a subsequent Note Hook with
// that same ID is a no-op. This test mocks the GitLab API server.
func TestPostCommentToGitLab_EchoLoopPrevention(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	// Start a fake GitLab API server that records calls and returns a note ID.
	var apiCalled int
	fakeGitLab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalled++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 9999}`))
	}))
	defer fakeGitLab.Close()
	t.Setenv("GITLAB_URL", fakeGitLab.URL)
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)

	// Create connection (no real token needed; fake server accepts anything).
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-echo",
		NamespaceType: "group",
		AccessToken:   "dummytoken",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Create issue via Issue Hook.
	issuePayload := `{"object_kind":"issue","object_attributes":{"iid":30,"title":"Echo test","description":"","action":"open"},"project":{"id":300,"path_with_namespace":"testorg-echo/repo","namespace":"testorg-echo"},"labels":[{"title":"agent"}],"assignees":[]}`
	issueReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(issuePayload))
	issueReq.Header.Set("X-Gitlab-Token", "s")
	issueReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), issueReq)

	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-echo/repo", GlIssueIid: 30,
	})
	if err != nil {
		t.Fatalf("gitlab_issue not found after seed: %v", err)
	}

	// Directly create a comment and call postCommentToGitLab.
	issue, _ := testHandler.Queries.GetIssue(ctx, row.IssueID)
	comment, err := testHandler.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "member",
		AuthorID:    userUUID,
		Content:     "Hello from Multica",
		Type:        "comment",
	})
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}

	// postCommentToGitLab runs inline (not goroutine) for testability.
	testHandler.postCommentToGitLab(ctx, comment, issue)

	// Verify gitlab_note_id was set.
	updatedComment, err := testHandler.Queries.GetCommentByGitLabNoteID(ctx, pgtype.Int8{Int64: 9999, Valid: true})
	if err != nil {
		t.Fatalf("gitlab_note_id not set on comment: %v", err)
	}
	if uuidToString(updatedComment.ID) != uuidToString(comment.ID) {
		t.Error("wrong comment has gitlab_note_id")
	}

	// Now fire Note Hook with the same note ID — should be skipped.
	notePayload := `{"object_kind":"note","object_attributes":{"notable_type":"Issue","system":false,"id":9999,"note":"Hello from Multica"},"project":{"path_with_namespace":"testorg-echo/repo","namespace":"testorg-echo"},"issue":{"iid":30},"user":{"username":"someone"}}`
	noteReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(notePayload))
	noteReq.Header.Set("X-Gitlab-Token", "s")
	noteReq.Header.Set("X-Gitlab-Event", "Note Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), noteReq)

	// Only one comment should exist for this issue.
	var count int
	testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, issue.ID).Scan(&count)
	if count != 1 {
		t.Errorf("echo loop: expected 1 comment, got %d", count)
	}
}
```

- [ ] **Step 2: Run test to confirm failure**

```bash
cd /Users/alex/paral/multica && go test ./server/internal/handler/ -run "TestPostCommentToGitLab" -v 2>&1 | tail -10
```

Expected: compile error (postCommentToGitLab not defined yet)

- [ ] **Step 3: Add `postCommentToGitLab` to `server/internal/handler/gitlab.go`**

Add after `handleGitLabNoteEvent`:

```go
// postCommentToGitLab posts a newly-created Multica comment to the linked
// GitLab issue via the API, then stores the returned note ID on the comment
// row for echo loop prevention. It is called as a goroutine from CreateComment.
func (h *Handler) postCommentToGitLab(ctx context.Context, comment db.Comment, issue db.Issue) {
	// Look up the gitlab_issue link.
	glIssue, err := h.Queries.GetGitLabIssueByIssueID(ctx, issue.ID)
	if err != nil {
		// Not a synced issue — nothing to do.
		return
	}

	// Load the connection for the access token.
	conn, err := h.Queries.GetGitLabConnectionByID(ctx, glIssue.ConnectionID)
	if err != nil {
		slog.Warn("gitlab: connection not found for comment post", "connection_id", uuidToString(glIssue.ConnectionID))
		return
	}

	// Check token expiry.
	if conn.TokenExpiresAt.Valid && conn.TokenExpiresAt.Time.Before(time.Now()) {
		slog.Warn("gitlab: access token expired, skipping comment post",
			"connection_id", uuidToString(conn.ID),
			"expired_at", conn.TokenExpiresAt.Time)
		return
	}

	// Decrypt token.
	if h.GitLabBox == nil {
		return
	}
	tokenBytes, err := base64.StdEncoding.DecodeString(conn.AccessToken)
	if err != nil {
		slog.Error("gitlab: failed to base64-decode token", "err", err)
		return
	}
	plainToken, err := h.GitLabBox.Open(tokenBytes)
	if err != nil {
		slog.Error("gitlab: failed to decrypt token", "err", err)
		return
	}

	// POST to GitLab notes API.
	apiURL := gitlabAPIURL() + fmt.Sprintf("/projects/%d/issues/%d/notes",
		glIssue.GlProjectId, glIssue.GlIssueIid)
	body, _ := json.Marshal(map[string]string{"body": comment.Content})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		slog.Error("gitlab: failed to build note request", "err", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+string(plainToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("gitlab: failed to post note", "err", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Error("gitlab: note post returned error status", "status", resp.StatusCode)
		return
	}

	var noteResp struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&noteResp); err != nil || noteResp.ID == 0 {
		slog.Error("gitlab: failed to decode note response", "err", err)
		return
	}

	// Store note_id on comment for echo loop prevention.
	if err := h.Queries.SetCommentGitLabNoteID(ctx, db.SetCommentGitLabNoteIDParams{
		ID:           comment.ID,
		GithubNoteId: pgtype.Int8{Int64: noteResp.ID, Valid: true},
	}); err != nil {
		slog.Warn("gitlab: failed to store gitlab_note_id", "err", err)
	}
}
```

**Note:** Replace `GithubNoteId` with the actual field name generated by sqlc (will be something like `GitlabNoteId`). Check `server/pkg/db/generated/comment.sql.go` after Task 1.

Also add `"bytes"` and `"fmt"` to the import block if not already present.

- [ ] **Step 4: Call `postCommentToGitLab` from `CreateComment` in `server/internal/handler/comment.go`**

After the comment is saved and before the function returns (around line 1253, after the `slog.Info("comment created"...)`), add:

```go
	// Post comment to GitLab if the issue is synced — fire and forget.
	go h.postCommentToGitLab(context.Background(), comment, issue)
```

Add `"context"` to the import block of `comment.go` if not already present.

- [ ] **Step 5: Run the echo loop test**

```bash
cd /Users/alex/paral/multica && go test ./server/internal/handler/ -run "TestPostCommentToGitLab" -v 2>&1 | tail -20
```

Expected: PASS

**Note:** The test calls `postCommentToGitLab` directly (not via goroutine) so it runs synchronously. This is intentional — goroutine is wired in `CreateComment`, but direct call is testable.

- [ ] **Step 6: Run the GitLab access token path without GitLabBox**

The `postCommentToGitLab` silently returns when `h.GitLabBox == nil` (which is the case in most tests since box requires `GITLAB_SECRET_KEY`). The echo test uses the fake server and sets `GITLAB_URL` but still needs decryption to work. For the test, use a dummy access token stored as plaintext in the connection AND mock or skip the GitLabBox check.

**Alternative for the echo test:** Rather than decryption, store the token without encryption (the fake server doesn't validate the token). To bypass decryption in tests, either:
(a) Add a `testGitLabBox` to the test handler that wraps a no-op cipher, or
(b) Check in `postCommentToGitLab` that if `GitLabBox == nil` AND the URL is the fake server, skip decryption.

The simplest option: make `postCommentToGitLab` use the raw token string if decryption fails gracefully. OR: for the echo test, initialize `testHandler.GitLabBox` with a test key.

**Recommended approach:** In the test, store the access token as a base64-encoded plaintext (pretend it's sealed). Add a test-only `secretbox` that round-trips the token through base64 only. Actually, the simplest is to just initialize a real secretbox in the test:

```go
// In TestPostCommentToGitLab_EchoLoopPrevention, add before creating connection:
import "github.com/multica-ai/multica/server/internal/secretbox"
// ...
key := make([]byte, 32)
rand.Read(key)
box, _ := secretbox.New(key)
testHandler.GitLabBox = box
// Store a sealed "dummytoken" as access_token:
sealed, _ := box.Seal([]byte("dummytoken"))
accessToken := base64.StdEncoding.EncodeToString(sealed)
// Use accessToken in CreateGitLabConnection call.
```

Update the test accordingly to use a real sealed token so decryption succeeds.

- [ ] **Step 7: Run full handler suite**

```bash
cd /Users/alex/paral/multica && go test ./server/internal/handler/ -count=1 2>&1 | tail -5
```

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add server/internal/handler/gitlab.go server/internal/handler/comment.go server/internal/handler/gitlab_test.go
git commit -m "feat(gitlab): post Multica comments to GitLab API with echo loop prevention"
```

---

### Task 5: GET /api/issues/:id/gitlab-issue Endpoint

**Files:**
- Modify: `server/internal/handler/gitlab.go` (add `GetGitLabIssueForIssue` handler + `GitLabIssueResponse` type)
- Modify: `server/cmd/server/router.go` (add route)
- Modify: `server/internal/handler/gitlab_test.go` (add test)

**Interfaces:**
- Consumes: `db.Queries.GetGitLabIssueByIssueID`
- Produces: `GET /api/issues/{id}/gitlab-issue` → `GitLabIssueResponse` JSON or `404`

- [ ] **Step 1: Write the failing test** in `server/internal/handler/gitlab_test.go`

Append:

```go
// TestGetGitLabIssueForIssue tests the GET /api/issues/:id/gitlab-issue endpoint.
func TestGetGitLabIssueForIssue(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}
	ctx := context.Background()

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)
	conn, err := testHandler.Queries.CreateGitLabConnection(ctx, db.CreateGitLabConnectionParams{
		WorkspaceID:   wsUUID,
		Namespace:     "testorg-get-issue",
		NamespaceType: "group",
		AccessToken:   "dummy",
		ConnectedByID: userUUID,
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteGitLabConnection(ctx, db.DeleteGitLabConnectionParams{
			ID: conn.ID, WorkspaceID: wsUUID,
		})
	})

	// Create issue via webhook.
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	issuePayload := `{"object_kind":"issue","object_attributes":{"iid":40,"title":"Get me","description":"","action":"open"},"project":{"id":400,"path_with_namespace":"testorg-get-issue/repo","namespace":"testorg-get-issue"},"labels":[{"title":"agent"}],"assignees":[{"username":"getuser"}]}`
	issueReq := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader(issuePayload))
	issueReq.Header.Set("X-Gitlab-Token", "s")
	issueReq.Header.Set("X-Gitlab-Event", "Issue Hook")
	testHandler.HandleGitLabWebhook(httptest.NewRecorder(), issueReq)

	row, err := testHandler.Queries.GetGitLabIssueByProjectAndIID(ctx, db.GetGitLabIssueByProjectAndIIDParams{
		WorkspaceID: wsUUID, ProjectPath: "testorg-get-issue/repo", GlIssueIid: 40,
	})
	if err != nil {
		t.Fatalf("seed issue not found: %v", err)
	}
	issueIDStr := uuidToString(row.IssueID)

	// GET /api/issues/:id/gitlab-issue
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", issueIDStr)
	req := httptest.NewRequest(http.MethodGet, "/api/issues/"+issueIDStr+"/gitlab-issue", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.GetGitLabIssueForIssue(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		GlIssueIID         int    `json:"gl_issue_iid"`
		ProjectPath        string `json:"project_path"`
		URL                string `json:"url"`
		GlAssigneeUsername string `json:"gl_assignee_username"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.GlIssueIID != 40 {
		t.Errorf("gl_issue_iid: got %d, want 40", resp.GlIssueIID)
	}
	if resp.ProjectPath != "testorg-get-issue/repo" {
		t.Errorf("project_path: got %q", resp.ProjectPath)
	}
	if resp.GlAssigneeUsername != "getuser" {
		t.Errorf("gl_assignee_username: got %q, want %q", resp.GlAssigneeUsername, "getuser")
	}
	if !strings.Contains(resp.URL, "testorg-get-issue/repo") {
		t.Errorf("url missing project path: %q", resp.URL)
	}
}

// TestGetGitLabIssueForIssue_NotFound tests that a non-linked issue returns 404.
func TestGetGitLabIssueForIssue_NotFound(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database available")
	}

	// Use an issue that has no gitlab_issue link — find the first issue in the workspace.
	ctx := context.Background()
	wsUUID := parseUUID(testWorkspaceID)
	issues, err := testHandler.Queries.ListMembers(ctx, wsUUID) // just reuse queries to avoid a new bare issue
	_ = issues
	_ = err

	// Create a bare Multica issue with no GitLab link.
	userUUID := parseUUID(testUserID)
	issue, err := testHandler.Queries.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID: wsUUID,
		Title:       "No GitLab",
		Status:      "todo",
		Priority:    "none",
		CreatorType: "member",
		CreatorID:   userUUID,
		Number:      9999,
	})
	if err != nil {
		t.Fatalf("create bare issue: %v", err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: issue.ID, WorkspaceID: wsUUID})
	})

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuidToString(issue.ID))
	req := httptest.NewRequest(http.MethodGet, "/api/issues/"+uuidToString(issue.ID)+"/gitlab-issue", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.GetGitLabIssueForIssue(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
```

**Note:** The `TestGetGitLabIssueForIssue_NotFound` test creates a bare issue via `CreateIssue`. The `CreateIssue` query's `Number` param requires a unique number per workspace. Use a high number to avoid conflicts, or use a query that auto-increments. Adjust as needed based on the actual `CreateIssue` query signature in `server/pkg/db/generated/issue.sql.go`.

- [ ] **Step 2: Run tests to confirm failure**

```bash
cd /Users/alex/paral/multica && go test ./server/internal/handler/ -run "TestGetGitLabIssueForIssue" -v 2>&1 | tail -10
```

Expected: compile error (GetGitLabIssueForIssue not defined)

- [ ] **Step 3: Add `GitLabIssueResponse` and `GetGitLabIssueForIssue` to `server/internal/handler/gitlab.go`**

Add after the existing response types (around line 115):

```go
// GitLabIssueResponse is the JSON shape returned by GET /api/issues/:id/gitlab-issue.
type GitLabIssueResponse struct {
	GlIssueIID         int32   `json:"gl_issue_iid"`
	ProjectPath        string  `json:"project_path"`
	URL                string  `json:"url"`
	GlAssigneeUsername *string `json:"gl_assignee_username"`
}

// GetGitLabIssueForIssue (GET /api/issues/{id}/gitlab-issue) returns the linked
// GitLab issue info for display in the sidebar, or 404 if none.
func (h *Handler) GetGitLabIssueForIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issueUUID, ok := parseUUIDOrBadRequest(w, issueID, "issue id")
	if !ok {
		return
	}

	glIssue, err := h.Queries.GetGitLabIssueByIssueID(r.Context(), issueUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no gitlab issue linked")
		return
	}

	issueURL := gitlabBaseURL() + "/" + glIssue.ProjectPath + "/-/issues/" + strconv.Itoa(int(glIssue.GlIssueIid))
	writeJSON(w, http.StatusOK, GitLabIssueResponse{
		GlIssueIID:         glIssue.GlIssueIid,
		ProjectPath:        glIssue.ProjectPath,
		URL:                issueURL,
		GlAssigneeUsername: textToPtr(glIssue.GlAssigneeUsername),
	})
}
```

Add `"strconv"` to imports if not already present.

- [ ] **Step 4: Register route in `server/cmd/server/router.go`**

Find the `r.Get("/merge-requests", ...)` line (around line 854) and add the new route alongside it:

```go
r.Get("/merge-requests", h.ListMergeRequestsForIssue)
r.Get("/gitlab-issue", h.GetGitLabIssueForIssue)
```

- [ ] **Step 5: Run the endpoint tests**

```bash
cd /Users/alex/paral/multica && go test ./server/internal/handler/ -run "TestGetGitLabIssueForIssue" -v 2>&1 | tail -20
```

Expected: PASS

- [ ] **Step 6: Run full handler suite**

```bash
cd /Users/alex/paral/multica && go test ./server/internal/handler/ -count=1 2>&1 | tail -5
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add server/internal/handler/gitlab.go server/cmd/server/router.go server/internal/handler/gitlab_test.go
git commit -m "feat(gitlab): add GET /api/issues/:id/gitlab-issue endpoint"
```

---

### Task 6: Frontend Types + API Client + Query Option + i18n

**Files:**
- Modify: `packages/core/types/gitlab.ts` (add `GitLabIssue` type)
- Modify: `packages/core/api/client.ts` (add `getGitLabIssue` method)
- Modify: `packages/core/gitlab/queries.ts` (add `issueGitLabIssueOptions`)
- Modify: `packages/core/gitlab/index.ts` (export new option)
- Modify: `packages/views/locales/en/issues.json` (add `gitlab_issue` keys)
- Modify: `packages/views/locales/ja/issues.json`
- Modify: `packages/views/locales/ko/issues.json`
- Modify: `packages/views/locales/zh-Hans/issues.json`

**Interfaces:**
- Consumes: `api.getGitLabIssue(issueId: string): Promise<GitLabIssue>` (throws ApiError 404 when not found)
- Produces: `issueGitLabIssueOptions(issueId: string)` — queryOptions returning `GitLabIssue | null`

- [ ] **Step 1: Add `GitLabIssue` type to `packages/core/types/gitlab.ts`**

Append:

```ts
export interface GitLabIssue {
  gl_issue_iid: number;
  project_path: string;
  url: string;
  gl_assignee_username: string | null;
}
```

- [ ] **Step 2: Add `getGitLabIssue` to `packages/core/api/client.ts`**

Find `listIssueMergeRequests` (around line 2246) and add after it:

```ts
  async getGitLabIssue(issueId: string): Promise<GitLabIssue> {
    return this.fetch(`/api/issues/${issueId}/gitlab-issue`);
  }
```

Also add `GitLabIssue` to the existing import block at the top of `client.ts`:

```ts
import type {
  // existing types...
  GitLabIssue,
} from "../types/gitlab";
```

- [ ] **Step 3: Add query option to `packages/core/gitlab/queries.ts`**

Add to the `gitlabKeys` object:

```ts
export const gitlabKeys = {
  all: (wsId: string) => ["gitlab", wsId] as const,
  connections: (wsId: string) => [...gitlabKeys.all(wsId), "connections"] as const,
  mergeRequests: (issueId: string) => ["gitlab", "merge-requests", issueId] as const,
  gitlabIssue: (issueId: string) => ["gitlab", "issue", issueId] as const,
};
```

Add the query option (requires importing `ApiError`):

```ts
import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import { ApiError } from "../api";
import type { GitLabIssue } from "../types/gitlab";

export const issueGitLabIssueOptions = (issueId: string) =>
  queryOptions<GitLabIssue | null>({
    queryKey: gitlabKeys.gitlabIssue(issueId),
    queryFn: async () => {
      try {
        return await api.getGitLabIssue(issueId);
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return null;
        throw e;
      }
    },
    enabled: !!issueId,
  });
```

- [ ] **Step 4: Export from `packages/core/gitlab/index.ts`**

```ts
export { gitlabKeys, gitlabConnectionsOptions, issueMergeRequestsOptions, issueGitLabIssueOptions } from "./queries";
export { useDeleteGitLabConnection } from "./settings";
```

- [ ] **Step 5: Add i18n keys to `packages/views/locales/en/issues.json`**

Find the `"merge_requests"` object (around line 490) and add after it:

```json
"gitlab_issue": {
  "title": "GitLab Issue",
  "assignee_label": "GitLab"
}
```

- [ ] **Step 6: Add i18n keys to the other locale files**

For `packages/views/locales/ja/issues.json`, add in the same position:

```json
"gitlab_issue": {
  "title": "GitLab Issue",
  "assignee_label": "GitLab"
}
```

For `packages/views/locales/ko/issues.json`:

```json
"gitlab_issue": {
  "title": "GitLab Issue",
  "assignee_label": "GitLab"
}
```

For `packages/views/locales/zh-Hans/issues.json`:

```json
"gitlab_issue": {
  "title": "GitLab Issue",
  "assignee_label": "GitLab"
}
```

(All four locales use the same English strings for this technical UI element — update with proper translations only if requested.)

- [ ] **Step 7: Run typecheck**

```bash
cd /Users/alex/paral/multica && pnpm typecheck 2>&1 | tail -20
```

Expected: no errors related to the new types

- [ ] **Step 8: Commit**

```bash
git add packages/core/types/gitlab.ts \
        packages/core/api/client.ts \
        packages/core/gitlab/queries.ts \
        packages/core/gitlab/index.ts \
        packages/views/locales/en/issues.json \
        packages/views/locales/ja/issues.json \
        packages/views/locales/ko/issues.json \
        packages/views/locales/zh-Hans/issues.json
git commit -m "feat(gitlab): add GitLabIssue type, API method, query option, i18n keys"
```

---

### Task 7: GitLabIssueBadge Component + Issue Detail Wiring

**Files:**
- Create: `packages/views/issues/components/gitlab-issue-badge.tsx`
- Create: `packages/views/issues/components/gitlab-issue-badge.test.tsx`
- Modify: `packages/views/issues/components/issue-detail.tsx`

**Interfaces:**
- Consumes: `issueGitLabIssueOptions` from `@multica/core/gitlab`, `GitLabIssue` type
- Produces: `<GitLabIssueBadge issueId={string} />` — renders null when no linked issue; renders badge + assignee row when linked

- [ ] **Step 1: Write the failing test** in `packages/views/issues/components/gitlab-issue-badge.test.tsx`

```tsx
import { describe, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { GitLabIssueBadge } from "./gitlab-issue-badge";

vi.mock("@multica/core/gitlab", () => ({
  issueGitLabIssueOptions: (issueId: string) => ({
    queryKey: ["gitlab", "issue", issueId],
    queryFn: async () => ({
      gl_issue_iid: 42,
      project_path: "paral/repo",
      url: "https://git.paral.no/paral/repo/-/issues/42",
      gl_assignee_username: "volumet",
    }),
    enabled: !!issueId,
  }),
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (fn: (keys: { gitlab_issue: { title: string; assignee_label: string } }) => string) =>
      fn({ gitlab_issue: { title: "GitLab Issue", assignee_label: "GitLab" } }),
  }),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe("GitLabIssueBadge", () => {
  it("renders the badge with project path and issue number", async () => {
    render(<GitLabIssueBadge issueId="abc" />, { wrapper });
    const link = await screen.findByRole("link");
    expect(link).toHaveTextContent("paral/repo#42");
    expect(link).toHaveAttribute("href", "https://git.paral.no/paral/repo/-/issues/42");
  });

  it("renders the GitLab assignee row", async () => {
    render(<GitLabIssueBadge issueId="abc" />, { wrapper });
    expect(await screen.findByText("GitLab: volumet")).toBeInTheDocument();
  });

  it("renders nothing when no linked issue", () => {
    vi.mocked(require("@multica/core/gitlab").issueGitLabIssueOptions).mockReturnValueOnce({
      queryKey: ["gitlab", "issue", "xyz"],
      queryFn: async () => null,
      enabled: true,
    });
    const { container } = render(<GitLabIssueBadge issueId="xyz" />, { wrapper });
    expect(container).toBeEmptyDOMElement();
  });
});
```

- [ ] **Step 2: Run test to confirm failure**

```bash
cd /Users/alex/paral/multica && pnpm test --filter @multica/views -- --run gitlab-issue-badge 2>&1 | tail -20
```

Expected: test failures (component not yet created)

- [ ] **Step 3: Create `packages/views/issues/components/gitlab-issue-badge.tsx`**

```tsx
"use client";

import { useQuery } from "@tanstack/react-query";
import { issueGitLabIssueOptions } from "@multica/core/gitlab";
import { useT } from "../../i18n";

export function GitLabIssueBadge({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const { data, isLoading } = useQuery(issueGitLabIssueOptions(issueId));

  if (isLoading || !data) return null;

  return (
    <div className="flex flex-col gap-1">
      <p className="px-1.5 text-xs font-medium text-muted-foreground">
        {t(($) => $.gitlab_issue.title)}
      </p>
      <a
        href={data.url}
        target="_blank"
        rel="noopener noreferrer"
        className="flex items-center gap-1.5 rounded-md px-1.5 py-1 text-sm hover:bg-muted/50 transition-colors text-foreground"
      >
        <span className="truncate">{data.project_path}#{data.gl_issue_iid}</span>
      </a>
      {data.gl_assignee_username && (
        <p className="px-1.5 text-xs text-muted-foreground">
          {t(($) => $.gitlab_issue.assignee_label)}: {data.gl_assignee_username}
        </p>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Run the component tests**

```bash
cd /Users/alex/paral/multica && pnpm test --filter @multica/views -- --run gitlab-issue-badge 2>&1 | tail -20
```

Expected: PASS

- [ ] **Step 5: Wire `GitLabIssueBadge` into `issue-detail.tsx`**

Find the `<MergeRequestList issueId={id} />` line (around line 1620) in `packages/views/issues/components/issue-detail.tsx` and add `GitLabIssueBadge` after it:

```tsx
      <MergeRequestList issueId={id} />
      <GitLabIssueBadge issueId={id} />
```

Also add the import at the top of the file alongside the `MergeRequestList` import:

```tsx
import { MergeRequestList } from "./merge-request-list";
import { GitLabIssueBadge } from "./gitlab-issue-badge";
```

- [ ] **Step 6: Run typecheck**

```bash
cd /Users/alex/paral/multica && pnpm typecheck 2>&1 | tail -20
```

Expected: no errors

- [ ] **Step 7: Run the full TS test suite**

```bash
cd /Users/alex/paral/multica && pnpm test 2>&1 | tail -20
```

Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add packages/views/issues/components/gitlab-issue-badge.tsx \
        packages/views/issues/components/gitlab-issue-badge.test.tsx \
        packages/views/issues/components/issue-detail.tsx
git commit -m "feat(gitlab): GitLabIssueBadge component — shows issue link and assignee in sidebar"
```

---

## Self-Review

### Spec Coverage Check

| Spec requirement | Task |
|---|---|
| GitLab issue labeled "agent" → create Multica issue | Task 2 |
| GitLab issue label "agent" removed → delete Multica issue | Task 2 |
| GitLab issue closed → mark Multica issue Done | Task 2 |
| GitLab issue reopened → mark Multica issue In Progress | Task 2 |
| GitLab issue description updated → update Multica description | Task 2 |
| GitLab issue assignee changed → update gl_assignee_username | Task 2 |
| GitLab comment → create Multica comment (new only, no backfill) | Task 3 |
| Multica comment → post to GitLab via API | Task 4 |
| GitLab issue number badge (`paral/repo#42`) | Task 7 |
| GitLab assignee read-only field | Task 7 |
| `gitlab_issue` table | Task 1 |
| `comment.gitlab_note_id` | Task 1 |
| Echo loop prevention | Tasks 3 + 4 |
| `GET /api/issues/:id/gitlab-issue` endpoint | Task 5 |
| Access token decryption via GitLabBox | Task 4 |
| Token expiry check | Task 4 |
| Webhook returns 204 always | Task 2 (HandleGitLabWebhook already does this) |
| INSERT ON CONFLICT DO NOTHING for idempotency | Task 1 (SQL query) |
| Frontend 404 returns null | Task 6 |

All spec requirements covered. ✓

### Placeholder Scan

- Task 3, Step 3: "Note on the SetCommentGitLabNoteID params" — field name depends on sqlc output. **Fix:** After Task 1 `make sqlc`, check `server/pkg/db/generated/comment.sql.go` for the exact struct field name and update accordingly before writing Task 3 code.
- Task 4, Step 6: Token decryption in test requires real secretbox. **Fix:** Fully specified in the step — use `secretbox.New(key)` with a random key and store a properly sealed token.
- Task 5, Step 1, `TestGetGitLabIssueForIssue_NotFound`: `CreateIssue` query params may differ. **Fix:** Check `server/pkg/db/generated/issue.sql.go` for `CreateIssueParams` and adjust the test's `db.CreateIssueParams{}` call to match the actual struct (it uses `number` auto-increment via sequence, so `Number` param may not exist or may be handled differently). The test can instead use `IssueService.Create` for correct issue creation.

### Type Consistency Check

- `GitLabIssue` defined in Task 6, Step 1; consumed in Task 7, Step 3 — matches ✓
- `issueGitLabIssueOptions` exported in Task 6, Step 4; imported in Task 7, Step 3 — matches ✓
- `GitLabIssueResponse` (Go) matches the `GitLabIssue` (TS) field names: `gl_issue_iid`, `project_path`, `url`, `gl_assignee_username` — matches ✓
- `gitlabKeys.gitlabIssue` defined in Task 6, Step 3; no other task references it directly — ✓
- `db.GetGitLabIssueByProjectAndIIDParams` — struct fields: `WorkspaceID pgtype.UUID`, `ProjectPath string`, `GlIssueIid int32` — used consistently in Tasks 2, 3, 5 tests ✓
- `db.InsertGitLabIssueParams` — fields: `WorkspaceID`, `ConnectionID`, `ProjectPath`, `GlIssueIid int32`, `GlProjectId int64`, `IssueID`, `GlAssigneeUsername pgtype.Text` — used in Task 2 ✓
