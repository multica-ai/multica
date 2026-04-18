# GitLab Issues Integration — Phase 3c Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable write-through for issue comment CRUD on GitLab-connected workspaces (`POST /api/issues/{id}/comments`, `PUT /api/comments/{id}`, `DELETE /api/comments/{id}`) + unblock the purely-internal `POST /api/issues/{id}/tasks/{taskId}/cancel` route.

**Architecture:** Mirror Phase 3a/3b write-through pattern: resolver picks actor-appropriate PAT → GitLab REST call → transactional cache upsert. Agent-authored comments PREFIX the GitLab note body with `**[agent:<slug>]** ` so Phase 2b's webhook round-trip correctly reattributes authorship (this is already the read-side convention). Attachments on comments stay Multica-local (no GitLab upload). `@mentions` in bodies pass through as-is (no syntax translation). Threading stays flat on the GitLab side — Multica's `comment.parent_id` is preserved in cache only.

**Tech Stack:** Go 1.26, Chi router, pgx/v5, sqlc, httptest for fake GitLab server.

---

## Scope

**In scope for Phase 3c:**
- `POST /api/issues/{id}/comments` — write-through create, including agent-prefix encoding
- `PUT /api/comments/{id}` — write-through update (body/content only)
- `DELETE /api/comments/{id}` — write-through delete, idempotent on GitLab 404
- `POST /api/issues/{id}/tasks/{taskId}/cancel` — remove 501 gate only (no GitLab work)
- GitLab REST client methods: `CreateNote`, `UpdateNote`, `DeleteNote`
- Translator function: `BuildCreateNoteBody(authorType, agentSlug, content)` — handles agent prefix encoding
- Tighten `UpsertCommentFromGitlab`'s ON CONFLICT with an `external_updated_at` clobber guard (mirror the issue upsert pattern from Phase 2b)

**Out of scope (Phase 3d+):**
- Issue-level reactions (`POST/DELETE /api/issues/{id}/reactions`) → Phase 3d
- Comment-level reactions (`POST/DELETE /api/comments/{id}/reactions`) → Phase 3d
- Issue subscribe/unsubscribe → Phase 3d
- Attachments round-tripped through GitLab → stays cache-only (possibly Phase 4 if revisited)
- @mention user-ID translation → Phase 4 (needs member↔GitLab user mapping)
- Backfill of pre-connection comments → never (fresh install)

## File Structure

**New files:** None.

**Files to modify:**

| File | Responsibility |
|---|---|
| `server/pkg/db/queries/gitlab_cache.sql` | Add `WHERE` clause on `UpsertCommentFromGitlab`'s ON CONFLICT DO UPDATE (external_updated_at clobber guard, matching the issue upsert) |
| `server/pkg/gitlab/notes.go` | Add `CreateNote`, `UpdateNote`, `DeleteNote` methods (parallel to `issues.go`'s CRUD) |
| `server/pkg/gitlab/notes_test.go` | Tests for the 3 new client methods |
| `server/internal/gitlab/translator.go` | Add `BuildCreateNoteBody(authorType, agentSlug, content) string` — encodes agent prefix |
| `server/internal/gitlab/translator_test.go` | Tests for the new translator function |
| `server/internal/handler/comment.go` | Add write-through branches to `CreateComment`, `UpdateComment`, `DeleteComment` |
| `server/internal/handler/comment_test.go` | Write-through tests for all three handlers (happy + GitLab error path) |
| `server/cmd/server/router.go` | Restructure `/api/comments/{commentId}` route to per-route middleware (so PUT/DELETE can be unmounted while `/reactions` stays gated). Unmount PUT/DELETE + `POST /comments` + `POST /tasks/{taskId}/cancel`. |

## Hard rules

1. **Write-through on connected workspaces is authoritative.** GitLab error → HTTP 502. Do NOT fall through to the legacy direct-DB path (Phase 3a/3b Blocker 1 precedent).
2. **Agent prefix symmetry.** When the actor is an agent, prefix the GitLab note body with `**[agent:<slug>]** ` so Phase 2b's `TranslateNote` correctly reattributes on webhook replay. For humans, no prefix. This is the read↔write contract.
3. **Author preservation via clobber guard.** Task 0 adds the `external_updated_at` WHERE clause to `UpsertCommentFromGitlab`. Without this, a racing webhook could overwrite our Multica-native `author_type`/`author_id` on human-authored comments (webhook can't reconstruct member authorship from body alone).
4. **Attachments stay local.** `linkAttachmentsByIDs` (cache-only) is called the same way in write-through as in legacy. Don't upload to GitLab.
5. **Idempotent DELETE.** `Client.DeleteNote` treats GitLab 404 as success (same pattern as `DeleteIssue`). Handler proceeds to cache cleanup regardless.
6. **Use `ResolveTokenForWrite`** with the comment's author (resolved via `h.resolveActor(r, userID, workspaceID)`), not the issue creator.
7. **Agent-task enqueue preserved.** Legacy `CreateComment` triggers `on_comment` agent tasks under specific conditions. Write-through path must preserve this post-commit, same as Phase 3a/3b did for issue-level agent tasks.

---

## Task 0: Tighten `UpsertCommentFromGitlab` ON CONFLICT clobber guard

**Files:**
- Modify: `server/pkg/db/queries/gitlab_cache.sql`
- Regenerate: run `make sqlc` after the SQL change

**Why:** The current ON CONFLICT has no `WHERE` clause, so any webhook replay overwrites cached fields unconditionally. The issue upsert uses `WHERE issue.external_updated_at IS NULL OR issue.external_updated_at < EXCLUDED.external_updated_at` to prevent race-between-write-through-and-webhook from losing Multica-native author info. Comments need the same protection before Phase 3c's write-through can safely set `author_type`/`author_id`.

- [ ] **Step 1: Add the WHERE clause**

Edit `server/pkg/db/queries/gitlab_cache.sql`. Locate `UpsertCommentFromGitlab` (around line 123). The current body:

```sql
-- name: UpsertCommentFromGitlab :one
INSERT INTO comment (
    workspace_id, issue_id, author_type, author_id,
    gitlab_author_user_id, content, type,
    gitlab_note_id, external_updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (gitlab_note_id) WHERE gitlab_note_id IS NOT NULL DO UPDATE SET
    content = EXCLUDED.content,
    type = EXCLUDED.type,
    author_type = EXCLUDED.author_type,
    author_id = EXCLUDED.author_id,
    gitlab_author_user_id = EXCLUDED.gitlab_author_user_id,
    external_updated_at = EXCLUDED.external_updated_at,
    updated_at = now()
RETURNING *;
```

Change the tail to add the WHERE clause (mirror the issue upsert pattern exactly):

```sql
ON CONFLICT (gitlab_note_id) WHERE gitlab_note_id IS NOT NULL DO UPDATE SET
    content = EXCLUDED.content,
    type = EXCLUDED.type,
    author_type = EXCLUDED.author_type,
    author_id = EXCLUDED.author_id,
    gitlab_author_user_id = EXCLUDED.gitlab_author_user_id,
    external_updated_at = EXCLUDED.external_updated_at,
    updated_at = now()
WHERE comment.external_updated_at IS NULL
   OR comment.external_updated_at < EXCLUDED.external_updated_at
RETURNING *;
```

- [ ] **Step 2: Regenerate sqlc**

Run: `cd server && make sqlc`
Expected: `server/pkg/db/generated/gitlab_cache.sql.go` updated with the new WHERE clause baked into the query constant. No signature change on `UpsertCommentFromGitlab` — caller code is unchanged.

- [ ] **Step 3: Verify existing webhook tests still pass**

Phase 2b has tests that exercise `UpsertCommentFromGitlab` through the webhook handler (`server/internal/gitlab/webhook_handlers_test.go` and `server/internal/handler/gitlab_webhook_test.go`). Run:

```bash
cd server && DATABASE_URL="postgres://multica:multica@localhost:5432/multica_multica_gitlab_phase_3c_<port>?sslmode=disable" go test ./internal/gitlab/ ./internal/handler/ -run 'Note|Comment|Webhook' -v
```

Expected: PASS. The WHERE clause changes behavior only when the same note is upserted twice with equal-or-older `external_updated_at`, which doesn't happen in the existing test scenarios.

If any test fails due to the new guard (e.g., a test upserting the same note twice), the test needs updating to pass distinct timestamps — NOT the WHERE clause rollback.

- [ ] **Step 4: Callers handle `pgx.ErrNoRows`**

Phase 2b webhook handlers (in `server/internal/gitlab/webhook_handlers.go`) call `UpsertCommentFromGitlab` and currently don't handle `pgx.ErrNoRows` (because the old query never returned it). With the WHERE clause, a skipped upsert returns `pgx.ErrNoRows` — existing callers must treat that as success (cache is newer than the event).

Grep: `grep -n "UpsertCommentFromGitlab" server/internal/gitlab/ server/internal/handler/`. For each caller, confirm the error check accepts `pgx.ErrNoRows` as success:

```go
row, err := qtx.UpsertCommentFromGitlab(ctx, params)
if err != nil && !errors.Is(err, pgx.ErrNoRows) {
    return fmt.Errorf("upsert comment: %w", err)
}
```

Update any caller missing this guard. This exactly mirrors how Phase 2b handled the same issue on `UpsertIssueFromGitlab`.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/db/queries/gitlab_cache.sql server/pkg/db/generated/gitlab_cache.sql.go server/internal/gitlab/webhook_handlers.go
git commit -m "fix(db): UpsertCommentFromGitlab respects external_updated_at clobber guard"
```

(Include other files if grep surfaced callers that needed updates.)

---

## Task 1: GitLab Client — `CreateNote` REST method

**Files:**
- Modify: `server/pkg/gitlab/notes.go` (add after existing `ListNotes`)
- Test: `server/pkg/gitlab/notes_test.go` (create if absent, or extend)

- [ ] **Step 1: Write the failing tests**

Create `server/pkg/gitlab/notes_test.go` (or extend if it already exists):

```go
package gitlab

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateNote_SendsPOSTWithBody(t *testing.T) {
	var capturedMethod, capturedPath, capturedToken, capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedToken = r.Header.Get("PRIVATE-TOKEN")
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":555,"body":"hello","author":{"id":9,"username":"u"},"created_at":"2026-04-17T12:00:00Z","updated_at":"2026-04-17T12:00:00Z"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	note, err := c.CreateNote(context.Background(), "tok", 42, 7, "hello")
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/7/notes" {
		t.Errorf("path = %s", capturedPath)
	}
	if capturedToken != "tok" {
		t.Errorf("token header = %s", capturedToken)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(capturedBody), &body)
	if body["body"] != "hello" {
		t.Errorf("body field = %v, want hello", body["body"])
	}
	if note.ID != 555 || note.Body != "hello" {
		t.Errorf("note = %+v, want ID=555 body=hello", note)
	}
}

func TestCreateNote_PropagatesNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	_, err := c.CreateNote(context.Background(), "tok", 1, 1, "x")
	if err == nil {
		t.Fatal("expected error on 403")
	}
	if !strings.Contains(err.Error(), "403") && !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("error = %v, want to contain 403 or forbidden", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd server && go test ./pkg/gitlab/ -run TestCreateNote -v`
Expected: FAIL (`CreateNote` undefined).

- [ ] **Step 3: Add the method**

Read `server/pkg/gitlab/notes.go` first to see `ListNotes`'s pattern and what's imported. Add after `ListNotes`:

```go
// CreateNote sends POST /api/v4/projects/:id/issues/:iid/notes.
// Returns the created Note.
func (c *Client) CreateNote(ctx context.Context, token string, projectID int, issueIID int, body string) (*Note, error) {
	path := fmt.Sprintf("/api/v4/projects/%d/issues/%d/notes", projectID, issueIID)
	payload := map[string]any{"body": body}
	var out Note
	if err := c.do(ctx, http.MethodPost, token, path, payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
```

Match the parameter types to `ListNotes`'s signature (likely `projectID int`, `issueIID int` — verify).

- [ ] **Step 4: Run tests**

Run: `cd server && go test ./pkg/gitlab/ -run TestCreateNote -v`
Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/gitlab/notes.go server/pkg/gitlab/notes_test.go
git commit -m "feat(gitlab): CreateNote REST client method"
```

---

## Task 2: GitLab Client — `UpdateNote` REST method

**Files:** same as Task 1.

- [ ] **Step 1: Write the failing tests**

Add to `server/pkg/gitlab/notes_test.go`:

```go
func TestUpdateNote_SendsPUTWithBody(t *testing.T) {
	var capturedMethod, capturedPath string
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":555,"body":"updated","author":{"id":9},"updated_at":"2026-04-17T13:00:00Z"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	note, err := c.UpdateNote(context.Background(), "tok", 42, 7, 555, "updated")
	if err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/7/notes/555" {
		t.Errorf("path = %s", capturedPath)
	}
	if capturedBody["body"] != "updated" {
		t.Errorf("body = %v, want updated", capturedBody["body"])
	}
	if note.ID != 555 || note.Body != "updated" {
		t.Errorf("note = %+v", note)
	}
}

func TestUpdateNote_PropagatesNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	_, err := c.UpdateNote(context.Background(), "tok", 1, 1, 1, "x")
	if err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd server && go test ./pkg/gitlab/ -run TestUpdateNote -v`
Expected: FAIL (`UpdateNote` undefined).

- [ ] **Step 3: Add the method**

Add to `notes.go`:

```go
// UpdateNote sends PUT /api/v4/projects/:id/issues/:iid/notes/:note_id.
func (c *Client) UpdateNote(ctx context.Context, token string, projectID int, issueIID int, noteID int64, body string) (*Note, error) {
	path := fmt.Sprintf("/api/v4/projects/%d/issues/%d/notes/%d", projectID, issueIID, noteID)
	payload := map[string]any{"body": body}
	var out Note
	if err := c.do(ctx, http.MethodPut, token, path, payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd server && go test ./pkg/gitlab/ -run TestUpdateNote -v` — PASS.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/gitlab/notes.go server/pkg/gitlab/notes_test.go
git commit -m "feat(gitlab): UpdateNote REST client method"
```

---

## Task 3: GitLab Client — `DeleteNote` REST method (404 idempotent)

**Files:** same as Task 1.

- [ ] **Step 1: Write the failing tests**

```go
func TestDeleteNote_SendsDELETE(t *testing.T) {
	var capturedMethod, capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.DeleteNote(context.Background(), "tok", 42, 7, 555); err != nil {
		t.Fatalf("DeleteNote: %v", err)
	}
	if capturedMethod != http.MethodDelete {
		t.Errorf("method = %s", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/7/notes/555" {
		t.Errorf("path = %s", capturedPath)
	}
}

func TestDeleteNote_404IsIdempotentSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"404 Not Found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.DeleteNote(context.Background(), "tok", 1, 1, 1); err != nil {
		t.Fatalf("expected 404 as success, got %v", err)
	}
}

func TestDeleteNote_PropagatesNon404Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	if err := c.DeleteNote(context.Background(), "tok", 1, 1, 1); err == nil {
		t.Fatal("expected error for 403")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd server && go test ./pkg/gitlab/ -run TestDeleteNote -v` — FAIL.

- [ ] **Step 3: Add the method**

Look at how `DeleteIssue` (from Phase 3b) handles 404 (idempotent success). Mirror it:

```go
// DeleteNote sends DELETE /api/v4/projects/:id/issues/:iid/notes/:note_id.
// Treats 404 as success (idempotent — if the note is already gone, that's
// the desired state).
func (c *Client) DeleteNote(ctx context.Context, token string, projectID int, issueIID int, noteID int64) error {
	path := fmt.Sprintf("/api/v4/projects/%d/issues/%d/notes/%d", projectID, issueIID, noteID)
	err := c.do(ctx, http.MethodDelete, token, path, nil, nil)
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
```

Add `"errors"` to the import block if not present.

- [ ] **Step 4: Run tests**

Run: `cd server && go test ./pkg/gitlab/ -run TestDeleteNote -v` — all 3 PASS.

- [ ] **Step 5: Commit**

```bash
git add server/pkg/gitlab/notes.go server/pkg/gitlab/notes_test.go
git commit -m "feat(gitlab): DeleteNote REST client method"
```

---

## Task 4: Translator — `BuildCreateNoteBody` (agent prefix encoding)

**Files:**
- Modify: `server/internal/gitlab/translator.go`
- Test: `server/internal/gitlab/translator_test.go`

**Semantics:** When the actor is an agent, the note body must start with `**[agent:<slug>]** ` so Phase 2b's `TranslateNote` can round-trip the authorship. For humans, the body is passed through unchanged.

- [ ] **Step 1: Write the failing tests**

Add to `server/internal/gitlab/translator_test.go`:

```go
func TestBuildCreateNoteBody(t *testing.T) {
	cases := []struct {
		name       string
		authorType string
		agentSlug  string
		content    string
		want       string
	}{
		{
			name:       "human author passes body through",
			authorType: "member",
			agentSlug:  "",
			content:    "hello world",
			want:       "hello world",
		},
		{
			name:       "agent prefixes body",
			authorType: "agent",
			agentSlug:  "builder",
			content:    "I will investigate this",
			want:       "**[agent:builder]** I will investigate this",
		},
		{
			name:       "agent with empty slug falls back to human-style (defensive)",
			authorType: "agent",
			agentSlug:  "",
			content:    "fallback",
			want:       "fallback",
		},
		{
			name:       "empty content still prefixed for agent",
			authorType: "agent",
			agentSlug:  "reviewer",
			content:    "",
			want:       "**[agent:reviewer]** ",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildCreateNoteBody(tc.authorType, tc.agentSlug, tc.content)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd server && go test ./internal/gitlab/ -run TestBuildCreateNoteBody -v` — FAIL.

- [ ] **Step 3: Add the function**

Add to `server/internal/gitlab/translator.go` (near `TranslateNote` for colocation):

```go
// BuildCreateNoteBody constructs the GitLab note body for a write-through
// comment. Agent-authored comments are prefixed with "**[agent:<slug>]** "
// so Phase 2b's TranslateNote round-trips the authorship on webhook replay.
// Human-authored comments pass the content through unchanged.
//
// Defensive: if authorType is "agent" but the slug is empty, we fall back to
// the human-style body (no prefix) rather than emitting a malformed
// "**[agent:]** " prefix that parseAgentPrefix won't recognize.
func BuildCreateNoteBody(authorType, agentSlug, content string) string {
	if authorType == "agent" && agentSlug != "" {
		return "**[agent:" + agentSlug + "]** " + content
	}
	return content
}
```

- [ ] **Step 4: Run tests**

Run: `cd server && go test ./internal/gitlab/ -run TestBuildCreateNoteBody -v` — all 4 subcases PASS.

- [ ] **Step 5: Verify round-trip with existing TranslateNote**

Add one more test that proves the round-trip invariant — `BuildCreateNoteBody` output followed by `TranslateNote`/`parseAgentPrefix` recovers the original slug + content:

```go
func TestBuildCreateNoteBody_RoundTripsWithTranslateNote(t *testing.T) {
	body := BuildCreateNoteBody("agent", "builder", "I will investigate this")
	// TranslateNote with a fake Note containing this body should recover
	// agent="builder" and content="I will investigate this".
	nv := TranslateNote(gitlabapi.Note{ID: 1, Body: body, UpdatedAt: "2026-04-17T12:00:00Z"})
	if nv.AuthorType != "agent" {
		t.Errorf("AuthorType = %q, want agent", nv.AuthorType)
	}
	if nv.AuthorSlug != "builder" {
		t.Errorf("AuthorSlug = %q, want builder", nv.AuthorSlug)
	}
	if nv.Body != "I will investigate this" {
		t.Errorf("Body = %q, want 'I will investigate this'", nv.Body)
	}
}
```

If `TranslateNote`'s Note-input param alias differs (e.g., `gitlab.Note` not `gitlabapi.Note`), match the existing usage in translator_test.go. Run this test — should PASS.

- [ ] **Step 6: Commit**

```bash
git add server/internal/gitlab/translator.go server/internal/gitlab/translator_test.go
git commit -m "feat(gitlab): BuildCreateNoteBody encodes agent authorship prefix"
```

---

## Task 5: Handler — `POST /api/issues/{id}/comments` write-through

**Files:**
- Modify: `server/internal/handler/comment.go` — `CreateComment` handler
- Test: `server/internal/handler/comment_test.go`

Insert a write-through branch at the top of `CreateComment`, after parsing the request and loading the parent issue, BEFORE the legacy `h.Queries.CreateComment(...)` call.

- [ ] **Step 1: Read prerequisites**

Study:
1. `CreateComment` in `server/internal/handler/comment.go` (~line 178). Understand: request shape (`Content`, `Type`, `ParentID`, `AttachmentIDs`), parent-comment validation, actor resolution, mention expansion, sanitization, insertion, attachment linking, WS publish, agent-task enqueue.
2. Phase 3a's `CreateIssue` write-through branch (in `issue.go`) for the pattern: resolver lookup, txn boundary, post-commit agent task.
3. `UpsertCommentFromGitlab`'s params + the webhook caller that already uses it — `server/internal/gitlab/webhook_handlers.go::ApplyNoteHookEvent`.

- [ ] **Step 2: Write the failing tests**

Add to `server/internal/handler/comment_test.go`:

```go
func TestCreateComment_WriteThroughHumanSendsNoteWithoutPrefix(t *testing.T) {
	ctx := context.Background()
	var capturedMethod, capturedPath, capturedBody string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":8801,"body":"hello","author":{"id":9},"created_at":"2026-04-17T12:00:00Z","updated_at":"2026-04-17T12:00:00Z"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, h, 500, 42) // helper you'll add in step 3 below
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	req := httptest.NewRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", strings.NewReader(`{"content":"hello"}`))
	req = authedRequest(req, testUserID, testWorkspaceID)
	req = withURLParam(req, "id", issueID)
	rec := httptest.NewRecorder()

	h.CreateComment(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/500/notes" {
		t.Errorf("path = %s", capturedPath)
	}
	// Human author: no prefix.
	var body map[string]any
	_ = json.Unmarshal([]byte(capturedBody), &body)
	if body["body"] != "hello" {
		t.Errorf("GitLab body = %v, want %q (no prefix)", body["body"], "hello")
	}

	// Cache row should exist with gitlab_note_id = 8801.
	var count int
	_ = testPool.QueryRow(ctx, `SELECT COUNT(*) FROM comment WHERE issue_id = $1 AND gitlab_note_id = 8801`, issueID).Scan(&count)
	if count != 1 {
		t.Errorf("cache row not present, count = %d", count)
	}
}

func TestCreateComment_WriteThroughAgentPrefixesBody(t *testing.T) {
	ctx := context.Background()
	var capturedBody string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":8802,"body":"**[agent:builder]** go","author":{"id":9},"created_at":"2026-04-17T12:00:00Z","updated_at":"2026-04-17T12:00:00Z"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	// Seed an agent named "builder" (slug derives from name via the handler helper).
	agentID := seedAgent(t, h, "builder") // helper you'll add in step 3 below
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID) })

	issueID := seedGitlabConnectedIssue(t, h, 501, 42)
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	req := httptest.NewRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", strings.NewReader(`{"content":"go"}`))
	req = authedRequest(req, testUserID, testWorkspaceID)
	req.Header.Set("X-Agent-ID", agentID)
	req = withURLParam(req, "id", issueID)
	rec := httptest.NewRecorder()

	h.CreateComment(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(capturedBody), &body)
	// Agent author: body should be prefixed.
	if body["body"] != "**[agent:builder]** go" {
		t.Errorf("GitLab body = %v, want prefixed", body["body"])
	}
}

func TestCreateComment_WriteThroughGitLabErrorReturns502(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, h, 502, 42)
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	req := httptest.NewRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", strings.NewReader(`{"content":"x"}`))
	req = authedRequest(req, testUserID, testWorkspaceID)
	req = withURLParam(req, "id", issueID)
	rec := httptest.NewRecorder()

	h.CreateComment(rec, req)

	if rec.Code < 400 {
		t.Fatalf("status = %d, want >=400", rec.Code)
	}
	// Cache row must NOT exist.
	var count int
	_ = testPool.QueryRow(ctx, `SELECT COUNT(*) FROM comment WHERE issue_id = $1`, issueID).Scan(&count)
	if count != 0 {
		t.Errorf("cache row leaked on error, count = %d", count)
	}
}
```

- [ ] **Step 3: Add test fixture helpers**

In `server/internal/handler/comment_test.go`, add these helpers if they don't already exist (grep first — Phase 3a/3b may have versions):

```go
// seedGitlabConnectedIssue inserts an issue cache row keyed to the given
// gitlab_iid / gitlab_project_id for the Phase 3c test fixture workspace.
// Returns the issue ID.
func seedGitlabConnectedIssue(t *testing.T, h *Handler, iid int, projectID int) string {
	t.Helper()
	ctx := context.Background()
	issueID := uuid.New().String()
	_, err := testPool.Exec(ctx,
		`INSERT INTO issue (id, workspace_id, number, title, description, status, priority,
		 gitlab_iid, gitlab_project_id, gitlab_issue_id, external_updated_at,
		 creator_type, creator_id, position)
		 VALUES ($1, $2, 1, 'T', '', 'todo', 'none', $3, $4, $5, '2026-04-17T12:00:00Z', 'member', $6, 0)`,
		issueID, parseUUID(testWorkspaceID), iid, projectID, 9000+iid, parseUUID(testUserID))
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	return issueID
}

// seedAgent inserts a minimal agent row with the given name for the Phase 3c
// test fixture workspace. The slug used by the handler derives from name
// (e.g., "Builder" → "builder"; see buildAgentUUIDSlugMap). Returns the
// agent ID as a string.
func seedAgent(t *testing.T, h *Handler, name string) string {
	t.Helper()
	ctx := context.Background()
	// Grep for how Phase 3a's issue tests seed agents (look for similar
	// helpers in issue_test.go). Use the existing pattern — likely also
	// requires inserting an agent_runtime row first, per Phase 2a's
	// schema discoveries.
	// [Implementer: match the exact pattern from existing tests.]
	agentID := uuid.New().String()
	// ... existing seed pattern ...
	return agentID
}
```

The `seedAgent` helper body depends on the codebase's existing agent-seeding pattern. Look for how `TestBacklogToTodoTriggersAgent` or similar tests seed agents and use the identical pattern. If a helper already exists, USE IT — don't duplicate.

- [ ] **Step 4: Run to verify tests fail**

Run:
```bash
cd server && DATABASE_URL="postgres://multica:multica@localhost:5432/multica_multica_gitlab_phase_3c_<port>?sslmode=disable" go test ./internal/handler/ -run TestCreateComment_WriteThrough -v
```
Expected: FAIL — handler doesn't have the write-through branch yet.

- [ ] **Step 5: Add the write-through branch to `CreateComment`**

Insert at the top of `CreateComment`, after parsing `req`, loading `issue`, validating `parentID`, and resolving `authorType/authorID` — BEFORE `h.Queries.CreateComment(...)` (around line 250 in the legacy body):

```go
	if h.GitlabEnabled && h.GitlabResolver != nil {
		wsConn, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), issue.WorkspaceID)
		if wsErr == nil {
			// Workspace connected → write-through is authoritative.
			if !issue.GitlabIid.Valid || !issue.GitlabProjectID.Valid {
				writeError(w, http.StatusBadGateway, "issue missing gitlab identifiers on connected workspace")
				return
			}

			token, _, tokErr := h.GitlabResolver.ResolveTokenForWrite(r.Context(), uuidToString(issue.WorkspaceID), authorType, authorID)
			if tokErr != nil {
				writeError(w, http.StatusInternalServerError, tokErr.Error())
				return
			}

			// Resolve agent slug for body prefix (only used when authorType == "agent").
			agentSlug := ""
			if authorType == "agent" {
				slugMap, slugErr := h.buildAgentUUIDSlugMap(r.Context(), issue.WorkspaceID)
				if slugErr != nil {
					writeError(w, http.StatusInternalServerError, slugErr.Error())
					return
				}
				agentSlug = slugMap[authorID]
			}

			glBody := gitlabsync.BuildCreateNoteBody(authorType, agentSlug, sanitizedContent)
			glNote, glErr := h.Gitlab.CreateNote(r.Context(), token, int(issue.GitlabProjectID.Int64), int(issue.GitlabIid.Int32), glBody)
			if glErr != nil {
				writeError(w, http.StatusBadGateway, glErr.Error())
				return
			}

			// Translate the GitLab response → cache values.
			agentSlugByUUID, _ := h.buildAgentUUIDSlugMap(r.Context(), issue.WorkspaceID)
			agentBySlug := invertAgentMap(agentSlugByUUID)
			nv := gitlabsync.TranslateNote(*glNote)

			// Map the note author back to Multica's author_type/author_id.
			// For agent prefix: use the parsed slug; for human: use the
			// actor from resolveActor (we know who typed it).
			cacheAuthorType := authorType
			cacheAuthorID := authorID
			if nv.AuthorType == "agent" && nv.AuthorSlug != "" {
				if uuid, ok := agentBySlug[nv.AuthorSlug]; ok {
					cacheAuthorType = "agent"
					cacheAuthorID = uuid
				}
			}

			glTx, txErr := h.TxStarter.Begin(r.Context())
			if txErr != nil {
				writeError(w, http.StatusInternalServerError, txErr.Error())
				return
			}
			defer glTx.Rollback(r.Context())
			qtxGL := h.Queries.WithTx(glTx)

			extUpdatedAt, _ := time.Parse(time.RFC3339, glNote.UpdatedAt)
			upsertParams := db.UpsertCommentFromGitlabParams{
				WorkspaceID:          issue.WorkspaceID,
				IssueID:              issue.ID,
				AuthorType:           pgtype.Text{String: cacheAuthorType, Valid: cacheAuthorType != ""},
				AuthorID:             pgUUIDOrNull(cacheAuthorID),
				GitlabAuthorUserID:   pgtype.Int8{Int64: nv.GitlabUserID, Valid: nv.GitlabUserID != 0},
				Content:              nv.Body,
				Type:                 nv.Type,
				GitlabNoteID:         pgtype.Int8{Int64: glNote.ID, Valid: true},
				ExternalUpdatedAt:    pgtype.Timestamptz{Time: extUpdatedAt, Valid: true},
			}
			cacheRow, upErr := qtxGL.UpsertCommentFromGitlab(r.Context(), upsertParams)
			if upErr != nil && !errors.Is(upErr, pgx.ErrNoRows) {
				writeError(w, http.StatusInternalServerError, upErr.Error())
				return
			}
			// If pgx.ErrNoRows: clobber guard rejected — a newer webhook
			// already wrote this. Load the existing row for the response.
			if errors.Is(upErr, pgx.ErrNoRows) {
				cacheRow, err = qtxGL.GetCommentByGitlabNoteID(r.Context(), pgtype.Int8{Int64: glNote.ID, Valid: true})
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
			}

			// Apply Multica-native parent_id if provided (comments can be threaded;
			// GitLab doesn't know about the parent). Update via bare narg if parentID present.
			if parentID != nil {
				updated, updErr := qtxGL.UpdateCommentParent(r.Context(), db.UpdateCommentParentParams{
					ID:       cacheRow.ID,
					ParentID: pgUUID(*parentID),
				})
				if updErr != nil {
					writeError(w, http.StatusInternalServerError, updErr.Error())
					return
				}
				cacheRow = updated
			}

			// Link attachments locally (cache-only — no GitLab upload).
			if len(req.AttachmentIDs) > 0 {
				if err := qtxGL.LinkAttachmentsToComment(r.Context(), db.LinkAttachmentsToCommentParams{
					CommentID: cacheRow.ID,
					IssueID:   issue.ID,
					Column3:   uuidSliceToPgUUIDSlice(req.AttachmentIDs),
				}); err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
			}

			if err := glTx.Commit(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}

			// Post-commit: agent-task enqueue (mirror legacy logic).
			h.maybeEnqueueCommentAgentTask(r.Context(), issue, cacheRow, authorType, authorID)

			resp := commentToResponse(cacheRow, h.loadCommentAttachments(r.Context(), cacheRow.ID))
			h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), authorType, authorID, map[string]any{"comment": resp})
			writeJSON(w, http.StatusCreated, resp)
			return
		}
		// else: workspace not connected → fall through to legacy path
	}
```

Several names are placeholders that may need adjustment:
- `pgUUIDOrNull(s string) pgtype.UUID` — returns `{Valid: false}` on empty string. Likely already exists; grep first.
- `UpdateCommentParent` — a new sqlc query if not present. If it's missing, add to `comment.sql`:
  ```sql
  -- name: UpdateCommentParent :one
  UPDATE comment SET parent_id = sqlc.narg('parent_id'), updated_at = now()
  WHERE id = $1 RETURNING *;
  ```
  Then run `make sqlc`. If the legacy `CreateComment` already uses an alternate path to set `parent_id`, follow that instead.
- `GetCommentByGitlabNoteID` — used in the clobber-guard recovery path. If absent, add:
  ```sql
  -- name: GetCommentByGitlabNoteID :one
  SELECT * FROM comment WHERE gitlab_note_id = $1 LIMIT 1;
  ```
- `maybeEnqueueCommentAgentTask(ctx, issue, comment, authorType, authorID)` — refactor the legacy agent-task-enqueue tail into a named helper so both legacy and write-through paths share it. Look at legacy `CreateComment` lines ~240–280 for the existing enqueue logic (with member-started-thread + @mention guards) and wrap it into this helper.
- `loadCommentAttachments(ctx, commentID) []AttachmentResponse` — already exists in the codebase (legacy response-building path uses it). Reuse.
- `commentToResponse(comment, attachments) *CommentResponse` — same. Reuse.
- `uuidSliceToPgUUIDSlice([]string) []pgtype.UUID` — likely already exists; grep. If not, trivial 3-line helper.

**Critical**: the `sanitizedContent` variable must be the output of `mention.ExpandIssueIdentifiers` + `sanitize.HTML` applied to `req.Content` BEFORE reaching the branch — the write-through branch must not skip sanitization. If the legacy path sanitizes inline inside the branch you're replacing, hoist the sanitization above the branch guard so both paths use the same input.

- [ ] **Step 6: Run tests**

Run: `cd server && ... go test ./internal/handler/ -run TestCreateComment_WriteThrough -v`
Expected: all 3 PASS.

- [ ] **Step 7: Run full handler suite**

Run: `cd server && ... go test ./internal/handler/` — expect only the 2 pre-existing date-bucket flakes to fail.

- [ ] **Step 8: Commit**

```bash
git add server/internal/handler/comment.go server/internal/handler/comment_test.go server/pkg/db/queries/comment.sql server/pkg/db/generated/comment.sql.go
git commit -m "feat(handler): POST /api/issues/{id}/comments writes through GitLab when connected"
```

---

## Task 6: Unmount 501 from `POST /api/issues/{id}/comments`

**Files:** `server/cmd/server/router.go`

- [ ] **Step 1: Remove `gw` wrap**

Find the line (around line 275):
```go
r.With(gw).Post("/comments", h.CreateComment)
```
Change to:
```go
r.Post("/comments", h.CreateComment)
```

Do NOT touch sibling routes (subscribe, unsubscribe, reactions, tasks cancel) — those are Phase 3c step 10 or Phase 3d.

- [ ] **Step 2: Verify**

Run: `cd server && go vet ./cmd/server/ && go build ./cmd/server/` — clean.
Run tests if DB is up: `go test ./cmd/server/` — PASS.

- [ ] **Step 3: Commit**

```bash
git add server/cmd/server/router.go
git commit -m "feat(server): unmount 501 stopgap from POST /api/issues/{id}/comments"
```

---

## Task 7: Handler — `PUT /api/comments/{id}` write-through

**Files:**
- Modify: `server/internal/handler/comment.go` — `UpdateComment` handler
- Test: `server/internal/handler/comment_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestUpdateComment_WriteThroughSendsPUT(t *testing.T) {
	ctx := context.Background()
	var capturedMethod, capturedPath string
	var capturedBody map[string]any
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":8810,"body":"edited","author":{"id":9},"updated_at":"2026-04-17T13:00:00Z"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, h, 510, 42)
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	// Seed a comment with gitlab_note_id so update has something to target.
	commentID := uuid.New().String()
	_, _ = testPool.Exec(ctx,
		`INSERT INTO comment (id, workspace_id, issue_id, author_type, author_id, content, gitlab_note_id, external_updated_at)
		 VALUES ($1, $2, $3, 'member', $4, 'original', 8810, '2026-04-17T12:00:00Z')`,
		commentID, parseUUID(testWorkspaceID), parseUUID(issueID), parseUUID(testUserID))
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID) })

	req := httptest.NewRequest(http.MethodPut, "/api/comments/"+commentID, strings.NewReader(`{"content":"edited"}`))
	req = authedRequest(req, testUserID, testWorkspaceID)
	req = withURLParam(req, "commentId", commentID)
	rec := httptest.NewRecorder()

	h.UpdateComment(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("method = %s", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/510/notes/8810" {
		t.Errorf("path = %s", capturedPath)
	}
	if capturedBody["body"] != "edited" {
		t.Errorf("body = %v", capturedBody["body"])
	}

	var cachedContent string
	_ = testPool.QueryRow(ctx, `SELECT content FROM comment WHERE id = $1`, commentID).Scan(&cachedContent)
	if cachedContent != "edited" {
		t.Errorf("cached content = %q, want edited", cachedContent)
	}
}

func TestUpdateComment_WriteThroughGitLabErrorReturns502(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, h, 511, 42)
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	commentID := uuid.New().String()
	_, _ = testPool.Exec(ctx,
		`INSERT INTO comment (id, workspace_id, issue_id, author_type, author_id, content, gitlab_note_id, external_updated_at)
		 VALUES ($1, $2, $3, 'member', $4, 'original', 8811, '2026-04-17T12:00:00Z')`,
		commentID, parseUUID(testWorkspaceID), parseUUID(issueID), parseUUID(testUserID))
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID) })

	req := httptest.NewRequest(http.MethodPut, "/api/comments/"+commentID, strings.NewReader(`{"content":"edited"}`))
	req = authedRequest(req, testUserID, testWorkspaceID)
	req = withURLParam(req, "commentId", commentID)
	rec := httptest.NewRecorder()

	h.UpdateComment(rec, req)

	if rec.Code < 400 {
		t.Fatalf("status = %d, want >=400", rec.Code)
	}
	var cachedContent string
	_ = testPool.QueryRow(ctx, `SELECT content FROM comment WHERE id = $1`, commentID).Scan(&cachedContent)
	if cachedContent != "original" {
		t.Errorf("cached content mutated on error: %q, want original", cachedContent)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd server && ... go test ./internal/handler/ -run TestUpdateComment_WriteThrough -v` — FAIL.

- [ ] **Step 3: Add the write-through branch to `UpdateComment`**

Insert at the top after parsing the body, loading `existing` comment, and the author/admin auth check, BEFORE the legacy `h.Queries.UpdateComment(...)` call:

```go
	if h.GitlabEnabled && h.GitlabResolver != nil {
		wsConn, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), existing.WorkspaceID)
		if wsErr == nil {
			// Load the parent issue for gitlab identifiers.
			issue, issueErr := h.Queries.GetIssue(r.Context(), existing.IssueID)
			if issueErr != nil {
				writeError(w, http.StatusInternalServerError, issueErr.Error())
				return
			}
			if !issue.GitlabIid.Valid || !issue.GitlabProjectID.Valid || !existing.GitlabNoteID.Valid {
				writeError(w, http.StatusBadGateway, "comment or issue missing gitlab identifiers")
				return
			}

			authorType, authorID := h.resolveActor(r, userID, uuidToString(existing.WorkspaceID))
			token, _, tokErr := h.GitlabResolver.ResolveTokenForWrite(r.Context(), uuidToString(existing.WorkspaceID), authorType, authorID)
			if tokErr != nil {
				writeError(w, http.StatusInternalServerError, tokErr.Error())
				return
			}

			// If the existing comment was agent-authored, preserve the agent prefix in
			// the updated body. Determine prefix from the CURRENT cache author (not
			// request actor — we're editing an existing comment).
			existingAuthorType := ""
			existingAgentSlug := ""
			if existing.AuthorType.Valid {
				existingAuthorType = existing.AuthorType.String
			}
			if existingAuthorType == "agent" && existing.AuthorID.Valid {
				slugMap, _ := h.buildAgentUUIDSlugMap(r.Context(), existing.WorkspaceID)
				existingAgentSlug = slugMap[uuidToString(existing.AuthorID)]
			}

			glBody := gitlabsync.BuildCreateNoteBody(existingAuthorType, existingAgentSlug, sanitizedContent)

			glNote, glErr := h.Gitlab.UpdateNote(r.Context(), token, int(issue.GitlabProjectID.Int64), int(issue.GitlabIid.Int32), existing.GitlabNoteID.Int64, glBody)
			if glErr != nil {
				writeError(w, http.StatusBadGateway, glErr.Error())
				return
			}

			// Translate response + upsert cache (respects clobber guard from Task 0).
			nv := gitlabsync.TranslateNote(*glNote)
			extUpdatedAt, _ := time.Parse(time.RFC3339, glNote.UpdatedAt)
			cacheRow, upErr := h.Queries.UpsertCommentFromGitlab(r.Context(), db.UpsertCommentFromGitlabParams{
				WorkspaceID:        existing.WorkspaceID,
				IssueID:            existing.IssueID,
				AuthorType:         existing.AuthorType,
				AuthorID:           existing.AuthorID,
				GitlabAuthorUserID: pgtype.Int8{Int64: nv.GitlabUserID, Valid: nv.GitlabUserID != 0},
				Content:            nv.Body,
				Type:               nv.Type,
				GitlabNoteID:       pgtype.Int8{Int64: glNote.ID, Valid: true},
				ExternalUpdatedAt:  pgtype.Timestamptz{Time: extUpdatedAt, Valid: true},
			})
			if upErr != nil && !errors.Is(upErr, pgx.ErrNoRows) {
				writeError(w, http.StatusInternalServerError, upErr.Error())
				return
			}
			if errors.Is(upErr, pgx.ErrNoRows) {
				// Clobber guard — webhook already wrote newer state. Load current row.
				cacheRow, _ = h.Queries.GetCommentByGitlabNoteID(r.Context(), pgtype.Int8{Int64: glNote.ID, Valid: true})
			}

			resp := commentToResponse(cacheRow, h.loadCommentAttachments(r.Context(), cacheRow.ID))
			h.publish(protocol.EventCommentUpdated, uuidToString(existing.WorkspaceID), authorType, authorID, map[string]any{"comment": resp})
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}
```

- [ ] **Step 4: Run tests**

Run: `cd server && ... go test ./internal/handler/ -run TestUpdateComment_WriteThrough -v` — PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/handler/comment.go server/internal/handler/comment_test.go
git commit -m "feat(handler): PUT /api/comments/{id} writes through GitLab when connected"
```

---

## Task 8: Handler — `DELETE /api/comments/{id}` write-through

**Files:**
- Modify: `server/internal/handler/comment.go` — `DeleteComment` handler
- Test: `server/internal/handler/comment_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestDeleteComment_WriteThroughSendsDELETE(t *testing.T) {
	ctx := context.Background()
	var capturedMethod, capturedPath string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, h, 520, 42)
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	commentID := uuid.New().String()
	_, _ = testPool.Exec(ctx,
		`INSERT INTO comment (id, workspace_id, issue_id, author_type, author_id, content, gitlab_note_id, external_updated_at)
		 VALUES ($1, $2, $3, 'member', $4, 'x', 8820, '2026-04-17T12:00:00Z')`,
		commentID, parseUUID(testWorkspaceID), parseUUID(issueID), parseUUID(testUserID))
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID) })

	req := httptest.NewRequest(http.MethodDelete, "/api/comments/"+commentID, nil)
	req = authedRequest(req, testUserID, testWorkspaceID)
	req = withURLParam(req, "commentId", commentID)
	rec := httptest.NewRecorder()

	h.DeleteComment(rec, req)

	if rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if capturedMethod != http.MethodDelete {
		t.Errorf("method = %s", capturedMethod)
	}
	if capturedPath != "/api/v4/projects/42/issues/520/notes/8820" {
		t.Errorf("path = %s", capturedPath)
	}

	var count int
	_ = testPool.QueryRow(ctx, `SELECT COUNT(*) FROM comment WHERE id = $1`, commentID).Scan(&count)
	if count != 0 {
		t.Errorf("cache row not deleted, count = %d", count)
	}
}

func TestDeleteComment_WriteThroughGitLab404IsIdempotent(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, h, 521, 42)
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	commentID := uuid.New().String()
	_, _ = testPool.Exec(ctx,
		`INSERT INTO comment (id, workspace_id, issue_id, author_type, author_id, content, gitlab_note_id, external_updated_at)
		 VALUES ($1, $2, $3, 'member', $4, 'x', 8821, '2026-04-17T12:00:00Z')`,
		commentID, parseUUID(testWorkspaceID), parseUUID(issueID), parseUUID(testUserID))
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID) })

	req := httptest.NewRequest(http.MethodDelete, "/api/comments/"+commentID, nil)
	req = authedRequest(req, testUserID, testWorkspaceID)
	req = withURLParam(req, "commentId", commentID)
	rec := httptest.NewRecorder()

	h.DeleteComment(rec, req)

	// 404 on GitLab is idempotent success — cache row should still be gone.
	if rec.Code >= 400 {
		t.Fatalf("status = %d, want 2xx for idempotent delete", rec.Code)
	}
	var count int
	_ = testPool.QueryRow(ctx, `SELECT COUNT(*) FROM comment WHERE id = $1`, commentID).Scan(&count)
	if count != 0 {
		t.Errorf("cache row still present after idempotent delete, count = %d", count)
	}
}

func TestDeleteComment_WriteThroughGitLab403PreservesCache(t *testing.T) {
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	seedGitlabWriteThroughFixture(t, h)

	issueID := seedGitlabConnectedIssue(t, h, 522, 42)
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	commentID := uuid.New().String()
	_, _ = testPool.Exec(ctx,
		`INSERT INTO comment (id, workspace_id, issue_id, author_type, author_id, content, gitlab_note_id, external_updated_at)
		 VALUES ($1, $2, $3, 'member', $4, 'x', 8822, '2026-04-17T12:00:00Z')`,
		commentID, parseUUID(testWorkspaceID), parseUUID(issueID), parseUUID(testUserID))
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID) })

	req := httptest.NewRequest(http.MethodDelete, "/api/comments/"+commentID, nil)
	req = authedRequest(req, testUserID, testWorkspaceID)
	req = withURLParam(req, "commentId", commentID)
	rec := httptest.NewRecorder()

	h.DeleteComment(rec, req)

	if rec.Code < 400 {
		t.Fatalf("status = %d, want >=400", rec.Code)
	}
	var count int
	_ = testPool.QueryRow(ctx, `SELECT COUNT(*) FROM comment WHERE id = $1`, commentID).Scan(&count)
	if count != 1 {
		t.Errorf("cache row mutated on error, count = %d", count)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd server && ... go test ./internal/handler/ -run TestDeleteComment_WriteThrough -v` — FAIL.

- [ ] **Step 3: Extract `cleanupAndDeleteCommentRow` helper**

Phase 3b did this for `cleanupAndDeleteIssueRow`. Mirror: take the legacy cleanup (collect attachment URLs → DeleteComment sqlc → deleteS3Objects) into:

```go
// cleanupAndDeleteCommentRow performs the Multica-side cleanup for a deleted
// comment: collects attachment URLs, deletes the cache row (cascade removes
// comment_reaction rows), and cleans up S3 objects. Shared between the
// legacy DeleteComment path and the GitLab write-through branch.
func (h *Handler) cleanupAndDeleteCommentRow(ctx context.Context, comment db.Comment) error {
	urls, err := h.Queries.ListAttachmentURLsByCommentID(ctx, comment.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("list attachment URLs: %w", err)
	}
	if err := h.Queries.DeleteComment(ctx, comment.ID); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	if len(urls) > 0 {
		h.deleteS3Objects(ctx, urls)
	}
	return nil
}
```

Update the legacy `DeleteComment` handler to call this helper instead of inlining the cleanup.

- [ ] **Step 4: Add the write-through branch to `DeleteComment`**

Insert at the top after loading `existing` comment + auth check:

```go
	if h.GitlabEnabled && h.GitlabResolver != nil {
		wsConn, wsErr := h.Queries.GetWorkspaceGitlabConnection(r.Context(), existing.WorkspaceID)
		if wsErr == nil {
			issue, issueErr := h.Queries.GetIssue(r.Context(), existing.IssueID)
			if issueErr != nil {
				writeError(w, http.StatusInternalServerError, issueErr.Error())
				return
			}
			if !issue.GitlabIid.Valid || !issue.GitlabProjectID.Valid || !existing.GitlabNoteID.Valid {
				writeError(w, http.StatusBadGateway, "comment or issue missing gitlab identifiers")
				return
			}

			authorType, authorID := h.resolveActor(r, userID, uuidToString(existing.WorkspaceID))
			token, _, tokErr := h.GitlabResolver.ResolveTokenForWrite(r.Context(), uuidToString(existing.WorkspaceID), authorType, authorID)
			if tokErr != nil {
				writeError(w, http.StatusInternalServerError, tokErr.Error())
				return
			}

			if err := h.Gitlab.DeleteNote(r.Context(), token, int(issue.GitlabProjectID.Int64), int(issue.GitlabIid.Int32), existing.GitlabNoteID.Int64); err != nil {
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}

			if err := h.cleanupAndDeleteCommentRow(r.Context(), existing); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}

			h.publish(protocol.EventCommentDeleted, uuidToString(existing.WorkspaceID), authorType, authorID, map[string]any{
				"comment_id": existing.ID.String(),
				"issue_id":   existing.IssueID.String(),
			})
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
```

- [ ] **Step 5: Run tests**

Run: `cd server && ... go test ./internal/handler/ -run TestDeleteComment_WriteThrough -v` — all 3 PASS.

Run full: `go test ./internal/handler/` — only pre-existing flakes fail.

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/comment.go server/internal/handler/comment_test.go
git commit -m "feat(handler): DELETE /api/comments/{id} writes through GitLab when connected"
```

---

## Task 9: Restructure `/api/comments/{commentId}` routing + unmount PUT/DELETE

**Files:** `server/cmd/server/router.go`

Currently:
```go
r.Route("/api/comments/{commentId}", func(r chi.Router) {
    r.Use(middleware.GitlabWritesBlocked(queries))   // group-wide
    r.Put("/", h.UpdateComment)
    r.Delete("/", h.DeleteComment)
    r.Post("/reactions", h.AddReaction)
    r.Delete("/reactions", h.RemoveReaction)
})
```

Needs to become (so PUT/DELETE unmount while reactions stay gated for Phase 3d):
```go
r.Route("/api/comments/{commentId}", func(r chi.Router) {
    gw := middleware.GitlabWritesBlocked(queries)
    r.Put("/", h.UpdateComment)          // unmounted (Phase 3c)
    r.Delete("/", h.DeleteComment)       // unmounted (Phase 3c)
    r.With(gw).Post("/reactions", h.AddReaction)     // still 501 (Phase 3d)
    r.With(gw).Delete("/reactions", h.RemoveReaction) // still 501 (Phase 3d)
})
```

- [ ] **Step 1: Apply the restructure**

Read `server/cmd/server/router.go` (around lines 330–336). Edit to the new shape.

- [ ] **Step 2: Verify**

Run: `cd server && go vet ./cmd/server/ && go build ./cmd/server/` — clean.
Run: `cd server && ... go test ./cmd/server/ ./internal/handler/ ./internal/middleware/ -run 'Comment|GitlabWritesBlocked'` — PASS.

Confirm reactions routes still return 501 on connected workspaces (the middleware test should still fire).

- [ ] **Step 3: Commit**

```bash
git add server/cmd/server/router.go
git commit -m "feat(server): per-route GitLab gate on /api/comments/{id}; unmount PUT/DELETE"
```

---

## Task 10: Unmount 501 from `POST /api/issues/{id}/tasks/{taskId}/cancel`

**Files:** `server/cmd/server/router.go`

Task cancel is a Multica-only concept (agent task queue). No GitLab call needed. Just remove the 501 gate.

- [ ] **Step 1: Remove `gw` wrap**

Find (around line 278):
```go
r.With(gw).Post("/tasks/{taskId}/cancel", h.CancelTask)
```
Change to:
```go
r.Post("/tasks/{taskId}/cancel", h.CancelTask)
```

- [ ] **Step 2: Verify**

Run: `cd server && go vet ./cmd/server/ && go build ./cmd/server/` — clean.
If DB is up: `go test ./cmd/server/ ./internal/handler/ -run 'CancelTask|GitlabWritesBlocked'` — PASS.

- [ ] **Step 3: Commit**

```bash
git add server/cmd/server/router.go
git commit -m "feat(server): unmount 501 stopgap from POST /api/issues/{id}/tasks/{taskId}/cancel"
```

---

## Task 11: Final verification

**Files:** none (verification only).

- [ ] **Step 1: Full Go test suite**

Run: `cd server && DATABASE_URL=... go test ./...`
Expected: all packages PASS except the 2 pre-existing date-bucket flakes.

- [ ] **Step 2: Frontend checks**

Run (repo root): `pnpm typecheck && pnpm test`
Expected: all green. Phase 3c didn't touch frontend, but verify.

- [ ] **Step 3: Confirm routing cleanup**

Run: `grep -n "gw\\|GitlabWritesBlocked" server/cmd/server/router.go`
Expected remaining gated routes (Phase 3d+ scope):
- `POST /api/issues/{id}/subscribe`
- `POST /api/issues/{id}/unsubscribe`
- `POST /api/issues/{id}/reactions`
- `DELETE /api/issues/{id}/reactions`
- `POST /api/comments/{commentId}/reactions`
- `DELETE /api/comments/{commentId}/reactions`

Expected UNMOUNTED (Phase 3c just delivered):
- `POST /api/issues/{id}/comments`
- `PUT /api/comments/{commentId}`
- `DELETE /api/comments/{commentId}`
- `POST /api/issues/{id}/tasks/{taskId}/cancel`

- [ ] **Step 4: Smoke build**

Run: `cd server && go build ./cmd/server/` — clean.

---

## Self-Review Checklist

1. **Spec coverage.** Every Phase 3c in-scope item has a task:
   - CreateNote/UpdateNote/DeleteNote client methods → Tasks 1, 2, 3 ✓
   - BuildCreateNoteBody translator → Task 4 ✓
   - POST comments write-through → Task 5 ✓
   - PUT comment write-through → Task 7 ✓
   - DELETE comment write-through → Task 8 ✓
   - Route unmounts → Tasks 6, 9, 10 ✓
   - Clobber-guard tightening → Task 0 ✓
2. **Placeholder scan.** Task 5 Step 3 notes that `seedAgent` may need to inline the existing agent-seed pattern (grep first, reuse if present). Tasks reference `UpdateCommentParent` and `GetCommentByGitlabNoteID` sqlc queries — Task 5 Step 5 flags both as "add if missing; grep first." Not a placeholder — action is clear.
3. **Type consistency.**
   - `CreateNote(ctx, token, projectID int, issueIID int, body string)` declared in Task 1, used in Task 5 ✓
   - `UpdateNote(ctx, token, projectID int, issueIID int, noteID int64, body string)` declared in Task 2, used in Task 7 ✓
   - `DeleteNote(ctx, token, projectID int, issueIID int, noteID int64)` declared in Task 3, used in Task 8 ✓
   - `BuildCreateNoteBody(authorType, agentSlug, content string) string` declared in Task 4, used in Tasks 5 and 7 ✓
   - `cleanupAndDeleteCommentRow(ctx, comment db.Comment) error` declared in Task 8 Step 3, used in Task 8 Step 4 ✓
4. **Hard rules enforced.**
   - Write-through authoritative (502 on GitLab error): Tasks 5, 7, 8 ✓
   - Agent prefix symmetry: Task 4 + Tasks 5/7 ✓
   - Clobber guard for author preservation: Task 0 ✓
   - Attachments stay local: Task 5 ✓
   - 404 idempotent DELETE: Tasks 3, 8 ✓
   - ResolveTokenForWrite with comment author: Tasks 5, 7, 8 ✓
   - Agent-task enqueue preserved: Task 5 (via `maybeEnqueueCommentAgentTask`) ✓
5. **TDD discipline.** Every behavior has a failing test first ✓.
6. **No frontend touches.** Phase 3c is pure backend ✓.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-17-gitlab-issues-integration-phase-3c.md`.

**Two execution options:**

1. **Subagent-Driven (recommended)** — fresh subagent per task, reviews between tasks, fast iteration. Parallelize where tasks touch different files (Wave plan TBD).

2. **Inline Execution** — batch execution with checkpoints.

**Which approach?**
