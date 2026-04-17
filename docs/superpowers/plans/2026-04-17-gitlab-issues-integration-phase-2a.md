# GitLab Issues Integration — Phase 2a: Cache Schema + Initial Sync — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a workspace admin connects their GitLab project (Phase 1), kick off a one-shot initial sync that paginates all issues, notes, award emoji, labels, and project members from gitlab.com into Multica's tables. Read endpoints serve the synced cache. Write endpoints return 501 for connected workspaces. No webhooks or reconciler yet — that's Phase 2b. After 2a ships, a workspace admin can browse a snapshot of GitLab issues in Multica; they'd need to disconnect/reconnect to get fresh data.

**Architecture:**
- **Schema is additive only.** New cache columns (`gitlab_iid`, `gitlab_project_id`, `external_updated_at`) and new cache tables (`gitlab_label`, `issue_to_label`, `gitlab_project_member`, `issue_position`). No DROPs of existing columns or tables — Phase 5 cleanup handles drops once everything has migrated to the cache shape. `creator_id` is relaxed from `NOT NULL` to nullable so synced rows can omit a Multica-mapped creator until Phase 3 introduces per-user PAT mapping.
- **GitLab client expansion** adds the read methods needed for sync: pagination helper, `ListLabels`, `CreateLabel`, `ListProjectMembers`, `ListIssues`, `ListNotes`, `ListAwardEmoji`. Each follows the existing per-call-token pattern from Phase 1's `Client`.
- **Translation layer** (`server/internal/gitlab/translator.go`) converts GitLab JSON shapes into Multica cache values. Status comes from `status::*` scoped labels; priority from `priority::*`; agent assignment from `agent::<slug>`. Native GitLab assignees are deferred to Phase 3 (no GitLab-user→Multica-member mapping until per-user PATs exist).
- **Sync worker** runs as a goroutine spawned by the connect handler. The connect handler writes the row with `connection_status='connecting'` and returns 200 immediately; the sync goroutine populates the cache, then transitions to `'connected'` (or `'error'` with a `status_message`). Progress + completion are published as WS events.
- **501 write stopgap.** A small middleware checks whether the workspace has a `workspace_gitlab_connection` row; if so, write requests against issues/comments/reactions/subscriptions/attachments return `501 Not Implemented` with body `{"error": "writes not yet wired to GitLab"}`. Reads continue to serve from the same tables (now populated by the sync).

**Tech Stack:**
- Go 1.26, Chi router, `pgx/v5`, `sqlc` (existing).
- Stdlib `net/http`, `context`, `sync` (semaphore for bounded concurrency).
- Existing `server/pkg/gitlab/` and `server/internal/handler/` from Phase 1.

**Design spec:** `docs/superpowers/specs/2026-04-17-gitlab-issues-integration-design.md`
**Phase 1 plan (for reference):** `docs/superpowers/plans/2026-04-17-gitlab-issues-integration-phase-1.md`

**Out of scope for 2a (Phase 2b will add):**
- Webhook receiver endpoint + worker pool + dedupe table (`gitlab_webhook_event`).
- Webhook creation in initial sync (and `DeleteProjectHook` in disconnect — for 2a, disconnect only truncates the cache).
- Reconciliation goroutine.
- Stale-webhook detection.

**Out of scope for Phase 2 entirely (Phase 3+ handles):**
- Native GitLab assignees → Multica members. The translator stores `assignee_type=NULL` for GitLab-user assignments; UI can render "outside Multica" later.
- Per-user PAT flow + writes through GitLab.
- Autopilot re-pointing (`autopilot_issue` mapping table).
- Dropping legacy columns (`parent_issue_id`, `acceptance_criteria`, `context_refs`, `origin_type`, `origin_id`, `position`) and removing dead-end endpoints (sub-issues, dependencies). All survive 2a; Phase 5 cleans up.

---

## File Structure

### New files (backend)

| Path | Responsibility |
|---|---|
| `server/migrations/050_gitlab_cache_schema.up.sql` | Additive: cache-ref columns on existing tables; new cache tables; relax `creator_id NOT NULL`. |
| `server/migrations/050_gitlab_cache_schema.down.sql` | Reverse — drop new columns + new tables, restore `creator_id NOT NULL`. |
| `server/pkg/db/queries/gitlab_cache.sql` | sqlc queries for gitlab_label, issue_to_label, gitlab_project_member CRUD + cache upserts (issues/comments/reactions by gitlab_iid/note_id/award_id). |
| `server/pkg/gitlab/pagination.go` | `iteratePages` helper that follows GitLab's `Link: next` header. |
| `server/pkg/gitlab/pagination_test.go` | Tests for pagination helper. |
| `server/pkg/gitlab/labels.go` | `ListLabels`, `CreateLabel`. |
| `server/pkg/gitlab/labels_test.go` | Tests. |
| `server/pkg/gitlab/members.go` | `ListProjectMembers`. |
| `server/pkg/gitlab/members_test.go` | Tests. |
| `server/pkg/gitlab/issues.go` | `ListIssues`. |
| `server/pkg/gitlab/issues_test.go` | Tests. |
| `server/pkg/gitlab/notes.go` | `ListNotes`. |
| `server/pkg/gitlab/notes_test.go` | Tests. |
| `server/pkg/gitlab/award_emoji.go` | `ListAwardEmoji`. |
| `server/pkg/gitlab/award_emoji_test.go` | Tests. |
| `server/internal/gitlab/translator.go` | `TranslateIssue`, `TranslateNote`, `TranslateAward` — pure functions, no I/O. |
| `server/internal/gitlab/translator_test.go` | Pure unit tests. |
| `server/internal/gitlab/labels_bootstrap.go` | `BootstrapScopedLabels` — provision the canonical `status::*` and `priority::*` labels in a project (idempotent). |
| `server/internal/gitlab/labels_bootstrap_test.go` | Tests against fake GitLab. |
| `server/internal/gitlab/initial_sync.go` | `RunInitialSync(ctx, deps, workspaceID)` — top-level sync orchestrator. |
| `server/internal/gitlab/initial_sync_test.go` | Integration test: fake GitLab + real DB. |
| `server/internal/middleware/gitlab_writes_blocked.go` | Middleware: 501 when workspace has gitlab connection. |
| `server/internal/middleware/gitlab_writes_blocked_test.go` | Tests. |

### Modified files (backend)

| Path | Change |
|---|---|
| `server/internal/handler/gitlab_connection.go` | Connect handler: insert row with `connection_status='connecting'`, dispatch sync goroutine. Disconnect handler: cascade-truncate cache for that workspace. |
| `server/internal/handler/gitlab_connection_test.go` | New test for the cascade truncation. Existing connect-success test now asserts status starts as `'connecting'`. |
| `server/cmd/server/router.go` | Apply `GitlabWritesBlockedMiddleware` to issue/comment/reaction/subscription/attachment write groups. |
| `server/cmd/server/main.go` | Pass any new dependencies (no new long-lived workers in 2a; sync runs on-demand via the connect handler). |

### No frontend changes in 2a

Phase 2a is purely backend. The existing Phase 1 `GitlabTab` continues to work — it shows the connected state once `connection_status='connected'`. The "connecting" state will visually be a loading spinner once the frontend is updated to handle it (separate UX work; out of scope for 2a). Connected workspaces won't see write operations succeed for any issue, comment, reaction, etc. — the UI will surface the 501 errors as toasts via existing error paths.

---

## Task 1: Migration 050 — Cache schema additions

**Files:**
- Create: `server/migrations/050_gitlab_cache_schema.up.sql`
- Create: `server/migrations/050_gitlab_cache_schema.down.sql`

- [ ] **Step 1.1: Verify migration number**

Run: `ls server/migrations | sort | tail -3`
Expected: highest is `049_gitlab_connection`. If somehow `050_` is taken, bump to `051_` throughout.

- [ ] **Step 1.2: Create the up migration**

```sql
-- Phase 2a: cache schema additions for the GitLab integration.
-- Purely additive — no DROPs. Existing functionality continues to work.
-- Phase 5 cleanup will drop the now-unused legacy columns/tables once
-- every issue has migrated to the cache shape.

-- Cache-ref columns on issue. Nullable for now: legacy direct-to-DB writes
-- (still allowed for non-connected workspaces) won't supply them.
ALTER TABLE issue
    ADD COLUMN gitlab_iid INT,
    ADD COLUMN gitlab_project_id BIGINT,
    ADD COLUMN external_updated_at TIMESTAMPTZ;

-- The sync writes synthetic rows whose Multica-side creator we don't yet
-- have a mapping for (Phase 3 introduces user_gitlab_connection lookup).
-- Relax the NOT NULL so synced rows can leave creator_id as NULL.
ALTER TABLE issue ALTER COLUMN creator_id DROP NOT NULL;
ALTER TABLE issue ALTER COLUMN creator_type DROP NOT NULL;

-- Unique cache key, scoped per workspace.
CREATE UNIQUE INDEX idx_issue_gitlab_iid
    ON issue(workspace_id, gitlab_iid)
    WHERE gitlab_iid IS NOT NULL;

-- Cache-ref columns on comment.
ALTER TABLE comment
    ADD COLUMN gitlab_note_id BIGINT,
    ADD COLUMN external_updated_at TIMESTAMPTZ;

CREATE UNIQUE INDEX idx_comment_gitlab_note
    ON comment(gitlab_note_id)
    WHERE gitlab_note_id IS NOT NULL;

-- Cache-ref columns on issue_reaction.
ALTER TABLE issue_reaction
    ADD COLUMN gitlab_award_id BIGINT,
    ADD COLUMN external_updated_at TIMESTAMPTZ;

CREATE UNIQUE INDEX idx_issue_reaction_gitlab_award
    ON issue_reaction(gitlab_award_id)
    WHERE gitlab_award_id IS NOT NULL;

-- Cache-ref column on attachment.
ALTER TABLE attachment
    ADD COLUMN gitlab_upload_url TEXT;

-- Label cache (one row per GitLab label per workspace).
CREATE TABLE IF NOT EXISTS gitlab_label (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    gitlab_label_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    color TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    external_updated_at TIMESTAMPTZ,
    PRIMARY KEY (workspace_id, gitlab_label_id)
);

CREATE INDEX idx_gitlab_label_name ON gitlab_label(workspace_id, name);

-- Issue ↔ label association (replaces the legacy issue_to_labels table once
-- Phase 5 drops it; both coexist during 2a–4).
CREATE TABLE IF NOT EXISTS issue_to_label (
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL,
    gitlab_label_id BIGINT NOT NULL,
    PRIMARY KEY (issue_id, gitlab_label_id),
    FOREIGN KEY (workspace_id, gitlab_label_id)
        REFERENCES gitlab_label(workspace_id, gitlab_label_id) ON DELETE CASCADE
);

CREATE INDEX idx_issue_to_label_label
    ON issue_to_label(workspace_id, gitlab_label_id);

-- Cache of GitLab project members for assignee picker / avatar rendering.
CREATE TABLE IF NOT EXISTS gitlab_project_member (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    gitlab_user_id BIGINT NOT NULL,
    username TEXT NOT NULL,
    name TEXT NOT NULL,
    avatar_url TEXT NOT NULL DEFAULT '',
    external_updated_at TIMESTAMPTZ,
    PRIMARY KEY (workspace_id, gitlab_user_id)
);

-- Server-only ordering (replaces issue.position once Phase 5 drops it).
-- Rows written on drag-reorder mutations; absent rows fall back to
-- created_at DESC ordering at read time.
CREATE TABLE IF NOT EXISTS issue_position (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    gitlab_iid INT NOT NULL,
    position NUMERIC NOT NULL,
    PRIMARY KEY (workspace_id, gitlab_iid)
);
```

- [ ] **Step 1.3: Create the down migration**

```sql
-- Reverse Phase 2a's cache schema additions.

DROP TABLE IF EXISTS issue_position;
DROP TABLE IF EXISTS gitlab_project_member;
DROP TABLE IF EXISTS issue_to_label;
DROP TABLE IF EXISTS gitlab_label;

ALTER TABLE attachment DROP COLUMN IF EXISTS gitlab_upload_url;

DROP INDEX IF EXISTS idx_issue_reaction_gitlab_award;
ALTER TABLE issue_reaction
    DROP COLUMN IF EXISTS external_updated_at,
    DROP COLUMN IF EXISTS gitlab_award_id;

DROP INDEX IF EXISTS idx_comment_gitlab_note;
ALTER TABLE comment
    DROP COLUMN IF EXISTS external_updated_at,
    DROP COLUMN IF EXISTS gitlab_note_id;

-- creator_id NOT NULL constraint cannot be safely re-added if any rows have
-- NULL — fail noisily if so.
ALTER TABLE issue ALTER COLUMN creator_type SET NOT NULL;
ALTER TABLE issue ALTER COLUMN creator_id SET NOT NULL;

DROP INDEX IF EXISTS idx_issue_gitlab_iid;
ALTER TABLE issue
    DROP COLUMN IF EXISTS external_updated_at,
    DROP COLUMN IF EXISTS gitlab_project_id,
    DROP COLUMN IF EXISTS gitlab_iid;
```

- [ ] **Step 1.4: Apply and verify**

Run from worktree root:
```bash
make migrate-up
```
Expected: applies without error.

Verify the schema using `psql` (connect via the worktree's `DATABASE_URL` from `.env.worktree`):
```bash
psql "$DATABASE_URL" -c "\d issue" -c "\d gitlab_label" -c "\d issue_to_label" -c "\d gitlab_project_member" -c "\d issue_position"
```
Expected:
- `issue` has new columns `gitlab_iid`, `gitlab_project_id`, `external_updated_at`; `creator_id` is now nullable.
- New tables exist with the columns + PKs from Step 1.2.

- [ ] **Step 1.5: Round-trip verify**

```bash
make migrate-down
make migrate-up
```
Expected: both clean.

- [ ] **Step 1.6: Commit**

```bash
git add server/migrations/050_gitlab_cache_schema.up.sql server/migrations/050_gitlab_cache_schema.down.sql
git commit -m "feat(db): cache schema additions for gitlab integration"
```

---

## Task 2: sqlc queries for cache tables

**Files:**
- Create: `server/pkg/db/queries/gitlab_cache.sql`
- Regenerate: `server/pkg/db/generated/`

- [ ] **Step 2.1: Create the query file**

```sql
-- gitlab_label CRUD --------------------------------------------------------

-- name: UpsertGitlabLabel :one
INSERT INTO gitlab_label (
    workspace_id, gitlab_label_id, name, color, description, external_updated_at
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, gitlab_label_id) DO UPDATE SET
    name = EXCLUDED.name,
    color = EXCLUDED.color,
    description = EXCLUDED.description,
    external_updated_at = EXCLUDED.external_updated_at
RETURNING *;

-- name: ListGitlabLabels :many
SELECT * FROM gitlab_label
WHERE workspace_id = $1
ORDER BY name;

-- name: GetGitlabLabelByName :one
SELECT * FROM gitlab_label
WHERE workspace_id = $1 AND name = $2;

-- name: DeleteWorkspaceGitlabLabels :exec
DELETE FROM gitlab_label WHERE workspace_id = $1;

-- issue_to_label ----------------------------------------------------------

-- name: ClearIssueLabels :exec
DELETE FROM issue_to_label WHERE issue_id = $1;

-- name: AddIssueLabels :exec
INSERT INTO issue_to_label (issue_id, workspace_id, gitlab_label_id)
SELECT $1, $2, unnest(sqlc.arg(label_ids)::bigint[])
ON CONFLICT DO NOTHING;

-- name: ListIssueLabels :many
SELECT l.*
FROM gitlab_label l
JOIN issue_to_label il ON il.workspace_id = l.workspace_id
                      AND il.gitlab_label_id = l.gitlab_label_id
WHERE il.issue_id = $1
ORDER BY l.name;

-- gitlab_project_member ----------------------------------------------------

-- name: UpsertGitlabProjectMember :one
INSERT INTO gitlab_project_member (
    workspace_id, gitlab_user_id, username, name, avatar_url, external_updated_at
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, gitlab_user_id) DO UPDATE SET
    username = EXCLUDED.username,
    name = EXCLUDED.name,
    avatar_url = EXCLUDED.avatar_url,
    external_updated_at = EXCLUDED.external_updated_at
RETURNING *;

-- name: ListGitlabProjectMembers :many
SELECT * FROM gitlab_project_member
WHERE workspace_id = $1
ORDER BY username;

-- name: DeleteWorkspaceGitlabMembers :exec
DELETE FROM gitlab_project_member WHERE workspace_id = $1;

-- issue cache upserts ------------------------------------------------------

-- name: UpsertIssueFromGitlab :one
INSERT INTO issue (
    workspace_id,
    gitlab_iid,
    gitlab_project_id,
    title,
    description,
    status,
    priority,
    assignee_type,
    assignee_id,
    creator_type,
    creator_id,
    due_date,
    external_updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (workspace_id, gitlab_iid) WHERE gitlab_iid IS NOT NULL DO UPDATE SET
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    status = EXCLUDED.status,
    priority = EXCLUDED.priority,
    assignee_type = EXCLUDED.assignee_type,
    assignee_id = EXCLUDED.assignee_id,
    due_date = EXCLUDED.due_date,
    external_updated_at = EXCLUDED.external_updated_at,
    updated_at = now()
RETURNING *;

-- name: GetIssueByGitlabIID :one
SELECT * FROM issue
WHERE workspace_id = $1 AND gitlab_iid = $2;

-- name: DeleteWorkspaceCachedIssues :exec
DELETE FROM issue WHERE workspace_id = $1 AND gitlab_iid IS NOT NULL;

-- comment cache upserts ----------------------------------------------------

-- name: UpsertCommentFromGitlab :one
INSERT INTO comment (
    issue_id,
    author_type,
    author_id,
    body,
    type,
    gitlab_note_id,
    external_updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (gitlab_note_id) WHERE gitlab_note_id IS NOT NULL DO UPDATE SET
    body = EXCLUDED.body,
    type = EXCLUDED.type,
    external_updated_at = EXCLUDED.external_updated_at,
    updated_at = now()
RETURNING *;

-- issue_reaction cache upserts --------------------------------------------

-- name: UpsertIssueReactionFromGitlab :one
INSERT INTO issue_reaction (
    issue_id,
    user_id,
    user_type,
    emoji,
    gitlab_award_id,
    external_updated_at
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (gitlab_award_id) WHERE gitlab_award_id IS NOT NULL DO UPDATE SET
    emoji = EXCLUDED.emoji,
    external_updated_at = EXCLUDED.external_updated_at
RETURNING *;
```

- [ ] **Step 2.2: Regenerate**

Run: `make sqlc`
Expected: clean.

- [ ] **Step 2.3: Verify build**

Run: `cd server && go build ./...`
Expected: clean. If sqlc generated a different parameter type for one of the columns (e.g., `assignee_id` as `pgtype.UUID` vs `uuid.UUID`), the generated `*Params` struct will tell you — that's the type the engineer of Task 14 (translator) will use.

- [ ] **Step 2.4: Note the generated Params struct types**

For your records, run:
```bash
grep -n -A 16 "type UpsertIssueFromGitlabParams struct" server/pkg/db/generated/gitlab_cache.sql.go
```

Confirm:
- `WorkspaceID pgtype.UUID`
- `GitlabIid pgtype.Int4` (since the column is INT not BIGINT, sqlc maps to Int4)
- `GitlabProjectID pgtype.Int8`
- `Title string`
- `Description pgtype.Text`
- `Status string`
- `Priority string`
- `AssigneeType pgtype.Text`
- `AssigneeID pgtype.UUID`
- `CreatorType pgtype.Text`
- `CreatorID pgtype.UUID`
- `DueDate pgtype.Date` or `pgtype.Timestamptz` (depends on the column type — check `\d issue` if unsure)
- `ExternalUpdatedAt pgtype.Timestamptz`

If any field name differs (e.g. `GitlabIID` vs `GitlabIid` casing), use whatever sqlc actually produced in subsequent tasks. Don't guess.

- [ ] **Step 2.5: Commit**

```bash
git add server/pkg/db/queries/gitlab_cache.sql server/pkg/db/generated/
git commit -m "feat(db): sqlc queries for gitlab cache tables"
```

---

## Task 3: GitLab client — pagination helper

**Files:**
- Create: `server/pkg/gitlab/pagination.go`
- Create: `server/pkg/gitlab/pagination_test.go`

GitLab's REST API paginates with a `Link` header containing `<…?page=N>; rel="next"`. The helper centralizes parsing + iteration so each list method doesn't repeat itself.

- [ ] **Step 3.1: Write failing test**

Create `server/pkg/gitlab/pagination_test.go`:

```go
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIteratePages_FollowsLinkNext(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1":
			// Provide an absolute Link: rel="next" pointing at page 2.
			w.Header().Set("Link", fmt.Sprintf(`<%s/api/v4/items?page=2>; rel="next"`, srvURLFromRequest(r)))
			json.NewEncoder(w).Encode([]map[string]any{{"id": 1}, {"id": 2}})
		case "2":
			// Final page: no Link header (or rel="next" absent).
			json.NewEncoder(w).Encode([]map[string]any{{"id": 3}})
		default:
			t.Fatalf("unexpected page %q", page)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	var collected []map[string]any
	err := iteratePages(context.Background(), c, "tok", "/items", func(items []map[string]any) error {
		collected = append(collected, items...)
		return nil
	})
	if err != nil {
		t.Fatalf("iteratePages: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 server calls, got %d", calls)
	}
	if len(collected) != 3 {
		t.Errorf("expected 3 items, got %d (%+v)", len(collected), collected)
	}
}

// srvURLFromRequest reconstructs the test server URL from the request.
// Used so the next-page Link header points at the same httptest server.
func srvURLFromRequest(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
```

- [ ] **Step 3.2: Run — expect compile error**

```bash
cd server && go test ./pkg/gitlab/ -run TestIteratePages -v
```
Expected: `iteratePages` undefined.

- [ ] **Step 3.3: Implement**

Create `server/pkg/gitlab/pagination.go`:

```go
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// iteratePages walks a paginated GitLab list endpoint, calling onPage with
// each batch of items as it arrives. It follows the "Link: ...; rel=\"next\""
// header until none is present (or onPage returns an error).
//
// The page handler is generic over T via a type parameter; callers pass a
// closure that decodes the items into the type they need.
func iteratePages[T any](ctx context.Context, c *Client, token, path string, onPage func([]T) error) error {
	url := c.baseURL + "/api/v4" + path
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("gitlab: build request: %w", err)
		}
		req.Header.Set("PRIVATE-TOKEN", token)
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("gitlab: http do: %w", err)
		}
		// Decode + handle status before deciding whether to continue.
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return classifyHTTPError(resp.StatusCode, respBody)
		}

		var batch []T
		if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
			resp.Body.Close()
			return fmt.Errorf("gitlab: decode page: %w", err)
		}
		linkHdr := resp.Header.Get("Link")
		resp.Body.Close()

		if err := onPage(batch); err != nil {
			return err
		}

		next := nextPageURL(linkHdr)
		if next == "" {
			return nil
		}
		url = next
	}
}

// classifyHTTPError mirrors the non-2xx classification in client.do so
// the pagination path produces the same sentinel errors callers expect.
func classifyHTTPError(status int, body []byte) error {
	var parsed struct {
		Message any `json:"message"`
	}
	_ = json.Unmarshal(body, &parsed)
	msg := formatGitlabMessage(parsed.Message)
	if msg == "" {
		msg = string(body)
	}
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrForbidden, msg)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, msg)
	default:
		return &APIError{StatusCode: status, Message: msg}
	}
}

// nextPageURL parses a `Link` header and returns the URL of the rel="next"
// entry, if any. Returns "" when the header is missing or has no next link.
//
// Format example:
//
//   <https://gitlab.com/api/v4/issues?page=2>; rel="next", <…?page=5>; rel="last"
func nextPageURL(header string) string {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		// Each segment looks like: <url>; rel="next"
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		open := strings.Index(part, "<")
		close := strings.Index(part, ">")
		if open == -1 || close == -1 || close <= open+1 {
			continue
		}
		return part[open+1 : close]
	}
	return ""
}
```

- [ ] **Step 3.4: Run — expect pass**

```bash
cd server && go test ./pkg/gitlab/ -v
```
Expected: all gitlab tests pass (existing 8 + new 1).

- [ ] **Step 3.5: Commit**

```bash
git add server/pkg/gitlab/pagination.go server/pkg/gitlab/pagination_test.go
git commit -m "feat(gitlab): pagination helper that follows Link: rel=\"next\""
```

---

## Task 4: GitLab client — `ListLabels` + `CreateLabel`

**Files:**
- Create: `server/pkg/gitlab/labels.go`
- Create: `server/pkg/gitlab/labels_test.go`

- [ ] **Step 4.1: Write failing tests**

```go
package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListLabels_PaginatesAndReturnsAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		switch page {
		case "1":
			w.Header().Set("Link", `<`+srvURLFromRequest(r)+`/api/v4/projects/7/labels?page=2>; rel="next"`)
			json.NewEncoder(w).Encode([]Label{
				{ID: 1, Name: "bug", Color: "#ff0000"},
				{ID: 2, Name: "status::todo", Color: "#888888"},
			})
		case "2":
			json.NewEncoder(w).Encode([]Label{
				{ID: 3, Name: "priority::high", Color: "#ff8800"},
			})
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	labels, err := c.ListLabels(context.Background(), "tok", 7)
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	if len(labels) != 3 {
		t.Errorf("got %d labels, want 3", len(labels))
	}
}

func TestCreateLabel_PostsCorrectBody(t *testing.T) {
	var got CreateLabelInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/7/labels" {
			t.Errorf("path = %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Label{ID: 99, Name: got.Name, Color: got.Color, Description: got.Description})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	out, err := c.CreateLabel(context.Background(), "tok", 7, CreateLabelInput{
		Name: "status::todo", Color: "#cccccc", Description: "Multica status",
	})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if got.Name != "status::todo" || got.Color != "#cccccc" || got.Description != "Multica status" {
		t.Errorf("server received %+v", got)
	}
	if out.ID != 99 {
		t.Errorf("returned ID = %d, want 99", out.ID)
	}
}
```

- [ ] **Step 4.2: Run — expect compile error**

```bash
cd server && go test ./pkg/gitlab/ -run "TestListLabels|TestCreateLabel" -v
```
Expected: undefined.

- [ ] **Step 4.3: Implement**

Create `server/pkg/gitlab/labels.go`:

```go
package gitlab

import (
	"context"
	"fmt"
)

// Label mirrors GitLab's project label representation.
type Label struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// CreateLabelInput is the body for POST /projects/:id/labels.
type CreateLabelInput struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description,omitempty"`
}

// ListLabels paginates GET /projects/:id/labels and returns every label.
func (c *Client) ListLabels(ctx context.Context, token string, projectID int64) ([]Label, error) {
	var all []Label
	path := fmt.Sprintf("/projects/%d/labels?per_page=100", projectID)
	err := iteratePages[Label](ctx, c, token, path, func(batch []Label) error {
		all = append(all, batch...)
		return nil
	})
	return all, err
}

// CreateLabel creates one project label and returns the created object.
func (c *Client) CreateLabel(ctx context.Context, token string, projectID int64, input CreateLabelInput) (*Label, error) {
	var out Label
	path := fmt.Sprintf("/projects/%d/labels", projectID)
	if err := c.do(ctx, "POST", token, path, input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
```

- [ ] **Step 4.4: Run — expect pass**

```bash
cd server && go test ./pkg/gitlab/ -v
```
Expected: all pass.

- [ ] **Step 4.5: Commit**

```bash
git add server/pkg/gitlab/labels.go server/pkg/gitlab/labels_test.go
git commit -m "feat(gitlab): ListLabels (paginated) and CreateLabel"
```

---

## Task 5: GitLab client — `ListProjectMembers`

**Files:**
- Create: `server/pkg/gitlab/members.go`
- Create: `server/pkg/gitlab/members_test.go`

- [ ] **Step 5.1: Write failing test**

```go
package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListProjectMembers_AllEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /members/all returns inherited members too — what we want.
		if r.URL.Path != "/api/v4/projects/7/members/all" {
			t.Errorf("path = %s, want /api/v4/projects/7/members/all", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]ProjectMember{
			{ID: 10, Username: "alice", Name: "Alice A", AvatarURL: "https://x/alice.png"},
			{ID: 11, Username: "bob", Name: "Bob B", AvatarURL: "https://x/bob.png"},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	members, err := c.ListProjectMembers(context.Background(), "tok", 7)
	if err != nil {
		t.Fatalf("ListProjectMembers: %v", err)
	}
	if len(members) != 2 || members[0].Username != "alice" {
		t.Errorf("unexpected members: %+v", members)
	}
}
```

- [ ] **Step 5.2: Run — expect compile error**

```bash
cd server && go test ./pkg/gitlab/ -run TestListProjectMembers -v
```

- [ ] **Step 5.3: Implement**

Create `server/pkg/gitlab/members.go`:

```go
package gitlab

import (
	"context"
	"fmt"
)

// ProjectMember mirrors the subset of GitLab's project-member shape we cache.
type ProjectMember struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// ListProjectMembers returns every member of the project including inherited
// ones (group → subgroup → project). Uses /members/all.
func (c *Client) ListProjectMembers(ctx context.Context, token string, projectID int64) ([]ProjectMember, error) {
	var all []ProjectMember
	path := fmt.Sprintf("/projects/%d/members/all?per_page=100", projectID)
	err := iteratePages[ProjectMember](ctx, c, token, path, func(batch []ProjectMember) error {
		all = append(all, batch...)
		return nil
	})
	return all, err
}
```

- [ ] **Step 5.4: Run — expect pass**

```bash
cd server && go test ./pkg/gitlab/ -v
```

- [ ] **Step 5.5: Commit**

```bash
git add server/pkg/gitlab/members.go server/pkg/gitlab/members_test.go
git commit -m "feat(gitlab): ListProjectMembers (paginated, inherited too)"
```

---

## Task 6: GitLab client — `ListIssues`

**Files:**
- Create: `server/pkg/gitlab/issues.go`
- Create: `server/pkg/gitlab/issues_test.go`

- [ ] **Step 6.1: Write failing test**

```go
package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListIssues_DefaultsToStateAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "all" {
			t.Errorf("state query = %q, want all", r.URL.Query().Get("state"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Issue{
			{IID: 1, Title: "one", State: "opened", Labels: []string{"status::todo"}},
			{IID: 2, Title: "two", State: "closed", Labels: []string{"status::done"}},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	issues, err := c.ListIssues(context.Background(), "tok", 7, ListIssuesParams{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 2 || issues[0].IID != 1 {
		t.Errorf("unexpected: %+v", issues)
	}
}

func TestListIssues_UpdatedAfterPropagated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("updated_after") != "2026-04-17T00:00:00Z" {
			t.Errorf("updated_after = %q", r.URL.Query().Get("updated_after"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	_, err := c.ListIssues(context.Background(), "tok", 7, ListIssuesParams{
		UpdatedAfter: "2026-04-17T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
}
```

- [ ] **Step 6.2: Run — expect compile error**

```bash
cd server && go test ./pkg/gitlab/ -run TestListIssues -v
```

- [ ] **Step 6.3: Implement**

Create `server/pkg/gitlab/issues.go`:

```go
package gitlab

import (
	"context"
	"fmt"
	"net/url"
)

// Issue mirrors the subset of GitLab's project issue shape we cache.
type Issue struct {
	ID          int64    `json:"id"`
	IID         int      `json:"iid"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	State       string   `json:"state"` // "opened" or "closed"
	Labels      []string `json:"labels"`
	Assignees   []User   `json:"assignees"`
	Author      User     `json:"author"`
	DueDate     string   `json:"due_date"` // YYYY-MM-DD or ""
	UpdatedAt   string   `json:"updated_at"`
	CreatedAt   string   `json:"created_at"`
	WebURL      string   `json:"web_url"`
}

// ListIssuesParams are the query params we plumb through.
type ListIssuesParams struct {
	// State filter: "opened", "closed", "all". Default "all".
	State string
	// UpdatedAfter: RFC3339 timestamp (e.g. "2026-04-17T00:00:00Z").
	UpdatedAfter string
}

// ListIssues paginates GET /projects/:id/issues with state=all by default.
func (c *Client) ListIssues(ctx context.Context, token string, projectID int64, params ListIssuesParams) ([]Issue, error) {
	state := params.State
	if state == "" {
		state = "all"
	}
	q := url.Values{}
	q.Set("state", state)
	q.Set("per_page", "100")
	q.Set("order_by", "updated_at")
	q.Set("sort", "asc")
	if params.UpdatedAfter != "" {
		q.Set("updated_after", params.UpdatedAfter)
	}
	path := fmt.Sprintf("/projects/%d/issues?%s", projectID, q.Encode())

	var all []Issue
	err := iteratePages[Issue](ctx, c, token, path, func(batch []Issue) error {
		all = append(all, batch...)
		return nil
	})
	return all, err
}
```

- [ ] **Step 6.4: Run — expect pass**

```bash
cd server && go test ./pkg/gitlab/ -v
```

- [ ] **Step 6.5: Commit**

```bash
git add server/pkg/gitlab/issues.go server/pkg/gitlab/issues_test.go
git commit -m "feat(gitlab): ListIssues (paginated, state/updated_after params)"
```

---

## Task 7: GitLab client — `ListNotes`

**Files:**
- Create: `server/pkg/gitlab/notes.go`
- Create: `server/pkg/gitlab/notes_test.go`

- [ ] **Step 7.1: Write failing test**

```go
package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListNotes_PerIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/7/issues/42/notes" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Note{
			{ID: 1, Body: "hello", System: false, Author: User{ID: 100, Username: "alice"}, UpdatedAt: "2026-04-17T10:00:00Z"},
			{ID: 2, Body: "added status::todo", System: true, Author: User{ID: 200, Username: "bot"}, UpdatedAt: "2026-04-17T10:01:00Z"},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	notes, err := c.ListNotes(context.Background(), "tok", 7, 42)
	if err != nil {
		t.Fatalf("ListNotes: %v", err)
	}
	if len(notes) != 2 || !notes[1].System {
		t.Errorf("unexpected: %+v", notes)
	}
}
```

- [ ] **Step 7.2: Run — expect compile error**

```bash
cd server && go test ./pkg/gitlab/ -run TestListNotes -v
```

- [ ] **Step 7.3: Implement**

Create `server/pkg/gitlab/notes.go`:

```go
package gitlab

import (
	"context"
	"fmt"
)

// Note mirrors the subset of GitLab's note shape we cache.
type Note struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	System    bool   `json:"system"`
	Author    User   `json:"author"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListNotes paginates notes (comments + system notes) on one issue.
func (c *Client) ListNotes(ctx context.Context, token string, projectID int64, issueIID int) ([]Note, error) {
	var all []Note
	path := fmt.Sprintf("/projects/%d/issues/%d/notes?per_page=100&sort=asc&order_by=created_at", projectID, issueIID)
	err := iteratePages[Note](ctx, c, token, path, func(batch []Note) error {
		all = append(all, batch...)
		return nil
	})
	return all, err
}
```

- [ ] **Step 7.4: Run — expect pass**

```bash
cd server && go test ./pkg/gitlab/ -v
```

- [ ] **Step 7.5: Commit**

```bash
git add server/pkg/gitlab/notes.go server/pkg/gitlab/notes_test.go
git commit -m "feat(gitlab): ListNotes per issue"
```

---

## Task 8: GitLab client — `ListAwardEmoji`

**Files:**
- Create: `server/pkg/gitlab/award_emoji.go`
- Create: `server/pkg/gitlab/award_emoji_test.go`

- [ ] **Step 8.1: Write failing test**

```go
package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListAwardEmoji_PerIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/projects/7/issues/42/award_emoji" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]AwardEmoji{
			{ID: 1, Name: "thumbsup", User: User{ID: 100, Username: "alice"}, UpdatedAt: "2026-04-17T10:00:00Z"},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	awards, err := c.ListAwardEmoji(context.Background(), "tok", 7, 42)
	if err != nil {
		t.Fatalf("ListAwardEmoji: %v", err)
	}
	if len(awards) != 1 || awards[0].Name != "thumbsup" {
		t.Errorf("unexpected: %+v", awards)
	}
}
```

- [ ] **Step 8.2: Run — expect compile error**

```bash
cd server && go test ./pkg/gitlab/ -run TestListAwardEmoji -v
```

- [ ] **Step 8.3: Implement**

Create `server/pkg/gitlab/award_emoji.go`:

```go
package gitlab

import (
	"context"
	"fmt"
)

// AwardEmoji mirrors GitLab's award emoji shape (an emoji reaction on an
// issue or note).
type AwardEmoji struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"` // "thumbsup", "heart", etc.
	User      User   `json:"user"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListAwardEmoji paginates award emoji on one issue.
func (c *Client) ListAwardEmoji(ctx context.Context, token string, projectID int64, issueIID int) ([]AwardEmoji, error) {
	var all []AwardEmoji
	path := fmt.Sprintf("/projects/%d/issues/%d/award_emoji?per_page=100", projectID, issueIID)
	err := iteratePages[AwardEmoji](ctx, c, token, path, func(batch []AwardEmoji) error {
		all = append(all, batch...)
		return nil
	})
	return all, err
}
```

- [ ] **Step 8.4: Run — expect pass**

```bash
cd server && go test ./pkg/gitlab/ -v
```

- [ ] **Step 8.5: Commit**

```bash
git add server/pkg/gitlab/award_emoji.go server/pkg/gitlab/award_emoji_test.go
git commit -m "feat(gitlab): ListAwardEmoji per issue"
```

---

## Task 9: Translator — GitLab Issue → cache values

**Files:**
- Create: `server/internal/gitlab/translator.go`
- Create: `server/internal/gitlab/translator_test.go`

The translator is the single source of truth for converting GitLab JSON into Multica cache values. Pure functions, no I/O. The functions take the parsed GitLab struct + a small context object (`workspaceID`, optionally a slug→agentUUID lookup) and return a struct of cache-row values.

- [ ] **Step 9.1: Write failing tests**

Create `server/internal/gitlab/translator_test.go`:

```go
package gitlab

import (
	"testing"

	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

func TestTranslateIssue_StatusFromScopedLabel(t *testing.T) {
	in := gitlabapi.Issue{
		IID:    42,
		Title:  "Hi",
		State:  "opened",
		Labels: []string{"status::in_progress", "bug"},
	}
	out := TranslateIssue(in, &TranslateContext{AgentBySlug: nil})
	if out.Status != "in_progress" {
		t.Errorf("Status = %q, want in_progress", out.Status)
	}
}

func TestTranslateIssue_StatusFallsBackToTodoForOpened(t *testing.T) {
	in := gitlabapi.Issue{
		IID:    42,
		Labels: []string{"bug"},
		State:  "opened",
	}
	out := TranslateIssue(in, &TranslateContext{})
	if out.Status != "todo" {
		t.Errorf("Status = %q, want todo (default for opened)", out.Status)
	}
}

func TestTranslateIssue_StatusFallsBackToDoneForClosed(t *testing.T) {
	in := gitlabapi.Issue{
		IID:    42,
		Labels: []string{"bug"},
		State:  "closed",
	}
	out := TranslateIssue(in, &TranslateContext{})
	if out.Status != "done" {
		t.Errorf("Status = %q, want done (default for closed)", out.Status)
	}
}

func TestTranslateIssue_PriorityFromScopedLabel(t *testing.T) {
	in := gitlabapi.Issue{Labels: []string{"priority::high"}, State: "opened"}
	out := TranslateIssue(in, &TranslateContext{})
	if out.Priority != "high" {
		t.Errorf("Priority = %q, want high", out.Priority)
	}
}

func TestTranslateIssue_PriorityDefaultsToNone(t *testing.T) {
	in := gitlabapi.Issue{Labels: []string{"bug"}, State: "opened"}
	out := TranslateIssue(in, &TranslateContext{})
	if out.Priority != "none" {
		t.Errorf("Priority = %q, want none", out.Priority)
	}
}

func TestTranslateIssue_AgentAssigneeFromScopedLabel(t *testing.T) {
	in := gitlabapi.Issue{Labels: []string{"agent::builder"}, State: "opened"}
	out := TranslateIssue(in, &TranslateContext{
		AgentBySlug: map[string]string{"builder": "agent-uuid-123"},
	})
	if out.AssigneeType != "agent" || out.AssigneeID != "agent-uuid-123" {
		t.Errorf("Assignee = (%q, %q), want (agent, agent-uuid-123)", out.AssigneeType, out.AssigneeID)
	}
}

func TestTranslateIssue_AgentLabelWithUnknownSlugLeavesUnassigned(t *testing.T) {
	in := gitlabapi.Issue{Labels: []string{"agent::ghost"}, State: "opened"}
	out := TranslateIssue(in, &TranslateContext{
		AgentBySlug: map[string]string{"builder": "uuid-builder"},
	})
	if out.AssigneeType != "" || out.AssigneeID != "" {
		t.Errorf("Assignee should be empty for unknown agent slug, got (%q, %q)", out.AssigneeType, out.AssigneeID)
	}
}

func TestTranslateIssue_NativeAssigneeIgnoredInPhase2a(t *testing.T) {
	// Phase 3 will resolve GitLab user → Multica member. Until then we leave
	// the assignee blank rather than guess.
	in := gitlabapi.Issue{
		Labels:    []string{},
		State:     "opened",
		Assignees: []gitlabapi.User{{ID: 100, Username: "alice"}},
	}
	out := TranslateIssue(in, &TranslateContext{})
	if out.AssigneeType != "" || out.AssigneeID != "" {
		t.Errorf("Native assignee should be ignored in 2a, got (%q, %q)", out.AssigneeType, out.AssigneeID)
	}
}

func TestTranslateIssue_MultipleAgentLabelsPicksFirstAlphabetically(t *testing.T) {
	in := gitlabapi.Issue{
		Labels: []string{"agent::zebra", "agent::alpha"},
		State:  "opened",
	}
	out := TranslateIssue(in, &TranslateContext{
		AgentBySlug: map[string]string{"alpha": "uuid-a", "zebra": "uuid-z"},
	})
	if out.AssigneeID != "uuid-a" {
		t.Errorf("AssigneeID = %q, want uuid-a (first alphabetical)", out.AssigneeID)
	}
}

func TestTranslateNote_StripsAgentPrefix(t *testing.T) {
	in := gitlabapi.Note{
		Body:   "**[agent:builder]** I'm working on it.",
		System: false,
	}
	out := TranslateNote(in)
	if out.AuthorType != "agent" || out.AuthorSlug != "builder" {
		t.Errorf("Author = (%q, %q), want (agent, builder)", out.AuthorType, out.AuthorSlug)
	}
	if out.Body != "I'm working on it." {
		t.Errorf("Body = %q, want stripped", out.Body)
	}
	if out.Type != "comment" {
		t.Errorf("Type = %q, want comment", out.Type)
	}
}

func TestTranslateNote_SystemNote(t *testing.T) {
	in := gitlabapi.Note{Body: "added status::todo", System: true}
	out := TranslateNote(in)
	if out.Type != "system" {
		t.Errorf("Type = %q, want system", out.Type)
	}
	if out.AuthorType != "" {
		t.Errorf("Author should be empty for system note, got %q", out.AuthorType)
	}
}

func TestTranslateAward_PassesEmoji(t *testing.T) {
	in := gitlabapi.AwardEmoji{Name: "thumbsup", User: gitlabapi.User{ID: 100}}
	out := TranslateAward(in)
	if out.Emoji != "thumbsup" {
		t.Errorf("Emoji = %q, want thumbsup", out.Emoji)
	}
	if out.GitlabUserID != 100 {
		t.Errorf("GitlabUserID = %d, want 100", out.GitlabUserID)
	}
}
```

- [ ] **Step 9.2: Run — expect compile error**

```bash
cd server && go test ./internal/gitlab/ -run TestTranslate -v
```

- [ ] **Step 9.3: Implement**

Create `server/internal/gitlab/translator.go`:

```go
// Package gitlab houses the domain glue between the raw gitlab REST client
// (server/pkg/gitlab) and Multica's cache schema. Pure translation lives in
// translator.go; orchestration (sync, webhook, reconcile) lives in sibling
// files.
package gitlab

import (
	"sort"
	"strings"

	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

// TranslateContext carries lookups the translator needs but doesn't fetch
// itself (so the function stays pure).
type TranslateContext struct {
	// AgentBySlug maps Multica agent slug → agent UUID (string form).
	// Populated by the caller from the agents table.
	AgentBySlug map[string]string
}

// IssueValues are the cache-row values we'll write into the issue table.
// Strings are returned in their final form; the SQL layer converts to
// pgtype where needed.
type IssueValues struct {
	Title        string
	Description  string
	Status       string // backlog | todo | in_progress | in_review | done | blocked | cancelled
	Priority     string // urgent | high | medium | low | none
	AssigneeType string // "" | "member" | "agent"
	AssigneeID   string // "" | UUID string
	DueDate      string // YYYY-MM-DD or "" — caller converts to pgtype.Date
	UpdatedAt    string // RFC3339 from GitLab — caller converts
}

// TranslateIssue converts a GitLab issue into Multica cache values.
func TranslateIssue(in gitlabapi.Issue, tc *TranslateContext) IssueValues {
	if tc == nil {
		tc = &TranslateContext{}
	}
	out := IssueValues{
		Title:       in.Title,
		Description: in.Description,
		DueDate:     in.DueDate,
		UpdatedAt:   in.UpdatedAt,
		Status:      pickStatus(in.Labels, in.State),
		Priority:    pickPriority(in.Labels),
	}
	out.AssigneeType, out.AssigneeID = pickAssignee(in.Labels, tc.AgentBySlug)
	return out
}

// pickStatus extracts a Multica status from `status::*` scoped labels.
// Defaults: "todo" for opened, "done" for closed (per the spec).
// Multiple status labels: pick first alphabetically (canonical order).
func pickStatus(labels []string, state string) string {
	const prefix = "status::"
	var found []string
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			found = append(found, strings.TrimPrefix(l, prefix))
		}
	}
	if len(found) == 0 {
		if state == "closed" {
			return "done"
		}
		return "todo"
	}
	sort.Strings(found)
	return found[0]
}

// pickPriority extracts a priority from `priority::*` scoped labels.
// Default: "none".
func pickPriority(labels []string) string {
	const prefix = "priority::"
	var found []string
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			found = append(found, strings.TrimPrefix(l, prefix))
		}
	}
	if len(found) == 0 {
		return "none"
	}
	sort.Strings(found)
	return found[0]
}

// pickAssignee resolves an agent assignment from `agent::<slug>` labels.
// Returns ("", "") for native GitLab assignees (Phase 3 will resolve those)
// or unknown agent slugs.
// Multiple agent labels: pick first alphabetically.
func pickAssignee(labels []string, agentBySlug map[string]string) (assigneeType, assigneeID string) {
	const prefix = "agent::"
	var slugs []string
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			slugs = append(slugs, strings.TrimPrefix(l, prefix))
		}
	}
	if len(slugs) == 0 {
		return "", ""
	}
	sort.Strings(slugs)
	for _, s := range slugs {
		if id, ok := agentBySlug[s]; ok {
			return "agent", id
		}
	}
	return "", ""
}

// NoteValues are cache-row values for one comment.
type NoteValues struct {
	Body         string
	Type         string // "comment" | "system"
	AuthorType   string // "" | "agent" | "member" — Phase 2a sets "agent" only when prefix detected
	AuthorSlug   string // populated when AuthorType=="agent"
	GitlabUserID int64  // GitLab user id from the note's author
	UpdatedAt    string
}

// TranslateNote converts a GitLab note into cache values. System notes are
// flagged so the caller can preserve type='system'. Agent-authored notes
// (recognized via the **[agent:slug]** prefix our future write path will
// emit) get AuthorType="agent" and the prefix is stripped from the body.
func TranslateNote(in gitlabapi.Note) NoteValues {
	out := NoteValues{
		Body:         in.Body,
		Type:         "comment",
		GitlabUserID: in.Author.ID,
		UpdatedAt:    in.UpdatedAt,
	}
	if in.System {
		out.Type = "system"
		return out
	}
	if slug, body, ok := parseAgentPrefix(in.Body); ok {
		out.AuthorType = "agent"
		out.AuthorSlug = slug
		out.Body = body
	}
	return out
}

// parseAgentPrefix detects "**[agent:<slug>]** " at the start of a note body.
// Returns slug, stripped body, ok.
func parseAgentPrefix(body string) (slug, stripped string, ok bool) {
	const open = "**[agent:"
	const close = "]** "
	if !strings.HasPrefix(body, open) {
		return "", "", false
	}
	rest := body[len(open):]
	idx := strings.Index(rest, close)
	if idx <= 0 {
		return "", "", false
	}
	return rest[:idx], rest[idx+len(close):], true
}

// AwardValues are cache-row values for one issue reaction.
type AwardValues struct {
	Emoji        string
	GitlabUserID int64
	UpdatedAt    string
}

// TranslateAward converts a GitLab award emoji into cache values.
func TranslateAward(in gitlabapi.AwardEmoji) AwardValues {
	return AwardValues{
		Emoji:        in.Name,
		GitlabUserID: in.User.ID,
		UpdatedAt:    in.UpdatedAt,
	}
}
```

- [ ] **Step 9.4: Run — expect pass**

```bash
cd server && go test ./internal/gitlab/ -v
```
Expected: all 11 translator tests PASS.

- [ ] **Step 9.5: Commit**

```bash
git add server/internal/gitlab/translator.go server/internal/gitlab/translator_test.go
git commit -m "feat(gitlab): translator for issue/note/award → cache values"
```

---

## Task 10: Labels bootstrap

**Files:**
- Create: `server/internal/gitlab/labels_bootstrap.go`
- Create: `server/internal/gitlab/labels_bootstrap_test.go`

The bootstrap provisions the canonical `status::*` and `priority::*` labels in a project so the translator's reads have something to find. Idempotent: skip labels that already exist.

- [ ] **Step 10.1: Write failing test**

```go
package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

func TestBootstrapScopedLabels_CreatesMissingOnly(t *testing.T) {
	var mu sync.Mutex
	existing := map[string]bool{
		"status::todo": true, // pre-existing — should NOT be re-created
		"bug":          true,
	}
	created := map[string]bool{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			out := []gitlabapi.Label{}
			mu.Lock()
			for n := range existing {
				out = append(out, gitlabapi.Label{ID: int64(len(out) + 1), Name: n})
			}
			mu.Unlock()
			json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			var in gitlabapi.CreateLabelInput
			json.NewDecoder(r.Body).Decode(&in)
			mu.Lock()
			created[in.Name] = true
			existing[in.Name] = true
			mu.Unlock()
			json.NewEncoder(w).Encode(gitlabapi.Label{ID: 999, Name: in.Name, Color: in.Color})
		}
	}))
	defer srv.Close()

	c := gitlabapi.NewClient(srv.URL, srv.Client())
	if err := BootstrapScopedLabels(context.Background(), c, "tok", 7); err != nil {
		t.Fatalf("BootstrapScopedLabels: %v", err)
	}

	// Should have created every status:: and priority:: label EXCEPT status::todo.
	mu.Lock()
	defer mu.Unlock()
	for _, name := range CanonicalScopedLabelNames() {
		if name == "status::todo" {
			if created[name] {
				t.Errorf("re-created pre-existing label %q", name)
			}
			continue
		}
		if !created[name] {
			t.Errorf("did not create missing label %q", name)
		}
	}
}
```

- [ ] **Step 10.2: Run — expect compile error**

```bash
cd server && go test ./internal/gitlab/ -run TestBootstrap -v
```

- [ ] **Step 10.3: Implement**

Create `server/internal/gitlab/labels_bootstrap.go`:

```go
package gitlab

import (
	"context"
	"fmt"

	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

// canonicalLabel describes a Multica-managed scoped label and its visual
// presentation in GitLab.
type canonicalLabel struct {
	Name  string
	Color string
}

// canonicalScopedLabels is the full set of Multica-managed scoped labels.
// Colors are roughly aligned with the issue-status / priority palette.
var canonicalScopedLabels = []canonicalLabel{
	{"status::backlog", "#9b9b9b"},
	{"status::todo", "#cccccc"},
	{"status::in_progress", "#3b82f6"},
	{"status::in_review", "#8b5cf6"},
	{"status::done", "#10b981"},
	{"status::blocked", "#ef4444"},
	{"status::cancelled", "#6b7280"},
	{"priority::urgent", "#dc2626"},
	{"priority::high", "#f97316"},
	{"priority::medium", "#eab308"},
	{"priority::low", "#84cc16"},
	{"priority::none", "#9ca3af"},
}

// CanonicalScopedLabelNames returns just the names — used by tests to
// assert what should be present.
func CanonicalScopedLabelNames() []string {
	out := make([]string, len(canonicalScopedLabels))
	for i, l := range canonicalScopedLabels {
		out[i] = l.Name
	}
	return out
}

// BootstrapScopedLabels ensures every canonical Multica scoped label exists
// in the project. Existing labels (including those an admin already created
// with a different color) are left untouched.
func BootstrapScopedLabels(ctx context.Context, c *gitlabapi.Client, token string, projectID int64) error {
	existing, err := c.ListLabels(ctx, token, projectID)
	if err != nil {
		return fmt.Errorf("bootstrap: list labels: %w", err)
	}
	have := make(map[string]bool, len(existing))
	for _, l := range existing {
		have[l.Name] = true
	}
	for _, l := range canonicalScopedLabels {
		if have[l.Name] {
			continue
		}
		if _, err := c.CreateLabel(ctx, token, projectID, gitlabapi.CreateLabelInput{
			Name:        l.Name,
			Color:       l.Color,
			Description: "Managed by Multica",
		}); err != nil {
			return fmt.Errorf("bootstrap: create label %q: %w", l.Name, err)
		}
	}
	return nil
}
```

- [ ] **Step 10.4: Run — expect pass**

```bash
cd server && go test ./internal/gitlab/ -v
```

- [ ] **Step 10.5: Commit**

```bash
git add server/internal/gitlab/labels_bootstrap.go server/internal/gitlab/labels_bootstrap_test.go
git commit -m "feat(gitlab): bootstrap canonical status/priority scoped labels"
```

---

## Task 11: Initial sync — orchestrator scaffold

**Files:**
- Create: `server/internal/gitlab/initial_sync.go`
- Create: `server/internal/gitlab/initial_sync_test.go`

The orchestrator runs in a goroutine spawned by the connect handler. It does the following in order:
1. Bootstrap canonical scoped labels.
2. List all labels and upsert into `gitlab_label`.
3. List project members and upsert into `gitlab_project_member`.
4. List all issues; for each one (with bounded parallelism of 5):
   - Upsert the issue.
   - Set the issue's labels via `SetIssueLabels`.
   - List + upsert notes.
   - List + upsert award emoji.
5. Update `connection_status='connected'` on success, or `'error'` with `status_message` on failure.

This task adds the scaffold + the labels/members steps. Tasks 12 and 13 add the issue/notes/awards step and finalize.

- [ ] **Step 11.1: Write failing test (scaffold + steps 1–3)**

Create `server/internal/gitlab/initial_sync_test.go`:

```go
package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

// connectTestPool connects to the worktree DB. Test is skipped if unreachable.
func connectTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil || pool.Ping(context.Background()) != nil {
		t.Skip("database not reachable")
	}
	return pool
}

// makeWorkspace inserts a throwaway workspace + returns its UUID.
func makeWorkspace(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('GL Sync Test', 'gl-sync-test-'||substr(gen_random_uuid()::text, 1, 8), '', 'GST')
		RETURNING id
	`).Scan(&id); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, id)
	})
	return id
}

func TestInitialSync_LabelsAndMembers(t *testing.T) {
	pool := connectTestPool(t)
	defer pool.Close()
	wsID := makeWorkspace(t, pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/7/labels":
			if r.Method == http.MethodGet {
				json.NewEncoder(w).Encode([]gitlabapi.Label{
					{ID: 1, Name: "bug", Color: "#ff0000"},
				})
			} else {
				// Bootstrap creating a missing label.
				w.Write([]byte(`{"id":99,"name":"x","color":"#000"}`))
			}
		case "/api/v4/projects/7/members/all":
			json.NewEncoder(w).Encode([]gitlabapi.ProjectMember{
				{ID: 100, Username: "alice", Name: "Alice", AvatarURL: "https://x"},
			})
		case "/api/v4/projects/7/issues":
			// No issues yet for this scoped test.
			json.NewEncoder(w).Encode([]gitlabapi.Issue{})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	queries := db.New(pool)
	deps := SyncDeps{
		Queries: queries,
		Client:  gitlabapi.NewClient(srv.URL, srv.Client()),
	}
	err := RunInitialSync(context.Background(), deps, RunInitialSyncInput{
		WorkspaceID: wsID,
		ProjectID:   7,
		Token:       "tok",
	})
	if err != nil {
		t.Fatalf("RunInitialSync: %v", err)
	}

	// Verify gitlab_label rows exist (bootstrap inserts canonical labels +
	// the single existing "bug" label).
	rows, _ := queries.ListGitlabLabels(context.Background(), parseUUID(t, wsID))
	if len(rows) == 0 {
		t.Errorf("no gitlab_label rows after sync")
	}

	// Verify gitlab_project_member row.
	members, _ := queries.ListGitlabProjectMembers(context.Background(), parseUUID(t, wsID))
	if len(members) != 1 || members[0].Username != "alice" {
		t.Errorf("members = %+v, want one alice", members)
	}
}

// parseUUID is defined alongside mustPGUUID in Step 11.2 below.
func parseUUID(t *testing.T, s string) pgtype.UUID {
	return mustPGUUID(t, s)
}
```

- [ ] **Step 11.2: Add the pgtype helper to the test file**

Add `"github.com/jackc/pgx/v5/pgtype"` to the imports at the top of `initial_sync_test.go`, then add this helper at the bottom of the file (above `mustPGUUID` is the helper used by `parseUUID`):

```go
func mustPGUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("scan uuid: %v", err)
	}
	return u
}
```

Update the earlier `parseUUID` test helper to delegate:

```go
func parseUUID(t *testing.T, s string) pgtype.UUID {
	return mustPGUUID(t, s)
}
```

- [ ] **Step 11.3: Run — expect compile error**

```bash
cd server && DATABASE_URL="$(grep DATABASE_URL .env.worktree 2>/dev/null | cut -d= -f2-)" go test ./internal/gitlab/ -run TestInitialSync -v
```
Expected: compile error — `RunInitialSync` and friends undefined.

- [ ] **Step 11.4: Implement scaffold + labels/members steps**

Create `server/internal/gitlab/initial_sync.go`:

```go
package gitlab

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

// SyncDeps is the set of plumbing the sync orchestrator needs.
type SyncDeps struct {
	Queries *db.Queries
	Client  *gitlabapi.Client
}

// RunInitialSyncInput is the per-call input.
type RunInitialSyncInput struct {
	WorkspaceID string // Multica workspace UUID
	ProjectID   int64  // GitLab project numeric ID
	Token       string // service PAT (decrypted)
}

// RunInitialSync orchestrates a one-shot pull of all GitLab project state
// into Multica's cache tables for one workspace.
func RunInitialSync(ctx context.Context, deps SyncDeps, in RunInitialSyncInput) error {
	wsUUID, err := pgUUID(in.WorkspaceID)
	if err != nil {
		return fmt.Errorf("initial sync: workspace_id: %w", err)
	}

	// 1. Bootstrap canonical scoped labels (idempotent).
	if err := BootstrapScopedLabels(ctx, deps.Client, in.Token, in.ProjectID); err != nil {
		return fmt.Errorf("initial sync: bootstrap labels: %w", err)
	}

	// 2. Fetch + upsert all labels.
	labels, err := deps.Client.ListLabels(ctx, in.Token, in.ProjectID)
	if err != nil {
		return fmt.Errorf("initial sync: list labels: %w", err)
	}
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	for _, l := range labels {
		if _, err := deps.Queries.UpsertGitlabLabel(ctx, db.UpsertGitlabLabelParams{
			WorkspaceID:       wsUUID,
			GitlabLabelID:     l.ID,
			Name:              l.Name,
			Color:             l.Color,
			Description:       l.Description,
			ExternalUpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("initial sync: upsert label %q: %w", l.Name, err)
		}
	}

	// 3. Fetch + upsert project members.
	members, err := deps.Client.ListProjectMembers(ctx, in.Token, in.ProjectID)
	if err != nil {
		return fmt.Errorf("initial sync: list members: %w", err)
	}
	for _, m := range members {
		if _, err := deps.Queries.UpsertGitlabProjectMember(ctx, db.UpsertGitlabProjectMemberParams{
			WorkspaceID:       wsUUID,
			GitlabUserID:      m.ID,
			Username:          m.Username,
			Name:              m.Name,
			AvatarUrl:         m.AvatarURL,
			ExternalUpdatedAt: now,
		}); err != nil {
			return fmt.Errorf("initial sync: upsert member %q: %w", m.Username, err)
		}
	}

	// 4. Issues + notes + awards (Task 12 implements this).
	if err := syncAllIssues(ctx, deps, in, wsUUID); err != nil {
		return fmt.Errorf("initial sync: issues: %w", err)
	}

	return nil
}

// pgUUID converts a string UUID to pgtype.UUID, returning an error for
// invalid input.
func pgUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return u, err
	}
	return u, nil
}

// syncAllIssues is implemented in Task 12. Stub here so the file compiles.
func syncAllIssues(ctx context.Context, deps SyncDeps, in RunInitialSyncInput, wsUUID pgtype.UUID) error {
	return nil
}
```

- [ ] **Step 11.5: Run — expect pass**

```bash
cd server && DATABASE_URL="postgres://multica:multica@localhost:5432/multica_multica_gitlab_phase_2a_NNN?sslmode=disable" go test ./internal/gitlab/ -run TestInitialSync_LabelsAndMembers -v
```

(Substitute the real worktree DB URL — read it from `.env.worktree`.)

Expected: PASS. Skipped if DB unreachable.

- [ ] **Step 11.6: Commit**

```bash
git add server/internal/gitlab/initial_sync.go server/internal/gitlab/initial_sync_test.go
git commit -m "feat(gitlab): initial sync scaffold (labels + members)"
```

---

## Task 12: Initial sync — issues + notes + awards step

**Files:**
- Modify: `server/internal/gitlab/initial_sync.go`
- Modify: `server/internal/gitlab/initial_sync_test.go`

Implements `syncAllIssues`: list every issue, then for each one (with bounded concurrency of 5) upsert the issue, replace its label associations, fetch+upsert its notes, fetch+upsert its awards.

For Phase 2a we only need the path that handles the simpler issue cases (no native assignees → Multica members yet). Notes whose author is a GitLab user (not the bot prefix) get author_type/id NULL.

- [ ] **Step 12.1: Append failing test**

Append to `initial_sync_test.go`:

```go
func TestInitialSync_IssuesNotesAwards(t *testing.T) {
	pool := connectTestPool(t)
	defer pool.Close()
	wsID := makeWorkspace(t, pool)

	// First insert an agent so an `agent::builder` label can resolve.
	var agentID string
	pool.QueryRow(context.Background(), `
		INSERT INTO agent (workspace_id, name, slug, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'Builder', 'builder', '', 'cloud', '{}'::jsonb, NULL, 'workspace', 1, NULL)
		RETURNING id
	`, wsID).Scan(&agentID)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v4/projects/7/labels" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode([]gitlabapi.Label{
				{ID: 10, Name: "status::in_progress", Color: "#3b82f6"},
				{ID: 11, Name: "agent::builder", Color: "#8b5cf6"},
			})
		case r.URL.Path == "/api/v4/projects/7/labels" && r.Method == http.MethodPost:
			// Bootstrap creating missing canonical labels.
			w.Write([]byte(`{"id":999,"name":"x"}`))
		case r.URL.Path == "/api/v4/projects/7/members/all":
			json.NewEncoder(w).Encode([]gitlabapi.ProjectMember{})
		case r.URL.Path == "/api/v4/projects/7/issues":
			json.NewEncoder(w).Encode([]gitlabapi.Issue{
				{
					ID: 1001, IID: 42, Title: "First issue",
					Description: "body", State: "opened",
					Labels: []string{"status::in_progress", "agent::builder"},
					UpdatedAt: "2026-04-17T10:00:00Z",
				},
			})
		case r.URL.Path == "/api/v4/projects/7/issues/42/notes":
			json.NewEncoder(w).Encode([]gitlabapi.Note{
				{ID: 1, Body: "hello", System: false,
					Author:    gitlabapi.User{ID: 100, Username: "alice"},
					UpdatedAt: "2026-04-17T10:01:00Z"},
			})
		case r.URL.Path == "/api/v4/projects/7/issues/42/award_emoji":
			json.NewEncoder(w).Encode([]gitlabapi.AwardEmoji{
				{ID: 5, Name: "thumbsup",
					User:      gitlabapi.User{ID: 100, Username: "alice"},
					UpdatedAt: "2026-04-17T10:02:00Z"},
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	queries := db.New(pool)
	deps := SyncDeps{Queries: queries, Client: gitlabapi.NewClient(srv.URL, srv.Client())}
	err := RunInitialSync(context.Background(), deps, RunInitialSyncInput{
		WorkspaceID: wsID,
		ProjectID:   7,
		Token:       "tok",
	})
	if err != nil {
		t.Fatalf("RunInitialSync: %v", err)
	}

	// Verify the issue exists with the right status + agent assignment.
	row, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: mustPGUUID(t, wsID),
		GitlabIid:   pgtype.Int4{Int32: 42, Valid: true},
	})
	if err != nil {
		t.Fatalf("GetIssueByGitlabIID: %v", err)
	}
	if row.Status != "in_progress" {
		t.Errorf("status = %q, want in_progress", row.Status)
	}
	if !row.AssigneeType.Valid || row.AssigneeType.String != "agent" {
		t.Errorf("assignee_type = %+v, want agent", row.AssigneeType)
	}
}
```

- [ ] **Step 12.2: Run — expect failure (issue isn't synced because syncAllIssues is a stub)**

```bash
cd server && DATABASE_URL="…" go test ./internal/gitlab/ -run TestInitialSync_IssuesNotesAwards -v
```
Expected: GetIssueByGitlabIID returns no rows.

- [ ] **Step 12.3: Implement `syncAllIssues`**

Replace the stub at the bottom of `initial_sync.go` with:

```go
import (
	// …existing…
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

// syncAllIssues fetches every project issue and upserts each (with notes,
// awards, and label associations) into the cache. Concurrency bounded at 5.
func syncAllIssues(ctx context.Context, deps SyncDeps, in RunInitialSyncInput, wsUUID pgtype.UUID) error {
	issues, err := deps.Client.ListIssues(ctx, in.Token, in.ProjectID, gitlabapi.ListIssuesParams{State: "all"})
	if err != nil {
		return fmt.Errorf("list issues: %w", err)
	}

	// Build the agent-slug → uuid lookup once for the translator.
	agentMap, err := buildAgentSlugMap(ctx, deps.Queries, wsUUID)
	if err != nil {
		return fmt.Errorf("agent map: %w", err)
	}

	// Build the gitlab_label_id by name lookup so we can set issue_to_label
	// associations from the issue's label-name list.
	labels, err := deps.Queries.ListGitlabLabels(ctx, wsUUID)
	if err != nil {
		return fmt.Errorf("list cached labels: %w", err)
	}
	labelIDByName := make(map[string]int64, len(labels))
	for _, l := range labels {
		labelIDByName[l.Name] = l.GitlabLabelID
	}

	const maxConcurrent = 5
	sem := make(chan struct{}, maxConcurrent)
	errs := make(chan error, len(issues))
	var wg sync.WaitGroup

	for _, issue := range issues {
		issue := issue
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := syncOneIssue(ctx, deps, in, wsUUID, issue, agentMap, labelIDByName); err != nil {
				errs <- fmt.Errorf("issue iid=%d: %w", issue.IID, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		// Return the first error; subsequent ones still drained off the chan.
		return e
	}
	return nil
}

func syncOneIssue(
	ctx context.Context,
	deps SyncDeps,
	in RunInitialSyncInput,
	wsUUID pgtype.UUID,
	issue gitlabapi.Issue,
	agentMap map[string]string,
	labelIDByName map[string]int64,
) error {
	// 1. Translate + upsert the issue.
	values := TranslateIssue(issue, &TranslateContext{AgentBySlug: agentMap})

	row, err := deps.Queries.UpsertIssueFromGitlab(ctx, buildUpsertIssueParams(wsUUID, in.ProjectID, issue, values))
	if err != nil {
		return fmt.Errorf("upsert issue: %w", err)
	}

	// 2. Replace label associations.
	labelIDs := make([]int64, 0, len(issue.Labels))
	for _, name := range issue.Labels {
		if id, ok := labelIDByName[name]; ok {
			labelIDs = append(labelIDs, id)
		}
	}
	if err := deps.Queries.ClearIssueLabels(ctx, row.ID); err != nil {
		return fmt.Errorf("clear labels: %w", err)
	}
	if len(labelIDs) > 0 {
		if err := deps.Queries.AddIssueLabels(ctx, db.AddIssueLabelsParams{
			IssueID:     row.ID,
			WorkspaceID: wsUUID,
			LabelIds:    labelIDs,
		}); err != nil {
			return fmt.Errorf("add labels: %w", err)
		}
	}

	// 3. Fetch + upsert notes.
	notes, err := deps.Client.ListNotes(ctx, in.Token, in.ProjectID, issue.IID)
	if err != nil {
		return fmt.Errorf("list notes: %w", err)
	}
	for _, n := range notes {
		nv := TranslateNote(n)
		// Phase 2a: leave author_type/id NULL for non-agent comments.
		// Phase 3 will resolve gitlab user → multica user.
		var authorType pgtype.Text
		var authorID pgtype.UUID
		if nv.AuthorType == "agent" {
			if uuidStr, ok := agentMap[nv.AuthorSlug]; ok {
				authorType = pgtype.Text{String: "agent", Valid: true}
				_ = authorID.Scan(uuidStr)
			}
		}
		if _, err := deps.Queries.UpsertCommentFromGitlab(ctx, db.UpsertCommentFromGitlabParams{
			IssueID:           row.ID,
			AuthorType:        authorType,
			AuthorID:          authorID,
			Body:              nv.Body,
			Type:              nv.Type,
			GitlabNoteID:      pgtype.Int8{Int64: n.ID, Valid: true},
			ExternalUpdatedAt: parseTS(nv.UpdatedAt),
		}); err != nil {
			return fmt.Errorf("upsert note %d: %w", n.ID, err)
		}
	}

	// 4. Fetch + upsert award emoji.
	awards, err := deps.Client.ListAwardEmoji(ctx, in.Token, in.ProjectID, issue.IID)
	if err != nil {
		return fmt.Errorf("list awards: %w", err)
	}
	for _, a := range awards {
		av := TranslateAward(a)
		// Phase 2a: leave user_id NULL for native gitlab users (Phase 3 maps).
		if _, err := deps.Queries.UpsertIssueReactionFromGitlab(ctx, db.UpsertIssueReactionFromGitlabParams{
			IssueID:           row.ID,
			UserID:            pgtype.UUID{},
			UserType:          pgtype.Text{},
			Emoji:             av.Emoji,
			GitlabAwardID:     pgtype.Int8{Int64: a.ID, Valid: true},
			ExternalUpdatedAt: parseTS(av.UpdatedAt),
		}); err != nil {
			return fmt.Errorf("upsert award %d: %w", a.ID, err)
		}
	}

	return nil
}

// buildUpsertIssueParams converts a translated IssueValues + the raw GitLab
// issue into the sqlc params struct.
func buildUpsertIssueParams(wsUUID pgtype.UUID, projectID int64, issue gitlabapi.Issue, values IssueValues) db.UpsertIssueFromGitlabParams {
	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	if values.AssigneeType != "" {
		assigneeType = pgtype.Text{String: values.AssigneeType, Valid: true}
		_ = assigneeID.Scan(values.AssigneeID)
	}
	desc := pgtype.Text{}
	if values.Description != "" {
		desc = pgtype.Text{String: values.Description, Valid: true}
	}
	return db.UpsertIssueFromGitlabParams{
		WorkspaceID:       wsUUID,
		GitlabIid:         pgtype.Int4{Int32: int32(issue.IID), Valid: true},
		GitlabProjectID:   pgtype.Int8{Int64: projectID, Valid: true},
		Title:             values.Title,
		Description:       desc,
		Status:            values.Status,
		Priority:          values.Priority,
		AssigneeType:      assigneeType,
		AssigneeID:        assigneeID,
		CreatorType:       pgtype.Text{}, // NULL — Phase 3 populates
		CreatorID:         pgtype.UUID{}, // NULL — Phase 3 populates
		DueDate:           parseDate(values.DueDate),
		ExternalUpdatedAt: parseTS(values.UpdatedAt),
	}
}

// buildAgentSlugMap loads slug→uuid for every agent in the workspace.
func buildAgentSlugMap(ctx context.Context, q *db.Queries, wsUUID pgtype.UUID) (map[string]string, error) {
	// We don't have a direct sqlc query yet — use a raw query via the existing
	// pattern. The agent table has columns id, slug, workspace_id.
	// (If a sqlc query for this already exists in the agent.sql file, prefer
	// that instead — the engineer should grep first.)
	type agentRow struct {
		ID   pgtype.UUID
		Slug string
	}
	// Cheap path: use the pool through deps.Queries — but Queries is opaque.
	// Simpler: list agents via existing "list agents in workspace" query if
	// present, otherwise add one.
	//
	// The pragmatic approach: add a new sqlc query to agent.sql in this same
	// task. Append to server/pkg/db/queries/agent.sql:
	//
	//   -- name: ListAgentSlugsInWorkspace :many
	//   SELECT id, slug FROM agent WHERE workspace_id = $1 AND slug IS NOT NULL;
	//
	// Then run `make sqlc` and call deps.Queries.ListAgentSlugsInWorkspace(ctx, wsUUID).
	rows, err := q.ListAgentSlugsInWorkspace(ctx, wsUUID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Slug] = uuidString(r.ID)
	}
	return out, nil
}

// parseTS converts a GitLab RFC3339 timestamp into pgtype.Timestamptz.
// Returns zero if input is empty or unparseable.
func parseTS(s string) pgtype.Timestamptz {
	if s == "" {
		return pgtype.Timestamptz{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// parseDate converts a GitLab "YYYY-MM-DD" date into pgtype.Date.
func parseDate(s string) pgtype.Date {
	if s == "" {
		return pgtype.Date{}
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}

// uuidString stringifies a pgtype.UUID. Returns "" if invalid.
func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	bs, _ := u.MarshalJSON()
	// MarshalJSON wraps in quotes; trim them.
	s := string(bs)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return s
}
```

Then add the sqlc query referenced above. Append to `server/pkg/db/queries/agent.sql`:

```sql
-- name: ListAgentSlugsInWorkspace :many
SELECT id, slug FROM agent WHERE workspace_id = $1 AND slug IS NOT NULL AND slug != '';
```

Run `make sqlc` to regenerate.

**Important caveat:** the sqlc query above requires the `slug` column on the `agent` table. If `agent` doesn't have a slug column today (it might use `name` as the identifier), the engineer should:
- Grep the agent table schema: `grep -A 20 "CREATE TABLE agent" server/migrations/001_init.up.sql`
- If no slug column exists, either (a) use `name` lowercased as the slug for Phase 2a (add a comment that Phase 4 might formalize agent slugs), OR (b) add a migration adding `slug TEXT NOT NULL DEFAULT ''` with a backfill from `lower(replace(name, ' ', '-'))`.

The simplest path for Phase 2a is **(a) — derive slug from agent.name at sync time**. Adjust `buildAgentSlugMap` accordingly:

```go
// If there's no slug column, derive from name.
type agentRow struct {
	ID   pgtype.UUID
	Name string
}
// Run the existing ListAgentsInWorkspace and convert names → slugs.
rows, err := q.ListAgentsInWorkspace(ctx, wsUUID)
if err != nil {
	return nil, err
}
out := make(map[string]string, len(rows))
for _, r := range rows {
	slug := strings.ToLower(strings.ReplaceAll(r.Name, " ", "-"))
	out[slug] = uuidString(r.ID)
}
return out, nil
```

The engineer should pick (a) or (b) based on what they find in the schema and update both the function and the test fixture (the test inserts an agent named "Builder" with slug "builder" — adjust the INSERT to match).

- [ ] **Step 12.4: Run — expect pass**

```bash
cd server && DATABASE_URL="…" go test ./internal/gitlab/ -run TestInitialSync_IssuesNotesAwards -v
```

Expected: PASS. The test asserts the issue exists in cache with status=in_progress and assignee_type=agent.

- [ ] **Step 12.5: Commit**

```bash
git add server/internal/gitlab/initial_sync.go server/internal/gitlab/initial_sync_test.go server/pkg/db/queries/agent.sql server/pkg/db/generated/
git commit -m "feat(gitlab): initial sync — issues, notes, awards (concurrency bounded at 5)"
```

---

## Task 13: Initial sync — status transitions on the connection row

**Files:**
- Modify: `server/internal/gitlab/initial_sync.go`
- Modify: `server/internal/gitlab/initial_sync_test.go`

The orchestrator currently runs and returns. To play nicely with the connect handler that dispatches it as a goroutine, it should also transition the `workspace_gitlab_connection.connection_status` from `'connecting'` → `'connected'` on success, or `'connecting'` → `'error'` (with a `status_message`) on failure.

This requires a new sqlc query.

- [ ] **Step 13.1: Add the status update query**

Append to `server/pkg/db/queries/gitlab_connection.sql`:

```sql
-- name: UpdateWorkspaceGitlabConnectionStatus :exec
UPDATE workspace_gitlab_connection
SET connection_status = $2,
    status_message    = $3,
    updated_at        = now()
WHERE workspace_id = $1;
```

Run `make sqlc`.

- [ ] **Step 13.2: Wrap the orchestrator's return**

Replace the body of `RunInitialSync` to call the existing logic via an inner function and report the result back to the connection row:

```go
func RunInitialSync(ctx context.Context, deps SyncDeps, in RunInitialSyncInput) error {
	wsUUID, err := pgUUID(in.WorkspaceID)
	if err != nil {
		return fmt.Errorf("initial sync: workspace_id: %w", err)
	}
	if err := runInitialSyncImpl(ctx, deps, in, wsUUID); err != nil {
		// Best-effort status update — log but don't override the original error.
		_ = deps.Queries.UpdateWorkspaceGitlabConnectionStatus(ctx, db.UpdateWorkspaceGitlabConnectionStatusParams{
			WorkspaceID:      wsUUID,
			ConnectionStatus: "error",
			StatusMessage:    pgtype.Text{String: err.Error(), Valid: true},
		})
		return err
	}
	return deps.Queries.UpdateWorkspaceGitlabConnectionStatus(ctx, db.UpdateWorkspaceGitlabConnectionStatusParams{
		WorkspaceID:      wsUUID,
		ConnectionStatus: "connected",
		StatusMessage:    pgtype.Text{},
	})
}

func runInitialSyncImpl(ctx context.Context, deps SyncDeps, in RunInitialSyncInput, wsUUID pgtype.UUID) error {
	// (existing body of RunInitialSync from Tasks 11–12 goes here, minus the
	// outer pgUUID call which is now done by the caller)
}
```

- [ ] **Step 13.3: Add a test for the status transition**

Append to `initial_sync_test.go`:

```go
func TestInitialSync_TransitionsStatusToConnected(t *testing.T) {
	pool := connectTestPool(t)
	defer pool.Close()
	wsID := makeWorkspace(t, pool)

	// Insert a connection row in 'connecting' state.
	pool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 7, 'g/a', '\x'::bytea, 1, 'connecting')
	`, wsID)

	// Minimal happy-path fake gitlab.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v4/projects/7/labels" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode([]gitlabapi.Label{})
		case r.URL.Path == "/api/v4/projects/7/labels" && r.Method == http.MethodPost:
			w.Write([]byte(`{"id":1}`))
		case r.URL.Path == "/api/v4/projects/7/members/all":
			json.NewEncoder(w).Encode([]gitlabapi.ProjectMember{})
		case r.URL.Path == "/api/v4/projects/7/issues":
			json.NewEncoder(w).Encode([]gitlabapi.Issue{})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	queries := db.New(pool)
	deps := SyncDeps{Queries: queries, Client: gitlabapi.NewClient(srv.URL, srv.Client())}
	err := RunInitialSync(context.Background(), deps, RunInitialSyncInput{
		WorkspaceID: wsID, ProjectID: 7, Token: "tok",
	})
	if err != nil {
		t.Fatalf("RunInitialSync: %v", err)
	}

	row, _ := queries.GetWorkspaceGitlabConnection(context.Background(), mustPGUUID(t, wsID))
	if row.ConnectionStatus != "connected" {
		t.Errorf("status = %q, want connected", row.ConnectionStatus)
	}
}
```

- [ ] **Step 13.4: Run — expect pass**

```bash
cd server && DATABASE_URL="…" go test ./internal/gitlab/ -run TestInitialSync -v
```

- [ ] **Step 13.5: Commit**

```bash
git add server/internal/gitlab/initial_sync.go server/internal/gitlab/initial_sync_test.go server/pkg/db/queries/gitlab_connection.sql server/pkg/db/generated/
git commit -m "feat(gitlab): initial sync transitions connection status (connected/error)"
```

---

## Task 14: Connect handler — dispatch sync as goroutine

**Files:**
- Modify: `server/internal/handler/gitlab_connection.go`
- Modify: `server/internal/handler/gitlab_connection_test.go`

The Phase 1 connect handler inserted with `connection_status='connected'` immediately after validating the token + project. Phase 2a flips this: insert with `'connecting'`, dispatch a sync goroutine, return 200 immediately. The goroutine flips to `'connected'` (or `'error'`) when done.

Two changes needed:
1. The `CreateWorkspaceGitlabConnection` sqlc query currently hardcodes `'connected'` in its INSERT. We need to parameterize it (or add a second variant).
2. The connect handler calls into `gitlab.RunInitialSync` in a goroutine after persisting the row.

- [ ] **Step 14.1: Update the sqlc query to accept status**

Replace the existing `CreateWorkspaceGitlabConnection` in `server/pkg/db/queries/gitlab_connection.sql` with:

```sql
-- name: CreateWorkspaceGitlabConnection :one
INSERT INTO workspace_gitlab_connection (
    workspace_id,
    gitlab_project_id,
    gitlab_project_path,
    service_token_encrypted,
    service_token_user_id,
    connection_status
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;
```

(The only change is the trailing `$6` instead of the hardcoded `'connected'`.)

Run `make sqlc`.

The Phase 1 handler's call site now passes a status parameter — update it in the next step.

- [ ] **Step 14.2: Update the handler**

In `server/internal/handler/gitlab_connection.go`, modify `ConnectGitlabWorkspace`:

a. After encrypting the token, change the `CreateWorkspaceGitlabConnection` call to pass `'connecting'`:

```go
row, err := h.Queries.CreateWorkspaceGitlabConnection(r.Context(), db.CreateWorkspaceGitlabConnectionParams{
    WorkspaceID:           parseUUID(workspaceID),
    GitlabProjectID:       project.ID,
    GitlabProjectPath:     project.PathWithNamespace,
    ServiceTokenEncrypted: encrypted,
    ServiceTokenUserID:    user.ID,
    ConnectionStatus:      "connecting",
})
```

b. After the row is persisted (and BEFORE writing the response), dispatch the sync goroutine:

```go
// Dispatch initial sync in the background. The goroutine flips the
// connection_status to 'connected' (or 'error' with a message) when done.
// Use a fresh context here — the request context will be cancelled before
// sync finishes.
go func(token string, projectID int64, workspaceID string) {
    syncCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
    defer cancel()
    if err := gitlab.RunInitialSync(syncCtx, gitlab.SyncDeps{
        Queries: h.Queries,
        Client:  h.Gitlab,
    }, gitlab.RunInitialSyncInput{
        WorkspaceID: workspaceID,
        ProjectID:   projectID,
        Token:       token,
    }); err != nil {
        slog.Error("initial gitlab sync failed",
            "error", err,
            "workspace_id", workspaceID,
            "project_id", projectID)
    }
}(req.Token, project.ID, workspaceID)
```

Add imports:

```go
import (
    "context"
    "time"
    "github.com/multica-ai/multica/server/internal/gitlab"
)
```

(Be sure not to shadow the existing `gitlab "github.com/multica-ai/multica/server/pkg/gitlab"` import — alias the new one as `gitlabsync`:

```go
import (
    gitlabsync "github.com/multica-ai/multica/server/internal/gitlab"
    "github.com/multica-ai/multica/server/pkg/gitlab"
)
```

…and use `gitlabsync.RunInitialSync`, `gitlabsync.SyncDeps`, `gitlabsync.RunInitialSyncInput`.)

- [ ] **Step 14.3: Update existing tests**

The existing `TestConnectGitlabWorkspace_Success` test asserts `connection_status: "connected"` in the response. After Phase 2a, the response has `connection_status: "connecting"` initially.

In `gitlab_connection_test.go`, find the assertion and update:

```go
if got["connection_status"] != "connecting" {
    t.Errorf("connection_status = %v, want connecting", got["connection_status"])
}
```

Also: the test's fake gitlab server only handles `/api/v4/user` and `/api/v4/projects/42`. Now that the connect handler dispatches a sync goroutine, the fake gitlab will get unexpected calls (`/api/v4/projects/42/labels`, etc.) **after the test returns**. This is a goroutine leak relative to the test boundary.

To keep the test deterministic, either:
- A) Extend the fake gitlab server to handle all the sync paths so the goroutine completes cleanly. (Easiest.)
- B) Add a `SyncDispatcher` indirection on the Handler so tests can swap in a no-op dispatcher.

Go with A — it's smaller. Expand the fake gitlab handler in the existing `TestConnectGitlabWorkspace_Success` test to also serve empty arrays for `/projects/42/labels`, `/projects/42/members/all`, `/projects/42/issues`, and a stub POST `/projects/42/labels` for label bootstrap creates.

Adjust:

```go
fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    switch r.URL.Path {
    case "/api/v4/user":
        w.Write([]byte(`{"id": 555, "username": "svc-bot", "name": "Service Bot"}`))
    case "/api/v4/projects/42":
        w.Write([]byte(`{"id": 42, "path_with_namespace": "team/app"}`))
    case "/api/v4/projects/42/labels":
        if r.Method == http.MethodGet {
            w.Write([]byte(`[]`))
        } else {
            w.Write([]byte(`{"id":1,"name":"x","color":"#000"}`))
        }
    case "/api/v4/projects/42/members/all":
        w.Write([]byte(`[]`))
    case "/api/v4/projects/42/issues":
        w.Write([]byte(`[]`))
    default:
        // Tolerate other sync calls — they shouldn't happen for an empty project.
        w.WriteHeader(http.StatusNotFound)
    }
}))
```

Apply the same expansion to `TestConnectGitlabWorkspace_BadToken` (the user check returns 401, so sync never starts — but the test should still tolerate any stray calls), `TestGetGitlabWorkspaceConnection_Connected`, `TestDisconnectGitlabWorkspace_Success`, and `TestConnectGitlabWorkspace_AlreadyConnectedReturns409`.

Lastly, add a small `time.Sleep` (50ms) at the end of any test that asserts on `connection_status` post-sync if you want the status to settle to `connected` before the test ends. Better: poll for up to 5 seconds in the assertion. The cleanest pattern:

```go
// Wait for sync to complete (status transitions from 'connecting' to 'connected').
// The goroutine completes within milliseconds for an empty fake project.
deadline := time.Now().Add(5 * time.Second)
for time.Now().Before(deadline) {
    row, _ := h.Queries.GetWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
    if row.ConnectionStatus == "connected" {
        break
    }
    time.Sleep(20 * time.Millisecond)
}
```

Use this in the `Success` test to assert the FINAL state is connected, while still asserting the IMMEDIATE response is `connecting`.

- [ ] **Step 14.4: Run — expect pass**

```bash
cd server && DATABASE_URL="…" go test ./internal/handler/ -run TestConnectGitlab -v
```

Expected: all gitlab handler tests pass.

- [ ] **Step 14.5: Commit**

```bash
git add server/internal/handler/gitlab_connection.go server/internal/handler/gitlab_connection_test.go server/pkg/db/queries/gitlab_connection.sql server/pkg/db/generated/
git commit -m "feat(handler): connect dispatches initial sync goroutine; status='connecting' until done"
```

---

## Task 15: Disconnect handler — cascade-truncate cache

**Files:**
- Modify: `server/internal/handler/gitlab_connection.go`
- Modify: `server/internal/handler/gitlab_connection_test.go`

When a workspace disconnects GitLab, the cache becomes garbage. Phase 2a removes:
- All `issue` rows for that workspace where `gitlab_iid IS NOT NULL` (the synced ones).
- Cascades remove `issue_to_label`, `comment`, `issue_reaction`, `attachment` for those issues via existing FKs.
- All `gitlab_label` rows for that workspace (the FK from `issue_to_label` already cascaded above, so the label rows can go now too).
- All `gitlab_project_member` rows.
- The `workspace_gitlab_connection` row itself (already done by Phase 1's handler).

In Phase 2b we'll also call `DeleteProjectHook` against GitLab. Skip that here.

- [ ] **Step 15.1: (No new sqlc queries needed)**

The three deletion queries we need already exist from Task 2:
- `DeleteWorkspaceCachedIssues` — `DELETE FROM issue WHERE workspace_id = $1 AND gitlab_iid IS NOT NULL`
- `DeleteWorkspaceGitlabLabels` — `DELETE FROM gitlab_label WHERE workspace_id = $1`
- `DeleteWorkspaceGitlabMembers` — `DELETE FROM gitlab_project_member WHERE workspace_id = $1`

The handler will call all three in sequence inside a transaction. (`comment`, `issue_reaction`, `issue_to_label`, `attachment` rows cascade automatically via the FK `ON DELETE CASCADE` from `issue.id`.)

- [ ] **Step 15.2: Modify `DisconnectGitlabWorkspace`**

In `server/internal/handler/gitlab_connection.go`, add the truncation BEFORE deleting the connection row:

```go
func (h *Handler) DisconnectGitlabWorkspace(w http.ResponseWriter, r *http.Request) {
    if !h.GitlabEnabled {
        writeError(w, http.StatusNotFound, "gitlab integration disabled")
        return
    }
    workspaceID := chi.URLParam(r, "id")
    if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
        return
    }
    // Cascade-truncate the cache before removing the connection row.
    // The cache rows are derived from GitLab; once we disconnect, they're
    // unreachable garbage. Run inside a transaction so partial failure
    // doesn't leave orphan rows.
    tx, err := h.TxStarter.Begin(r.Context())
    if err != nil {
        slog.Error("begin tx for cache truncate", "error", err)
        writeError(w, http.StatusInternalServerError, "failed to clear cache")
        return
    }
    defer tx.Rollback(r.Context())
    qtx := h.Queries.WithTx(tx)
    wsUUID := parseUUID(workspaceID)
    if err := qtx.DeleteWorkspaceCachedIssues(r.Context(), wsUUID); err != nil {
        slog.Error("delete cached issues failed", "error", err)
        writeError(w, http.StatusInternalServerError, "failed to clear cache")
        return
    }
    if err := qtx.DeleteWorkspaceGitlabLabels(r.Context(), wsUUID); err != nil {
        slog.Error("delete cached labels failed", "error", err)
        writeError(w, http.StatusInternalServerError, "failed to clear cache")
        return
    }
    if err := qtx.DeleteWorkspaceGitlabMembers(r.Context(), wsUUID); err != nil {
        slog.Error("delete cached members failed", "error", err)
        writeError(w, http.StatusInternalServerError, "failed to clear cache")
        return
    }
    if err := qtx.DeleteWorkspaceGitlabConnection(r.Context(), wsUUID); err != nil {
        slog.Error("delete workspace_gitlab_connection failed", "error", err)
        writeError(w, http.StatusInternalServerError, "failed to disconnect")
        return
    }
    if err := tx.Commit(r.Context()); err != nil {
        slog.Error("commit cache truncate", "error", err)
        writeError(w, http.StatusInternalServerError, "failed to clear cache")
        return
    }
    w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 15.3: Add a test for the truncation**

Append to `gitlab_connection_test.go`:

```go
func TestDisconnectGitlabWorkspace_TruncatesCache(t *testing.T) {
    fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        switch r.URL.Path {
        case "/api/v4/user":
            w.Write([]byte(`{"id":1,"username":"svc"}`))
        case "/api/v4/projects/1":
            w.Write([]byte(`{"id":1,"path_with_namespace":"g/a"}`))
        default:
            w.Write([]byte(`[]`))
        }
    }))
    defer fake.Close()

    h := buildHandlerWithGitlab(t, fake.URL)
    h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
    h.Queries.TruncateWorkspaceGitlabCache(context.Background(), parseUUID(testWorkspaceID))

    // Connect.
    body, _ := json.Marshal(map[string]string{"project": "1", "token": "glpat-x"})
    req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
    req.Header.Set("X-User-ID", testUserID)
    req = withURLParam(req, "id", testWorkspaceID)
    h.ConnectGitlabWorkspace(httptest.NewRecorder(), req)

    // Insert a synthetic cached row (so we have something to delete).
    h.Queries.UpsertGitlabLabel(context.Background(), db.UpsertGitlabLabelParams{
        WorkspaceID:   parseUUID(testWorkspaceID),
        GitlabLabelID: 9999,
        Name:          "test-label",
        Color:         "#000",
    })

    // Disconnect.
    delReq := httptest.NewRequest(http.MethodDelete, "/", nil)
    delReq.Header.Set("X-User-ID", testUserID)
    delReq = withURLParam(delReq, "id", testWorkspaceID)
    rr := httptest.NewRecorder()
    h.DisconnectGitlabWorkspace(rr, delReq)
    if rr.Code != http.StatusNoContent {
        t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
    }

    // Verify the label is gone.
    labels, _ := h.Queries.ListGitlabLabels(context.Background(), parseUUID(testWorkspaceID))
    if len(labels) != 0 {
        t.Errorf("expected cache truncated, found %d labels", len(labels))
    }
}
```

- [ ] **Step 15.4: Run — expect pass**

```bash
cd server && DATABASE_URL="…" go test ./internal/handler/ -run TestDisconnectGitlabWorkspace -v
```

- [ ] **Step 15.5: Commit**

```bash
git add server/internal/handler/gitlab_connection.go server/internal/handler/gitlab_connection_test.go
git commit -m "feat(handler): disconnect cascades cache truncation"
```

---

## Task 16: 501 write-stopgap middleware

**Files:**
- Create: `server/internal/middleware/gitlab_writes_blocked.go`
- Create: `server/internal/middleware/gitlab_writes_blocked_test.go`

When a workspace has a `workspace_gitlab_connection` row, all issue-related write requests must return 501. Reads pass through.

- [ ] **Step 16.1: Write failing test**

Create `server/internal/middleware/gitlab_writes_blocked_test.go`:

```go
package middleware

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/go-chi/chi/v5"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgtype"
    db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeQueries is a tiny stub that implements just the one method the
// middleware needs. hasConn flips the response between "found" and pgx.ErrNoRows.
type fakeQueries struct {
    hasConn bool
}

func (q *fakeQueries) GetWorkspaceGitlabConnection(ctx context.Context, _ pgtype.UUID) (db.WorkspaceGitlabConnection, error) {
    if q.hasConn {
        return db.WorkspaceGitlabConnection{}, nil
    }
    return db.WorkspaceGitlabConnection{}, pgx.ErrNoRows
}

func TestGitlabWritesBlocked_AllowsReadsAndUnconnectedWorkspaces(t *testing.T) {
    var nextCalled bool
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nextCalled = true; w.WriteHeader(http.StatusOK) })

    // Workspace not connected → all methods pass through.
    h := GitlabWritesBlocked(&fakeQueries{hasConn: false})(next)
    for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
        nextCalled = false
        req := httptest.NewRequest(method, "/api/issues", nil)
        req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000000")
        rr := httptest.NewRecorder()
        h.ServeHTTP(rr, req)
        if !nextCalled {
            t.Errorf("%s without connection should pass through, got %d", method, rr.Code)
        }
    }
}

func TestGitlabWritesBlocked_BlocksWritesForConnectedWorkspaces(t *testing.T) {
    var nextCalled bool
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nextCalled = true })
    h := GitlabWritesBlocked(&fakeQueries{hasConn: true})(next)

    for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
        nextCalled = false
        req := httptest.NewRequest(method, "/api/issues", nil)
        req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000000")
        rr := httptest.NewRecorder()
        h.ServeHTTP(rr, req)
        if nextCalled {
            t.Errorf("%s with connection should be blocked, but reached next", method)
        }
        if rr.Code != http.StatusNotImplemented {
            t.Errorf("%s status = %d, want 501", method, rr.Code)
        }
    }
}

func TestGitlabWritesBlocked_AllowsReadsForConnectedWorkspaces(t *testing.T) {
    var nextCalled bool
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nextCalled = true; w.WriteHeader(http.StatusOK) })
    h := GitlabWritesBlocked(&fakeQueries{hasConn: true})(next)

    req := httptest.NewRequest(http.MethodGet, "/api/issues", nil)
    req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000000")
    rr := httptest.NewRecorder()
    h.ServeHTTP(rr, req)
    if !nextCalled {
        t.Errorf("GET should pass through even with connection, got %d", rr.Code)
    }
}

// withURLParam mirrors the handler-package test helper for chi route context.
func withURLParam(req *http.Request, key, value string) *http.Request {
    rctx := chi.NewRouteContext()
    rctx.URLParams.Add(key, value)
    return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}
```

- [ ] **Step 16.2: Run — expect compile error**

```bash
cd server && go test ./internal/middleware/ -run TestGitlabWritesBlocked -v
```

- [ ] **Step 16.3: Implement**

Create `server/internal/middleware/gitlab_writes_blocked.go`:

```go
package middleware

import (
    "context"
    "encoding/json"
    "errors"
    "net/http"

    "github.com/go-chi/chi/v5"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgtype"
    db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// gitlabConnectionLookup is the narrow surface of db.Queries that this
// middleware needs. *db.Queries satisfies it; tests stub with a fake.
type gitlabConnectionLookup interface {
    GetWorkspaceGitlabConnection(ctx context.Context, workspaceID pgtype.UUID) (db.WorkspaceGitlabConnection, error)
}

// GitlabWritesBlocked returns a chi-compatible middleware that responds 501
// to any non-GET/HEAD/OPTIONS request when the workspace (resolved from the
// URL param "id" or the X-Workspace-ID header) has a workspace_gitlab_connection
// row. Reads always pass through.
func GitlabWritesBlocked(q gitlabConnectionLookup) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            switch r.Method {
            case http.MethodGet, http.MethodHead, http.MethodOptions:
                next.ServeHTTP(w, r)
                return
            }
            workspaceID := workspaceIDFromRequest(r)
            if workspaceID == "" {
                // No workspace context — defer to downstream (typically 400).
                next.ServeHTTP(w, r)
                return
            }
            var u pgtype.UUID
            if err := u.Scan(workspaceID); err != nil {
                next.ServeHTTP(w, r)
                return
            }
            _, err := q.GetWorkspaceGitlabConnection(r.Context(), u)
            if errors.Is(err, pgx.ErrNoRows) {
                next.ServeHTTP(w, r)
                return
            }
            if err != nil {
                // Lookup error — don't block; defer to downstream which has
                // its own error handling.
                next.ServeHTTP(w, r)
                return
            }
            // Connection exists → block this write.
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusNotImplemented)
            json.NewEncoder(w).Encode(map[string]string{
                "error": "writes not yet wired to GitLab — Phase 3 will enable this",
            })
        })
    }
}

// workspaceIDFromRequest pulls the workspace ID from the chi URL param "id"
// or the X-Workspace-ID header. Mirrors the existing handler helper pattern.
func workspaceIDFromRequest(r *http.Request) string {
    if id := chi.URLParam(r, "id"); id != "" {
        return id
    }
    return r.Header.Get("X-Workspace-ID")
}
```

Update the test file: the `fakeQueries` struct should implement just `GetWorkspaceGitlabConnection`. Replace the `errNoRows` indirection with `pgx.ErrNoRows` directly (import `github.com/jackc/pgx/v5`).

- [ ] **Step 16.4: Run — expect pass**

```bash
cd server && go test ./internal/middleware/ -run TestGitlabWritesBlocked -v
```

- [ ] **Step 16.5: Commit**

```bash
git add server/internal/middleware/gitlab_writes_blocked.go server/internal/middleware/gitlab_writes_blocked_test.go
git commit -m "feat(middleware): 501 stopgap for issue writes when workspace has gitlab connection"
```

---

## Task 17: Apply the 501 middleware to issue write routes

**Files:**
- Modify: `server/cmd/server/router.go`

The middleware needs to be applied to:
- All issue write routes (`POST /api/issues`, `PUT /api/issues/{id}`, `DELETE /api/issues/{id}`, batch-update, batch-delete)
- Comment write routes (`POST /api/issues/{id}/comments`, `PUT/PATCH/DELETE /api/comments/{id}`)
- Reaction write routes (`POST/DELETE /api/issues/{id}/reactions`, `POST/DELETE /api/comments/{id}/reactions`)
- Subscriber write routes (`POST/DELETE /api/issues/{id}/subscribers`)
- Attachment write routes (`POST /api/issues/{id}/attachments`)

The cleanest approach: wrap the entire `/api/issues` route group + the `/api/comments/{id}` group with the middleware.

- [ ] **Step 17.1: Wire the middleware**

In `server/cmd/server/router.go`, find the `r.Route("/api/issues", ...)` block (around line 218). Add the middleware:

```go
r.Route("/api/issues", func(r chi.Router) {
    r.Use(middleware.GitlabWritesBlocked(queries))
    // …existing routes…
})
```

Same for `/api/comments/{id}`:

```go
r.Route("/api/comments/{id}", func(r chi.Router) {
    r.Use(middleware.GitlabWritesBlocked(queries))
    // …existing routes…
})
```

The middleware itself only blocks non-GET methods, so wrapping the whole group is safe — reads still pass.

- [ ] **Step 17.2: Verify build**

```bash
cd server && go build ./...
```
Expected: clean.

- [ ] **Step 17.3: Add a smoke handler test**

Append to `server/internal/handler/gitlab_connection_test.go`:

```go
func TestGitlabConnectedWorkspace_WriteReturns501(t *testing.T) {
    // This needs the full router so the middleware fires. Set up via the
    // existing router-building code path used by integration_test.go.
    // (Or: directly assemble a chi router with the issue routes + middleware
    //  and exercise it. Simpler — let's do that.)

    // Set up: workspace with a fake gitlab connection row.
    h := buildHandlerWithGitlab(t, "http://unused")
    h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
    h.Queries.CreateWorkspaceGitlabConnection(context.Background(), db.CreateWorkspaceGitlabConnectionParams{
        WorkspaceID:           parseUUID(testWorkspaceID),
        GitlabProjectID:       42,
        GitlabProjectPath:     "team/app",
        ServiceTokenEncrypted: []byte("x"),
        ServiceTokenUserID:    1,
        ConnectionStatus:      "connected",
    })
    defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

    // Build a tiny router that mounts CreateIssue under the middleware.
    r := chi.NewRouter()
    r.Route("/api/workspaces/{id}/issues", func(r chi.Router) {
        r.Use(middleware.GitlabWritesBlocked(h.Queries))
        r.Post("/", h.CreateIssue)
    })

    body, _ := json.Marshal(map[string]any{"title": "Test", "status": "todo", "priority": "medium"})
    req := httptest.NewRequest(http.MethodPost,
        fmt.Sprintf("/api/workspaces/%s/issues/", testWorkspaceID), bytes.NewReader(body))
    req.Header.Set("X-User-ID", testUserID)
    rr := httptest.NewRecorder()
    r.ServeHTTP(rr, req)
    if rr.Code != http.StatusNotImplemented {
        t.Fatalf("status = %d, want 501; body = %s", rr.Code, rr.Body.String())
    }
}
```

- [ ] **Step 17.4: Run — expect pass**

```bash
cd server && DATABASE_URL="…" go test ./internal/handler/ -run TestGitlabConnectedWorkspace_WriteReturns501 -v
```

- [ ] **Step 17.5: Commit**

```bash
git add server/cmd/server/router.go server/internal/handler/gitlab_connection_test.go
git commit -m "feat(server): apply gitlab-writes-blocked middleware to issue/comment routes"
```

---

## Task 18: Full verification — `make check`

**Files:** (none)

- [ ] **Step 18.1: Run the full pipeline**

```bash
cd /Users/jimmy.mills/Developer/multica && make check
```

Expected: typecheck + vitest + go test pass. (E2E can be skipped — Phase 2a doesn't add user-facing flows yet.)

- [ ] **Step 18.2: Manual smoke (optional but recommended)**

Set up a throwaway GitLab.com test project. Point your local Multica server at it:

```bash
MULTICA_GITLAB_ENABLED=true MULTICA_SECRETS_KEY=$(head -c 32 /dev/urandom | base64) make server
```

In a connected browser, navigate to a workspace's Settings → GitLab tab, paste a real PAT + project path, submit. Verify:
- Settings shows "Connecting…" briefly, then "Connected: <project>".
- Server logs show "initial sync starting" then "initial sync complete" within seconds.
- The issue list (or a direct GET `/api/issues?workspace_id=…`) returns the synced issues with the correct status, priority, and labels.
- Trying to POST `/api/issues?workspace_id=…` returns 501 with the documented error message.
- Disconnecting clears the cache (subsequent GET returns empty).

Document any deviations as Phase 2b follow-ups.

- [ ] **Step 18.3: Commit any final fix-ups**

If verification surfaced a real bug, fix and commit. Don't mark Phase 2a complete until `make check` is green.

---

## Out of scope for Phase 2a (Phase 2b will add)

- Webhook receiver endpoint (`POST /api/gitlab/webhook`) — until Phase 2b ships, the cache populated by initial sync goes stale immediately after sync. Users would need to disconnect/reconnect to refresh.
- Webhook event dedupe table (`gitlab_webhook_event`) — added in Phase 2b's migration.
- Webhook worker pool goroutines.
- Reconciler goroutine (5-minute drift detection).
- Webhook creation in initial sync — Phase 2b extends `RunInitialSync` to also `CreateProjectHook`.
- `DeleteProjectHook` in disconnect — Phase 2b extends the disconnect handler.

## Definition of done

Phase 2a is complete when:

1. A workspace admin (with `MULTICA_GITLAB_ENABLED=true` and `NEXT_PUBLIC_GITLAB_ENABLED=true`) connects their GitLab project via the existing settings tab.
2. Connection row appears with `connection_status='connecting'`. Within ~10 seconds, transitions to `'connected'`.
3. The `issue` table contains rows for every GitLab issue, with status/priority derived from `status::*` / `priority::*` labels and agent assignment derived from `agent::<slug>` labels.
4. The `gitlab_label`, `gitlab_project_member` tables are populated.
5. POST/PUT/DELETE `/api/issues` (and friends) return `501 Not Implemented` with the documented error message.
6. Reads (GET `/api/issues`, GET `/api/issues/{id}`) work and serve from the cache.
7. Disconnect cleans up: cache rows for the workspace are removed, `workspace_gitlab_connection` row is gone.
8. `make check` is green (modulo the two pre-existing date-bucket failures from Phase 1).
