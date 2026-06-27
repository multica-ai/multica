# GitLab Self-Hosted Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add GitLab self-hosted support: MR tracking on issues (webhook-driven), OAuth login via GitLab, and HTTPS agent clone with automatic OAuth token injection.

**Architecture:** Parallel GitLab tables alongside the existing GitHub tables — zero changes to working GitHub code. A shared `gitutil.go` in the handler package holds the closing-keyword regex used by both providers. Three backend tasks (webhook, workspace OAuth, user login), one daemon task (clone injection), and three frontend tasks (types+API, MR list, settings tab + login button).

**Tech Stack:** Go 1.26.1 (sqlc, Chi, secretbox), PostgreSQL 17, Next.js App Router, TanStack Query, Zod, Vitest.

## Global Constraints

- Go: `gofmt`, `go vet`, all errors checked. Code comments in English.
- Migration number: `128` (next after `127_task_squad_id`).
- Env vars: `GITLAB_URL`, `GITLAB_APP_ID`, `GITLAB_APP_SECRET`, `GITLAB_WEBHOOK_SECRET`, `GITLAB_SECRET_KEY` (secretbox for access token encryption).
- All GitLab features disabled (routes 404/503) when `GITLAB_URL` is unset.
- State HMAC key for OAuth CSRF: `GITLAB_WEBHOOK_SECRET`.
- TypeScript strict mode; parse API responses through Zod schemas with `parseWithFallback`.
- No new npm/Go dependencies beyond what's already in the repo.
- Spec: `docs/superpowers/specs/2026-06-27-gitlab-integration-design.md`

---

## File Map

**Create:**
- `server/migrations/128_gitlab_integration.up.sql`
- `server/migrations/128_gitlab_integration.down.sql`
- `server/pkg/db/queries/gitlab.sql`
- `server/internal/handler/gitutil.go`
- `server/internal/handler/gitutil_test.go`
- `server/internal/handler/gitlab.go`
- `server/internal/handler/gitlab_test.go`
- `packages/core/types/gitlab.ts`
- `packages/core/gitlab/queries.ts`
- `packages/core/gitlab/settings.ts`
- `packages/core/gitlab/index.ts`
- `packages/views/issues/components/merge-request-list.tsx`
- `packages/views/issues/components/merge-request-list.test.tsx`
- `packages/views/settings/components/gitlab-mark.tsx`
- `packages/views/settings/components/gitlab-tab.tsx`
- `packages/views/settings/components/gitlab-tab.test.tsx`

**Modify:**
- `server/internal/handler/github.go` — remove `closingIdentifierRe`, `identifierRe`, `extractClosingIdentifiers`, `extractIdentifiers` (moved to `gitutil.go`)
- `server/internal/handler/auth.go` — add `GitLabLogin`, `GitLabCallback` (~60 lines)
- `server/internal/handler/config.go` — add `GitLabEnabled bool` to `AppConfig`
- `server/internal/handler/handler.go` — add `GitLabBox *secretbox.Box` to `Handler`
- `server/cmd/server/router.go` — register all GitLab routes, wire `GITLAB_SECRET_KEY` secretbox
- `server/internal/handler/daemon.go` — add `GitLabAccessToken *string` to `daemonWorkspaceReposResponse`, populate it
- `server/internal/daemon/client.go` — add `GitLabAccessToken *string` to `WorkspaceReposResponse`
- `server/internal/daemon/daemon.go` — inject GitLab credentials in `syncWorkspaceRepos`
- `packages/core/types/index.ts` — re-export GitLab types
- `packages/core/api/client.ts` — add `listGitLabConnections`, `deleteGitLabConnection`, `listIssueMergeRequests`
- `packages/views/issues/components/issue-detail.tsx` — render `<MergeRequestList>`
- `packages/views/settings/components/settings-page.tsx` — add GitLab tab
- `packages/views/auth/login-page.tsx` — add `onGitLabLogin` prop
- `apps/web/app/(auth)/login/page.tsx` — wire `onGitLabLogin`
- `packages/views/locales/en/settings.json` — add GitLab strings
- `packages/views/locales/en/issues.json` — add MR strings

---

## Task 1: DB migration + sqlc codegen

**Files:**
- Create: `server/migrations/128_gitlab_integration.up.sql`
- Create: `server/migrations/128_gitlab_integration.down.sql`
- Create: `server/pkg/db/queries/gitlab.sql`
- Generate: `server/pkg/db/generated/gitlab.sql.go` (via `make sqlc`)

**Interfaces:**
- Produces: `db.GitlabConnection`, `db.GitlabMergeRequest`, `db.IssueMergeRequest` Go types; all named query functions used by Tasks 3 and 4.

- [ ] **Step 1: Write the up migration**

```sql
-- server/migrations/128_gitlab_integration.up.sql

CREATE TABLE gitlab_connection (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    namespace        TEXT NOT NULL,
    namespace_type   TEXT NOT NULL CHECK (namespace_type IN ('group', 'user')),
    avatar_url       TEXT,
    access_token     TEXT NOT NULL,
    token_expires_at TIMESTAMPTZ,
    connected_by_id  UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, namespace)
);

CREATE INDEX idx_gitlab_connection_workspace ON gitlab_connection(workspace_id);

CREATE TABLE gitlab_merge_request (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    connection_id     UUID NOT NULL REFERENCES gitlab_connection(id) ON DELETE CASCADE,
    project_path      TEXT NOT NULL,
    mr_iid            INTEGER NOT NULL,
    title             TEXT NOT NULL,
    state             TEXT NOT NULL CHECK (state IN ('open', 'closed', 'merged', 'locked')),
    html_url          TEXT NOT NULL,
    source_branch     TEXT,
    author_username   TEXT,
    author_avatar_url TEXT,
    merged_at         TIMESTAMPTZ,
    closed_at         TIMESTAMPTZ,
    mr_created_at     TIMESTAMPTZ NOT NULL,
    mr_updated_at     TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, project_path, mr_iid)
);

CREATE INDEX idx_gitlab_merge_request_workspace ON gitlab_merge_request(workspace_id);
CREATE INDEX idx_gitlab_merge_request_connection ON gitlab_merge_request(connection_id);

CREATE TABLE issue_merge_request (
    issue_id         UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    merge_request_id UUID NOT NULL REFERENCES gitlab_merge_request(id) ON DELETE CASCADE,
    close_intent     BOOLEAN NOT NULL DEFAULT false,
    linked_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, merge_request_id)
);

CREATE INDEX idx_issue_merge_request_mr ON issue_merge_request(merge_request_id);
```

- [ ] **Step 2: Write the down migration**

```sql
-- server/migrations/128_gitlab_integration.down.sql
DROP TABLE IF EXISTS issue_merge_request;
DROP TABLE IF EXISTS gitlab_merge_request;
DROP TABLE IF EXISTS gitlab_connection;
```

- [ ] **Step 3: Write the sqlc queries**

```sql
-- server/pkg/db/queries/gitlab.sql

-- name: CreateGitLabConnection :one
INSERT INTO gitlab_connection (
    workspace_id, namespace, namespace_type, avatar_url, access_token,
    token_expires_at, connected_by_id
) VALUES (
    $1, $2, $3, sqlc.narg('avatar_url'), $4,
    sqlc.narg('token_expires_at'), sqlc.narg('connected_by_id')
)
ON CONFLICT (workspace_id, namespace) DO UPDATE SET
    namespace_type   = EXCLUDED.namespace_type,
    avatar_url       = EXCLUDED.avatar_url,
    access_token     = EXCLUDED.access_token,
    token_expires_at = EXCLUDED.token_expires_at,
    connected_by_id  = EXCLUDED.connected_by_id,
    updated_at       = now()
RETURNING *;

-- name: ListGitLabConnectionsByWorkspace :many
SELECT * FROM gitlab_connection
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetGitLabConnectionByID :one
SELECT * FROM gitlab_connection WHERE id = $1;

-- name: GetGitLabConnectionByNamespace :one
SELECT * FROM gitlab_connection
WHERE workspace_id = $1 AND namespace = $2;

-- name: GetGitLabConnectionByNamespaceGlobal :one
-- Used by the webhook handler to resolve workspace from project namespace
-- without knowing the workspace ID upfront.
-- ponytail: full-table scan on namespace; add index if connection count grows large.
SELECT * FROM gitlab_connection WHERE namespace = $1 LIMIT 1;

-- name: DeleteGitLabConnection :exec
DELETE FROM gitlab_connection WHERE id = $1 AND workspace_id = $2;

-- name: GetFirstGitLabConnectionByWorkspace :one
SELECT * FROM gitlab_connection WHERE workspace_id = $1 LIMIT 1;

-- name: UpsertGitLabMergeRequest :one
INSERT INTO gitlab_merge_request (
    workspace_id, connection_id, project_path, mr_iid,
    title, state, html_url, source_branch,
    author_username, author_avatar_url,
    merged_at, closed_at, mr_created_at, mr_updated_at
) VALUES (
    $1, $2, $3, $4,
    $5, $6, $7, sqlc.narg('source_branch'),
    sqlc.narg('author_username'), sqlc.narg('author_avatar_url'),
    sqlc.narg('merged_at'), sqlc.narg('closed_at'), $8, $9
)
ON CONFLICT (workspace_id, project_path, mr_iid) DO UPDATE SET
    connection_id    = EXCLUDED.connection_id,
    title            = EXCLUDED.title,
    state            = EXCLUDED.state,
    html_url         = EXCLUDED.html_url,
    source_branch    = EXCLUDED.source_branch,
    author_username  = EXCLUDED.author_username,
    author_avatar_url = EXCLUDED.author_avatar_url,
    merged_at        = EXCLUDED.merged_at,
    closed_at        = EXCLUDED.closed_at,
    mr_updated_at    = EXCLUDED.mr_updated_at,
    updated_at       = now()
RETURNING *;

-- name: GetGitLabMergeRequest :one
SELECT * FROM gitlab_merge_request
WHERE workspace_id = $1 AND project_path = $2 AND mr_iid = $3;

-- name: ListMergeRequestsByIssue :many
SELECT mr.*
FROM gitlab_merge_request mr
JOIN issue_merge_request imr ON imr.merge_request_id = mr.id
WHERE imr.issue_id = $1
ORDER BY mr.mr_created_at DESC;

-- name: LinkIssueToMergeRequest :exec
INSERT INTO issue_merge_request (issue_id, merge_request_id, close_intent)
VALUES ($1, $2, $3)
ON CONFLICT (issue_id, merge_request_id) DO UPDATE SET
    close_intent = EXCLUDED.close_intent;

-- name: ListIssueIDsForMergeRequest :many
SELECT issue_id FROM issue_merge_request WHERE merge_request_id = $1;

-- name: GetIssueMergeRequestCloseAggregate :one
SELECT
    COALESCE(SUM(CASE WHEN mr.state IN ('open') THEN 1 ELSE 0 END), 0)::bigint AS open_count,
    COALESCE(SUM(CASE WHEN mr.state = 'merged' AND imr.close_intent THEN 1 ELSE 0 END), 0)::bigint AS merged_with_close_intent_count
FROM gitlab_merge_request mr
JOIN issue_merge_request imr ON imr.merge_request_id = mr.id
WHERE imr.issue_id = $1;
```

- [ ] **Step 4: Run sqlc**

```bash
cd server && make sqlc
```

Expected: `server/pkg/db/generated/gitlab.sql.go` created with `CreateGitLabConnection`, `UpsertGitLabMergeRequest`, `ListMergeRequestsByIssue`, etc.

- [ ] **Step 5: Commit**

```bash
git add server/migrations/128_gitlab_integration.up.sql \
        server/migrations/128_gitlab_integration.down.sql \
        server/pkg/db/queries/gitlab.sql \
        server/pkg/db/generated/gitlab.sql.go \
        server/pkg/db/generated/db.go \
        server/pkg/db/generated/models.go
git commit -m "feat(gitlab): add DB schema and sqlc queries"
```

---

## Task 2: Extract shared git utilities

**Files:**
- Create: `server/internal/handler/gitutil.go`
- Create: `server/internal/handler/gitutil_test.go`
- Modify: `server/internal/handler/github.go` (remove extracted symbols)

**Interfaces:**
- Produces: `extractClosingIdentifiers(parts ...string) []string`, `extractIdentifiers(parts ...string) []string` — available to both `github.go` and `gitlab.go`

- [ ] **Step 1: Write the failing test**

```go
// server/internal/handler/gitutil_test.go
package handler

import (
	"testing"
)

func TestExtractClosingIdentifiers(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"closes keyword", "Closes MUL-123", []string{"MUL-123"}},
		{"fixes keyword", "Fixes MUL-42", []string{"MUL-42"}},
		{"resolves keyword", "Resolves MUL-7", []string{"MUL-7"}},
		{"case insensitive", "closes mul-1", []string{"MUL-1"}},
		{"no adjacency", "Fix login MUL-1", []string{}},
		{"dedup", "Closes MUL-1\nCloses MUL-1", []string{"MUL-1"}},
		{"multiple", "Closes MUL-1 and Fixes MUL-2", []string{"MUL-1", "MUL-2"}},
		{"colon separator", "Closes: MUL-5", []string{"MUL-5"}},
		{"empty", "", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractClosingIdentifiers(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestExtractIdentifiers(t *testing.T) {
	got := extractIdentifiers("Fix MUL-1 and see MUL-2 or FOO-99")
	want := []string{"MUL-1", "MUL-2", "FOO-99"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd server && go test ./internal/handler/ -run TestExtractClos -v
```

Expected: FAIL — `extractClosingIdentifiers` is in `github.go`, not yet in `gitutil.go` (functions exist but this verifies the test file compiles and targets the right symbols).

Actually since the functions currently exist in `github.go` in the same package, the test will PASS at this point. That's fine — proceed to Step 3 to move the code and confirm it still passes after the move.

- [ ] **Step 3: Create gitutil.go with the moved symbols**

```go
// server/internal/handler/gitutil.go
package handler

import (
	"regexp"
	"strings"
)

// closingIdentifierRe extracts identifiers that appear immediately after a
// closing keyword ("close[sd]?", "fix(e[sd])?", "resolve[sd]?"),
// optionally separated by a colon and whitespace. Matching is intentionally
// strict on adjacency — "Fix MUL-1" closes MUL-1, but "Fix login MUL-1"
// does not.
var closingIdentifierRe = regexp.MustCompile(
	`(?i)\b(?:close[sd]?|fix(?:e[sd])?|resolve[sd]?)[:\s]+([a-z][a-z0-9]{1,9})-(\d+)\b`,
)

// identifierRe extracts identifiers like "MUL-1510" from text. Case-insensitive;
// prefix is 2–10 alphanumeric chars starting with a letter.
var identifierRe = regexp.MustCompile(`(?i)\b([a-z][a-z0-9]{1,9})-(\d+)\b`)

// extractClosingIdentifiers pulls every "PREFIX-NUMBER" identifier that
// appears immediately after a closing keyword in the supplied fields,
// deduplicating in input order.
func extractClosingIdentifiers(parts ...string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, src := range parts {
		for _, m := range closingIdentifierRe.FindAllStringSubmatch(src, -1) {
			ident := strings.ToUpper(m[1]) + "-" + m[2]
			if _, dup := seen[ident]; dup {
				continue
			}
			seen[ident] = struct{}{}
			out = append(out, ident)
		}
	}
	return out
}

// extractIdentifiers pulls every "PREFIX-NUMBER" match across the supplied
// fields, deduplicating in input order.
func extractIdentifiers(parts ...string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, src := range parts {
		for _, m := range identifierRe.FindAllStringSubmatch(src, -1) {
			ident := strings.ToUpper(m[1]) + "-" + m[2]
			if _, dup := seen[ident]; dup {
				continue
			}
			seen[ident] = struct{}{}
			out = append(out, ident)
		}
	}
	return out
}
```

- [ ] **Step 4: Remove the duplicated symbols from github.go**

In `server/internal/handler/github.go`, delete:
- The `closingIdentifierRe` var declaration and its comment block (around lines 586–597)
- The `identifierRe` var declaration and its comment (around lines 579–584)
- The `extractClosingIdentifiers` function and its comment (around lines 1332–1351)
- The `extractIdentifiers` function and its comment (around lines 1314–1330)
- The `"regexp"` import if it's no longer used elsewhere in github.go (check with `go build`)

- [ ] **Step 5: Run tests to verify nothing broke**

```bash
cd server && go test ./internal/handler/ -run "TestExtract" -v
```

Expected: PASS — both `TestExtractClosingIdentifiers` and `TestExtractIdentifiers` pass.

```bash
cd server && go build ./...
```

Expected: no compile errors.

- [ ] **Step 6: Commit**

```bash
git add server/internal/handler/gitutil.go \
        server/internal/handler/gitutil_test.go \
        server/internal/handler/github.go
git commit -m "refactor(handler): extract shared git utilities to gitutil.go"
```

---

## Task 3: GitLab webhook handler + MR mirroring

**Files:**
- Create: `server/internal/handler/gitlab.go`
- Create: `server/internal/handler/gitlab_test.go`
- Modify: `server/cmd/server/router.go` (add webhook + setup routes)
- Modify: `server/internal/handler/handler.go` (add `GitLabBox *secretbox.Box`)

**Interfaces:**
- Consumes: `db.UpsertGitLabMergeRequest`, `db.LinkIssueToMergeRequest`, `db.GetIssueMergeRequestCloseAggregate`, `db.ListIssueIDsForMergeRequest`, `db.ListGitLabConnectionsByWorkspace`, `db.GetGitLabConnectionByNamespace` from Task 1; `extractClosingIdentifiers` from Task 2
- Produces: `h.HandleGitLabWebhook`, `h.GitLabConnect`, `h.GitLabSetupCallback`, `h.ListGitLabConnections`, `h.DeleteGitLabConnection`

- [ ] **Step 1: Write failing webhook verification test**

```go
// server/internal/handler/gitlab_test.go
package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestHandleGitLabWebhook_MissingSecret(t *testing.T) {
	t.Setenv("GITLAB_WEBHOOK_SECRET", "")
	h := &Handler{Queries: &db.Queries{}}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader("{}"))
	req.Header.Set("X-Gitlab-Token", "anything")
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	w := httptest.NewRecorder()
	h.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestHandleGitLabWebhook_WrongToken(t *testing.T) {
	t.Setenv("GITLAB_WEBHOOK_SECRET", "correct-secret")
	h := &Handler{Queries: &db.Queries{}}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader("{}"))
	req.Header.Set("X-Gitlab-Token", "wrong")
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	w := httptest.NewRecorder()
	h.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleGitLabWebhook_UnknownEvent(t *testing.T) {
	t.Setenv("GITLAB_WEBHOOK_SECRET", "s")
	h := &Handler{Queries: &db.Queries{}}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/gitlab", strings.NewReader("{}"))
	req.Header.Set("X-Gitlab-Token", "s")
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	w := httptest.NewRecorder()
	h.HandleGitLabWebhook(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd server && go test ./internal/handler/ -run TestHandleGitLab -v
```

Expected: FAIL (compile error — `HandleGitLabWebhook` not defined yet).

- [ ] **Step 3: Create gitlab.go**

```go
// server/internal/handler/gitlab.go
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ── Config helpers ───────────────────────────────────────────────────────────

func gitlabBaseURL() string { return strings.TrimRight(os.Getenv("GITLAB_URL"), "/") }
func gitlabAPIURL() string  { return gitlabBaseURL() + "/api/v4" }

func isGitLabConfigured() bool {
	return os.Getenv("GITLAB_URL") != "" &&
		os.Getenv("GITLAB_APP_ID") != "" &&
		os.Getenv("GITLAB_APP_SECRET") != ""
}

func gitlabWebhookSecret() string { return strings.TrimSpace(os.Getenv("GITLAB_WEBHOOK_SECRET")) }

// signGitLabState and verifyGitLabState mirror the GitHub state-token pattern,
// using GITLAB_WEBHOOK_SECRET as the HMAC key.
func signGitLabState(workspaceID string) (string, error) {
	secret := gitlabWebhookSecret()
	if secret == "" {
		return "", errors.New("gitlab webhook secret not configured")
	}
	nonceBytes := make([]byte, 12)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(nonceBytes)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(workspaceID))
	mac.Write([]byte("."))
	mac.Write([]byte(nonce))
	sig := hex.EncodeToString(mac.Sum(nil))
	return workspaceID + "." + nonce + "." + sig, nil
}

func verifyGitLabState(token string) (string, bool) {
	secret := gitlabWebhookSecret()
	if secret == "" {
		return "", false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	workspaceID, nonce, sig := parts[0], parts[1], parts[2]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(workspaceID))
	mac.Write([]byte("."))
	mac.Write([]byte(nonce))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return "", false
	}
	return workspaceID, true
}

// Add to imports: "crypto/hmac", "crypto/rand", "crypto/sha256", "encoding/hex"

// ── Response types ───────────────────────────────────────────────────────────

type GitLabConnectionResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	Namespace     string  `json:"namespace"`
	NamespaceType string  `json:"namespace_type"`
	AvatarURL     *string `json:"avatar_url"`
	CreatedAt     string  `json:"created_at"`
}

type GitLabMergeRequestResponse struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspace_id"`
	ProjectPath      string  `json:"project_path"`
	MRIID            int32   `json:"mr_iid"`
	Title            string  `json:"title"`
	State            string  `json:"state"`
	HtmlURL          string  `json:"html_url"`
	SourceBranch     *string `json:"source_branch"`
	AuthorUsername   *string `json:"author_username"`
	AuthorAvatarURL  *string `json:"author_avatar_url"`
	MergedAt         *string `json:"merged_at"`
	ClosedAt         *string `json:"closed_at"`
	MRCreatedAt      string  `json:"mr_created_at"`
	MRUpdatedAt      string  `json:"mr_updated_at"`
}

type ListGitLabConnectionsResponse struct {
	Connections []GitLabConnectionResponse `json:"connections"`
	Configured  bool                       `json:"configured"`
	CanManage   bool                       `json:"can_manage"`
}

func gitlabConnectionToResponse(c db.GitlabConnection) GitLabConnectionResponse {
	return GitLabConnectionResponse{
		ID:            uuidToString(c.ID),
		WorkspaceID:   uuidToString(c.WorkspaceID),
		Namespace:     c.Namespace,
		NamespaceType: c.NamespaceType,
		AvatarURL:     textToPtr(c.AvatarUrl),
		CreatedAt:     timestampToString(c.CreatedAt),
	}
}

func gitlabMRToResponse(mr db.GitlabMergeRequest) GitLabMergeRequestResponse {
	return GitLabMergeRequestResponse{
		ID:              uuidToString(mr.ID),
		WorkspaceID:     uuidToString(mr.WorkspaceID),
		ProjectPath:     mr.ProjectPath,
		MRIID:           mr.MrIid,
		Title:           mr.Title,
		State:           mr.State,
		HtmlURL:         mr.HtmlUrl,
		SourceBranch:    textToPtr(mr.SourceBranch),
		AuthorUsername:  textToPtr(mr.AuthorUsername),
		AuthorAvatarURL: textToPtr(mr.AuthorAvatarUrl),
		MergedAt:        timestampToPtr(mr.MergedAt),
		ClosedAt:        timestampToPtr(mr.ClosedAt),
		MRCreatedAt:     timestampToString(mr.MrCreatedAt),
		MRUpdatedAt:     timestampToString(mr.MrUpdatedAt),
	}
}

// ── Webhook ──────────────────────────────────────────────────────────────────

// HandleGitLabWebhook (POST /api/webhooks/gitlab) verifies X-Gitlab-Token and
// routes Merge Request Hook events to handleGitLabMergeRequestEvent.
func (h *Handler) HandleGitLabWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read body")
		return
	}
	secret := gitlabWebhookSecret()
	if secret == "" {
		writeError(w, http.StatusServiceUnavailable, "gitlab integration not configured")
		return
	}
	if r.Header.Get("X-Gitlab-Token") != secret {
		writeError(w, http.StatusUnauthorized, "invalid webhook token")
		return
	}
	if r.Header.Get("X-Gitlab-Event") != "Merge Request Hook" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.handleGitLabMergeRequestEvent(r.Context(), body)
	w.WriteHeader(http.StatusNoContent)
}

// gitlabMRPayload is the subset of GitLab's merge_request webhook we consume.
type gitlabMRPayload struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		IID          int32   `json:"iid"`
		Title        string  `json:"title"`
		Description  string  `json:"description"`
		State        string  `json:"state"`
		Action       string  `json:"action"`
		URL          string  `json:"url"`
		SourceBranch string  `json:"source_branch"`
		MergedAt     *string `json:"merged_at"`
		UpdatedAt    string  `json:"updated_at"`
		CreatedAt    string  `json:"created_at"`
	} `json:"object_attributes"`
	Project struct {
		PathWithNamespace string `json:"path_with_namespace"`
		Namespace         string `json:"namespace"`
	} `json:"project"`
	User struct {
		Username  string `json:"username"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
}

func (h *Handler) handleGitLabMergeRequestEvent(ctx context.Context, body []byte) {
	var p gitlabMRPayload
	if err := json.Unmarshal(body, &p); err != nil {
		slog.Error("gitlab: failed to parse MR payload", "err", err)
		return
	}

	namespace := p.Project.Namespace
	projectPath := p.Project.PathWithNamespace

	// Resolve workspace via connection namespace.
	conn, err := h.resolveGitLabConnectionByNamespace(ctx, namespace)
	if err != nil {
		slog.Warn("gitlab: no connection for namespace", "namespace", namespace)
		return
	}
	workspaceID := uuidToString(conn.WorkspaceID)

	// Parse timestamps.
	mrCreatedAt, err := time.Parse(time.RFC3339, p.ObjectAttributes.CreatedAt)
	if err != nil {
		mrCreatedAt = time.Now()
	}
	mrUpdatedAt, err := time.Parse(time.RFC3339, p.ObjectAttributes.UpdatedAt)
	if err != nil {
		mrUpdatedAt = time.Now()
	}

	// Normalize state: GitLab sends "opened" for open MRs.
	state := p.ObjectAttributes.State
	if state == "opened" {
		state = "open"
	}

	var mergedAt pgtype.Timestamptz
	if p.ObjectAttributes.MergedAt != nil && *p.ObjectAttributes.MergedAt != "" {
		if t, err := time.Parse(time.RFC3339, *p.ObjectAttributes.MergedAt); err == nil {
			mergedAt = pgtype.Timestamptz{Time: t, Valid: true}
		}
	}

	avatarURL := p.User.AvatarURL

	mr, err := h.Queries.UpsertGitLabMergeRequest(ctx, db.UpsertGitLabMergeRequestParams{
		WorkspaceID:     conn.WorkspaceID,
		ConnectionID:    conn.ID,
		ProjectPath:     projectPath,
		MrIid:           p.ObjectAttributes.IID,
		Title:           p.ObjectAttributes.Title,
		State:           state,
		HtmlUrl:         p.ObjectAttributes.URL,
		SourceBranch:    pgtype.Text{String: p.ObjectAttributes.SourceBranch, Valid: p.ObjectAttributes.SourceBranch != ""},
		AuthorUsername:  pgtype.Text{String: p.User.Username, Valid: p.User.Username != ""},
		AuthorAvatarUrl: pgtype.Text{String: avatarURL, Valid: avatarURL != ""},
		MergedAt:        mergedAt,
		ClosedAt:        pgtype.Timestamptz{}, // set below if closed
		MrCreatedAt:     pgtype.Timestamptz{Time: mrCreatedAt, Valid: true},
		MrUpdatedAt:     pgtype.Timestamptz{Time: mrUpdatedAt, Valid: true},
	})
	if err != nil {
		slog.Error("gitlab: failed to upsert MR", "err", err, "project", projectPath, "iid", p.ObjectAttributes.IID)
		return
	}

	// Extract and link closing identifiers.
	closingIdents := map[string]struct{}{}
	for _, c := range extractClosingIdentifiers(p.ObjectAttributes.Title, p.ObjectAttributes.Description) {
		closingIdents[c] = struct{}{}
	}

	for _, ident := range extractIdentifiers(p.ObjectAttributes.Title, p.ObjectAttributes.Description) {
		prefix, number, ok := splitIdentifier(ident)
		if !ok {
			continue
		}
		issue, found := h.lookupIssueByIdentifier(ctx, conn.WorkspaceID, prefix, number)
		if !found {
			continue
		}
		_, hasCloseIntent := closingIdents[ident]
		if err := h.Queries.LinkIssueToMergeRequest(ctx, db.LinkIssueToMergeRequestParams{
			IssueID:         issue.ID,
			MergeRequestID:  mr.ID,
			CloseIntent:     hasCloseIntent,
		}); err != nil {
			slog.Warn("gitlab: failed to link issue to MR", "issue", issue.ID, "mr", mr.ID, "err", err)
		}
	}

	// Auto-advance issues when MR merges with close intent and no open MRs remain.
	if state == "merged" {
		h.maybeAdvanceIssuesOnGitLabMerge(ctx, mr, workspaceID)
	}

	// Publish realtime event.
	h.publish(protocol.EventGitLabMergeRequestUpdated, workspaceID, "system", "", map[string]any{
		"merge_request": gitlabMRToResponse(mr),
	})
}

func (h *Handler) maybeAdvanceIssuesOnGitLabMerge(ctx context.Context, mr db.GitlabMergeRequest, workspaceID string) {
	issueIDs, err := h.Queries.ListIssueIDsForMergeRequest(ctx, mr.ID)
	if err != nil {
		return
	}
	for _, issueID := range issueIDs {
		agg, err := h.Queries.GetIssueMergeRequestCloseAggregate(ctx, issueID)
		if err != nil {
			continue
		}
		if agg.OpenCount == 0 && agg.MergedWithCloseIntentCount > 0 {
			issue, err := h.Queries.GetIssue(ctx, issueID)
			if err != nil {
				continue
			}
			h.advanceIssueToDone(ctx, issue, workspaceID)
		}
	}
}

// resolveGitLabConnectionByNamespace finds the first workspace connection whose
// namespace matches the project's top-level group/user.
func (h *Handler) resolveGitLabConnectionByNamespace(ctx context.Context, namespace string) (db.GitlabConnection, error) {
	// namespace from GitLab payload is the top-level group; we match against
	// the stored namespace (which is also the top-level group).
	// This is a linear scan over all connections — acceptable for the number of
	// integrations a workspace would have.
	// A workspace-scoped lookup would require knowing the workspace first, which
	// we don't have from the webhook. Instead we search across all connections.
	// ponytail: full-table scan; add index on namespace if connection count grows large.
	rows, err := h.Queries.GetGitLabConnectionByNamespaceGlobal(ctx, namespace)
	if err != nil {
		return db.GitlabConnection{}, err
	}
	return rows, nil
}

// splitIdentifier splits "MUL-123" into ("mul", "123", true).
func splitIdentifier(ident string) (prefix, number string, ok bool) {
	parts := strings.SplitN(ident, "-", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return strings.ToLower(parts[0]), parts[1], true
}

// ── Workspace OAuth ──────────────────────────────────────────────────────────

// GitLabConnect (GET /api/workspaces/{id}/gitlab/connect) begins the OAuth flow.
func (h *Handler) GitLabConnect(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	if _, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id"); !ok {
		return
	}
	if !isGitLabConfigured() {
		writeJSON(w, http.StatusOK, map[string]bool{"configured": false})
		return
	}
	state, err := signGitLabState(workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign state")
		return
	}
	serverURL := strings.TrimRight(os.Getenv("MULTICA_PUBLIC_URL"), "/")
	if serverURL == "" {
		serverURL = strings.TrimRight(os.Getenv("FRONTEND_ORIGIN"), "/")
	}
	if serverURL == "" {
		serverURL = "http://localhost:3000"
	}
	callbackURL := serverURL + "/api/gitlab/setup"
	params := url.Values{
		"client_id":     {os.Getenv("GITLAB_APP_ID")},
		"redirect_uri":  {callbackURL},
		"response_type": {"code"},
		"scope":         {"api"},
		"state":         {state},
	}
	oauthURL := gitlabBaseURL() + "/oauth/authorize?" + params.Encode()
	writeJSON(w, http.StatusOK, map[string]any{"url": oauthURL, "configured": true})
}

// GitLabSetupCallback (GET /api/gitlab/setup) handles the OAuth redirect.
func (h *Handler) GitLabSetupCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := q.Get("code")
	state := q.Get("state")
	frontend := strings.TrimRight(os.Getenv("FRONTEND_ORIGIN"), "/")
	if frontend == "" {
		frontend = "http://localhost:3000"
	}
	settingsURL := frontend + "/settings?tab=gitlab"

	if code == "" || state == "" {
		http.Redirect(w, r, settingsURL+"&gitlab_error=missing_params", http.StatusFound)
		return
	}
	workspaceID, ok := verifyGitLabState(state)
	if !ok {
		http.Redirect(w, r, settingsURL+"&gitlab_error=invalid_state", http.StatusFound)
		return
	}
	wsUUID, err := parseStrictUUID(workspaceID)
	if err != nil {
		http.Redirect(w, r, settingsURL+"&gitlab_error=bad_workspace", http.StatusFound)
		return
	}

	token, expiresAt, err := gitlabExchangeCode(r.Context(), code)
	if err != nil {
		slog.Error("gitlab: token exchange failed", "err", err)
		http.Redirect(w, r, settingsURL+"&gitlab_error=token_exchange_failed", http.StatusFound)
		return
	}

	userInfo, err := gitlabFetchUser(r.Context(), token)
	if err != nil {
		slog.Error("gitlab: fetch user failed", "err", err)
		http.Redirect(w, r, settingsURL+"&gitlab_error=user_fetch_failed", http.StatusFound)
		return
	}

	// Encrypt token before storing.
	if h.GitLabBox == nil {
		http.Redirect(w, r, settingsURL+"&gitlab_error=not_configured", http.StatusFound)
		return
	}
	sealed, err := h.GitLabBox.Seal([]byte(token))
	if err != nil {
		http.Redirect(w, r, settingsURL+"&gitlab_error=encrypt_failed", http.StatusFound)
		return
	}

	connectedBy := pgtype.UUID{}
	if userID := requestUserID(r); userID != "" {
		if u, err := parseStrictUUID(userID); err == nil {
			connectedBy = u
		}
	}

	conn, err := h.Queries.CreateGitLabConnection(r.Context(), db.CreateGitLabConnectionParams{
		WorkspaceID:    wsUUID,
		Namespace:      userInfo.Namespace,
		NamespaceType:  userInfo.NamespaceType,
		AvatarUrl:      pgtype.Text{String: userInfo.AvatarURL, Valid: userInfo.AvatarURL != ""},
		AccessToken:    string(sealed),
		TokenExpiresAt: expiresAt,
		ConnectedByID:  connectedBy,
	})
	if err != nil {
		slog.Error("gitlab: failed to persist connection", "err", err)
		http.Redirect(w, r, settingsURL+"&gitlab_error=persist_failed", http.StatusFound)
		return
	}

	h.publish(protocol.EventGitLabConnectionCreated, workspaceID, "system", "", map[string]any{
		"connection": gitlabConnectionToResponse(conn),
	})
	http.Redirect(w, r, settingsURL+"&gitlab_connected=1", http.StatusFound)
}

// ListGitLabConnections (GET /api/workspaces/{id}/gitlab/connections)
func (h *Handler) ListGitLabConnections(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	conns, err := h.Queries.ListGitLabConnectionsByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list connections")
		return
	}
	resp := make([]GitLabConnectionResponse, len(conns))
	for i, c := range conns {
		resp[i] = gitlabConnectionToResponse(c)
	}
	writeJSON(w, http.StatusOK, ListGitLabConnectionsResponse{
		Connections: resp,
		Configured:  isGitLabConfigured(),
		CanManage:   true, // caller already passed admin middleware
	})
}

// DeleteGitLabConnection (DELETE /api/workspaces/{id}/gitlab/connections/{connectionId})
func (h *Handler) DeleteGitLabConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	connectionID := chi.URLParam(r, "connectionId")
	connUUID, ok := parseUUIDOrBadRequest(w, connectionID, "connection id")
	if !ok {
		return
	}
	if err := h.Queries.DeleteGitLabConnection(r.Context(), db.DeleteGitLabConnectionParams{
		ID:          connUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete connection")
		return
	}
	h.publish(protocol.EventGitLabConnectionDeleted, workspaceID, "system", "", map[string]any{
		"connection_id": connectionID,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ── GitLab API helpers ───────────────────────────────────────────────────────

type gitlabUserInfo struct {
	Namespace     string
	NamespaceType string
	AvatarURL     string
}

func gitlabExchangeCode(ctx context.Context, code string) (token string, expiresAt pgtype.Timestamptz, err error) {
	serverURL := strings.TrimRight(os.Getenv("MULTICA_PUBLIC_URL"), "/")
	if serverURL == "" {
		serverURL = strings.TrimRight(os.Getenv("FRONTEND_ORIGIN"), "/")
	}
	callbackURL := serverURL + "/api/gitlab/setup"

	resp, err := http.PostForm(gitlabBaseURL()+"/oauth/token", url.Values{
		"client_id":     {os.Getenv("GITLAB_APP_ID")},
		"client_secret": {os.Getenv("GITLAB_APP_SECRET")},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {callbackURL},
	})
	if err != nil {
		return "", pgtype.Timestamptz{}, err
	}
	defer resp.Body.Close()

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", pgtype.Timestamptz{}, err
	}
	if body.AccessToken == "" {
		return "", pgtype.Timestamptz{}, errors.New("empty access_token in response")
	}
	exp := pgtype.Timestamptz{}
	if body.ExpiresIn > 0 {
		exp = pgtype.Timestamptz{Time: time.Now().Add(time.Duration(body.ExpiresIn) * time.Second), Valid: true}
	}
	return body.AccessToken, exp, nil
}

func gitlabFetchUser(ctx context.Context, token string) (gitlabUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gitlabAPIURL()+"/user", nil)
	if err != nil {
		return gitlabUserInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return gitlabUserInfo{}, err
	}
	defer resp.Body.Close()

	var body struct {
		Username  string `json:"username"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return gitlabUserInfo{}, err
	}
	return gitlabUserInfo{
		Namespace:     body.Username,
		NamespaceType: "user",
		AvatarURL:     body.AvatarURL,
	}, nil
}

// ListMergeRequestsForIssue (GET /api/issues/{issueId}/merge-requests)
func (h *Handler) ListMergeRequestsForIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "issueId")
	issueUUID, ok := parseUUIDOrBadRequest(w, issueID, "issue id")
	if !ok {
		return
	}
	mrs, err := h.Queries.ListMergeRequestsByIssue(r.Context(), issueUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list merge requests")
		return
	}
	resp := make([]GitLabMergeRequestResponse, len(mrs))
	for i, mr := range mrs {
		resp[i] = gitlabMRToResponse(mr)
	}
	writeJSON(w, http.StatusOK, map[string]any{"merge_requests": resp})
}
```

**Important:** The `resolveGitLabConnectionByNamespace` function needs `GetGitLabConnectionByNamespaceGlobal` — add this query to `gitlab.sql`:

```sql
-- name: GetGitLabConnectionByNamespaceGlobal :one
SELECT * FROM gitlab_connection WHERE namespace = $1 LIMIT 1;
```

Then re-run `make sqlc`.

Also add `signGitLabState` and `verifyGitLabState` in `gitlab.go` — copy `signState`/`verifyState` from `github.go` but use `gitlabWebhookSecret()` instead of `githubWebhookSecret()`.

Also add three protocol event constants in `server/pkg/protocol/events.go` after the GitHub constants (line ~124):

```go
EventGitLabConnectionCreated  = "gitlab_connection:created"
EventGitLabConnectionDeleted  = "gitlab_connection:deleted"
EventGitLabMergeRequestUpdated = "gitlab_merge_request:updated"
```

- [ ] **Step 4: Add GitLabBox to Handler struct**

In `server/internal/handler/handler.go`, inside `Handler` struct, add after `LarkAPIClient`:

```go
// GitLabBox encrypts/decrypts OAuth access tokens stored in gitlab_connection.
// Nil when GITLAB_SECRET_KEY is not configured; the GitLab OAuth handlers
// return an error in that case.
GitLabBox *secretbox.Box
```

Add import `"github.com/multica-ai/multica/server/internal/util/secretbox"` to `handler.go` imports.

- [ ] **Step 5: Register routes in router.go**

In `server/cmd/server/router.go`, after the Lark secretbox block (around line 424), add:

```go
// GitLab integration token encryption. Nil when GITLAB_SECRET_KEY is unset;
// the GitLab OAuth handlers return a clear error in that case.
if gitlabKey, err := secretbox.LoadKey("GITLAB_SECRET_KEY"); err == nil {
    box, err := secretbox.New(gitlabKey)
    if err != nil {
        slog.Error("gitlab: secretbox.New failed; GitLab OAuth disabled", "error", err)
    } else {
        h.GitLabBox = box
        slog.Info("gitlab integration enabled")
    }
}
```

In the public routes block (near line 563), add:

```go
r.Post("/api/webhooks/gitlab", h.HandleGitLabWebhook)
r.Get("/api/gitlab/setup", h.GitLabSetupCallback)
```

In the workspace admin group (near line 690), add alongside the GitHub routes:

```go
// GitLab integration — connect/disconnect are admin-only; list is member-visible.
r.Get("/gitlab/connections", h.ListGitLabConnections)  // add to member group
// in admin group:
r.Get("/gitlab/connect", h.GitLabConnect)
r.Delete("/gitlab/connections/{connectionId}", h.DeleteGitLabConnection)
```

For issue MR list (in issue routes), add:

```go
r.Get("/api/issues/{issueId}/merge-requests", h.ListMergeRequestsForIssue)
```

- [ ] **Step 6: Run webhook tests**

```bash
cd server && go test ./internal/handler/ -run TestHandleGitLab -v
```

Expected: all three tests PASS.

```bash
cd server && go build ./...
```

Expected: no compile errors.

- [ ] **Step 7: Commit**

```bash
git add server/internal/handler/gitlab.go \
        server/internal/handler/gitlab_test.go \
        server/internal/handler/handler.go \
        server/pkg/db/queries/gitlab.sql \
        server/pkg/db/generated/ \
        server/cmd/server/router.go \
        server/pkg/protocol/
git commit -m "feat(gitlab): webhook handler, workspace OAuth, MR mirroring"
```

---

## Task 4: GitLab user login

**Files:**
- Modify: `server/internal/handler/auth.go` (add `GitLabLogin`, `GitLabCallback`)
- Modify: `server/internal/handler/config.go` (add `GitLabEnabled` to `AppConfig`)
- Modify: `server/cmd/server/router.go` (add login routes)

**Interfaces:**
- Consumes: `gitlabBaseURL()`, `gitlabAPIURL()`, `gitlabExchangeCode()`, `gitlabFetchUser()` from Task 3
- Produces: `GET /auth/gitlab` → redirect, `GET /auth/gitlab/callback` → session + redirect

- [ ] **Step 1: Write failing test**

```go
// In server/internal/handler/gitlab_test.go, add:

func TestGitLabLogin_NotConfigured(t *testing.T) {
	t.Setenv("GITLAB_URL", "")
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/auth/gitlab", nil)
	w := httptest.NewRecorder()
	h.GitLabLogin(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd server && go test ./internal/handler/ -run TestGitLabLogin -v
```

Expected: FAIL (compile error).

- [ ] **Step 3: Add GitLabLogin and GitLabCallback to auth.go**

At the end of `server/internal/handler/auth.go`, add:

```go
// GitLabLogin (GET /auth/gitlab) begins the user login OAuth flow with scope read_user.
func (h *Handler) GitLabLogin(w http.ResponseWriter, r *http.Request) {
	if !isGitLabConfigured() {
		writeError(w, http.StatusServiceUnavailable, "gitlab integration not configured")
		return
	}
	// Re-use the workspace state signing but with an empty workspace ID
	// to produce a CSRF token. The callback recovers it with verifyGitLabState.
	state, err := signGitLabState("login")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign state")
		return
	}
	serverURL := strings.TrimRight(os.Getenv("MULTICA_PUBLIC_URL"), "/")
	if serverURL == "" {
		serverURL = strings.TrimRight(os.Getenv("FRONTEND_ORIGIN"), "/")
	}
	if serverURL == "" {
		serverURL = "http://localhost:3000"
	}
	callbackURL := serverURL + "/auth/gitlab/callback"
	params := url.Values{
		"client_id":     {os.Getenv("GITLAB_APP_ID")},
		"redirect_uri":  {callbackURL},
		"response_type": {"code"},
		"scope":         {"read_user"},
		"state":         {state},
	}
	http.Redirect(w, r, gitlabBaseURL()+"/oauth/authorize?"+params.Encode(), http.StatusFound)
}

// GitLabCallback (GET /auth/gitlab/callback) exchanges the code, finds or creates
// the user by email, and issues a Multica session cookie.
func (h *Handler) GitLabCallback(w http.ResponseWriter, r *http.Request) {
	frontend := strings.TrimRight(os.Getenv("FRONTEND_ORIGIN"), "/")
	if frontend == "" {
		frontend = "http://localhost:3000"
	}
	failURL := frontend + "/login?error=gitlab_auth_failed"

	q := r.URL.Query()
	code := q.Get("code")
	state := q.Get("state")

	if code == "" || state == "" {
		http.Redirect(w, r, failURL, http.StatusFound)
		return
	}
	if _, ok := verifyGitLabState(state); !ok {
		http.Redirect(w, r, failURL, http.StatusFound)
		return
	}

	// Exchange code for access token (read_user scope only — we don't store this token).
	serverURL := strings.TrimRight(os.Getenv("MULTICA_PUBLIC_URL"), "/")
	if serverURL == "" {
		serverURL = frontend
	}
	callbackURL := serverURL + "/auth/gitlab/callback"
	params := url.Values{
		"client_id":     {os.Getenv("GITLAB_APP_ID")},
		"client_secret": {os.Getenv("GITLAB_APP_SECRET")},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {callbackURL},
	}
	resp, err := http.PostForm(gitlabBaseURL()+"/oauth/token", params)
	if err != nil || resp.StatusCode != http.StatusOK {
		slog.Warn("gitlab: token exchange failed on login callback", "err", err)
		http.Redirect(w, r, failURL, http.StatusFound)
		return
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil || tok.AccessToken == "" {
		http.Redirect(w, r, failURL, http.StatusFound)
		return
	}

	// Fetch GitLab user info (email + name).
	req2, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, gitlabAPIURL()+"/user", nil)
	req2.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	userResp, err := http.DefaultClient.Do(req2)
	if err != nil {
		http.Redirect(w, r, failURL, http.StatusFound)
		return
	}
	defer userResp.Body.Close()
	var glUser struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(userResp.Body).Decode(&glUser); err != nil || glUser.Email == "" {
		http.Redirect(w, r, failURL, http.StatusFound)
		return
	}

	// Find or create the Multica user by email (same pattern as GoogleLogin).
	user, err := h.findOrCreateUserByEmail(r.Context(), glUser.Email, glUser.Name)
	if err != nil {
		slog.Warn("gitlab: find-or-create user failed", "email", glUser.Email, "err", err)
		http.Redirect(w, r, failURL, http.StatusFound)
		return
	}

	token, err := h.issueSessionToken(r.Context(), user.ID)
	if err != nil {
		http.Redirect(w, r, failURL, http.StatusFound)
		return
	}

	setSessionCookie(w, token)
	http.Redirect(w, r, frontend, http.StatusFound)
}
```

Note: `findOrCreateUserByEmail`, `issueSessionToken`, and `setSessionCookie` are private helpers already in `auth.go`. Check their exact names with `rg "func.*findOrCreate\|func.*issueSession\|func.*setSession" server/internal/handler/auth.go` and use the actual names.

- [ ] **Step 4: Add GitLabEnabled to AppConfig in config.go**

In `server/internal/handler/config.go`, add to `AppConfig` struct:

```go
// GitLabEnabled is true when GITLAB_URL + GITLAB_APP_ID + GITLAB_APP_SECRET are set.
// Controls whether the frontend shows the "Sign in with GitLab" button and
// the GitLab settings tab.
GitLabEnabled bool `json:"gitlab_enabled,omitempty"`
```

In `GetConfig`, before `writeJSON`:

```go
config.GitLabEnabled = isGitLabConfigured()
```

- [ ] **Step 5: Register routes in router.go**

Near the Google auth route (line ~550):

```go
r.With(authRL).Get("/auth/gitlab", h.GitLabLogin)
r.Get("/auth/gitlab/callback", h.GitLabCallback)
```

- [ ] **Step 6: Run tests**

```bash
cd server && go test ./internal/handler/ -run TestGitLabLogin -v
```

Expected: PASS.

```bash
cd server && go build ./...
```

Expected: no compile errors.

- [ ] **Step 7: Commit**

```bash
git add server/internal/handler/auth.go \
        server/internal/handler/config.go \
        server/internal/handler/gitlab_test.go \
        server/cmd/server/router.go
git commit -m "feat(gitlab): user OAuth login + gitlab_enabled config flag"
```

---

## Task 5: Daemon HTTPS clone token injection

**Files:**
- Modify: `server/internal/handler/daemon.go` — add `GitLabAccessToken *string` to `daemonWorkspaceReposResponse`
- Modify: `server/internal/daemon/client.go` — add `GitLabAccessToken *string` to `WorkspaceReposResponse`
- Modify: `server/internal/daemon/daemon.go` — inject credentials in `syncWorkspaceRepos`

**Interfaces:**
- Consumes: `db.GetFirstGitLabConnectionByWorkspace` from Task 1; `h.GitLabBox` from Task 3
- Produces: daemon injects `oauth2:{token}@` into HTTPS clone URLs matching `GITLAB_URL`

- [ ] **Step 1: Write the failing test**

```go
// server/internal/daemon/daemon_gitlab_test.go
package daemon

import (
	"testing"
)

func TestInjectGitLabCreds(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		token  string
		want   string
	}{
		{
			name:   "injects creds",
			rawURL: "https://gitlab.company.com/group/repo.git",
			token:  "tok123",
			want:   "https://oauth2:tok123@gitlab.company.com/group/repo.git",
		},
		{
			name:   "empty token is no-op",
			rawURL: "https://gitlab.company.com/group/repo.git",
			token:  "",
			want:   "https://gitlab.company.com/group/repo.git",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := injectGitLabCreds(tc.rawURL, tc.token)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd server && go test ./internal/daemon/ -run TestInjectGitLab -v
```

Expected: FAIL (compile error — `injectGitLabCreds` not yet defined).

- [ ] **Step 3: Add injectGitLabCreds to daemon.go**

Near the top of `server/internal/daemon/daemon.go`, add:

```go
// injectGitLabCreds rewrites an HTTPS URL to include OAuth2 credentials.
// Returns rawURL unchanged when token is empty.
func injectGitLabCreds(rawURL, token string) string {
	if token == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return rawURL
	}
	u.User = url.UserPassword("oauth2", token)
	return u.String()
}
```

Add `"net/url"` to imports if not already present.

- [ ] **Step 4: Add GitLabAccessToken to daemonWorkspaceReposResponse (server-side)**

In `server/internal/handler/daemon.go`, find `daemonWorkspaceReposResponse` struct and add:

```go
GitLabAccessToken *string `json:"gitlab_access_token,omitempty"`
```

In `workspaceReposResponse` function (or `GetDaemonWorkspaceRepos` handler), populate it:

```go
// After building the base response, populate GitLab token if configured.
if h.GitLabBox != nil {
    conn, err := h.Queries.GetFirstGitLabConnectionByWorkspace(ctx, wsUUID)
    if err == nil {
        plain, err := h.GitLabBox.Open([]byte(conn.AccessToken))
        if err == nil {
            tok := string(plain)
            resp.GitLabAccessToken = &tok
        }
    }
}
```

Note: `workspaceReposResponse` is currently a pure function. You may need to pass the token in or convert it to a method. The cleanest approach: in `GetDaemonWorkspaceRepos`, build the response, then populate `GitLabAccessToken` after calling `workspaceReposResponse`.

- [ ] **Step 5: Add GitLabAccessToken to client-side WorkspaceReposResponse**

In `server/internal/daemon/client.go`, add to `WorkspaceReposResponse`:

```go
GitLabAccessToken *string `json:"gitlab_access_token,omitempty"`
```

- [ ] **Step 6: Apply injection in syncWorkspaceRepos**

In `server/internal/daemon/daemon.go`, find `syncWorkspaceRepos`. It currently calls `repocache.Cache.Sync`. Before calling Sync, rewrite URLs when a GitLab token is available:

```go
func (d *Daemon) syncWorkspaceRepos(workspaceID string, repos []RepoData) {
	// ... existing code that gets gitlabURL and gitlabToken from workspace state ...
	// Add: for each repo, if URL matches GITLAB_URL scheme+host and token exists, inject creds
	ws := d.getWorkspaceState(workspaceID)
	gitlabToken := ""
	gitlabURL := os.Getenv("GITLAB_URL")
	if ws != nil && ws.gitLabAccessToken != "" {
		gitlabToken = ws.gitLabAccessToken
	}

	injected := make([]RepoData, len(repos))
	for i, r := range repos {
		injected[i] = r
		if gitlabToken != "" && gitlabURL != "" && strings.HasPrefix(r.URL, strings.TrimRight(gitlabURL, "/")+"/") {
			injected[i].URL = injectGitLabCreds(r.URL, gitlabToken)
		}
	}
	// call repocache.Sync with injected instead of repos
}
```

Also add `gitLabAccessToken string` to `workspaceState` struct and populate it from `WorkspaceReposResponse.GitLabAccessToken` when it's received (in `refreshWorkspaceRepos` and `newWorkspaceState`).

- [ ] **Step 7: Run tests**

```bash
cd server && go test ./internal/daemon/ -run TestInjectGitLab -v
```

Expected: PASS.

```bash
cd server && go build ./...
```

Expected: no compile errors.

- [ ] **Step 8: Commit**

```bash
git add server/internal/handler/daemon.go \
        server/internal/daemon/client.go \
        server/internal/daemon/daemon.go \
        server/internal/daemon/daemon_gitlab_test.go
git commit -m "feat(gitlab): daemon HTTPS clone token injection"
```

---

## Task 6: Frontend types + API client

**Files:**
- Create: `packages/core/types/gitlab.ts`
- Modify: `packages/core/types/index.ts`
- Create: `packages/core/gitlab/queries.ts`
- Create: `packages/core/gitlab/settings.ts`
- Create: `packages/core/gitlab/index.ts`
- Modify: `packages/core/api/client.ts`

**Interfaces:**
- Produces: `GitLabConnection`, `GitLabMergeRequest`, `GitLabMRState`; `gitlabConnectionsOptions`, `issueMergeRequestsOptions`; `api.listGitLabConnections`, `api.deleteGitLabConnection`, `api.listIssueMergeRequests`

- [ ] **Step 1: Create types/gitlab.ts**

```typescript
// packages/core/types/gitlab.ts

export type GitLabMRState = "open" | "closed" | "merged" | "locked";

export interface GitLabConnection {
  id: string;
  workspace_id: string;
  namespace: string;
  namespace_type: "group" | "user";
  avatar_url: string | null;
  created_at: string;
}

export interface GitLabMergeRequest {
  id: string;
  workspace_id: string;
  project_path: string;
  mr_iid: number;
  title: string;
  state: GitLabMRState;
  html_url: string;
  source_branch: string | null;
  author_username: string | null;
  author_avatar_url: string | null;
  merged_at: string | null;
  closed_at: string | null;
  mr_created_at: string;
  mr_updated_at: string;
}

export interface ListGitLabConnectionsResponse {
  connections: GitLabConnection[];
  configured: boolean;
  can_manage?: boolean;
}
```

- [ ] **Step 2: Export from types/index.ts**

Add to `packages/core/types/index.ts`:

```typescript
export type {
  GitLabMRState,
  GitLabConnection,
  GitLabMergeRequest,
  ListGitLabConnectionsResponse,
} from "./gitlab";
```

- [ ] **Step 3: Add API methods to client.ts**

In `packages/core/api/client.ts`, add alongside the GitHub methods:

```typescript
async listGitLabConnections(workspaceId: string): Promise<ListGitLabConnectionsResponse> {
  return this.fetch(`/api/workspaces/${workspaceId}/gitlab/connections`);
}

async deleteGitLabConnection(workspaceId: string, connectionId: string): Promise<void> {
  await this.fetch(`/api/workspaces/${workspaceId}/gitlab/connections/${connectionId}`, {
    method: "DELETE",
  });
}

async listIssueMergeRequests(issueId: string): Promise<{ merge_requests: GitLabMergeRequest[] }> {
  return this.fetch(`/api/issues/${issueId}/merge-requests`);
}
```

Add the import for `ListGitLabConnectionsResponse` and `GitLabMergeRequest` from `"../types"`.

- [ ] **Step 4: Create gitlab/queries.ts**

```typescript
// packages/core/gitlab/queries.ts
import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const gitlabKeys = {
  all: (wsId: string) => ["gitlab", wsId] as const,
  connections: (wsId: string) => [...gitlabKeys.all(wsId), "connections"] as const,
  mergeRequests: (issueId: string) => ["gitlab", "merge-requests", issueId] as const,
};

export const gitlabConnectionsOptions = (wsId: string) =>
  queryOptions({
    queryKey: gitlabKeys.connections(wsId),
    queryFn: () => api.listGitLabConnections(wsId),
    enabled: !!wsId,
  });

export const issueMergeRequestsOptions = (issueId: string) =>
  queryOptions({
    queryKey: gitlabKeys.mergeRequests(issueId),
    queryFn: () => api.listIssueMergeRequests(issueId),
    enabled: !!issueId,
  });
```

- [ ] **Step 5: Create gitlab/settings.ts**

```typescript
// packages/core/gitlab/settings.ts
import { useQueryClient, useMutation } from "@tanstack/react-query";
import { api } from "../api";
import { gitlabKeys } from "./queries";

export function useDeleteGitLabConnection(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (connectionId: string) => api.deleteGitLabConnection(wsId, connectionId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: gitlabKeys.connections(wsId) });
    },
  });
}
```

- [ ] **Step 6: Create gitlab/index.ts**

```typescript
// packages/core/gitlab/index.ts
export { gitlabKeys, gitlabConnectionsOptions, issueMergeRequestsOptions } from "./queries";
export { useDeleteGitLabConnection } from "./settings";
```

- [ ] **Step 7: Run typecheck**

```bash
pnpm typecheck
```

Expected: no errors in `packages/core/`.

- [ ] **Step 8: Commit**

```bash
git add packages/core/types/gitlab.ts \
        packages/core/types/index.ts \
        packages/core/gitlab/ \
        packages/core/api/client.ts
git commit -m "feat(gitlab): core types, query hooks, API client methods"
```

---

## Task 7: MR list component

**Files:**
- Create: `packages/views/issues/components/merge-request-list.tsx`
- Create: `packages/views/issues/components/merge-request-list.test.tsx`
- Modify: `packages/views/issues/components/issue-detail.tsx`
- Modify: `packages/views/locales/en/issues.json`

**Interfaces:**
- Consumes: `issueMergeRequestsOptions` from Task 6; `GitLabMergeRequest`, `GitLabMRState` from Task 6
- Produces: `<MergeRequestList issueId={string} />` component

- [ ] **Step 1: Add i18n strings**

In `packages/views/locales/en/issues.json`, add a `merge_requests` section (find the existing structure and follow the same pattern):

```json
"merge_requests": {
  "title": "Merge Requests",
  "show_more": "Show {{count}} more",
  "show_less": "Show less",
  "state": {
    "open": "Open",
    "merged": "Merged",
    "closed": "Closed",
    "locked": "Locked"
  }
}
```

- [ ] **Step 2: Write the failing test**

```tsx
// packages/views/issues/components/merge-request-list.test.tsx
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MergeRequestList } from "./merge-request-list";

vi.mock("@multica/core/gitlab", () => ({
  issueMergeRequestsOptions: (issueId: string) => ({
    queryKey: ["gitlab", "merge-requests", issueId],
    queryFn: async () => ({ merge_requests: [] }),
    enabled: !!issueId,
  }),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe("MergeRequestList", () => {
  it("renders without crashing when empty", () => {
    render(<MergeRequestList issueId="abc" />, { wrapper });
    // No MR rows — nothing to assert beyond no error
  });
});
```

- [ ] **Step 3: Run to verify it fails**

```bash
pnpm test packages/views/issues/components/merge-request-list.test.tsx
```

Expected: FAIL (module not found).

- [ ] **Step 4: Create merge-request-list.tsx**

```tsx
// packages/views/issues/components/merge-request-list.tsx
"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  GitMerge,
  GitPullRequest,
  GitPullRequestClosed,
  Lock,
} from "lucide-react";
import { issueMergeRequestsOptions } from "@multica/core/gitlab";
import type { GitLabMergeRequest, GitLabMRState } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

const MR_LIMIT_BEFORE_COLLAPSE = 4;

const STATE_ICON: Record<
  GitLabMRState,
  { icon: React.ComponentType<{ className?: string }>; className: string }
> = {
  open:   { icon: GitPullRequest,       className: "text-emerald-600 dark:text-emerald-400" },
  merged: { icon: GitMerge,             className: "text-violet-600 dark:text-violet-400" },
  closed: { icon: GitPullRequestClosed, className: "text-rose-600 dark:text-rose-400" },
  locked: { icon: Lock,                 className: "text-muted-foreground" },
};

function MergeRequestRow({ mr }: { mr: GitLabMergeRequest }) {
  const StateIcon = STATE_ICON[mr.state] ?? STATE_ICON.open;
  return (
    <a
      href={mr.html_url}
      target="_blank"
      rel="noopener noreferrer"
      className="flex items-start gap-2 rounded-md p-1.5 text-sm hover:bg-muted/50 transition-colors"
    >
      <StateIcon.icon className={cn("mt-0.5 h-4 w-4 shrink-0", StateIcon.className)} />
      <span className="min-w-0 flex-1 truncate text-foreground">{mr.title}</span>
    </a>
  );
}

export function MergeRequestList({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const [expanded, setExpanded] = useState(false);
  const { data, isLoading } = useQuery(issueMergeRequestsOptions(issueId));
  const mrs = data?.merge_requests ?? [];

  if (isLoading || mrs.length === 0) return null;

  const visible = expanded ? mrs : mrs.slice(0, MR_LIMIT_BEFORE_COLLAPSE);
  const hiddenCount = mrs.length - MR_LIMIT_BEFORE_COLLAPSE;

  return (
    <div className="flex flex-col gap-1">
      <p className="px-1.5 text-xs font-medium text-muted-foreground">
        {t(($) => $.merge_requests.title)}
      </p>
      {visible.map((mr) => (
        <MergeRequestRow key={mr.id} mr={mr} />
      ))}
      {mrs.length >= MR_LIMIT_BEFORE_COLLAPSE && (
        <button
          type="button"
          className="px-1.5 text-left text-xs text-muted-foreground hover:text-foreground"
          onClick={() => setExpanded((v) => !v)}
        >
          {expanded
            ? t(($) => $.merge_requests.show_less)
            : t(($) => $.merge_requests.show_more, { count: hiddenCount })}
        </button>
      )}
    </div>
  );
}
```

- [ ] **Step 5: Add MergeRequestList to issue-detail.tsx**

In `packages/views/issues/components/issue-detail.tsx`, import and render `<MergeRequestList>` directly alongside `<PullRequestList>`. Find where `<PullRequestList issueId={issue.id} />` is rendered and add:

```tsx
import { MergeRequestList } from "./merge-request-list";
// ...
<PullRequestList issueId={issue.id} />
<MergeRequestList issueId={issue.id} />
```

- [ ] **Step 6: Run tests**

```bash
pnpm test packages/views/issues/components/merge-request-list.test.tsx
```

Expected: PASS.

```bash
pnpm typecheck
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add packages/views/issues/components/merge-request-list.tsx \
        packages/views/issues/components/merge-request-list.test.tsx \
        packages/views/issues/components/issue-detail.tsx \
        packages/views/locales/en/issues.json
git commit -m "feat(gitlab): MR list component in issue detail sidebar"
```

---

## Task 8: GitLab settings tab + login button

**Files:**
- Create: `packages/views/settings/components/gitlab-mark.tsx`
- Create: `packages/views/settings/components/gitlab-tab.tsx`
- Create: `packages/views/settings/components/gitlab-tab.test.tsx`
- Modify: `packages/views/settings/components/settings-page.tsx`
- Modify: `packages/views/auth/login-page.tsx`
- Modify: `apps/web/app/(auth)/login/page.tsx`
- Modify: `packages/views/locales/en/settings.json`

**Interfaces:**
- Consumes: `gitlabConnectionsOptions`, `useDeleteGitLabConnection` from Task 6; `GitLabConnection`, `ListGitLabConnectionsResponse` from Task 6
- Produces: `<GitLabTab />`, `<GitLabMark />`, `onGitLabLogin` prop on `LoginPage`

- [ ] **Step 1: Add i18n strings to settings.json**

In `packages/views/locales/en/settings.json`, find the structure and add a `gitlab` key:

```json
"gitlab": {
  "title": "GitLab",
  "description": "Connect your GitLab namespace to track merge requests on issues.",
  "connect": "Connect GitLab",
  "connected_as": "Connected as {{namespace}}",
  "disconnect": "Disconnect",
  "disconnect_confirm_title": "Disconnect GitLab?",
  "disconnect_confirm_description": "Existing MR links will remain but new events will not be processed.",
  "disconnect_confirm_action": "Disconnect",
  "not_configured": "GitLab integration is not configured on this server."
}
```

- [ ] **Step 2: Write failing test**

```tsx
// packages/views/settings/components/gitlab-tab.test.tsx
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { GitLabTab } from "./gitlab-tab";

vi.mock("@multica/core/gitlab", () => ({
  gitlabConnectionsOptions: () => ({
    queryKey: ["gitlab", "ws1", "connections"],
    queryFn: async () => ({ connections: [], configured: false, can_manage: false }),
    enabled: true,
  }),
  useDeleteGitLabConnection: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws1" }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: (fn: any) => fn({ user: { id: "u1" } }) }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: async () => [] }),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe("GitLabTab", () => {
  it("renders without crashing", () => {
    render(<GitLabTab />, { wrapper });
  });
});
```

- [ ] **Step 3: Run to verify it fails**

```bash
pnpm test packages/views/settings/components/gitlab-tab.test.tsx
```

Expected: FAIL (module not found).

- [ ] **Step 4: Create gitlab-mark.tsx**

```tsx
// packages/views/settings/components/gitlab-mark.tsx
export function GitLabMark({ className }: { className?: string }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 380 380"
      className={className}
      aria-hidden="true"
    >
      {/* GitLab fox logo — simplified version */}
      <path
        d="M282.83 170.73l-.27-.69-26.14-68.22a6.81 6.81 0 00-2.69-3.24 7 7 0 00-8 .43 7 7 0 00-2.32 3.52l-17.65 54H154.29l-17.65-54a6.86 6.86 0 00-2.32-3.52 7 7 0 00-8-.43 6.85 6.85 0 00-2.69 3.24L97.44 170l-.26.69a48.54 48.54 0 0016.1 56.1l.09.07.24.17 39.82 29.82 19.7 14.91 12 9.06a8.07 8.07 0 009.76 0l12-9.06 19.7-14.91 40.06-30 .1-.08a48.56 48.56 0 0016.08-56.04z"
        fill="#E24329"
      />
      <path
        d="M282.83 170.73l-.27-.69a88.3 88.3 0 00-35.15 15.8L190 229.25c19.55 14.79 36.57 27.64 36.57 27.64l40.06-30 .1-.08a48.56 48.56 0 0016.1-56.08z"
        fill="#FC6D26"
      />
      <path
        d="M153.43 256.89l19.7 14.91 12 9.06a8.07 8.07 0 009.76 0l12-9.06 19.7-14.91S209.55 244 190 229.25c-19.55 14.79-36.57 27.64-36.57 27.64z"
        fill="#FCA326"
      />
      <path
        d="M132.58 185.84A88.19 88.19 0 0097.44 170l-.26.69a48.54 48.54 0 0016.1 56.1l.09.07.24.17 39.82 29.82S170.45 244 190 229.25c-19.55-14.8-57.42-43.41-57.42-43.41z"
        fill="#FC6D26"
      />
    </svg>
  );
}
```

- [ ] **Step 5: Create gitlab-tab.tsx**

```tsx
// packages/views/settings/components/gitlab-tab.tsx
"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { gitlabConnectionsOptions, useDeleteGitLabConnection } from "@multica/core/gitlab";
import { api } from "@multica/core/api";
import { useT } from "../../i18n";
import { GitLabMark } from "./gitlab-mark";

export function GitLabTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const [connecting, setConnecting] = useState(false);
  const [disconnectTarget, setDisconnectTarget] = useState<string | null>(null);

  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = members.find((m) => m.user_id === user?.id) ?? null;
  const canView = !!currentMember;

  const { data: connectionData } = useQuery({
    ...gitlabConnectionsOptions(wsId),
    enabled: !!wsId && canView,
  });
  const connections = connectionData?.connections ?? [];
  const configured = connectionData?.configured ?? false;
  const canManage = connectionData?.can_manage === true;

  const deleteMutation = useDeleteGitLabConnection(wsId);

  async function handleConnect() {
    setConnecting(true);
    try {
      const res = await api.fetch<{ url: string; configured: boolean }>(
        `/api/workspaces/${wsId}/gitlab/connect`,
      );
      if (res.url) {
        window.location.href = res.url;
      }
    } catch {
      toast.error("Failed to start GitLab connection");
    } finally {
      setConnecting(false);
    }
  }

  async function handleDisconnect(connectionId: string) {
    try {
      await deleteMutation.mutateAsync(connectionId);
      toast.success("GitLab disconnected");
    } catch {
      toast.error("Failed to disconnect GitLab");
    } finally {
      setDisconnectTarget(null);
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-start gap-3">
        <GitLabMark className="h-8 w-8 shrink-0" />
        <div>
          <h3 className="font-medium">{t(($) => $.gitlab.title)}</h3>
          <p className="text-sm text-muted-foreground">{t(($) => $.gitlab.description)}</p>
        </div>
      </div>

      {!configured && (
        <p className="text-sm text-muted-foreground">{t(($) => $.gitlab.not_configured)}</p>
      )}

      {configured && connections.length === 0 && canManage && (
        <Button onClick={handleConnect} disabled={connecting} variant="outline">
          {t(($) => $.gitlab.connect)}
        </Button>
      )}

      {connections.map((conn) => (
        <div key={conn.id} className="flex items-center justify-between rounded-md border p-3">
          <div className="flex items-center gap-2">
            {conn.avatar_url && (
              <img src={conn.avatar_url} alt="" className="h-6 w-6 rounded-full" />
            )}
            <span className="text-sm">
              {t(($) => $.gitlab.connected_as, { namespace: conn.namespace })}
            </span>
          </div>
          {canManage && (
            <Button
              variant="ghost"
              size="sm"
              className="text-destructive hover:text-destructive"
              onClick={() => setDisconnectTarget(conn.id)}
            >
              {t(($) => $.gitlab.disconnect)}
            </Button>
          )}
        </div>
      ))}

      <AlertDialog open={!!disconnectTarget} onOpenChange={() => setDisconnectTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.gitlab.disconnect_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.gitlab.disconnect_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.common?.cancel ?? "Cancel")}</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive hover:bg-destructive/90"
              onClick={() => disconnectTarget && handleDisconnect(disconnectTarget)}
            >
              {t(($) => $.gitlab.disconnect_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
```

- [ ] **Step 6: Add GitLab tab to settings-page.tsx**

In `packages/views/settings/components/settings-page.tsx`, find where the GitHub tab is added and add GitLab alongside it. The exact code depends on how tabs are structured — follow the same pattern as the GitHub tab entry:

```tsx
import { GitLabTab } from "./gitlab-tab";
// Add "gitlab" to the tab list and render <GitLabTab /> for that tab value.
// Gate visibility on connectionData?.configured or a prop passed from the app shell.
```

- [ ] **Step 7: Add onGitLabLogin prop to login-page.tsx**

In `packages/views/auth/login-page.tsx`:

1. Add to `LoginPageProps` interface:
```tsx
/** Override GitLab login handler. When provided, renders the GitLab button. */
onGitLabLogin?: () => void;
```

2. Add to the component's destructured props:
```tsx
export function LoginPage({
  // ... existing props ...
  onGitLabLogin,
}: LoginPageProps) {
```

3. In the email step's `<CardFooter>`, after the Google button block, add:
```tsx
{onGitLabLogin && (
  <>
    {!google && !onGoogleLogin && (
      <div className="relative w-full">
        <div className="absolute inset-0 flex items-center">
          <span className="w-full border-t" />
        </div>
        <div className="relative flex justify-center text-xs uppercase">
          <span className="bg-card px-2 text-muted-foreground">
            {t(($) => $.signin.divider)}
          </span>
        </div>
      </div>
    )}
    <Button
      type="button"
      variant="outline"
      className="w-full"
      size="lg"
      onClick={onGitLabLogin}
      disabled={loading}
    >
      <GitLabMark className="mr-2 h-4 w-4" />
      {t(($) => $.signin.gitlab)}
    </Button>
  </>
)}
```

4. Add `gitlab: "Sign in with GitLab"` to `packages/views/locales/en/auth.json` (or wherever `signin.google` lives).

- [ ] **Step 8: Wire onGitLabLogin in the web login page**

In `apps/web/app/(auth)/login/page.tsx`, find where `google` prop is built from `useConfigStore`. Add:

```tsx
const gitLabEnabled = useConfigStore((state) => state.gitLabEnabled); // add this field to config store
// ...
<LoginPage
  // ...existing props...
  onGitLabLogin={gitLabEnabled ? () => { window.location.href = "/auth/gitlab"; } : undefined}
/>
```

You also need to add `gitLabEnabled` to the config store. Find where the config store is defined (likely in `packages/core/` or `apps/web/`) and add it, populated from `AppConfig.gitlab_enabled`.

- [ ] **Step 9: Run tests and typecheck**

```bash
pnpm test packages/views/settings/components/gitlab-tab.test.tsx
pnpm typecheck
```

Expected: all PASS.

- [ ] **Step 10: Commit**

```bash
git add packages/views/settings/components/gitlab-mark.tsx \
        packages/views/settings/components/gitlab-tab.tsx \
        packages/views/settings/components/gitlab-tab.test.tsx \
        packages/views/settings/components/settings-page.tsx \
        packages/views/auth/login-page.tsx \
        apps/web/app/\(auth\)/login/page.tsx \
        packages/views/locales/en/settings.json \
        packages/views/locales/en/auth.json
git commit -m "feat(gitlab): settings tab, login button, feature gating"
```

---

## Self-Review Checklist (run before marking done)

- [ ] All five env vars documented: `GITLAB_URL`, `GITLAB_APP_ID`, `GITLAB_APP_SECRET`, `GITLAB_WEBHOOK_SECRET`, `GITLAB_SECRET_KEY`
- [ ] `make sqlc` was run after every SQL change; generated files committed
- [ ] `go build ./...` passes with no errors
- [ ] `pnpm typecheck` passes with no errors
- [ ] `make test` (Go) passes
- [ ] `pnpm test` (TS/Vitest) passes
- [ ] GitLab features return 503/disabled when `GITLAB_URL` is unset (manually verify with `curl`)
