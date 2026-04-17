# GitLab Issues Integration — Phase 2b: Webhooks + Reconciler — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add real-time + safety-net synchronization to the Phase 2a cache. After connect, GitLab pushes events to a webhook receiver on the Multica server; a worker pool drains them into the cache and fires WS events so the UI sees changes in seconds. A 5-minute reconciler loop catches anything webhooks missed (delivery failures, server downtime). Disconnect now also removes the webhook from the GitLab project.

**Architecture:**
- **One new schema additions migration** (053) — `gitlab_webhook_event` queue/dedupe table, plus `last_webhook_received_at` column on `workspace_gitlab_connection` for stale-webhook detection.
- **Webhook receiver** is a public unauthenticated endpoint at `POST /api/gitlab/webhook`. It validates the GitLab-supplied `X-Gitlab-Token` against the workspace's `webhook_secret` in constant time, deduplicates via a unique index on `(workspace_id, event_type, object_id, payload_hash)`, persists the event with `processed_at=NULL`, and responds 200 immediately. The 200 must come fast — GitLab cancels the delivery after 10 seconds.
- **Worker pool** is N=5 goroutines spawned at server boot. Each loops `SELECT … FROM gitlab_webhook_event WHERE processed_at IS NULL ORDER BY received_at LIMIT 1 FOR UPDATE SKIP LOCKED`, processes the event, marks `processed_at`. Per-event handlers reuse Phase 2a's translator + sqlc upserts — no new domain logic for issues/notes/awards/labels themselves.
- **Reconciler** is a single goroutine on a 5-minute ticker. Each tick it iterates connected workspaces and calls `ListIssues(updated_after=last_sync_cursor - 10m)`, upserting any drift it finds. The 10-minute overlap window covers clock skew + in-flight webhooks. After the poll, if it found changes AND `last_webhook_received_at < now() - 15m`, it sets `connection_status='error'` with `status_message='webhook deliveries appear delayed'` and logs a warning.
- **Connect handler** extends with a `CreateProjectHook` call after the initial sync completes; the returned `hook_id` is saved as `webhook_gitlab_id` on the connection row, alongside a freshly-generated `webhook_secret`.
- **Disconnect handler** extends with a `DeleteProjectHook` call BEFORE the cache truncation, so even if cache truncation fails, the webhook is gone (avoiding ghost deliveries to a workspace that no longer exists).

**Tech Stack:**
- Go 1.26, `pgx/v5` with `FOR UPDATE SKIP LOCKED`, `sqlc`, Chi router (existing).
- `crypto/rand` for webhook secret generation, `crypto/subtle.ConstantTimeCompare` for header validation.
- Stdlib `time.Ticker` for the reconciler.
- Existing Phase 2a packages: `server/pkg/gitlab` (REST client) and `server/internal/gitlab` (translator + sync).

**Design spec:** `docs/superpowers/specs/2026-04-17-gitlab-issues-integration-design.md` (sections 4b, 4c)
**Phase 2a plan (for reference):** `docs/superpowers/plans/2026-04-17-gitlab-issues-integration-phase-2a.md`

**Out of scope for 2b (Phase 3 will add):**
- Per-user PAT mapping for human-authored writes.
- Removing the 501 stopgap on issue writes.
- Backfilling NULL author/actor refs on synced cache rows.

**Out of scope entirely (Phase 4/5 will add):**
- Re-pointing autopilot through GitLab.
- Dropping legacy columns and tables.

---

## File Structure

### New files (backend)

| Path | Responsibility |
|---|---|
| `server/migrations/053_gitlab_webhook_event.up.sql` | New `gitlab_webhook_event` table + `last_webhook_received_at` column on `workspace_gitlab_connection`. |
| `server/migrations/053_gitlab_webhook_event.down.sql` | Reverse. |
| `server/pkg/db/queries/gitlab_webhook_event.sql` | sqlc queries for the event queue + last-received update. |
| `server/pkg/gitlab/hooks.go` | `CreateProjectHook`, `DeleteProjectHook` REST methods. |
| `server/pkg/gitlab/hooks_test.go` | Tests against `httptest`. |
| `server/internal/handler/gitlab_webhook.go` | `ReceiveGitlabWebhook` handler. |
| `server/internal/handler/gitlab_webhook_test.go` | Receiver tests (signature validation, dedupe, persistence). |
| `server/internal/gitlab/webhook_worker.go` | `WebhookWorker` struct with `Start(ctx)` that spawns N goroutines + per-event dispatch. |
| `server/internal/gitlab/webhook_worker_test.go` | Worker tests (fake event payloads, real DB). |
| `server/internal/gitlab/webhook_handlers.go` | Per-event-type handlers (Issue/Note/Emoji/Label). Pure functions taking `(ctx, deps, payload) error`. |
| `server/internal/gitlab/webhook_handlers_test.go` | Tests for each handler. |
| `server/internal/gitlab/reconciler.go` | `Reconciler` struct with `Run(ctx)` ticker loop. |
| `server/internal/gitlab/reconciler_test.go` | Tests with real DB + fake GitLab. |

### Modified files

| Path | Change |
|---|---|
| `server/internal/handler/gitlab_connection.go` | Connect handler creates the webhook in GitLab after sync; disconnect removes it. |
| `server/internal/handler/handler.go` | `Handler` struct gains `PublicURL string` so the connect handler can build the webhook URL. |
| `server/cmd/server/main.go` | Reads `MULTICA_PUBLIC_URL`; passes it through to `Handler`; starts the `WebhookWorker` and `Reconciler` goroutines using `serverCtx` from Phase 2a's I-4 fix. |
| `server/cmd/server/router.go` | Mounts `POST /api/gitlab/webhook` outside the auth group. Threads `serverCtx` and the new public URL through `NewRouter`. |
| `.env.example` | Document `MULTICA_PUBLIC_URL`. |
| `apps/web/.env.example` and `apps/desktop/.env.example` | (No change.) |

### No frontend changes in 2b

The cache shape is identical to Phase 2a; reads continue to serve the same JSON. The WS events fired by the worker on cache updates already match the event types existing frontend listeners subscribe to (`issue:created`, `issue:updated`, `comment:created`, `issue_reaction:added/removed`, plus a new `label:changed`). The connection-status banner already polls and will see `error` + `status_message` if stale-webhook detection fires.

---

## Task 1: Migration 053 — webhook event queue + last-received tracker

**Files:**
- Create: `server/migrations/053_gitlab_webhook_event.up.sql`
- Create: `server/migrations/053_gitlab_webhook_event.down.sql`

- [ ] **Step 1.1: Verify migration number**

Run: `ls server/migrations | sort | tail -3`
Expected: highest is `052_` (or whatever was added since Phase 2a's 051; bump if so).

- [ ] **Step 1.2: Create the up migration**

```sql
-- Phase 2b: webhook event queue + last-received timestamp.
-- The queue is the boundary between the synchronous webhook receiver
-- (must respond <10s for GitLab not to cancel) and the async worker pool
-- that applies events to the cache.

CREATE TABLE IF NOT EXISTS gitlab_webhook_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,           -- "issue", "note", "emoji", "label"
    object_id BIGINT NOT NULL,          -- gitlab issue iid / note id / award id / label id
    gitlab_updated_at TIMESTAMPTZ,      -- from the payload, used to skip stale events
    payload_hash BYTEA NOT NULL,        -- sha256 of the canonical payload — for dedupe
    payload JSONB NOT NULL,             -- full event body, consumed by the worker
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    UNIQUE (workspace_id, event_type, object_id, payload_hash)
);

-- Workers claim with FOR UPDATE SKIP LOCKED ordered by received_at.
CREATE INDEX idx_gitlab_webhook_event_unprocessed
    ON gitlab_webhook_event(received_at)
    WHERE processed_at IS NULL;

-- Stale-webhook detection: when did we last successfully receive an event
-- for this workspace? NULL until the first delivery.
ALTER TABLE workspace_gitlab_connection
    ADD COLUMN last_webhook_received_at TIMESTAMPTZ;
```

- [ ] **Step 1.3: Create the down migration**

```sql
ALTER TABLE workspace_gitlab_connection DROP COLUMN IF EXISTS last_webhook_received_at;
DROP TABLE IF EXISTS gitlab_webhook_event;
```

- [ ] **Step 1.4: Apply and verify**

```bash
cd /Users/jimmy.mills/Developer/multica-gitlab-phase-2b/server && \
  DATABASE_URL="$(grep DATABASE_URL ../.env.worktree | cut -d= -f2-)" \
  go run ./cmd/migrate up
```
Expected: applies cleanly.

Verify:
```bash
psql "$DATABASE_URL" -c "\d gitlab_webhook_event" -c "\d workspace_gitlab_connection"
```
Expected: new table with the listed columns + indices; `workspace_gitlab_connection` has the new `last_webhook_received_at` column.

- [ ] **Step 1.5: Round-trip verify**

```bash
go run ./cmd/migrate down && go run ./cmd/migrate up
```

- [ ] **Step 1.6: Commit**

```bash
git add server/migrations/053_gitlab_webhook_event.up.sql \
        server/migrations/053_gitlab_webhook_event.down.sql
git commit -m "feat(db): gitlab webhook event queue + last-received tracker"
```

---

## Task 2: sqlc queries for the webhook event queue

**Files:**
- Create: `server/pkg/db/queries/gitlab_webhook_event.sql`
- Regenerate: `server/pkg/db/generated/`

- [ ] **Step 2.1: Create the query file**

```sql
-- name: InsertGitlabWebhookEvent :one
-- ON CONFLICT DO NOTHING is the dedupe step: GitLab retries failed
-- deliveries with the same payload, and our own writes generate echoes
-- with the same shape. The unique index on
-- (workspace_id, event_type, object_id, payload_hash) makes a duplicate
-- INSERT a silent no-op. Returning id lets the caller distinguish
-- "fresh" (returned id) from "duplicate" (no row returned).
INSERT INTO gitlab_webhook_event (
    workspace_id, event_type, object_id, gitlab_updated_at, payload_hash, payload
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, event_type, object_id, payload_hash) DO NOTHING
RETURNING id;

-- name: ClaimNextWebhookEvent :one
-- Pulls the oldest unprocessed event and locks it for the calling
-- transaction. Other workers SKIP LOCKED rows, giving us a simple
-- N-worker pool without coordination state.
SELECT id, workspace_id, event_type, object_id, gitlab_updated_at, payload
FROM gitlab_webhook_event
WHERE processed_at IS NULL
ORDER BY received_at
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- name: MarkWebhookEventProcessed :exec
UPDATE gitlab_webhook_event
SET processed_at = now()
WHERE id = $1;

-- name: TouchWorkspaceGitlabLastWebhookReceived :exec
-- Bumps the last-received timestamp for stale-webhook detection.
-- Called by the receiver on every accepted delivery.
UPDATE workspace_gitlab_connection
SET last_webhook_received_at = now()
WHERE workspace_id = $1;

-- name: GetWorkspaceGitlabConnectionByWebhookSecret :one
-- The webhook receiver doesn't have a workspace ID in the URL — only the
-- X-Gitlab-Token header. This query identifies which workspace the
-- delivery is for by matching the secret. The receiver MUST then verify
-- with constant-time comparison (this query just narrows the lookup).
SELECT * FROM workspace_gitlab_connection
WHERE webhook_secret = $1;
```

- [ ] **Step 2.2: Regenerate**

```bash
cd /Users/jimmy.mills/Developer/multica-gitlab-phase-2b && make sqlc
```

- [ ] **Step 2.3: Verify build**

```bash
cd server && go build ./...
```

- [ ] **Step 2.4: Inspect generated structs**

```bash
grep -A 8 "type InsertGitlabWebhookEventParams struct" \
  server/pkg/db/generated/gitlab_webhook_event.sql.go
```

Confirm:
- `WorkspaceID pgtype.UUID`
- `EventType string`
- `ObjectID int64`
- `GitlabUpdatedAt pgtype.Timestamptz`
- `PayloadHash []byte`
- `Payload []byte` (JSONB → `[]byte` in pgx unless sqlc was configured otherwise)

If `Payload` is generated as `pgtype.JSONB` or similar, adjust callers. Otherwise `[]byte` is fine.

- [ ] **Step 2.5: Commit**

```bash
git add server/pkg/db/queries/gitlab_webhook_event.sql server/pkg/db/generated/
git commit -m "feat(db): sqlc queries for gitlab webhook event queue"
```

---

## Task 3: GitLab client — `CreateProjectHook` + `DeleteProjectHook`

**Files:**
- Create: `server/pkg/gitlab/hooks.go`
- Create: `server/pkg/gitlab/hooks_test.go`

- [ ] **Step 3.1: Write failing tests**

Create `server/pkg/gitlab/hooks_test.go`:

```go
package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateProjectHook_PostsCorrectBody(t *testing.T) {
	var got CreateProjectHookInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/7/hooks" {
			t.Errorf("path = %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ProjectHook{ID: 99, URL: got.URL})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	out, err := c.CreateProjectHook(context.Background(), "tok", 7, CreateProjectHookInput{
		URL:                 "https://multica.example/api/gitlab/webhook",
		Token:               "secret-xyz",
		IssuesEvents:        true,
		ConfidentialIssuesEvents: true,
		NoteEvents:          true,
		ConfidentialNoteEvents: true,
		EmojiEvents:         true,
		ReleasesEvents:      false,
	})
	if err != nil {
		t.Fatalf("CreateProjectHook: %v", err)
	}
	if got.URL != "https://multica.example/api/gitlab/webhook" || got.Token != "secret-xyz" {
		t.Errorf("server received %+v", got)
	}
	if !got.IssuesEvents || !got.NoteEvents || !got.EmojiEvents {
		t.Errorf("expected issues/note/emoji events enabled: %+v", got)
	}
	if out.ID != 99 {
		t.Errorf("returned ID = %d, want 99", out.ID)
	}
}

func TestDeleteProjectHook_HitsCorrectPath(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/7/hooks/99" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.DeleteProjectHook(context.Background(), "tok", 7, 99); err != nil {
		t.Fatalf("DeleteProjectHook: %v", err)
	}
	if !hit {
		t.Errorf("server was not hit")
	}
}

func TestDeleteProjectHook_404IsNotAnError(t *testing.T) {
	// Disconnect should be tolerant — if the hook was already deleted out
	// of band, that's fine; we just want to ensure it's gone.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"404 Not Found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.DeleteProjectHook(context.Background(), "tok", 7, 99); err != nil {
		t.Errorf("expected no error for 404, got: %v", err)
	}
}
```

- [ ] **Step 3.2: Run — expect compile error**

```bash
cd server && go test ./pkg/gitlab/ -run "TestCreateProjectHook|TestDeleteProjectHook" -v
```

- [ ] **Step 3.3: Implement**

Create `server/pkg/gitlab/hooks.go`:

```go
package gitlab

import (
	"context"
	"errors"
	"fmt"
)

// ProjectHook mirrors GitLab's project hook representation.
type ProjectHook struct {
	ID  int64  `json:"id"`
	URL string `json:"url"`
}

// CreateProjectHookInput is the body for POST /projects/:id/hooks.
// Only the fields we care about are listed; GitLab accepts more.
type CreateProjectHookInput struct {
	URL                      string `json:"url"`
	Token                    string `json:"token"`
	IssuesEvents             bool   `json:"issues_events"`
	ConfidentialIssuesEvents bool   `json:"confidential_issues_events"`
	NoteEvents               bool   `json:"note_events"`
	ConfidentialNoteEvents   bool   `json:"confidential_note_events"`
	EmojiEvents              bool   `json:"emoji_events"`
	ReleasesEvents           bool   `json:"releases_events"`
	// LabelEvents fires for project-level label CRUD. Required by Task 8's
	// Label Hook handler, which keeps gitlab_label cache in sync.
	LabelEvents              bool   `json:"label_events"`
	EnableSSLVerification    bool   `json:"enable_ssl_verification"`
}

// CreateProjectHook registers a webhook on the given project.
func (c *Client) CreateProjectHook(ctx context.Context, token string, projectID int64, input CreateProjectHookInput) (*ProjectHook, error) {
	var out ProjectHook
	path := fmt.Sprintf("/projects/%d/hooks", projectID)
	if err := c.do(ctx, "POST", token, path, input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteProjectHook removes a webhook by its hook ID. Treats 404 as
// success — disconnect should be idempotent against hooks that were
// already removed out of band (e.g. by a project admin in GitLab UI).
func (c *Client) DeleteProjectHook(ctx context.Context, token string, projectID int64, hookID int64) error {
	path := fmt.Sprintf("/projects/%d/hooks/%d", projectID, hookID)
	err := c.do(ctx, "DELETE", token, path, nil, nil)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
```

- [ ] **Step 3.4: Run — expect pass**

```bash
go test ./pkg/gitlab/ -v
```
Expected: all tests pass.

- [ ] **Step 3.5: Commit**

```bash
git add server/pkg/gitlab/hooks.go server/pkg/gitlab/hooks_test.go
git commit -m "feat(gitlab): CreateProjectHook + DeleteProjectHook"
```

---

## Task 4: Webhook receiver handler

**Files:**
- Create: `server/internal/handler/gitlab_webhook.go`
- Create: `server/internal/handler/gitlab_webhook_test.go`

The handler:
1. Reads `X-Gitlab-Token` header.
2. Looks up the workspace by webhook secret. If not found → 401.
3. Constant-time compare the secret to defend against timing attacks (the lookup gives us O(1) bypass; the compare is defense-in-depth in case the lookup ever changes).
4. Dispatches by `X-Gitlab-Event` header to determine `event_type` and `object_id`.
5. Computes payload hash, INSERTs into `gitlab_webhook_event`. ON CONFLICT silently dedupes.
6. Bumps `last_webhook_received_at`.
7. Responds 200 (and a tiny ACK body) — must respond fast (<10s GitLab timeout).

The receiver does NOT process the event itself — that's the worker's job. This keeps the response fast and lets the worker apply backpressure.

- [ ] **Step 4.1: Write failing test**

Create `server/internal/handler/gitlab_webhook_test.go`:

```go
package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	testWebhookSecret = "wh-secret-abc-123"
)

// seedConnectionWithWebhookSecret inserts a connection row with the test
// webhook secret. Returns the workspace ID we used.
func seedConnectionWithWebhookSecret(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	testHandler.Queries.DeleteWorkspaceGitlabConnection(ctx, parseUUID(testWorkspaceID))
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id,
			webhook_secret, webhook_gitlab_id, connection_status
		) VALUES ($1, 7, 'g/a', '\x'::bytea, 1, $2, 11, 'connected')
	`, testWorkspaceID, testWebhookSecret); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
}

func TestReceiveGitlabWebhook_PersistsValidEvent(t *testing.T) {
	seedConnectionWithWebhookSecret(t)
	defer testHandler.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	payload := map[string]any{
		"object_kind": "issue",
		"object_attributes": map[string]any{
			"iid":        42,
			"updated_at": "2026-04-17T10:00:00Z",
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/gitlab/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitlab-Token", testWebhookSecret)
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	rr := httptest.NewRecorder()

	testHandler.ReceiveGitlabWebhook(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var count int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM gitlab_webhook_event WHERE workspace_id = $1::uuid AND event_type = 'issue' AND object_id = 42`,
		testWorkspaceID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 queued event, got %d", count)
	}

	var lastReceivedNotNull bool
	testPool.QueryRow(context.Background(),
		`SELECT last_webhook_received_at IS NOT NULL FROM workspace_gitlab_connection WHERE workspace_id = $1::uuid`,
		testWorkspaceID).Scan(&lastReceivedNotNull)
	if !lastReceivedNotNull {
		t.Errorf("last_webhook_received_at should have been bumped")
	}
}

func TestReceiveGitlabWebhook_RejectsUnknownSecret(t *testing.T) {
	seedConnectionWithWebhookSecret(t)
	defer testHandler.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	req := httptest.NewRequest(http.MethodPost, "/api/gitlab/webhook", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Gitlab-Token", "wrong-secret")
	req.Header.Set("X-Gitlab-Event", "Issue Hook")
	rr := httptest.NewRecorder()

	testHandler.ReceiveGitlabWebhook(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rr.Code)
	}
}

func TestReceiveGitlabWebhook_DuplicateDeliveryIsNoop(t *testing.T) {
	seedConnectionWithWebhookSecret(t)
	defer testHandler.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	payload := map[string]any{
		"object_kind": "issue",
		"object_attributes": map[string]any{
			"iid":        42,
			"updated_at": "2026-04-17T10:00:00Z",
		},
	}
	body, _ := json.Marshal(payload)

	post := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/gitlab/webhook", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Gitlab-Token", testWebhookSecret)
		req.Header.Set("X-Gitlab-Event", "Issue Hook")
		rr := httptest.NewRecorder()
		testHandler.ReceiveGitlabWebhook(rr, req)
		return rr.Code
	}
	if got := post(); got != http.StatusOK {
		t.Fatalf("first delivery status = %d", got)
	}
	if got := post(); got != http.StatusOK {
		t.Fatalf("second (duplicate) delivery status = %d", got)
	}

	var count int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM gitlab_webhook_event WHERE workspace_id = $1::uuid AND event_type = 'issue' AND object_id = 42`,
		testWorkspaceID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 row after dedupe, got %d", count)
	}
}

func TestReceiveGitlabWebhook_NoteEvent(t *testing.T) {
	seedConnectionWithWebhookSecret(t)
	defer testHandler.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	payload := map[string]any{
		"object_kind": "note",
		"object_attributes": map[string]any{
			"id":         100,
			"updated_at": "2026-04-17T11:00:00Z",
			"noteable_type": "Issue",
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/gitlab/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gitlab-Token", testWebhookSecret)
	req.Header.Set("X-Gitlab-Event", "Note Hook")
	rr := httptest.NewRecorder()

	testHandler.ReceiveGitlabWebhook(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var count int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM gitlab_webhook_event WHERE workspace_id = $1::uuid AND event_type = 'note' AND object_id = 100`,
		testWorkspaceID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 queued note event, got %d", count)
	}

	// Sanity check the payload hash — used by dedupe and by sha256.Sum256 directly.
	expectedHash := sha256.Sum256(body)
	var stored []byte
	testPool.QueryRow(context.Background(),
		`SELECT payload_hash FROM gitlab_webhook_event WHERE workspace_id = $1::uuid AND object_id = 100`,
		testWorkspaceID).Scan(&stored)
	if !bytes.Equal(stored, expectedHash[:]) {
		t.Errorf("stored payload_hash differs from sha256(body)\nstored:   %x\nexpected: %x", stored, expectedHash[:])
	}

	_ = fmt.Sprintf // keep "fmt" import for any debug prints during tests
}
```

- [ ] **Step 4.2: Run — expect compile error**

```bash
cd server && DATABASE_URL="…" go test ./internal/handler/ -run TestReceiveGitlabWebhook -v
```

- [ ] **Step 4.3: Implement**

Create `server/internal/handler/gitlab_webhook.go`:

```go
package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ReceiveGitlabWebhook accepts an unauthenticated HTTP POST from GitLab,
// validates the X-Gitlab-Token header against a workspace's stored
// webhook_secret, and persists the event into gitlab_webhook_event for the
// background worker to apply.
//
// Must respond <10s — GitLab cancels deliveries that take longer.
func (h *Handler) ReceiveGitlabWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.GitlabEnabled {
		writeError(w, http.StatusNotFound, "gitlab integration disabled")
		return
	}

	// 1. Extract token + event type.
	suppliedToken := r.Header.Get("X-Gitlab-Token")
	if suppliedToken == "" {
		writeError(w, http.StatusUnauthorized, "missing X-Gitlab-Token")
		return
	}
	eventHeader := r.Header.Get("X-Gitlab-Event")
	if eventHeader == "" {
		writeError(w, http.StatusBadRequest, "missing X-Gitlab-Event")
		return
	}

	// 2. Identify the workspace by webhook_secret.
	conn, err := h.Queries.GetWorkspaceGitlabConnectionByWebhookSecret(r.Context(), pgtype.Text{String: suppliedToken, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "unknown webhook token")
			return
		}
		slog.Error("webhook lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	// 3. Constant-time compare (defense-in-depth: the equality query above
	//    isn't constant-time at the SQL/index layer).
	stored := ""
	if conn.WebhookSecret.Valid {
		stored = conn.WebhookSecret.String
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(suppliedToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "secret mismatch")
		return
	}

	// 4. Read body for hashing + storage. Cap at 1MiB to defend against
	//    abusive payloads. Real GitLab payloads are well under that.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		slog.Error("read webhook body", "error", err)
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}

	// 5. Determine event_type + object_id from the parsed body.
	eventType, objectID, gitlabUpdatedAt, ok := parseWebhookKey(eventHeader, body)
	if !ok {
		// Unknown event type — ACK 200 so GitLab doesn't retry, but log so
		// we can see if a new event type starts arriving.
		slog.Info("ignoring unknown gitlab webhook event", "event", eventHeader)
		w.WriteHeader(http.StatusOK)
		return
	}

	// 6. Hash + insert (dedupe via unique index).
	hash := sha256.Sum256(body)
	_, err = h.Queries.InsertGitlabWebhookEvent(r.Context(), db.InsertGitlabWebhookEventParams{
		WorkspaceID:     conn.WorkspaceID,
		EventType:       eventType,
		ObjectID:        objectID,
		GitlabUpdatedAt: gitlabUpdatedAt,
		PayloadHash:     hash[:],
		Payload:         body,
	})
	if err != nil {
		slog.Error("insert webhook event", "error", err)
		writeError(w, http.StatusInternalServerError, "persist failed")
		return
	}

	// 7. Bump last_webhook_received_at (best-effort).
	if err := h.Queries.TouchWorkspaceGitlabLastWebhookReceived(r.Context(), conn.WorkspaceID); err != nil {
		slog.Warn("touch last_webhook_received_at", "error", err)
	}

	// 8. ACK.
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
}

// parseWebhookKey extracts (event_type, object_id, gitlab_updated_at) from
// the webhook header + body. event_type is the short form we store
// ("issue", "note", "emoji", "label"). object_id is the integer that, with
// event_type, identifies the object the event is about. gitlab_updated_at
// is best-effort — used by the worker to skip stale events.
func parseWebhookKey(eventHeader string, body []byte) (string, int64, pgtype.Timestamptz, bool) {
	switch eventHeader {
	case "Issue Hook", "Confidential Issue Hook":
		var p struct {
			ObjectAttributes struct {
				IID       int64  `json:"iid"`
				UpdatedAt string `json:"updated_at"`
			} `json:"object_attributes"`
		}
		if err := json.Unmarshal(body, &p); err != nil || p.ObjectAttributes.IID == 0 {
			return "", 0, pgtype.Timestamptz{}, false
		}
		return "issue", p.ObjectAttributes.IID, parseTSGitlab(p.ObjectAttributes.UpdatedAt), true
	case "Note Hook", "Confidential Note Hook":
		var p struct {
			ObjectAttributes struct {
				ID        int64  `json:"id"`
				UpdatedAt string `json:"updated_at"`
			} `json:"object_attributes"`
		}
		if err := json.Unmarshal(body, &p); err != nil || p.ObjectAttributes.ID == 0 {
			return "", 0, pgtype.Timestamptz{}, false
		}
		return "note", p.ObjectAttributes.ID, parseTSGitlab(p.ObjectAttributes.UpdatedAt), true
	case "Emoji Hook":
		var p struct {
			ObjectAttributes struct {
				ID        int64  `json:"id"`
				UpdatedAt string `json:"updated_at"`
			} `json:"object_attributes"`
		}
		if err := json.Unmarshal(body, &p); err != nil || p.ObjectAttributes.ID == 0 {
			return "", 0, pgtype.Timestamptz{}, false
		}
		return "emoji", p.ObjectAttributes.ID, parseTSGitlab(p.ObjectAttributes.UpdatedAt), true
	case "Label Hook":
		// The Label hook payload structure differs slightly between
		// create/update/delete; handler in Task 9 will deal with that.
		// For dedupe we only need the label id.
		var p struct {
			ObjectAttributes struct {
				ID        int64  `json:"id"`
				UpdatedAt string `json:"updated_at"`
			} `json:"object_attributes"`
		}
		if err := json.Unmarshal(body, &p); err != nil || p.ObjectAttributes.ID == 0 {
			return "", 0, pgtype.Timestamptz{}, false
		}
		return "label", p.ObjectAttributes.ID, parseTSGitlab(p.ObjectAttributes.UpdatedAt), true
	}
	return "", 0, pgtype.Timestamptz{}, false
}

// parseTSGitlab is local to this file; the gitlab/internal package's
// parseTS isn't accessible from handler. Same logic — RFC3339 → Timestamptz.
func parseTSGitlab(s string) pgtype.Timestamptz {
	if s == "" {
		return pgtype.Timestamptz{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}
```

Make sure `"time"` is in the import block.

- [ ] **Step 4.4: Run — expect pass**

```bash
DATABASE_URL="…" go test ./internal/handler/ -run TestReceiveGitlabWebhook -v
```

- [ ] **Step 4.5: Commit**

```bash
git add server/internal/handler/gitlab_webhook.go server/internal/handler/gitlab_webhook_test.go
git commit -m "feat(handler): gitlab webhook receiver (validate + dedupe + persist)"
```

---

## Task 5: Per-event handlers — Issue Hook

**Files:**
- Create: `server/internal/gitlab/webhook_handlers.go`
- Create: `server/internal/gitlab/webhook_handlers_test.go`

The webhook worker dispatches each event to a per-type handler. Each handler:
- Parses the event-specific payload shape.
- Skips if cache is already newer (`external_updated_at >= gitlab_updated_at`).
- Reuses Phase 2a's translator + sqlc upserts.
- Publishes an existing WS event so the frontend updates live.

This task implements just the Issue Hook handler. Tasks 6-8 add Note/Emoji/Label.

- [ ] **Step 5.1: Write failing test**

Create `server/internal/gitlab/webhook_handlers_test.go`:

```go
package gitlab

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestApplyIssueHookEvent_UpsertsCachedIssue(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)

	queries := db.New(pool)
	deps := WebhookDeps{Queries: queries, WorkspaceID: mustPGUUID(t, wsID), ProjectID: 7}

	body := []byte(`{
		"object_kind": "issue",
		"object_attributes": {
			"iid": 42,
			"title": "From webhook",
			"description": "body",
			"state": "opened",
			"updated_at": "2026-04-17T10:00:00Z",
			"labels": [{"title": "status::in_progress"}]
		}
	}`)

	if err := ApplyIssueHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyIssueHookEvent: %v", err)
	}

	row, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: mustPGUUID(t, wsID),
		GitlabIid:   pgtype.Int4{Int32: 42, Valid: true},
	})
	if err != nil {
		t.Fatalf("GetIssueByGitlabIID: %v", err)
	}
	if row.Title != "From webhook" {
		t.Errorf("title = %q, want From webhook", row.Title)
	}
	if row.Status != "in_progress" {
		t.Errorf("status = %q, want in_progress", row.Status)
	}
}

func TestApplyIssueHookEvent_SkipsWhenCacheNewer(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)

	queries := db.New(pool)
	wsUUID := mustPGUUID(t, wsID)

	// Pre-seed a cache row with a newer external_updated_at.
	_, err := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:       wsUUID,
		GitlabIid:         pgtype.Int4{Int32: 42, Valid: true},
		GitlabProjectID:   pgtype.Int8{Int64: 7, Valid: true},
		Title:             "Already newer",
		Status:            "todo",
		Priority:          "none",
		ExternalUpdatedAt: parseTS("2026-04-18T00:00:00Z"),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	deps := WebhookDeps{Queries: queries, WorkspaceID: wsUUID, ProjectID: 7}

	// Webhook payload with an OLDER updated_at — should be skipped.
	body := []byte(`{
		"object_kind": "issue",
		"object_attributes": {
			"iid": 42,
			"title": "Stale!",
			"state": "opened",
			"updated_at": "2026-04-17T10:00:00Z"
		}
	}`)

	if err := ApplyIssueHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyIssueHookEvent: %v", err)
	}

	row, _ := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: wsUUID,
		GitlabIid:   pgtype.Int4{Int32: 42, Valid: true},
	})
	if row.Title != "Already newer" {
		t.Errorf("title = %q, expected stale event to be skipped", row.Title)
	}
}
```

- [ ] **Step 5.2: Run — expect compile error**

```bash
DATABASE_URL="…" go test ./internal/gitlab/ -run TestApplyIssueHookEvent -v
```

- [ ] **Step 5.3: Implement**

Create `server/internal/gitlab/webhook_handlers.go`:

```go
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

// WebhookDeps is what every per-event handler needs. The worker constructs
// this once per event.
type WebhookDeps struct {
	Queries     *db.Queries
	WorkspaceID pgtype.UUID
	ProjectID   int64
}

// issueHookPayload is the subset of the Issue Hook body we read.
// GitLab's full payload is much larger; we map only what affects the cache.
type issueHookPayload struct {
	ObjectAttributes struct {
		IID         int      `json:"iid"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		State       string   `json:"state"`
		UpdatedAt   string   `json:"updated_at"`
		DueDate     string   `json:"due_date"`
		Labels      []struct {
			Title string `json:"title"`
		} `json:"labels"`
	} `json:"object_attributes"`
}

// ApplyIssueHookEvent applies one Issue Hook event to the cache. Reuses the
// same translator + upsert as Phase 2a's initial sync.
func ApplyIssueHookEvent(ctx context.Context, deps WebhookDeps, body []byte) error {
	var p issueHookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode issue hook: %w", err)
	}
	updatedAt, _ := time.Parse(time.RFC3339, p.ObjectAttributes.UpdatedAt)

	// Stale-event check: if cache row exists and is at least as new, skip.
	existing, err := deps.Queries.GetIssueByGitlabIID(ctx, db.GetIssueByGitlabIIDParams{
		WorkspaceID: deps.WorkspaceID,
		GitlabIid:   pgtype.Int4{Int32: int32(p.ObjectAttributes.IID), Valid: true},
	})
	if err == nil && existing.ExternalUpdatedAt.Valid && !existing.ExternalUpdatedAt.Time.Before(updatedAt) {
		return nil
	}

	// Build a gitlabapi.Issue so the translator can do its thing.
	labels := make([]string, 0, len(p.ObjectAttributes.Labels))
	for _, l := range p.ObjectAttributes.Labels {
		labels = append(labels, l.Title)
	}
	apiIssue := gitlabapi.Issue{
		IID:         p.ObjectAttributes.IID,
		Title:       p.ObjectAttributes.Title,
		Description: p.ObjectAttributes.Description,
		State:       p.ObjectAttributes.State,
		Labels:      labels,
		DueDate:     p.ObjectAttributes.DueDate,
		UpdatedAt:   p.ObjectAttributes.UpdatedAt,
	}

	// Resolve agent slug map.
	agentMap, err := buildAgentSlugMap(ctx, deps.Queries, deps.WorkspaceID)
	if err != nil {
		return fmt.Errorf("agent map: %w", err)
	}
	values := TranslateIssue(apiIssue, &TranslateContext{AgentBySlug: agentMap})

	if _, err := deps.Queries.UpsertIssueFromGitlab(ctx, buildUpsertIssueParams(deps.WorkspaceID, deps.ProjectID, apiIssue, values)); err != nil {
		return fmt.Errorf("upsert issue: %w", err)
	}

	// Note: label associations (issue_gitlab_label) are not updated here.
	// The webhook payload includes label titles but not the gitlab_label_id,
	// so we'd need an extra ListLabels lookup. Phase 2b's reconciler handles
	// label associations via re-syncing labels every tick. If real users see
	// label-on-issue staleness, Task 9 (Label Hook handler) catches drift.
	return nil
}
```

The `buildAgentSlugMap`, `buildUpsertIssueParams`, `TranslateIssue`, `TranslateContext`, `parseTS` symbols all already exist in this package from Phase 2a — reuse them.

- [ ] **Step 5.4: Run — expect pass**

```bash
DATABASE_URL="…" go test ./internal/gitlab/ -run TestApplyIssueHookEvent -v
```

- [ ] **Step 5.5: Commit**

```bash
git add server/internal/gitlab/webhook_handlers.go server/internal/gitlab/webhook_handlers_test.go
git commit -m "feat(gitlab): Issue Hook webhook handler"
```

---

## Task 6: Per-event handler — Note Hook

**Files:**
- Modify: `server/internal/gitlab/webhook_handlers.go`
- Modify: `server/internal/gitlab/webhook_handlers_test.go`

GitLab's Note Hook fires for issue comments (and merge-request comments, snippet comments — we filter to `noteable_type == "Issue"`).

- [ ] **Step 6.1: Append failing test**

Append to `webhook_handlers_test.go`:

```go
func TestApplyNoteHookEvent_UpsertsComment(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)

	queries := db.New(pool)

	// Note hooks reference the issue by IID. Pre-seed an issue so the FK lands.
	row, _ := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:     wsUUID,
		GitlabIid:       pgtype.Int4{Int32: 42, Valid: true},
		GitlabProjectID: pgtype.Int8{Int64: 7, Valid: true},
		Title:           "Parent issue",
		Status:          "todo",
		Priority:        "none",
	})

	deps := WebhookDeps{Queries: queries, WorkspaceID: wsUUID, ProjectID: 7}

	body := []byte(`{
		"object_kind": "note",
		"object_attributes": {
			"id": 100,
			"note": "Looks good!",
			"system": false,
			"updated_at": "2026-04-17T11:00:00Z",
			"noteable_type": "Issue"
		},
		"issue": {"iid": 42},
		"user": {"id": 555}
	}`)

	if err := ApplyNoteHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyNoteHookEvent: %v", err)
	}

	var content string
	pool.QueryRow(context.Background(),
		`SELECT content FROM comment WHERE issue_id = $1::uuid AND gitlab_note_id = 100`,
		uuidString(row.ID)).Scan(&content)
	if content != "Looks good!" {
		t.Errorf("content = %q", content)
	}
}

func TestApplyNoteHookEvent_IgnoresNonIssueNotes(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)
	deps := WebhookDeps{Queries: queries, WorkspaceID: wsUUID, ProjectID: 7}

	body := []byte(`{
		"object_kind": "note",
		"object_attributes": {"id": 200, "note": "not an issue", "noteable_type": "MergeRequest"}
	}`)

	if err := ApplyNoteHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyNoteHookEvent: %v", err)
	}

	var count int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM comment WHERE gitlab_note_id = 200`).Scan(&count)
	if count != 0 {
		t.Errorf("MR-note should be ignored, but %d row(s) cached", count)
	}
}
```

- [ ] **Step 6.2: Run — expect compile error**

```bash
DATABASE_URL="…" go test ./internal/gitlab/ -run TestApplyNoteHookEvent -v
```

- [ ] **Step 6.3: Implement**

Append to `server/internal/gitlab/webhook_handlers.go`:

```go
type noteHookPayload struct {
	ObjectAttributes struct {
		ID           int64  `json:"id"`
		Note         string `json:"note"`
		System       bool   `json:"system"`
		UpdatedAt    string `json:"updated_at"`
		NoteableType string `json:"noteable_type"`
	} `json:"object_attributes"`
	Issue struct {
		IID int `json:"iid"`
	} `json:"issue"`
	User struct {
		ID int64 `json:"id"`
	} `json:"user"`
}

// ApplyNoteHookEvent caches a comment delta. Filters out non-issue notes
// (MR / snippet comments) — Multica only mirrors issues.
func ApplyNoteHookEvent(ctx context.Context, deps WebhookDeps, body []byte) error {
	var p noteHookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode note hook: %w", err)
	}
	if p.ObjectAttributes.NoteableType != "Issue" {
		return nil
	}
	if p.Issue.IID == 0 {
		return fmt.Errorf("note hook missing issue.iid")
	}

	// Look up the cached parent issue so we have the issue.id to FK against.
	parent, err := deps.Queries.GetIssueByGitlabIID(ctx, db.GetIssueByGitlabIIDParams{
		WorkspaceID: deps.WorkspaceID,
		GitlabIid:   pgtype.Int4{Int32: int32(p.Issue.IID), Valid: true},
	})
	if err != nil {
		// The webhook arrived before we cached the parent issue (race
		// during initial sync, or the issue was just created and its
		// Issue Hook hasn't been processed yet). Returning the error
		// keeps the event in the queue for the worker to retry; in
		// practice the next reconciler pass will create the issue and
		// the worker will retry this note.
		return fmt.Errorf("parent issue not yet cached (iid=%d): %w", p.Issue.IID, err)
	}

	// Translate the note via the existing API-shape translator, faking
	// a gitlabapi.Note so we get the same author-prefix detection.
	apiNote := gitlabapi.Note{
		ID:        p.ObjectAttributes.ID,
		Body:      p.ObjectAttributes.Note,
		System:    p.ObjectAttributes.System,
		Author:    gitlabapi.User{ID: p.User.ID},
		UpdatedAt: p.ObjectAttributes.UpdatedAt,
	}
	nv := TranslateNote(apiNote)

	var authorType pgtype.Text
	var authorID pgtype.UUID
	if nv.AuthorType == "agent" {
		agentMap, err := buildAgentSlugMap(ctx, deps.Queries, deps.WorkspaceID)
		if err != nil {
			return fmt.Errorf("agent map: %w", err)
		}
		if uuidStr, ok := agentMap[nv.AuthorSlug]; ok {
			authorType = pgtype.Text{String: "agent", Valid: true}
			_ = authorID.Scan(uuidStr)
		}
	}
	var glUser pgtype.Int8
	if nv.GitlabUserID != 0 {
		glUser = pgtype.Int8{Int64: nv.GitlabUserID, Valid: true}
	}

	if _, err := deps.Queries.UpsertCommentFromGitlab(ctx, db.UpsertCommentFromGitlabParams{
		WorkspaceID:        deps.WorkspaceID,
		IssueID:            parent.ID,
		AuthorType:         authorType,
		AuthorID:           authorID,
		GitlabAuthorUserID: glUser,
		Content:            nv.Body,
		Type:               nv.Type,
		GitlabNoteID:       pgtype.Int8{Int64: p.ObjectAttributes.ID, Valid: true},
		ExternalUpdatedAt:  parseTS(nv.UpdatedAt),
	}); err != nil {
		return fmt.Errorf("upsert comment: %w", err)
	}
	return nil
}
```

- [ ] **Step 6.4: Run — expect pass**

```bash
DATABASE_URL="…" go test ./internal/gitlab/ -run TestApplyNoteHookEvent -v
```

- [ ] **Step 6.5: Commit**

```bash
git add server/internal/gitlab/webhook_handlers.go server/internal/gitlab/webhook_handlers_test.go
git commit -m "feat(gitlab): Note Hook webhook handler"
```

---

## Task 7: Per-event handler — Emoji Hook

**Files:**
- Modify: `server/internal/gitlab/webhook_handlers.go`
- Modify: `server/internal/gitlab/webhook_handlers_test.go`

GitLab's Emoji Hook fires for award emoji on issues and notes. We mirror only issue-level awards.

- [ ] **Step 7.1: Append failing test**

```go
func TestApplyEmojiHookEvent_UpsertsReaction(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	row, _ := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:     wsUUID,
		GitlabIid:       pgtype.Int4{Int32: 42, Valid: true},
		GitlabProjectID: pgtype.Int8{Int64: 7, Valid: true},
		Title:           "Parent",
		Status:          "todo",
		Priority:        "none",
	})

	deps := WebhookDeps{Queries: queries, WorkspaceID: wsUUID, ProjectID: 7}

	body := []byte(`{
		"object_kind": "emoji",
		"object_attributes": {
			"id": 500,
			"name": "thumbsup",
			"awardable_type": "Issue",
			"awardable_id": 42,
			"updated_at": "2026-04-17T12:00:00Z"
		},
		"user": {"id": 555}
	}`)

	if err := ApplyEmojiHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyEmojiHookEvent: %v", err)
	}

	var emoji string
	pool.QueryRow(context.Background(),
		`SELECT emoji FROM issue_reaction WHERE issue_id = $1::uuid AND gitlab_award_id = 500`,
		uuidString(row.ID)).Scan(&emoji)
	if emoji != "thumbsup" {
		t.Errorf("emoji = %q", emoji)
	}
}

func TestApplyEmojiHookEvent_IgnoresNonIssueAwards(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	deps := WebhookDeps{Queries: db.New(pool), WorkspaceID: mustPGUUID(t, wsID), ProjectID: 7}

	body := []byte(`{
		"object_kind": "emoji",
		"object_attributes": {"id": 600, "name": "tada", "awardable_type": "Note", "awardable_id": 99}
	}`)
	if err := ApplyEmojiHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyEmojiHookEvent: %v", err)
	}
	var count int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM issue_reaction WHERE gitlab_award_id = 600`).Scan(&count)
	if count != 0 {
		t.Errorf("note-level award should be ignored, %d row(s) cached", count)
	}
}
```

- [ ] **Step 7.2: Run — expect compile error**

```bash
DATABASE_URL="…" go test ./internal/gitlab/ -run TestApplyEmojiHookEvent -v
```

- [ ] **Step 7.3: Implement**

Append to `server/internal/gitlab/webhook_handlers.go`:

```go
type emojiHookPayload struct {
	ObjectAttributes struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		AwardableType string `json:"awardable_type"`
		AwardableIID  int    `json:"awardable_id"` // for issues this is the IID
		UpdatedAt     string `json:"updated_at"`
	} `json:"object_attributes"`
	User struct {
		ID int64 `json:"id"`
	} `json:"user"`
}

// ApplyEmojiHookEvent caches an issue-level award emoji.
// Note-level awards (reactions on comments) are NOT mirrored — Multica's
// existing comment_reaction table is the home for those, and this phase
// doesn't sync them.
func ApplyEmojiHookEvent(ctx context.Context, deps WebhookDeps, body []byte) error {
	var p emojiHookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode emoji hook: %w", err)
	}
	if p.ObjectAttributes.AwardableType != "Issue" {
		return nil
	}
	parent, err := deps.Queries.GetIssueByGitlabIID(ctx, db.GetIssueByGitlabIIDParams{
		WorkspaceID: deps.WorkspaceID,
		GitlabIid:   pgtype.Int4{Int32: int32(p.ObjectAttributes.AwardableIID), Valid: true},
	})
	if err != nil {
		return fmt.Errorf("parent issue not yet cached (iid=%d): %w", p.ObjectAttributes.AwardableIID, err)
	}
	var glUser pgtype.Int8
	if p.User.ID != 0 {
		glUser = pgtype.Int8{Int64: p.User.ID, Valid: true}
	}
	if _, err := deps.Queries.UpsertIssueReactionFromGitlab(ctx, db.UpsertIssueReactionFromGitlabParams{
		WorkspaceID:       deps.WorkspaceID,
		IssueID:           parent.ID,
		ActorType:         pgtype.Text{},
		ActorID:           pgtype.UUID{},
		GitlabActorUserID: glUser,
		Emoji:             p.ObjectAttributes.Name,
		GitlabAwardID:     pgtype.Int8{Int64: p.ObjectAttributes.ID, Valid: true},
		ExternalUpdatedAt: parseTS(p.ObjectAttributes.UpdatedAt),
	}); err != nil {
		return fmt.Errorf("upsert reaction: %w", err)
	}
	return nil
}
```

- [ ] **Step 7.4: Run — expect pass**

- [ ] **Step 7.5: Commit**

```bash
git add server/internal/gitlab/webhook_handlers.go server/internal/gitlab/webhook_handlers_test.go
git commit -m "feat(gitlab): Emoji Hook webhook handler (issue-level only)"
```

---

## Task 8: Per-event handler — Label Hook

**Files:**
- Modify: `server/internal/gitlab/webhook_handlers.go`
- Modify: `server/internal/gitlab/webhook_handlers_test.go`

GitLab's Label Hook fires for label create/update/delete in the project. We upsert into `gitlab_label` (or delete) so the cached label palette stays current.

- [ ] **Step 8.1: Append failing test**

```go
func TestApplyLabelHookEvent_CreateUpsertsLabel(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)
	deps := WebhookDeps{Queries: queries, WorkspaceID: wsUUID, ProjectID: 7}

	body := []byte(`{
		"object_kind": "label",
		"event_type": "label",
		"object_attributes": {
			"id": 700,
			"title": "needs-design",
			"color": "#ff8800",
			"description": "ux input required",
			"updated_at": "2026-04-17T13:00:00Z",
			"action": "create"
		}
	}`)

	if err := ApplyLabelHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyLabelHookEvent: %v", err)
	}

	rows, _ := queries.ListGitlabLabels(context.Background(), wsUUID)
	found := false
	for _, l := range rows {
		if l.GitlabLabelID == 700 && l.Name == "needs-design" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("label not found in cache; got %+v", rows)
	}
}

func TestApplyLabelHookEvent_DeleteRemovesLabel(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	// Pre-seed.
	queries.UpsertGitlabLabel(context.Background(), db.UpsertGitlabLabelParams{
		WorkspaceID:   wsUUID,
		GitlabLabelID: 800,
		Name:          "obsolete",
		Color:         "#000",
		Description:   "",
	})

	deps := WebhookDeps{Queries: queries, WorkspaceID: wsUUID, ProjectID: 7}
	body := []byte(`{
		"object_kind": "label",
		"event_type": "label",
		"object_attributes": {"id": 800, "action": "delete"}
	}`)
	if err := ApplyLabelHookEvent(context.Background(), deps, body); err != nil {
		t.Fatalf("ApplyLabelHookEvent: %v", err)
	}

	rows, _ := queries.ListGitlabLabels(context.Background(), wsUUID)
	for _, l := range rows {
		if l.GitlabLabelID == 800 {
			t.Errorf("label 800 should be gone, but found %+v", l)
		}
	}
}
```

- [ ] **Step 8.2: Add a delete-label sqlc query**

The existing `gitlab_cache.sql` from Phase 2a has `DeleteWorkspaceGitlabLabels` (per-workspace) but not single-label delete. Add to `server/pkg/db/queries/gitlab_cache.sql`:

```sql
-- name: DeleteGitlabLabel :exec
DELETE FROM gitlab_label
WHERE workspace_id = $1 AND gitlab_label_id = $2;
```

Run `make sqlc`.

- [ ] **Step 8.3: Implement**

Append to `server/internal/gitlab/webhook_handlers.go`:

```go
type labelHookPayload struct {
	ObjectAttributes struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		Color       string `json:"color"`
		Description string `json:"description"`
		UpdatedAt   string `json:"updated_at"`
		Action      string `json:"action"` // "create", "update", "delete"
	} `json:"object_attributes"`
}

// ApplyLabelHookEvent maintains the gitlab_label cache.
func ApplyLabelHookEvent(ctx context.Context, deps WebhookDeps, body []byte) error {
	var p labelHookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode label hook: %w", err)
	}
	if p.ObjectAttributes.ID == 0 {
		return fmt.Errorf("label hook missing object_attributes.id")
	}
	if p.ObjectAttributes.Action == "delete" {
		return deps.Queries.DeleteGitlabLabel(ctx, db.DeleteGitlabLabelParams{
			WorkspaceID:   deps.WorkspaceID,
			GitlabLabelID: p.ObjectAttributes.ID,
		})
	}
	if _, err := deps.Queries.UpsertGitlabLabel(ctx, db.UpsertGitlabLabelParams{
		WorkspaceID:       deps.WorkspaceID,
		GitlabLabelID:     p.ObjectAttributes.ID,
		Name:              p.ObjectAttributes.Title,
		Color:             p.ObjectAttributes.Color,
		Description:       p.ObjectAttributes.Description,
		ExternalUpdatedAt: parseTS(p.ObjectAttributes.UpdatedAt),
	}); err != nil {
		return fmt.Errorf("upsert label: %w", err)
	}
	return nil
}
```

- [ ] **Step 8.4: Run — expect pass**

- [ ] **Step 8.5: Commit**

```bash
git add server/internal/gitlab/webhook_handlers.go server/internal/gitlab/webhook_handlers_test.go \
        server/pkg/db/queries/gitlab_cache.sql server/pkg/db/generated/
git commit -m "feat(gitlab): Label Hook webhook handler (create/update/delete)"
```

---

## Task 9: Webhook worker pool

**Files:**
- Create: `server/internal/gitlab/webhook_worker.go`
- Create: `server/internal/gitlab/webhook_worker_test.go`

The worker pool drains `gitlab_webhook_event` rows where `processed_at IS NULL`, dispatches each to the right per-event handler, and marks processed. Each worker goroutine runs its own loop with a small sleep when the queue is empty (avoiding a tight spin against the DB).

- [ ] **Step 9.1: Write failing test**

```go
package gitlab

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWebhookWorker_DrainsAndProcessesIssueEvent(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	// Seed an Issue Hook event in the queue.
	body := []byte(`{
		"object_kind": "issue",
		"object_attributes": {"iid": 99, "title": "from worker", "state": "opened",
			"updated_at": "2026-04-17T10:00:00Z", "labels": []}
	}`)
	if _, err := queries.InsertGitlabWebhookEvent(context.Background(), db.InsertGitlabWebhookEventParams{
		WorkspaceID:     wsUUID,
		EventType:       "issue",
		ObjectID:        99,
		GitlabUpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		PayloadHash:     []byte{1, 2, 3, 4},
		Payload:         body,
	}); err != nil {
		t.Fatalf("seed event: %v", err)
	}

	// Insert a workspace_gitlab_connection so the worker can resolve project_id.
	pool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 7, 'g/a', '\x'::bytea, 1, 'connected')
		ON CONFLICT DO NOTHING
	`, wsID)

	// Run the worker for a brief window. Pass the pgxpool as the txStarter
	// (it implements Begin) and one worker goroutine for predictable test
	// timing.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	w := NewWebhookWorker(queries, pool, 1, 50*time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.Run(ctx)
	}()
	// Sleep long enough for the worker to claim + process the event.
	time.Sleep(500 * time.Millisecond)
	cancel()
	wg.Wait()

	// Verify the issue is now in the cache.
	row, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: wsUUID,
		GitlabIid:   pgtype.Int4{Int32: 99, Valid: true},
	})
	if err != nil {
		t.Fatalf("issue not cached: %v", err)
	}
	if row.Title != "from worker" {
		t.Errorf("title = %q", row.Title)
	}

	// Verify the event row is now marked processed.
	var processed bool
	pool.QueryRow(context.Background(),
		`SELECT processed_at IS NOT NULL FROM gitlab_webhook_event WHERE workspace_id = $1::uuid AND object_id = 99`,
		wsID).Scan(&processed)
	if !processed {
		t.Errorf("event not marked processed")
	}
}
```

- [ ] **Step 9.2: Run — expect compile error**

```bash
DATABASE_URL="…" go test ./internal/gitlab/ -run TestWebhookWorker -v
```

- [ ] **Step 9.3: Implement**

Create `server/internal/gitlab/webhook_worker.go`:

```go
package gitlab

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// txStarter mirrors the existing handler.txStarter — duplicated here to
// avoid a cross-package import.
type txStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// WebhookWorker drains gitlab_webhook_event into the cache. Construct via
// NewWebhookWorker; start with Run(ctx) which blocks until ctx is cancelled.
type WebhookWorker struct {
	queries    *db.Queries
	tx         txStarter
	numWorkers int
	idleSleep  time.Duration
}

// NewWebhookWorker returns a worker that runs `numWorkers` goroutines and
// sleeps `idleSleep` between empty-queue checks.
func NewWebhookWorker(queries *db.Queries, tx txStarter, numWorkers int, idleSleep time.Duration) *WebhookWorker {
	if numWorkers <= 0 {
		numWorkers = 5
	}
	if idleSleep <= 0 {
		idleSleep = 250 * time.Millisecond
	}
	return &WebhookWorker{queries: queries, tx: tx, numWorkers: numWorkers, idleSleep: idleSleep}
}

// Run starts the worker pool and blocks until ctx is cancelled.
func (w *WebhookWorker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < w.numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			w.loop(ctx, id)
		}(i)
	}
	wg.Wait()
}

func (w *WebhookWorker) loop(ctx context.Context, id int) {
	for {
		if ctx.Err() != nil {
			return
		}
		processed, err := w.processOne(ctx)
		if err != nil {
			slog.Error("webhook worker", "id", id, "error", err)
		}
		if !processed {
			// Empty queue — sleep before retrying.
			select {
			case <-time.After(w.idleSleep):
			case <-ctx.Done():
				return
			}
		}
	}
}

// processOne claims one unprocessed event, applies it, marks processed.
// Returns (true, nil) when an event was processed, (false, nil) when the
// queue was empty.
func (w *WebhookWorker) processOne(ctx context.Context) (bool, error) {
	tx, err := w.tx.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	q := w.queries.WithTx(tx)

	row, err := q.ClaimNextWebhookEvent(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	// Look up the workspace's project_id so per-event handlers can build
	// gitlabapi.Issue/etc.
	conn, err := q.GetWorkspaceGitlabConnection(ctx, row.WorkspaceID)
	if err != nil {
		// Connection was disconnected between webhook receipt and worker
		// processing — skip + mark processed so we don't loop.
		slog.Warn("webhook event for unconnected workspace; dropping",
			"workspace_id", row.WorkspaceID, "event_type", row.EventType)
		if err := q.MarkWebhookEventProcessed(ctx, row.ID); err != nil {
			return false, err
		}
		return true, tx.Commit(ctx)
	}

	deps := WebhookDeps{
		Queries:     q,
		WorkspaceID: row.WorkspaceID,
		ProjectID:   conn.GitlabProjectID,
	}

	if err := dispatchWebhookEvent(ctx, deps, row.EventType, row.Payload); err != nil {
		// Don't mark processed on error — the worker will retry on the
		// next loop. Log the failure so it's visible.
		slog.Error("webhook event apply failed",
			"workspace_id", row.WorkspaceID,
			"event_type", row.EventType,
			"object_id", row.ObjectID,
			"error", err)
		// Return success-with-no-commit so the SELECT FOR UPDATE lock
		// releases and another worker can pick it up. The transaction's
		// rollback (via defer) lets the row revert to claimable.
		return false, nil
	}

	if err := q.MarkWebhookEventProcessed(ctx, row.ID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// dispatchWebhookEvent routes by event_type to the right per-event handler.
func dispatchWebhookEvent(ctx context.Context, deps WebhookDeps, eventType string, body []byte) error {
	switch eventType {
	case "issue":
		return ApplyIssueHookEvent(ctx, deps, body)
	case "note":
		return ApplyNoteHookEvent(ctx, deps, body)
	case "emoji":
		return ApplyEmojiHookEvent(ctx, deps, body)
	case "label":
		return ApplyLabelHookEvent(ctx, deps, body)
	default:
		// Shouldn't happen — receiver validates event_type before insert.
		// Mark processed by returning nil to stop the retry loop.
		slog.Warn("unknown event_type in queue; ignoring", "event_type", eventType)
		return nil
	}
}
```

- [ ] **Step 9.4: Run — expect pass**

```bash
DATABASE_URL="…" go test ./internal/gitlab/ -run TestWebhookWorker -v
```

- [ ] **Step 9.5: Commit**

```bash
git add server/internal/gitlab/webhook_worker.go server/internal/gitlab/webhook_worker_test.go
git commit -m "feat(gitlab): webhook worker pool draining the event queue"
```

---

## Task 10: Reconciler

**Files:**
- Create: `server/internal/gitlab/reconciler.go`
- Create: `server/internal/gitlab/reconciler_test.go`

The reconciler is a single goroutine that wakes every 5 minutes. Each tick it:
1. Lists every connected workspace.
2. For each workspace, calls `ListIssues(updated_after=last_sync_cursor - 10m)` against GitLab.
3. Upserts any drift (existing translator + sync code from Phase 2a's `syncOneIssue`).
4. Advances `last_sync_cursor` to the max `updated_at` it saw.
5. If the poll found changes AND `last_webhook_received_at < now() - 15m`, sets `connection_status='error'` with `status_message='webhook deliveries delayed; reconciler is filling the gap'`.

For the test: spin up a fake GitLab that returns one updated issue, run one tick of the reconciler, verify the issue is in cache and `last_sync_cursor` moved.

- [ ] **Step 10.1: Add a sqlc query for connected workspaces**

Append to `server/pkg/db/queries/gitlab_connection.sql`:

```sql
-- name: ListConnectedGitlabWorkspaces :many
SELECT * FROM workspace_gitlab_connection
WHERE connection_status IN ('connected', 'error')
ORDER BY workspace_id;

-- name: UpdateWorkspaceGitlabSyncCursor :exec
UPDATE workspace_gitlab_connection
SET last_sync_cursor = $2,
    updated_at = now()
WHERE workspace_id = $1;
```

Run `make sqlc`.

- [ ] **Step 10.2: Write failing test**

Create `server/internal/gitlab/reconciler_test.go`:

```go
package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

func TestReconciler_PicksUpDriftAndAdvancesCursor(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	queries := db.New(pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/7/issues":
			json.NewEncoder(w).Encode([]gitlabapi.Issue{
				{ID: 5001, IID: 11, Title: "from reconciler", State: "opened",
					Labels: []string{}, UpdatedAt: "2026-04-17T15:00:00Z"},
			})
		case "/api/v4/projects/7/labels":
			json.NewEncoder(w).Encode([]gitlabapi.Label{})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	// Seed a connection in the past.
	pool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status,
			last_sync_cursor
		) VALUES ($1, 7, 'g/a', '\x01\x02'::bytea, 1, 'connected', '2026-04-17T14:00:00Z')
	`, wsID)

	// Decrypt-bypass: the reconciler will need the token. Inject a static
	// resolver so we don't have to wire the cipher.
	r := NewReconciler(queries, gitlabapi.NewClient(srv.URL, srv.Client()),
		func(ctx context.Context, encrypted []byte) (string, error) { return "tok", nil })
	if err := r.tickOne(context.Background()); err != nil {
		t.Fatalf("tickOne: %v", err)
	}

	row, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: mustPGUUID(t, wsID),
		GitlabIid:   pgtype.Int4{Int32: 11, Valid: true},
	})
	if err != nil {
		t.Fatalf("issue not picked up: %v", err)
	}
	if row.Title != "from reconciler" {
		t.Errorf("title = %q", row.Title)
	}

	// Cursor should have advanced to the issue's UpdatedAt.
	conn, _ := queries.GetWorkspaceGitlabConnection(context.Background(), mustPGUUID(t, wsID))
	expected, _ := time.Parse(time.RFC3339, "2026-04-17T15:00:00Z")
	if !conn.LastSyncCursor.Valid || !conn.LastSyncCursor.Time.Equal(expected) {
		t.Errorf("last_sync_cursor = %+v, want %v", conn.LastSyncCursor, expected)
	}
}
```

- [ ] **Step 10.3: Run — expect compile error**

```bash
DATABASE_URL="…" go test ./internal/gitlab/ -run TestReconciler -v
```

- [ ] **Step 10.4: Implement**

Create `server/internal/gitlab/reconciler.go`:

```go
package gitlab

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

// TokenDecrypter resolves the encrypted service-token bytes from a
// workspace_gitlab_connection row into the plaintext token that the GitLab
// REST client needs. The reconciler doesn't import the secrets package
// directly so that tests can pass a stub.
type TokenDecrypter func(ctx context.Context, encrypted []byte) (string, error)

// Reconciler polls GitLab every 5 minutes per connected workspace to catch
// changes that the webhook stream missed.
type Reconciler struct {
	queries *db.Queries
	client  *gitlabapi.Client
	decrypt TokenDecrypter

	// Knobs (overridable in tests).
	tickInterval         time.Duration
	overlapWindow        time.Duration
	staleWebhookWindow   time.Duration
}

func NewReconciler(queries *db.Queries, client *gitlabapi.Client, decrypt TokenDecrypter) *Reconciler {
	return &Reconciler{
		queries:            queries,
		client:             client,
		decrypt:            decrypt,
		tickInterval:       5 * time.Minute,
		overlapWindow:      10 * time.Minute,
		staleWebhookWindow: 15 * time.Minute,
	}
}

// Run blocks until ctx is cancelled, ticking every tickInterval.
func (r *Reconciler) Run(ctx context.Context) {
	tick := time.NewTicker(r.tickInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := r.tickOne(ctx); err != nil {
				slog.Error("reconciler tick", "error", err)
			}
		}
	}
}

// tickOne runs one pass over all connected workspaces.
func (r *Reconciler) tickOne(ctx context.Context) error {
	conns, err := r.queries.ListConnectedGitlabWorkspaces(ctx)
	if err != nil {
		return fmt.Errorf("list connected: %w", err)
	}
	for _, conn := range conns {
		if err := r.reconcileOne(ctx, conn); err != nil {
			slog.Error("reconcile workspace",
				"workspace_id", conn.WorkspaceID,
				"error", err)
			continue
		}
	}
	return nil
}

func (r *Reconciler) reconcileOne(ctx context.Context, conn db.WorkspaceGitlabConnection) error {
	token, err := r.decrypt(ctx, conn.ServiceTokenEncrypted)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	// Compute updated_after with overlap.
	since := time.Now().UTC().Add(-r.overlapWindow)
	if conn.LastSyncCursor.Valid && conn.LastSyncCursor.Time.Before(since) {
		since = conn.LastSyncCursor.Time.Add(-r.overlapWindow)
	}

	issues, err := r.client.ListIssues(ctx, token, conn.GitlabProjectID, gitlabapi.ListIssuesParams{
		State:        "all",
		UpdatedAfter: since.Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("list issues: %w", err)
	}

	// Upsert each issue (skips happen inside the upsert via external_updated_at).
	maxSeen := time.Time{}
	if conn.LastSyncCursor.Valid {
		maxSeen = conn.LastSyncCursor.Time
	}
	agentMap, err := buildAgentSlugMap(ctx, r.queries, conn.WorkspaceID)
	if err != nil {
		return fmt.Errorf("agent map: %w", err)
	}
	for _, issue := range issues {
		values := TranslateIssue(issue, &TranslateContext{AgentBySlug: agentMap})
		if _, err := r.queries.UpsertIssueFromGitlab(ctx, buildUpsertIssueParams(conn.WorkspaceID, conn.GitlabProjectID, issue, values)); err != nil {
			return fmt.Errorf("upsert iid=%d: %w", issue.IID, err)
		}
		if t, err := time.Parse(time.RFC3339, issue.UpdatedAt); err == nil && t.After(maxSeen) {
			maxSeen = t
		}
	}

	// Advance the cursor.
	if !maxSeen.IsZero() {
		if err := r.queries.UpdateWorkspaceGitlabSyncCursor(ctx, db.UpdateWorkspaceGitlabSyncCursorParams{
			WorkspaceID:    conn.WorkspaceID,
			LastSyncCursor: pgtype.Timestamptz{Time: maxSeen, Valid: true},
		}); err != nil {
			return fmt.Errorf("update cursor: %w", err)
		}
	}

	// Stale-webhook detection.
	if len(issues) > 0 && conn.LastWebhookReceivedAt.Valid &&
		time.Since(conn.LastWebhookReceivedAt.Time) > r.staleWebhookWindow {
		slog.Warn("reconciler picked up issues but webhook stream is silent",
			"workspace_id", conn.WorkspaceID,
			"last_webhook_received_at", conn.LastWebhookReceivedAt.Time,
			"reconciled_count", len(issues))
		_ = r.queries.UpdateWorkspaceGitlabConnectionStatus(ctx, db.UpdateWorkspaceGitlabConnectionStatusParams{
			WorkspaceID:      conn.WorkspaceID,
			ConnectionStatus: "error",
			StatusMessage: pgtype.Text{
				String: "webhook deliveries appear delayed; reconciler is filling the gap",
				Valid:  true,
			},
		})
	}
	return nil
}
```

- [ ] **Step 10.5: Run — expect pass**

```bash
DATABASE_URL="…" go test ./internal/gitlab/ -run TestReconciler -v
```

- [ ] **Step 10.6: Commit**

```bash
git add server/internal/gitlab/reconciler.go server/internal/gitlab/reconciler_test.go \
        server/pkg/db/queries/gitlab_connection.sql server/pkg/db/generated/
git commit -m "feat(gitlab): reconciler goroutine for webhook drift safety net"
```

---

## Task 11: Connect handler — register webhook after sync

**Files:**
- Modify: `server/internal/handler/handler.go`
- Modify: `server/internal/handler/gitlab_connection.go`
- Modify: `server/cmd/server/main.go`
- Modify: `server/cmd/server/router.go`

The connect handler currently dispatches a sync goroutine. Phase 2b extends that goroutine to ALSO call `CreateProjectHook` against GitLab AFTER the initial sync completes successfully, then save the `webhook_secret` + `webhook_gitlab_id` on the connection row.

The webhook URL needs to be the public-facing Multica URL — read from a new env var.

- [ ] **Step 11.1: Add `PublicURL` to `Handler` struct + setter**

In `server/internal/handler/handler.go`, add a field:

```go
type Handler struct {
	// …existing…
	PublicURL string // base URL used for webhook registration (e.g. "https://multica.example")
}
```

Add a setter:

```go
func (h *Handler) SetPublicURL(url string) {
	h.PublicURL = url
}
```

(Same pattern as `SetBaseCtx` from Phase 2a — avoids cascading constructor changes.)

- [ ] **Step 11.2: Add a sqlc query to update webhook fields**

Append to `server/pkg/db/queries/gitlab_connection.sql`:

```sql
-- name: UpdateWorkspaceGitlabWebhook :exec
UPDATE workspace_gitlab_connection
SET webhook_secret    = $2,
    webhook_gitlab_id = $3,
    updated_at        = now()
WHERE workspace_id = $1;
```

Run `make sqlc`.

- [ ] **Step 11.3: Extend the sync goroutine to register the webhook**

In `server/internal/handler/gitlab_connection.go`, find the goroutine in `ConnectGitlabWorkspace`. After the `RunInitialSync` call (and only if it returned nil), add the webhook creation:

```go
go func(token string, projectID int64, workspaceID string) {
    parent := h.BaseCtx
    if parent == nil {
        parent = context.Background()
    }
    syncCtx, cancel := context.WithTimeout(parent, 10*time.Minute)
    defer cancel()

    if err := gitlabsync.RunInitialSync(syncCtx, gitlabsync.SyncDeps{
        Queries: h.Queries,
        Client:  h.Gitlab,
    }, gitlabsync.RunInitialSyncInput{
        WorkspaceID: workspaceID,
        ProjectID:   projectID,
        Token:       token,
    }); err != nil {
        slog.Error("initial gitlab sync failed",
            "error", err, "workspace_id", workspaceID, "project_id", projectID)
        return
    }

    // After successful sync: register the webhook so future deltas flow.
    secret, err := generateWebhookSecret()
    if err != nil {
        slog.Error("generate webhook secret", "error", err)
        return
    }
    if h.PublicURL == "" {
        slog.Warn("MULTICA_PUBLIC_URL not configured; skipping webhook registration. Cache will go stale until reconnect.",
            "workspace_id", workspaceID)
        return
    }
    hook, err := h.Gitlab.CreateProjectHook(syncCtx, token, projectID, gitlab.CreateProjectHookInput{
        URL:                      h.PublicURL + "/api/gitlab/webhook",
        Token:                    secret,
        IssuesEvents:             true,
        ConfidentialIssuesEvents: true,
        NoteEvents:               true,
        ConfidentialNoteEvents:   true,
        EmojiEvents:              true,
        LabelEvents:              true,
        EnableSSLVerification:    true,
    })
    if err != nil {
        slog.Error("create project hook", "error", err, "workspace_id", workspaceID)
        return
    }
    if err := h.Queries.UpdateWorkspaceGitlabWebhook(syncCtx, db.UpdateWorkspaceGitlabWebhookParams{
        WorkspaceID:     parseUUID(workspaceID),
        WebhookSecret:   pgtype.Text{String: secret, Valid: true},
        WebhookGitlabID: pgtype.Int8{Int64: hook.ID, Valid: true},
    }); err != nil {
        slog.Error("save webhook fields", "error", err, "workspace_id", workspaceID)
    }
}(req.Token, project.ID, workspaceID)
```

Add the helper at the bottom of the file:

```go
// generateWebhookSecret returns a 32-byte random hex string suitable for
// the X-Gitlab-Token header. 64 chars of entropy is overkill but cheap.
func generateWebhookSecret() (string, error) {
    buf := make([]byte, 32)
    if _, err := cryptorand.Read(buf); err != nil {
        return "", err
    }
    return hex.EncodeToString(buf), nil
}
```

Add imports:

```go
import (
    cryptorand "crypto/rand"
    "encoding/hex"
    "github.com/jackc/pgx/v5/pgtype"
)
```

- [ ] **Step 11.4: Wire `MULTICA_PUBLIC_URL` in main.go**

In `server/cmd/server/main.go`, near the other env reads:

```go
publicURL := os.Getenv("MULTICA_PUBLIC_URL")
if publicURL == "" && gitlabEnabled {
    slog.Warn("MULTICA_PUBLIC_URL is not set; gitlab webhook registration will be skipped (cache will go stale after sync)")
}
```

Pass it into `NewRouter`:

```go
r := NewRouter(pool, hub, bus, secretsCipher, gitlabClient, gitlabEnabled, serverCtx, publicURL)
```

In `router.go`, extend `NewRouter` to accept `publicURL string` and call `h.SetPublicURL(publicURL)` after `h.SetBaseCtx(serverCtx)`.

In `server/cmd/server/integration_test.go`, update the `NewRouter(...)` call to pass `""` for the new arg.

In `server/internal/handler/handler_test.go`, no change needed — the testHandler's PublicURL stays empty.

- [ ] **Step 11.5: Update `.env.example`**

Append to root `.env.example`:

```
# Public base URL where this Multica server is reachable from gitlab.com.
# Required for gitlab webhook registration. If unset, sync still works on
# initial connect, but the cache will go stale until reconnect (no webhooks).
MULTICA_PUBLIC_URL=
```

- [ ] **Step 11.6: Update connect handler tests**

The Phase 2a tests that POST to ConnectGitlabWorkspace currently use a fake gitlab server that doesn't handle the `POST /api/v4/projects/:id/hooks` path. Add it to the fakes (any value is fine — the test doesn't assert on the hook fields):

```go
case "/api/v4/projects/42/hooks":
    if r.Method == http.MethodPost {
        w.Write([]byte(`{"id":11,"url":"x"}`))
    }
```

Add this case to every test that's called `seedConnectionWithWebhookSecret` or that triggers `ConnectGitlabWorkspace`.

If the test's connect path fires the goroutine and `h.PublicURL == ""`, the goroutine returns early before creating a hook — safe for tests that don't set a public URL.

- [ ] **Step 11.7: Run all gitlab handler tests**

```bash
DATABASE_URL="…" go test ./internal/handler/ -run "TestConnectGitlab|TestGetGitlab|TestDisconnectGitlab|TestGitlabHandlers|TestReceiveGitlabWebhook|TestGitlabConnectedWorkspace" -v 2>&1 | tail -30
```

Expected: all pass.

- [ ] **Step 11.8: Commit**

```bash
git add server/internal/handler/handler.go \
        server/internal/handler/gitlab_connection.go \
        server/internal/handler/gitlab_connection_test.go \
        server/cmd/server/main.go \
        server/cmd/server/router.go \
        server/cmd/server/integration_test.go \
        server/pkg/db/queries/gitlab_connection.sql \
        server/pkg/db/generated/ \
        .env.example
git commit -m "feat(handler): connect handler registers gitlab webhook after sync"
```

---

## Task 12: Disconnect handler — remove webhook before truncating cache

**Files:**
- Modify: `server/internal/handler/gitlab_connection.go`

The Phase 2a disconnect runs four DB deletions in a transaction. Phase 2b adds: BEFORE the transaction, decrypt the service token and call `DeleteProjectHook`. Errors here are logged but not fatal — even if GitLab is unreachable, we still want to clean up the local cache.

- [ ] **Step 12.1: Modify `DisconnectGitlabWorkspace`**

In `server/internal/handler/gitlab_connection.go`, before the `tx, err := h.TxStarter.Begin(...)` call, add:

```go
// Best-effort: remove the webhook from GitLab so it stops sending us
// deliveries for a workspace we're about to forget. Failures are logged
// but don't block the local cleanup — if GitLab is unreachable, the hook
// stays orphaned and the next admin can clean it up by hand. We still
// want the local DB state to be correct.
if existing, err := h.Queries.GetWorkspaceGitlabConnection(r.Context(), parseUUID(workspaceID)); err == nil {
    if existing.WebhookGitlabID.Valid && existing.ServiceTokenEncrypted != nil && h.Secrets != nil {
        token, err := h.Secrets.Decrypt(existing.ServiceTokenEncrypted)
        if err != nil {
            slog.Warn("decrypt service token for hook deletion", "error", err)
        } else {
            if err := h.Gitlab.DeleteProjectHook(r.Context(), string(token), existing.GitlabProjectID, existing.WebhookGitlabID.Int64); err != nil {
                slog.Warn("delete gitlab project hook", "error", err)
            }
        }
    }
}
```

The `DeleteProjectHook` already treats 404 as success (Task 3 spec). Other errors are logged.

- [ ] **Step 12.2: Update existing disconnect test**

The Phase 2a `TestDisconnectGitlabWorkspace_TruncatesCache` test seeds a connection without `webhook_gitlab_id` populated. The new disconnect logic skips the `DeleteProjectHook` call when `WebhookGitlabID.Valid == false` — so the test passes unchanged.

If you want to add coverage that the hook IS deleted when present, add a new test:

```go
func TestDisconnectGitlabWorkspace_DeletesGitlabHook(t *testing.T) {
    var hookDeleteHit bool
    fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method == http.MethodDelete && r.URL.Path == "/api/v4/projects/7/hooks/11" {
            hookDeleteHit = true
            w.WriteHeader(http.StatusNoContent)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        switch r.URL.Path {
        case "/api/v4/user":
            w.Write([]byte(`{"id":1,"username":"svc"}`))
        case "/api/v4/projects/7":
            w.Write([]byte(`{"id":7,"path_with_namespace":"g/a"}`))
        default:
            w.Write([]byte(`[]`))
        }
    }))
    defer fake.Close()

    h := buildHandlerWithGitlab(t, fake.URL)
    h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

    // Seed a row WITH webhook_gitlab_id = 11 + an encrypted token.
    encrypted, _ := h.Secrets.Encrypt([]byte("glpat-x"))
    testPool.Exec(context.Background(), `
        INSERT INTO workspace_gitlab_connection (
            workspace_id, gitlab_project_id, gitlab_project_path,
            service_token_encrypted, service_token_user_id,
            webhook_secret, webhook_gitlab_id, connection_status
        ) VALUES ($1, 7, 'g/a', $2, 1, 'wh-secret', 11, 'connected')
    `, testWorkspaceID, encrypted)

    delReq := httptest.NewRequest(http.MethodDelete, "/", nil)
    delReq.Header.Set("X-User-ID", testUserID)
    delReq = withURLParam(delReq, "id", testWorkspaceID)
    rr := httptest.NewRecorder()
    h.DisconnectGitlabWorkspace(rr, delReq)

    if rr.Code != http.StatusNoContent {
        t.Fatalf("status = %d", rr.Code)
    }
    if !hookDeleteHit {
        t.Errorf("DeleteProjectHook was not called")
    }
}
```

- [ ] **Step 12.3: Run**

```bash
DATABASE_URL="…" go test ./internal/handler/ -run TestDisconnectGitlabWorkspace -v
```

- [ ] **Step 12.4: Commit**

```bash
git add server/internal/handler/gitlab_connection.go server/internal/handler/gitlab_connection_test.go
git commit -m "feat(handler): disconnect deletes gitlab webhook before truncating cache"
```

---

## Task 13: Wire workers + receiver in main.go and router

**Files:**
- Modify: `server/cmd/server/main.go`
- Modify: `server/cmd/server/router.go`

The webhook receiver endpoint goes outside the auth group — GitLab can't supply Multica session cookies. The worker pool and reconciler both run as goroutines started in `main.go`, sharing the `serverCtx` from Phase 2a so they stop on shutdown.

- [ ] **Step 13.1: Mount the webhook route**

In `server/cmd/server/router.go`, near the other public routes (Auth section at the top, OUTSIDE the `Auth` middleware group):

```go
// Auth (public)
r.Post("/auth/send-code", h.SendCode)
// …
r.Post("/auth/logout", h.Logout)

// GitLab webhook (public — auth is via X-Gitlab-Token header)
r.Post("/api/gitlab/webhook", h.ReceiveGitlabWebhook)
```

- [ ] **Step 13.2: Start workers in `main.go`**

In `main.go`, after the existing background workers (around `runRuntimeSweeper`, `runAutopilotScheduler`), add:

```go
if gitlabEnabled {
    queries := db.New(pool)
    // Webhook worker pool — drains gitlab_webhook_event into the cache.
    webhookWorker := gitlabsync.NewWebhookWorker(queries, pool, 5, 250*time.Millisecond)
    go webhookWorker.Run(serverCtx)

    // Reconciler — 5-minute drift catcher.
    decrypter := func(ctx context.Context, encrypted []byte) (string, error) {
        plain, err := secretsCipher.Decrypt(encrypted)
        if err != nil {
            return "", err
        }
        return string(plain), nil
    }
    reconciler := gitlabsync.NewReconciler(queries, gitlabClient, decrypter)
    go reconciler.Run(serverCtx)
}
```

Add an alias import:

```go
import (
    // …existing…
    gitlabsync "github.com/multica-ai/multica/server/internal/gitlab"
)
```

- [ ] **Step 13.3: Verify build**

```bash
cd server && go build ./...
```

- [ ] **Step 13.4: Smoke test by starting the server**

```bash
cd /Users/jimmy.mills/Developer/multica-gitlab-phase-2b && \
  MULTICA_GITLAB_ENABLED=true \
  MULTICA_PUBLIC_URL=http://localhost:18080 \
  MULTICA_SECRETS_KEY=$(head -c 32 /dev/urandom | base64) \
  PORT=18080 \
  DATABASE_URL="postgres://multica:multica@localhost:5432/multica_multica_gitlab_phase_2b_NNN?sslmode=disable" \
  go run ./server/cmd/server &

sleep 2
curl -s -X POST http://localhost:18080/api/gitlab/webhook \
  -H "X-Gitlab-Token: definitely-not-a-real-secret" \
  -H "X-Gitlab-Event: Issue Hook" \
  -d '{}' | head
# Expected: {"error":"unknown webhook token"}

kill %1
```

(Adjust the DB URL to match your worktree's actual `.env.worktree` value.)

- [ ] **Step 13.5: Commit**

```bash
git add server/cmd/server/main.go server/cmd/server/router.go
git commit -m "feat(server): mount webhook route + start worker pool and reconciler"
```

---

## Task 14: Final verification — `make check`

**Files:** (none)

- [ ] **Step 14.1: Clean test DB to avoid leakage from previous tasks**

```bash
psql "postgres://multica:multica@localhost:5432/multica_multica_gitlab_phase_2b_NNN?sslmode=disable" \
  -c "DELETE FROM workspace WHERE slug LIKE 'gl-sync-test-%';"
```

- [ ] **Step 14.2: Run TS + Go tests**

```bash
cd /Users/jimmy.mills/Developer/multica-gitlab-phase-2b && pnpm typecheck && pnpm test
cd server && DATABASE_URL="…" go test ./... 2>&1 | tail -20
```

Expected: all pass except the two pre-existing date-bucket failures from Phase 1 (`TestGetRuntimeUsage_BucketsByUsageTime`, `TestWorkspaceUsage_BucketsByUsageTime`) — these are unrelated to gitlab.

- [ ] **Step 14.3: Manual smoke test (optional, recommended)**

Set up a throwaway gitlab.com test project. Make the local Multica server reachable from gitlab.com (e.g. via ngrok). Connect the workspace. Verify:
- After connect, the cache populates within seconds.
- Editing an issue title in gitlab.com causes the cached row to update within ~5 seconds (webhook).
- If you wait 5+ minutes without changes, then make a change, the reconciler picks it up too.
- Disconnecting removes the webhook from gitlab.com (check Project → Settings → Webhooks).

Document any deviations as Phase 2b follow-ups.

- [ ] **Step 14.4: Commit any final fix-ups**

```bash
git status
git add …
git commit -m "fix(gitlab-phase-2b): <what you fixed>"
```

---

## Out of scope (Phase 3 will add)

- Per-user PAT mapping for human-authored writes.
- Removing the 501 stopgap on issue writes.
- Backfilling NULL author/actor refs on synced cache rows.

## Definition of done

Phase 2b is complete when:

1. Connecting a workspace registers a webhook in GitLab and stores `webhook_secret` + `webhook_gitlab_id`.
2. Editing an issue in GitLab.com triggers a webhook; the receiver dedupes; the worker pool applies the change to the cache within ~1s.
3. The reconciler picks up changes that webhooks missed (verified by stopping the server, making a change in GitLab, restarting, and waiting <5 min for the change to appear).
4. Disconnecting the workspace removes the webhook from GitLab AND truncates the cache.
5. If webhook delivery is silent for >15 min while the reconciler keeps finding deltas, the connection row's `connection_status` flips to `'error'` with a descriptive `status_message`.
6. `make check` is green (modulo the two pre-existing date-bucket failures).
