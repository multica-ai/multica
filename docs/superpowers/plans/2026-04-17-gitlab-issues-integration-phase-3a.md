# GitLab Issues Integration — Phase 3a: Per-user PATs + first write path — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Land the per-user PAT plumbing (so a Multica user's writes can post to GitLab as themselves), build the `ResolveTokenForWrite` selector that picks the right token per request, and refactor the **first** write endpoint (`POST /api/issues`) end-to-end through GitLab. After 3a ships, a connected workspace's members can create new issues from Multica that round-trip to GitLab and back into the cache. All other write endpoints continue to return 501 — Phase 3b extends the same pattern to them.

**Architecture:**
- The `user_gitlab_connection` table from Phase 1's migration 049 already has the right shape — Phase 3a just adds sqlc queries + the handler endpoints + the frontend UX. No schema changes.
- A new `gitlab.Resolver` (in `server/internal/gitlab/`) holds the queries + a `TokenDecrypter` callback (same pattern Phase 2b's reconciler uses). Its only public method, `ResolveTokenForWrite(ctx, workspaceID, actorType, actorID)`, returns the plaintext token + a `source` string ("user" or "service") so handler call sites can attribute the cache row to the right Multica identity.
- Token-selection rules:
  - **Human user with their PAT registered** → user PAT, source "user".
  - **Human user without** → workspace service PAT, source "service".
  - **Agent (any)** → workspace service PAT, source "service" (agents identify themselves via the `agent::<slug>` label, not via GitLab user identity).
- The first write-through path: refactor `POST /api/issues`. Translate the Multica request → GitLab `CreateIssue` body (status/priority/agent assignment expressed as scoped labels), call `gitlab.Client.CreateIssue`, run the response through Phase 2a's `TranslateIssue` + `UpsertIssueFromGitlab`. The cache update fires the existing `issue:created` WS event so the UI updates without waiting for the webhook echo.
- The 501 stopgap stays in place for every other write endpoint. `POST /api/issues` is the only route exempted in 3a — done by moving that one route out of the middleware-wrapped group.

**Tech Stack:**
- Go 1.26, Chi router, `pgx/v5`, `sqlc` (existing).
- TypeScript, React, TanStack Query, Vitest, Testing Library (existing).
- Reuses Phase 2a's translator (`TranslateIssue`, `IssueValues`) and Phase 1's `Cipher.Decrypt`.

**Design spec:** `docs/superpowers/specs/2026-04-17-gitlab-issues-integration-design.md` (sections 5b "PAT registration", 5c "Token selection", and the first write-path bullet of section "API surface")
**Phase 1 plan:** `docs/superpowers/plans/2026-04-17-gitlab-issues-integration-phase-1.md`
**Phase 2a plan:** `docs/superpowers/plans/2026-04-17-gitlab-issues-integration-phase-2a.md`
**Phase 2b plan:** `docs/superpowers/plans/2026-04-17-gitlab-issues-integration-phase-2b.md`

**Out of scope for 3a (Phase 3b will add):**
- All other write endpoints: `PUT/DELETE /api/issues/{id}`, batch-update, batch-delete, all comment writes, all reaction writes, subscribe, attachments. They keep returning 501.
- Backfilling NULL `creator_id`/`author_id`/`actor_id` columns on existing cached rows (Phase 3b runs the backfill once write-through is wired everywhere).
- Removing the `comment_type` CHECK constraint (synced agent notes still use the agent prefix; that comes off in Phase 4).

---

## File Structure

### New files (backend)

| Path | Responsibility |
|---|---|
| `server/pkg/db/queries/user_gitlab_connection.sql` | sqlc queries for `user_gitlab_connection` (table created in Phase 1, queries deferred). |
| `server/internal/gitlab/resolver.go` | `Resolver` struct + `ResolveTokenForWrite` method. |
| `server/internal/gitlab/resolver_test.go` | Unit tests with a stub query interface. |
| `server/internal/handler/gitlab_user_connection.go` | POST/GET/DELETE `/api/me/gitlab/connect` handlers. |
| `server/internal/handler/gitlab_user_connection_test.go` | Handler tests against fake gitlab + real DB. |
| `packages/core/gitlab/user-queries.ts` | TanStack hook `useUserGitlabConnection(wsId)`. |
| `packages/core/gitlab/user-mutations.ts` | `useConnectUserGitlabMutation`, `useDisconnectUserGitlabMutation`. |
| `packages/core/gitlab/user-types.ts` | `UserGitlabConnection`, `ConnectUserGitlabInput` interfaces. |
| `packages/core/gitlab/user-mutations.test.tsx` | Vitest coverage. |
| `packages/views/workspace/banners/gitlab-pat-banner.tsx` | Banner component shown when the workspace has GitLab connected but the current user hasn't registered a PAT. |
| `packages/views/workspace/banners/gitlab-pat-banner.test.tsx` | Tests. |

### Modified files

| Path | Change |
|---|---|
| `server/pkg/gitlab/issues.go` | Add `CreateIssue(ctx, token, projectID, CreateIssueInput) (*Issue, error)`. |
| `server/pkg/gitlab/issues_test.go` | Add tests for the new method. |
| `server/internal/gitlab/translator.go` | Add `BuildCreateIssueInput(req CreateIssueRequest, agents map[string]string) gitlabapi.CreateIssueInput` (Multica → GitLab direction). |
| `server/internal/gitlab/translator_test.go` | Add tests for the forward translation. |
| `server/internal/handler/handler.go` | `Handler` struct gains `GitlabResolver *gitlab.Resolver`; setter `SetGitlabResolver`. |
| `server/internal/handler/issue.go` | Refactor `CreateIssue` to call resolver → GitLab → cache; only when workspace has gitlab connection. |
| `server/internal/handler/issue_test.go` | Add tests for the new write-through path. |
| `server/cmd/server/router.go` | Move `POST /api/issues` out of the middleware-wrapped group (the route stays at the same URL — only its middleware stack changes). Mount the new `/api/me/gitlab/connect` endpoints. |
| `server/cmd/server/main.go` | Construct the `gitlab.Resolver` and call `h.SetGitlabResolver(...)`. |
| `packages/core/api/client.ts` | Add three methods: `getUserGitlabConnection`, `connectUserGitlab`, `disconnectUserGitlab`. |
| `packages/core/gitlab/index.ts` | Re-export the new user-side hooks. |
| `packages/views/settings/components/gitlab-tab.tsx` | Add a "Your personal GitLab connection" section below the workspace section so members can manage their own PATs without an admin role. |
| `packages/views/workspace/layout/...` | Render `<GitlabPatBanner />` once per dashboard page so the banner is visible everywhere. (Identify the existing layout file in Step 12.1.) |

---

## Task 1: sqlc queries for `user_gitlab_connection`

**Files:**
- Create: `server/pkg/db/queries/user_gitlab_connection.sql`
- Regenerate: `server/pkg/db/generated/`

The table was created in Phase 1's migration 049 but has no sqlc queries yet. Phase 3a is the first time we need to read/write it.

- [ ] **Step 1.1: Create the query file**

```sql
-- name: UpsertUserGitlabConnection :one
-- A user can re-register a PAT to refresh it. ON CONFLICT updates the
-- existing row in place (one PAT per (user, workspace)).
INSERT INTO user_gitlab_connection (
    user_id, workspace_id, gitlab_user_id, gitlab_username, pat_encrypted
)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, workspace_id) DO UPDATE SET
    gitlab_user_id  = EXCLUDED.gitlab_user_id,
    gitlab_username = EXCLUDED.gitlab_username,
    pat_encrypted   = EXCLUDED.pat_encrypted
RETURNING *;

-- name: GetUserGitlabConnection :one
SELECT * FROM user_gitlab_connection
WHERE user_id = $1 AND workspace_id = $2;

-- name: DeleteUserGitlabConnection :exec
DELETE FROM user_gitlab_connection
WHERE user_id = $1 AND workspace_id = $2;
```

- [ ] **Step 1.2: Regenerate**

```bash
cd /Users/jimmy.mills/Developer/multica-gitlab-phase-3a && make sqlc
```
Expected: clean.

- [ ] **Step 1.3: Verify build**

```bash
cd server && go build ./...
```
Expected: clean.

- [ ] **Step 1.4: Inspect generated structs**

```bash
grep -A 6 "type UpsertUserGitlabConnectionParams struct" server/pkg/db/generated/user_gitlab_connection.sql.go
```

Expected fields: `UserID pgtype.UUID`, `WorkspaceID pgtype.UUID`, `GitlabUserID int64`, `GitlabUsername string`, `PatEncrypted []byte`.

```bash
grep -A 5 "type GetUserGitlabConnectionParams struct" server/pkg/db/generated/user_gitlab_connection.sql.go
```

Expected: `UserID pgtype.UUID`, `WorkspaceID pgtype.UUID`. (Same shape for `DeleteUserGitlabConnectionParams`.)

- [ ] **Step 1.5: Commit**

```bash
cd /Users/jimmy.mills/Developer/multica-gitlab-phase-3a && \
  git add server/pkg/db/queries/user_gitlab_connection.sql server/pkg/db/generated/ && \
  git commit -m "feat(db): sqlc queries for user_gitlab_connection (registered in phase 1)"
```

---

## Task 2: Token resolver

**Files:**
- Create: `server/internal/gitlab/resolver.go`
- Create: `server/internal/gitlab/resolver_test.go`

The `Resolver` is the single source of truth for "given a request, what's the right GitLab token?" Three rules:
- Human user with their own PAT registered → user PAT, source `"user"`.
- Human user without their PAT → workspace service PAT, source `"service"`.
- Agent → workspace service PAT, source `"service"` (agents don't have GitLab identities).

Construction takes a `TokenDecrypter` (same callback shape Phase 2b's reconciler uses) so tests can stub the cipher.

- [ ] **Step 2.1: Write failing tests**

Create `server/internal/gitlab/resolver_test.go`:

```go
package gitlab

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeResolverQueries lets us simulate "found" / "not found" for both
// connection types without wiring a real DB.
type fakeResolverQueries struct {
	workspaceConn *db.WorkspaceGitlabConnection
	userConn      *db.UserGitlabConnection
}

func (f *fakeResolverQueries) GetWorkspaceGitlabConnection(_ context.Context, _ pgtype.UUID) (db.WorkspaceGitlabConnection, error) {
	if f.workspaceConn == nil {
		return db.WorkspaceGitlabConnection{}, pgx.ErrNoRows
	}
	return *f.workspaceConn, nil
}

func (f *fakeResolverQueries) GetUserGitlabConnection(_ context.Context, _ db.GetUserGitlabConnectionParams) (db.UserGitlabConnection, error) {
	if f.userConn == nil {
		return db.UserGitlabConnection{}, pgx.ErrNoRows
	}
	return *f.userConn, nil
}

// stubDecrypt returns the plaintext "{prefix}|{hex}" so tests can assert
// which encrypted column we resolved against.
func stubDecrypt(prefix string) TokenDecrypter {
	return func(_ context.Context, encrypted []byte) (string, error) {
		return prefix + "|" + string(encrypted), nil
	}
}

func TestResolveTokenForWrite_HumanWithPATPicksUserPAT(t *testing.T) {
	q := &fakeResolverQueries{
		workspaceConn: &db.WorkspaceGitlabConnection{
			ServiceTokenEncrypted: []byte("svc"),
		},
		userConn: &db.UserGitlabConnection{
			PatEncrypted: []byte("usr"),
			GitlabUserID: 100,
		},
	}
	r := NewResolver(q, stubDecrypt("dec"))
	tok, src, err := r.ResolveTokenForWrite(context.Background(), "ws-1", "member", "user-1")
	if err != nil {
		t.Fatalf("ResolveTokenForWrite: %v", err)
	}
	if src != "user" {
		t.Errorf("source = %q, want user", src)
	}
	if tok != "dec|usr" {
		t.Errorf("token = %q, want dec|usr (the decrypted user PAT)", tok)
	}
}

func TestResolveTokenForWrite_HumanWithoutPATFallsBackToServicePAT(t *testing.T) {
	q := &fakeResolverQueries{
		workspaceConn: &db.WorkspaceGitlabConnection{
			ServiceTokenEncrypted: []byte("svc"),
		},
		userConn: nil, // user hasn't connected
	}
	r := NewResolver(q, stubDecrypt("dec"))
	tok, src, err := r.ResolveTokenForWrite(context.Background(), "ws-1", "member", "user-1")
	if err != nil {
		t.Fatalf("ResolveTokenForWrite: %v", err)
	}
	if src != "service" {
		t.Errorf("source = %q, want service", src)
	}
	if tok != "dec|svc" {
		t.Errorf("token = %q, want dec|svc", tok)
	}
}

func TestResolveTokenForWrite_AgentAlwaysUsesServicePAT(t *testing.T) {
	// Even if a "user_gitlab_connection" row somehow exists for the agent UUID,
	// the resolver MUST ignore it and pick the service PAT. Agents don't have
	// GitLab identities.
	q := &fakeResolverQueries{
		workspaceConn: &db.WorkspaceGitlabConnection{ServiceTokenEncrypted: []byte("svc")},
		userConn: &db.UserGitlabConnection{
			PatEncrypted: []byte("usr"),
			GitlabUserID: 100,
		},
	}
	r := NewResolver(q, stubDecrypt("dec"))
	tok, src, err := r.ResolveTokenForWrite(context.Background(), "ws-1", "agent", "agent-1")
	if err != nil {
		t.Fatalf("ResolveTokenForWrite: %v", err)
	}
	if src != "service" {
		t.Errorf("source = %q, want service", src)
	}
	if tok != "dec|svc" {
		t.Errorf("token = %q, want dec|svc (service PAT)", tok)
	}
}

func TestResolveTokenForWrite_NoWorkspaceConnection(t *testing.T) {
	q := &fakeResolverQueries{} // both nil
	r := NewResolver(q, stubDecrypt("dec"))
	_, _, err := r.ResolveTokenForWrite(context.Background(), "ws-1", "member", "user-1")
	if err == nil {
		t.Fatalf("expected error when workspace has no connection")
	}
}
```

- [ ] **Step 2.2: Run — expect compile error**

```bash
cd /Users/jimmy.mills/Developer/multica-gitlab-phase-3a/server && \
  DATABASE_URL="postgres://multica:multica@localhost:5432/multica_multica_gitlab_phase_3a_NNN?sslmode=disable" \
  go test ./internal/gitlab/ -run TestResolveTokenForWrite -v
```

(Substitute the real worktree DB URL.)

- [ ] **Step 2.3: Implement**

Create `server/internal/gitlab/resolver.go`:

```go
package gitlab

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// resolverQueries is the narrow surface of *db.Queries the resolver needs.
// Defined as an interface so tests can stub without a DB.
type resolverQueries interface {
	GetWorkspaceGitlabConnection(ctx context.Context, workspaceID pgtype.UUID) (db.WorkspaceGitlabConnection, error)
	GetUserGitlabConnection(ctx context.Context, arg db.GetUserGitlabConnectionParams) (db.UserGitlabConnection, error)
}

// Resolver picks the right GitLab token for a write request.
//
// Construction takes a TokenDecrypter so tests can stub it; production wires
// in the secrets.Cipher's Decrypt method.
type Resolver struct {
	queries resolverQueries
	decrypt TokenDecrypter
}

// NewResolver constructs a Resolver. queries can be *db.Queries (production)
// or any stub implementing the resolverQueries interface (tests).
func NewResolver(queries resolverQueries, decrypt TokenDecrypter) *Resolver {
	return &Resolver{queries: queries, decrypt: decrypt}
}

// ResolveTokenForWrite returns the plaintext token to use for a GitLab API
// write call, plus a "source" string ("user" or "service") so the caller
// can attribute the cache row correctly.
//
// Rules (per spec section 5c):
//   - actorType="member", user PAT registered → user PAT, "user"
//   - actorType="member", no PAT             → workspace service PAT, "service"
//   - actorType="agent"                      → workspace service PAT, "service"
//
// Returns an error when the workspace itself has no GitLab connection
// (writes shouldn't have been routed here in that case).
func (r *Resolver) ResolveTokenForWrite(ctx context.Context, workspaceID, actorType, actorID string) (token string, source string, err error) {
	wsUUID, err := pgUUID(workspaceID)
	if err != nil {
		return "", "", fmt.Errorf("resolver: workspace_id: %w", err)
	}
	wsConn, err := r.queries.GetWorkspaceGitlabConnection(ctx, wsUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", fmt.Errorf("resolver: workspace has no gitlab connection")
		}
		return "", "", fmt.Errorf("resolver: workspace lookup: %w", err)
	}

	if actorType == "member" {
		userUUID, err := pgUUID(actorID)
		if err == nil {
			userConn, err := r.queries.GetUserGitlabConnection(ctx, db.GetUserGitlabConnectionParams{
				UserID:      userUUID,
				WorkspaceID: wsUUID,
			})
			if err == nil {
				token, err := r.decrypt(ctx, userConn.PatEncrypted)
				if err != nil {
					return "", "", fmt.Errorf("resolver: decrypt user pat: %w", err)
				}
				return token, "user", nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return "", "", fmt.Errorf("resolver: user lookup: %w", err)
			}
			// no user PAT → fall through to service PAT
		}
	}

	token, err = r.decrypt(ctx, wsConn.ServiceTokenEncrypted)
	if err != nil {
		return "", "", fmt.Errorf("resolver: decrypt service pat: %w", err)
	}
	return token, "service", nil
}
```

`pgUUID` and `TokenDecrypter` are already defined in `server/internal/gitlab/initial_sync.go` and `server/internal/gitlab/reconciler.go` respectively — reuse them.

- [ ] **Step 2.4: Run — expect pass**

```bash
cd server && DATABASE_URL="…" go test ./internal/gitlab/ -run TestResolveTokenForWrite -v
```
Expected: 4/4 tests pass.

- [ ] **Step 2.5: Commit**

```bash
git add server/internal/gitlab/resolver.go server/internal/gitlab/resolver_test.go
git commit -m "feat(gitlab): ResolveTokenForWrite picks user PAT or service PAT per actor"
```

---

## Task 3: Per-user PAT handlers (POST/GET/DELETE `/api/me/gitlab/connect`)

**Files:**
- Create: `server/internal/handler/gitlab_user_connection.go`
- Create: `server/internal/handler/gitlab_user_connection_test.go`

The endpoint is workspace-scoped via the `X-Workspace-ID` header (which the existing auth/workspace middleware already extracts). The handler:
1. Resolves the actor (must be `member` — agents can't have personal PATs).
2. Calls `gitlab.Client.CurrentUser(token)` to validate + capture `gitlab_user_id` + `gitlab_username`.
3. Encrypts via `Handler.Secrets`.
4. Upserts `user_gitlab_connection`.
5. Returns sanitized status (never the token).

- [ ] **Step 3.1: Write failing tests**

Create `server/internal/handler/gitlab_user_connection_test.go`:

```go
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConnectUserGitlab_Success(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/user" {
			t.Errorf("path = %s, want /api/v4/user", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 999, "username": "alice", "name": "Alice"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteUserGitlabConnection(context.Background(), parseUUID(testUserID), parseUUID(testWorkspaceID))

	body, _ := json.Marshal(map[string]string{"token": "glpat-user-abc"})
	req := httptest.NewRequest(http.MethodPost, "/api/me/gitlab/connect", bytes.NewReader(body))
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.ConnectUserGitlab(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	json.Unmarshal(rr.Body.Bytes(), &got)
	if got["gitlab_username"] != "alice" {
		t.Errorf("gitlab_username = %v", got["gitlab_username"])
	}
	if _, hasTok := got["pat_encrypted"]; hasTok {
		t.Errorf("response leaks pat_encrypted: %+v", got)
	}
}

func TestConnectUserGitlab_BadToken(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"401 Unauthorized"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)

	body, _ := json.Marshal(map[string]string{"token": "bad"})
	req := httptest.NewRequest(http.MethodPost, "/api/me/gitlab/connect", bytes.NewReader(body))
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.ConnectUserGitlab(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestGetUserGitlabConnection_Connected(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 999, "username": "alice"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteUserGitlabConnection(context.Background(), parseUUID(testUserID), parseUUID(testWorkspaceID))

	// Seed via the connect handler.
	body, _ := json.Marshal(map[string]string{"token": "glpat-x"})
	connReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	connReq.Header.Set("X-User-ID", testUserID)
	connReq.Header.Set("X-Workspace-ID", testWorkspaceID)
	h.ConnectUserGitlab(httptest.NewRecorder(), connReq)

	req := httptest.NewRequest(http.MethodGet, "/api/me/gitlab/connect", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()
	h.GetUserGitlabConnection(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var got map[string]any
	json.Unmarshal(rr.Body.Bytes(), &got)
	if got["connected"] != true || got["gitlab_username"] != "alice" {
		t.Errorf("got %+v", got)
	}
}

func TestGetUserGitlabConnection_NotConnected(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	h.Queries.DeleteUserGitlabConnection(context.Background(), parseUUID(testUserID), parseUUID(testWorkspaceID))

	req := httptest.NewRequest(http.MethodGet, "/api/me/gitlab/connect", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()
	h.GetUserGitlabConnection(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var got map[string]any
	json.Unmarshal(rr.Body.Bytes(), &got)
	if got["connected"] != false {
		t.Errorf("connected = %v, want false", got["connected"])
	}
}

func TestDisconnectUserGitlab_Success(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 999, "username": "alice"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	body, _ := json.Marshal(map[string]string{"token": "glpat-x"})
	connReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	connReq.Header.Set("X-User-ID", testUserID)
	connReq.Header.Set("X-Workspace-ID", testWorkspaceID)
	h.ConnectUserGitlab(httptest.NewRecorder(), connReq)

	delReq := httptest.NewRequest(http.MethodDelete, "/", nil)
	delReq.Header.Set("X-User-ID", testUserID)
	delReq.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()
	h.DisconnectUserGitlab(rr, delReq)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}

	// GET should now show disconnected.
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getReq.Header.Set("X-User-ID", testUserID)
	getReq.Header.Set("X-Workspace-ID", testWorkspaceID)
	getRr := httptest.NewRecorder()
	h.GetUserGitlabConnection(getRr, getReq)
	var got map[string]any
	json.Unmarshal(getRr.Body.Bytes(), &got)
	if got["connected"] != false {
		t.Errorf("connected = %v after delete, want false", got["connected"])
	}
}
```

- [ ] **Step 3.2: Run — expect compile error**

```bash
cd server && DATABASE_URL="…" go test ./internal/handler/ -run "TestConnectUserGitlab|TestGetUserGitlabConnection|TestDisconnectUserGitlab" -v
```

- [ ] **Step 3.3: Implement**

Create `server/internal/handler/gitlab_user_connection.go`:

```go
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/gitlab"
)

type connectUserGitlabRequest struct {
	Token string `json:"token"`
}

type userGitlabConnectionResponse struct {
	Connected      bool   `json:"connected"`
	GitlabUserID   int64  `json:"gitlab_user_id,omitempty"`
	GitlabUsername string `json:"gitlab_username,omitempty"`
}

// ConnectUserGitlab registers a user's personal PAT for the current workspace.
// Validates by calling /user, captures GitLab user identity, encrypts the
// PAT, and upserts user_gitlab_connection.
func (h *Handler) ConnectUserGitlab(w http.ResponseWriter, r *http.Request) {
	if !h.GitlabEnabled {
		writeError(w, http.StatusNotFound, "gitlab integration disabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}

	var req connectUserGitlabRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	user, err := h.Gitlab.CurrentUser(r.Context(), req.Token)
	if err != nil {
		if errors.Is(err, gitlab.ErrUnauthorized) {
			writeError(w, http.StatusUnauthorized, "gitlab token is invalid")
			return
		}
		slog.Error("gitlab CurrentUser failed", "error", err)
		writeError(w, http.StatusBadGateway, "gitlab /user call failed")
		return
	}

	encrypted, err := h.Secrets.Encrypt([]byte(req.Token))
	if err != nil {
		slog.Error("encrypt user pat", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to encrypt token")
		return
	}

	row, err := h.Queries.UpsertUserGitlabConnection(r.Context(), db.UpsertUserGitlabConnectionParams{
		UserID:         parseUUID(userID),
		WorkspaceID:    parseUUID(workspaceID),
		GitlabUserID:   user.ID,
		GitlabUsername: user.Username,
		PatEncrypted:   encrypted,
	})
	if err != nil {
		slog.Error("persist user_gitlab_connection", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to persist connection")
		return
	}

	writeJSON(w, http.StatusOK, userGitlabConnectionResponse{
		Connected:      true,
		GitlabUserID:   row.GitlabUserID,
		GitlabUsername: row.GitlabUsername,
	})
}

// GetUserGitlabConnection returns connected/not-connected for the current
// (user, workspace) pair. Never returns the token.
func (h *Handler) GetUserGitlabConnection(w http.ResponseWriter, r *http.Request) {
	if !h.GitlabEnabled {
		writeError(w, http.StatusNotFound, "gitlab integration disabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	row, err := h.Queries.GetUserGitlabConnection(r.Context(), db.GetUserGitlabConnectionParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, userGitlabConnectionResponse{Connected: false})
			return
		}
		slog.Error("read user_gitlab_connection", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read connection")
		return
	}
	writeJSON(w, http.StatusOK, userGitlabConnectionResponse{
		Connected:      true,
		GitlabUserID:   row.GitlabUserID,
		GitlabUsername: row.GitlabUsername,
	})
}

// DisconnectUserGitlab removes the user's PAT for the current workspace.
// Returns 204 even when the row didn't exist (idempotent).
func (h *Handler) DisconnectUserGitlab(w http.ResponseWriter, r *http.Request) {
	if !h.GitlabEnabled {
		writeError(w, http.StatusNotFound, "gitlab integration disabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	if err := h.Queries.DeleteUserGitlabConnection(r.Context(), db.DeleteUserGitlabConnectionParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	}); err != nil {
		slog.Error("delete user_gitlab_connection", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to disconnect")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// (No need to define a parseUUID helper — handler.go already has one.)
// (No need to define pgtype import here unless used directly — the calls
//  above thread through the parseUUID helper.)
//
// Suppress the unused-import linter:
var _ = pgtype.UUID{}
```

(The `_ = pgtype.UUID{}` line is a marker so a future refactor that adds direct pgtype use compiles cleanly. If you'd rather drop the unused import entirely now, do so and re-add when needed.)

- [ ] **Step 3.4: Run — expect pass**

```bash
DATABASE_URL="…" go test ./internal/handler/ -run "TestConnectUserGitlab|TestGetUserGitlabConnection|TestDisconnectUserGitlab" -v
```

- [ ] **Step 3.5: Commit**

```bash
git add server/internal/handler/gitlab_user_connection.go server/internal/handler/gitlab_user_connection_test.go
git commit -m "feat(handler): per-user gitlab PAT connect/get/disconnect endpoints"
```

---

## Task 4: Wire `Resolver` into Handler + main.go

**Files:**
- Modify: `server/internal/handler/handler.go`
- Modify: `server/cmd/server/main.go`
- Modify: `server/cmd/server/router.go`

The Handler needs access to a `*gitlab.Resolver` so write handlers can call it. Same plumbing pattern Phase 1 used: setter on the Handler, called from main.go after construction.

- [ ] **Step 4.1: Add field + setter on `Handler`**

In `server/internal/handler/handler.go`, add an import alias if not present:

```go
import (
    // …existing…
    gitlabsync "github.com/multica-ai/multica/server/internal/gitlab"
)
```

Add a field after `PublicURL`:

```go
type Handler struct {
    // …existing…
    GitlabResolver *gitlabsync.Resolver
}
```

Add a setter:

```go
// SetGitlabResolver wires the per-request token resolver. Called once from
// main.go after server boot.
func (h *Handler) SetGitlabResolver(r *gitlabsync.Resolver) {
    h.GitlabResolver = r
}
```

- [ ] **Step 4.2: Construct + wire in main.go**

In `server/cmd/server/main.go`, near where the existing reconciler/decrypter is built, construct the resolver and pass it through. Inside the `if gitlabEnabled` block:

```go
if gitlabEnabled {
    queries := db.New(pool)
    decrypter := func(ctx context.Context, encrypted []byte) (string, error) {
        plain, err := secretsCipher.Decrypt(encrypted)
        if err != nil {
            return "", err
        }
        return string(plain), nil
    }

    // Resolver — used by write-path handlers to pick user vs service PAT.
    resolver := gitlabsync.NewResolver(queries, decrypter)

    // …existing reconciler/worker setup uses the same `decrypter` — refactor
    // so all three (resolver, worker, reconciler) share it…
}
```

Then thread the resolver into `NewRouter`:

```go
r := NewRouter(pool, hub, bus, secretsCipher, gitlabClient, gitlabEnabled, serverCtx, publicURL, resolver)
```

- [ ] **Step 4.3: Update `NewRouter` signature**

In `server/cmd/server/router.go`, extend the signature:

```go
func NewRouter(
    pool *pgxpool.Pool,
    hub *realtime.Hub,
    bus *events.Bus,
    secretsCipher *secrets.Cipher,
    gitlabClient *gitlab.Client,
    gitlabEnabled bool,
    serverCtx context.Context,
    publicURL string,
    gitlabResolver *gitlabsync.Resolver,
) chi.Router {
```

After `h.SetPublicURL(publicURL)`, add:

```go
    h.SetGitlabResolver(gitlabResolver)
```

The `gitlabsync` import is already there from Phase 2b. `gitlabResolver` may be nil when `gitlabEnabled=false`; that's OK because callers gate on `h.GitlabEnabled` first.

In `server/cmd/server/integration_test.go`, update the `NewRouter(...)` call to pass `nil` for the new arg.

- [ ] **Step 4.4: Verify build**

```bash
cd server && go build ./...
```
Expected: clean.

- [ ] **Step 4.5: Commit**

```bash
git add server/internal/handler/handler.go \
        server/cmd/server/main.go \
        server/cmd/server/router.go \
        server/cmd/server/integration_test.go
git commit -m "feat(server): wire gitlab.Resolver into Handler"
```

---

## Task 5: GitLab client — `CreateIssue` method

**Files:**
- Modify: `server/pkg/gitlab/issues.go`
- Modify: `server/pkg/gitlab/issues_test.go`

- [ ] **Step 5.1: Append failing test**

```go
func TestCreateIssue_PostsCorrectBody(t *testing.T) {
	var got CreateIssueInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v4/projects/7/issues" {
			t.Errorf("path = %s, want /api/v4/projects/7/issues", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Issue{
			ID: 9001, IID: 11, Title: got.Title, State: "opened",
			Labels: got.Labels, UpdatedAt: "2026-04-17T15:00:00Z",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, srv.Client())
	out, err := c.CreateIssue(context.Background(), "tok", 7, CreateIssueInput{
		Title:       "hi",
		Description: "body",
		Labels:      []string{"status::todo", "priority::high"},
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if got.Title != "hi" || got.Description != "body" {
		t.Errorf("server received %+v", got)
	}
	if len(got.Labels) != 2 {
		t.Errorf("labels = %v", got.Labels)
	}
	if out.IID != 11 {
		t.Errorf("returned IID = %d, want 11", out.IID)
	}
}
```

- [ ] **Step 5.2: Run — expect compile error**

```bash
cd server && go test ./pkg/gitlab/ -run TestCreateIssue -v
```

- [ ] **Step 5.3: Implement**

Append to `server/pkg/gitlab/issues.go`:

```go
// CreateIssueInput is the body for POST /projects/:id/issues. Only fields
// we set are listed; GitLab accepts more.
type CreateIssueInput struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	// AssigneeIDs is the list of GitLab user IDs to assign. Empty when
	// Multica is assigning to an agent (we use the agent::<slug> label
	// instead).
	AssigneeIDs []int64 `json:"assignee_ids,omitempty"`
	DueDate     string  `json:"due_date,omitempty"`
}

// CreateIssue creates a new issue in the project and returns the GitLab
// representation (which the caller can run through the translator + cache
// upsert).
func (c *Client) CreateIssue(ctx context.Context, token string, projectID int64, input CreateIssueInput) (*Issue, error) {
	var out Issue
	path := fmt.Sprintf("/projects/%d/issues", projectID)
	if err := c.do(ctx, "POST", token, path, input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
```

- [ ] **Step 5.4: Run — expect pass**

```bash
go test ./pkg/gitlab/ -v
```

- [ ] **Step 5.5: Commit**

```bash
git add server/pkg/gitlab/issues.go server/pkg/gitlab/issues_test.go
git commit -m "feat(gitlab): CreateIssue REST method"
```

---

## Task 6: Forward translator — `BuildCreateIssueInput`

**Files:**
- Modify: `server/internal/gitlab/translator.go`
- Modify: `server/internal/gitlab/translator_test.go`

The forward direction: take a Multica `CreateIssueRequest` (already defined in `server/pkg/db/generated/`) and produce a `gitlabapi.CreateIssueInput`. Status/priority/agent assignment go to scoped labels; native assignees come through if a member is being assigned.

For Phase 3a, **member assignees from Multica are simply ignored** — we don't yet have the inverse "Multica user → GitLab user ID" mapping (Phase 3b will add it via the `user_gitlab_connection` lookup). Issues created via Multica will land unassigned in GitLab, and Phase 3b will fill in the gap.

- [ ] **Step 6.1: Append failing tests**

In `server/internal/gitlab/translator_test.go`:

```go
// CreateIssueRequest is shaped like Multica's existing handler input.
// We define it locally in tests (and as a type in translator.go) so the
// translator stays decoupled from the handler package.

func TestBuildCreateIssueInput_StatusAndPriorityToLabels(t *testing.T) {
	in := CreateIssueRequest{
		Title:    "hi",
		Status:   "in_progress",
		Priority: "high",
	}
	out := BuildCreateIssueInput(in, nil)
	if out.Title != "hi" {
		t.Errorf("title = %q", out.Title)
	}
	hasStatus := false
	hasPriority := false
	for _, l := range out.Labels {
		if l == "status::in_progress" {
			hasStatus = true
		}
		if l == "priority::high" {
			hasPriority = true
		}
	}
	if !hasStatus {
		t.Errorf("labels missing status::in_progress: %v", out.Labels)
	}
	if !hasPriority {
		t.Errorf("labels missing priority::high: %v", out.Labels)
	}
}

func TestBuildCreateIssueInput_AgentAssigneeToLabel(t *testing.T) {
	in := CreateIssueRequest{
		Title:        "hi",
		Status:       "todo",
		Priority:     "none",
		AssigneeType: "agent",
		AssigneeID:   "agent-uuid-1",
	}
	out := BuildCreateIssueInput(in, map[string]string{"agent-uuid-1": "builder"})
	hasAgentLabel := false
	for _, l := range out.Labels {
		if l == "agent::builder" {
			hasAgentLabel = true
		}
	}
	if !hasAgentLabel {
		t.Errorf("labels missing agent::builder: %v", out.Labels)
	}
	if len(out.AssigneeIDs) != 0 {
		t.Errorf("AssigneeIDs should be empty when assigning to agent, got %v", out.AssigneeIDs)
	}
}

func TestBuildCreateIssueInput_MemberAssigneeIgnoredInPhase3a(t *testing.T) {
	// Phase 3b will resolve member UUID → GitLab user ID. Until then,
	// member assignees are silently dropped.
	in := CreateIssueRequest{
		Title:        "hi",
		Status:       "todo",
		Priority:     "none",
		AssigneeType: "member",
		AssigneeID:   "user-uuid-1",
	}
	out := BuildCreateIssueInput(in, nil)
	if len(out.AssigneeIDs) != 0 {
		t.Errorf("AssigneeIDs should be empty for member assignee in 3a, got %v", out.AssigneeIDs)
	}
}

func TestBuildCreateIssueInput_PriorityNoneOmitted(t *testing.T) {
	// priority::none is the default — emitting the label clutters GitLab UI.
	in := CreateIssueRequest{
		Title:    "hi",
		Status:   "todo",
		Priority: "none",
	}
	out := BuildCreateIssueInput(in, nil)
	for _, l := range out.Labels {
		if l == "priority::none" {
			t.Errorf("priority::none should not be emitted as a label; got %v", out.Labels)
		}
	}
}
```

- [ ] **Step 6.2: Run — expect compile error**

```bash
DATABASE_URL="…" go test ./internal/gitlab/ -run TestBuildCreateIssueInput -v
```

- [ ] **Step 6.3: Implement**

Append to `server/internal/gitlab/translator.go`:

```go
// CreateIssueRequest mirrors the subset of Multica's create-issue HTTP body
// we translate to GitLab. Defined here so the translator stays handler-free.
type CreateIssueRequest struct {
	Title        string
	Description  string
	Status       string // backlog|todo|in_progress|in_review|done|blocked|cancelled
	Priority     string // urgent|high|medium|low|none
	AssigneeType string // "" | "member" | "agent"
	AssigneeID   string // UUID string when AssigneeType is set
	DueDate      string // YYYY-MM-DD or ""
	Labels       []string
}

// BuildCreateIssueInput converts a Multica create-issue request into the
// GitLab REST body. agentSlugByUUID maps Multica agent UUID → slug so we
// can express agent assignment as the agent::<slug> label.
//
// Phase 3a behaviour: member assignees are dropped (no GitLab user mapping
// yet — Phase 3b adds it). Agent assignees become the corresponding label.
func BuildCreateIssueInput(req CreateIssueRequest, agentSlugByUUID map[string]string) gitlabapi.CreateIssueInput {
	labels := append([]string(nil), req.Labels...)
	if req.Status != "" {
		labels = append(labels, "status::"+req.Status)
	}
	if req.Priority != "" && req.Priority != "none" {
		labels = append(labels, "priority::"+req.Priority)
	}
	if req.AssigneeType == "agent" && req.AssigneeID != "" && agentSlugByUUID != nil {
		if slug, ok := agentSlugByUUID[req.AssigneeID]; ok {
			labels = append(labels, "agent::"+slug)
		}
	}
	return gitlabapi.CreateIssueInput{
		Title:       req.Title,
		Description: req.Description,
		Labels:      labels,
		DueDate:     req.DueDate,
	}
}
```

- [ ] **Step 6.4: Run — expect pass**

```bash
DATABASE_URL="…" go test ./internal/gitlab/ -v
```
Expected: all translator tests pass (Phase 2a's + the new ones).

- [ ] **Step 6.5: Commit**

```bash
git add server/internal/gitlab/translator.go server/internal/gitlab/translator_test.go
git commit -m "feat(gitlab): BuildCreateIssueInput translates Multica → GitLab create-issue body"
```

---

## Task 7: Refactor `POST /api/issues` to write through GitLab

**Files:**
- Modify: `server/internal/handler/issue.go`
- Modify: `server/internal/handler/issue_test.go`

When the workspace has a GitLab connection, the create-issue handler must:
1. Resolve the actor (member or agent) via `h.resolveActor`.
2. Call `h.GitlabResolver.ResolveTokenForWrite` to pick the right token.
3. Build a `CreateIssueInput` via the translator.
4. Call `h.Gitlab.CreateIssue`.
5. Run the response through `gitlabsync.TranslateIssue` + `UpsertIssueFromGitlab` so the cache row is consistent with what the eventual webhook echo will deliver.
6. Publish the existing `issue:created` WS event (so the UI updates without waiting for the webhook).
7. Respond 201 with the cache row (existing JSON shape unchanged).

When the workspace does NOT have a GitLab connection, fall through to the legacy direct-DB path (unchanged from today).

- [ ] **Step 7.1: Read the existing handler shape**

```bash
grep -n "func (h \*Handler) CreateIssue" /Users/jimmy.mills/Developer/multica-gitlab-phase-3a/server/internal/handler/issue.go
```

Open that function. The current body validates input, calls `h.Queries.CreateIssue` directly, publishes `issue:created`, and returns the row as JSON. We'll insert a "if gitlab connected, do write-through; else fall through" branch at the top.

- [ ] **Step 7.2: Write failing tests**

Append to `server/internal/handler/issue_test.go`:

```go
func TestCreateIssue_WriteThroughHumanWithoutPATUsesServicePAT(t *testing.T) {
	var capturedToken string
	var serverReceived map[string]any
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/42/issues":
			if r.Method == http.MethodPost {
				capturedToken = r.Header.Get("PRIVATE-TOKEN")
				json.NewDecoder(r.Body).Decode(&serverReceived)
				w.Write([]byte(`{"id":9001,"iid":99,"title":"From Multica","state":"opened",
					"labels":["status::todo","priority::medium"],"updated_at":"2026-04-17T15:00:00Z"}`))
				return
			}
		}
		w.Write([]byte(`{}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	// Seed a workspace_gitlab_connection (without webhook) so the handler
	// takes the write-through branch.
	encrypted, _ := h.Secrets.Encrypt([]byte("svc-token-xyz"))
	testPool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 42, 'g/a', $2, 1, 'connected')
		ON CONFLICT (workspace_id) DO UPDATE SET
			gitlab_project_id = EXCLUDED.gitlab_project_id,
			service_token_encrypted = EXCLUDED.service_token_encrypted,
			service_token_user_id = EXCLUDED.service_token_user_id
	`, testWorkspaceID, encrypted)

	// Wire a real resolver on the handler so the write-through branch works.
	h.SetGitlabResolver(gitlabsync.NewResolver(h.Queries, func(_ context.Context, b []byte) (string, error) {
		plain, err := h.Secrets.Decrypt(b)
		if err != nil {
			return "", err
		}
		return string(plain), nil
	}))

	body, _ := json.Marshal(map[string]any{
		"title":    "From Multica",
		"status":   "todo",
		"priority": "medium",
	})
	req := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, nil)
	// newRequest doesn't accept a body in our test helper; build manually.
	req = httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.CreateIssue(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if capturedToken != "svc-token-xyz" {
		t.Errorf("PRIVATE-TOKEN sent to gitlab = %q, want svc-token-xyz (service PAT)", capturedToken)
	}

	// Verify the cache row exists with the GitLab IID.
	var iid int
	testPool.QueryRow(context.Background(),
		`SELECT gitlab_iid FROM issue WHERE workspace_id = $1::uuid AND title = 'From Multica'`,
		testWorkspaceID).Scan(&iid)
	if iid != 99 {
		t.Errorf("cached gitlab_iid = %d, want 99", iid)
	}
}

func TestCreateIssue_WriteThroughHumanWithPATUsesUserPAT(t *testing.T) {
	var capturedToken string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			w.Write([]byte(`{"id":555,"username":"alice"}`))
		case "/api/v4/projects/42/issues":
			if r.Method == http.MethodPost {
				capturedToken = r.Header.Get("PRIVATE-TOKEN")
				w.Write([]byte(`{"id":9001,"iid":100,"title":"From Alice","state":"opened",
					"labels":["status::todo","priority::medium"],"updated_at":"2026-04-17T15:00:00Z"}`))
				return
			}
		}
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	defer h.Queries.DeleteUserGitlabConnection(context.Background(), db.DeleteUserGitlabConnectionParams{
		UserID:      parseUUID(testUserID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})

	// Seed both connections.
	svcEnc, _ := h.Secrets.Encrypt([]byte("svc-token"))
	usrEnc, _ := h.Secrets.Encrypt([]byte("user-token-alice"))
	testPool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 42, 'g/a', $2, 1, 'connected')
		ON CONFLICT (workspace_id) DO UPDATE SET
			service_token_encrypted = EXCLUDED.service_token_encrypted
	`, testWorkspaceID, svcEnc)
	h.Queries.UpsertUserGitlabConnection(context.Background(), db.UpsertUserGitlabConnectionParams{
		UserID:         parseUUID(testUserID),
		WorkspaceID:    parseUUID(testWorkspaceID),
		GitlabUserID:   555,
		GitlabUsername: "alice",
		PatEncrypted:   usrEnc,
	})

	h.SetGitlabResolver(gitlabsync.NewResolver(h.Queries, func(_ context.Context, b []byte) (string, error) {
		plain, err := h.Secrets.Decrypt(b)
		if err != nil {
			return "", err
		}
		return string(plain), nil
	}))

	body, _ := json.Marshal(map[string]any{"title": "From Alice", "status": "todo", "priority": "medium"})
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.CreateIssue(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d", rr.Code)
	}
	if capturedToken != "user-token-alice" {
		t.Errorf("PRIVATE-TOKEN = %q, want user-token-alice", capturedToken)
	}
}

func TestCreateIssue_LegacyPathWhenNoGitlabConnection(t *testing.T) {
	// No workspace_gitlab_connection row → handler takes the legacy direct-DB
	// path. (Same behaviour as pre-Phase-3a.)
	h := buildHandlerWithGitlab(t, "http://unused")
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	body, _ := json.Marshal(map[string]any{"title": "Legacy", "status": "todo", "priority": "medium"})
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.CreateIssue(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
}
```

(`bytes`, `context`, `db`, `gitlabsync` imports may need adding at the top of `issue_test.go`. The Phase 1 setup probably already has most.)

- [ ] **Step 7.3: Run — expect compile error or test failure**

```bash
DATABASE_URL="…" go test ./internal/handler/ -run TestCreateIssue_WriteThrough -v
```

- [ ] **Step 7.4: Implement**

In `server/internal/handler/issue.go`, in `CreateIssue`, near the top of the function (after parsing the request body and validating workspace membership but BEFORE the legacy `h.Queries.CreateIssue` call), insert:

```go
// Phase 3a write-through: when the workspace has a GitLab connection,
// create the issue in GitLab first, then upsert the cache row from the
// returned representation. Falls through to legacy direct-DB path when
// no connection exists.
if h.GitlabEnabled && h.GitlabResolver != nil {
    wsConn, err := h.Queries.GetWorkspaceGitlabConnection(r.Context(), parseUUID(workspaceID))
    if err == nil {
        actorType, actorID := h.resolveActor(r, userID, workspaceID)
        token, _, err := h.GitlabResolver.ResolveTokenForWrite(r.Context(), workspaceID, actorType, actorID)
        if err != nil {
            slog.Error("resolve gitlab token", "error", err)
            writeError(w, http.StatusBadGateway, "could not resolve gitlab token")
            return
        }

        // Build agent slug map for translator.
        agentSlugMap, err := h.buildAgentUUIDSlugMap(r.Context(), parseUUID(workspaceID))
        if err != nil {
            slog.Error("agent slug map", "error", err)
            writeError(w, http.StatusInternalServerError, "build agent map failed")
            return
        }

        glInput := gitlabsync.BuildCreateIssueInput(gitlabsync.CreateIssueRequest{
            Title:        req.Title,
            Description:  req.Description,
            Status:       req.Status,
            Priority:     req.Priority,
            AssigneeType: req.AssigneeType,
            AssigneeID:   req.AssigneeID,
            DueDate:      req.DueDate,
        }, agentSlugMap)

        glIssue, err := h.Gitlab.CreateIssue(r.Context(), token, wsConn.GitlabProjectID, glInput)
        if err != nil {
            slog.Error("gitlab create issue", "error", err)
            writeError(w, http.StatusBadGateway, "gitlab create issue failed")
            return
        }

        agentByLabel := make(map[string]string, len(agentSlugMap))
        for uuid, slug := range agentSlugMap {
            agentByLabel[slug] = uuid
        }
        values := gitlabsync.TranslateIssue(*glIssue, &gitlabsync.TranslateContext{AgentBySlug: agentByLabel})

        // Upsert into the cache (returns the cached row).
        cacheRow, err := h.Queries.UpsertIssueFromGitlab(r.Context(), buildUpsertParamsFromCreate(parseUUID(workspaceID), wsConn.GitlabProjectID, *glIssue, values))
        if err != nil {
            slog.Error("upsert gitlab cache row", "error", err)
            writeError(w, http.StatusInternalServerError, "cache upsert failed")
            return
        }

        h.publish(protocol.EventIssueCreated, workspaceID, actorType, actorID, map[string]any{
            "issue": issueToResponse(cacheRow, h.getIssuePrefix(r.Context(), parseUUID(workspaceID))),
        })

        writeJSON(w, http.StatusCreated, issueToResponse(cacheRow, h.getIssuePrefix(r.Context(), parseUUID(workspaceID))))
        return
    }
    // err != nil from GetWorkspaceGitlabConnection (likely pgx.ErrNoRows)
    // → fall through to legacy path.
}

// (existing legacy CreateIssue body remains unchanged below this block)
```

Two helpers referenced above need to exist on the handler:

**`buildAgentUUIDSlugMap`** — derives slug from agent name (same logic as Phase 2a's `buildAgentSlugMap` in `internal/gitlab/initial_sync.go`, but indexed differently). Add to `handler.go`:

```go
// buildAgentUUIDSlugMap returns a map of agent UUID → slug for every agent
// in the workspace. Slugs are derived from agent.name (lowercased, spaces
// → hyphens). Same convention as Phase 2a's read-side buildAgentSlugMap.
func (h *Handler) buildAgentUUIDSlugMap(ctx context.Context, workspaceID pgtype.UUID) (map[string]string, error) {
    rows, err := h.Queries.ListAgents(ctx, workspaceID)
    if err != nil {
        return nil, err
    }
    out := make(map[string]string, len(rows))
    for _, row := range rows {
        slug := strings.ToLower(strings.ReplaceAll(row.Name, " ", "-"))
        out[uuidToString(row.ID)] = slug
    }
    return out, nil
}
```

(Add `"strings"` to handler.go imports if missing.)

**`buildUpsertParamsFromCreate`** — local helper that mirrors `buildUpsertIssueParams` in `internal/gitlab/initial_sync.go` but lives in handler scope to avoid an import cycle. Add to `issue.go`:

```go
func buildUpsertParamsFromCreate(wsUUID pgtype.UUID, projectID int64, issue gitlab.Issue, values gitlabsync.IssueValues) db.UpsertIssueFromGitlabParams {
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
    extUpdated := pgtype.Timestamptz{}
    if values.UpdatedAt != "" {
        if t, err := time.Parse(time.RFC3339, values.UpdatedAt); err == nil {
            extUpdated = pgtype.Timestamptz{Time: t, Valid: true}
        }
    }
    return db.UpsertIssueFromGitlabParams{
        WorkspaceID:       wsUUID,
        GitlabIid:         pgtype.Int4{Int32: int32(issue.IID), Valid: true},
        GitlabProjectID:   pgtype.Int8{Int64: projectID, Valid: true},
        GitlabIssueID:     pgtype.Int8{Int64: issue.ID, Valid: issue.ID != 0},
        Title:             values.Title,
        Description:       desc,
        Status:            values.Status,
        Priority:          values.Priority,
        AssigneeType:      assigneeType,
        AssigneeID:        assigneeID,
        CreatorType:       pgtype.Text{}, // Phase 3b backfill
        CreatorID:         pgtype.UUID{},
        DueDate:           pgtype.Timestamptz{}, // due_date populated by webhook echo if needed
        ExternalUpdatedAt: extUpdated,
    }
}
```

Add imports to `issue.go`: `"github.com/multica-ai/multica/server/pkg/gitlab"` (the REST client package) and `gitlabsync "github.com/multica-ai/multica/server/internal/gitlab"`. May also need `"time"`.

`protocol.EventIssueCreated` is the existing event constant — already imported by `issue.go`.

- [ ] **Step 7.5: Run — expect pass**

```bash
DATABASE_URL="…" go test ./internal/handler/ -run "TestCreateIssue_WriteThrough|TestCreateIssue_LegacyPath" -v
```
Expected: all 3 tests pass.

- [ ] **Step 7.6: Commit**

```bash
git add server/internal/handler/issue.go server/internal/handler/issue_test.go server/internal/handler/handler.go
git commit -m "feat(handler): POST /api/issues writes through GitLab when workspace connected"
```

---

## Task 8: Selectively unmount the 501 middleware from POST /api/issues

**Files:**
- Modify: `server/cmd/server/router.go`

The Phase 2a `GitlabWritesBlocked` middleware sits on the `/api/issues` route group and 501s every non-GET. Now that POST `/api/issues` has write-through, remove it from this single route while keeping it on PUT/DELETE/batch/etc.

The cleanest refactor: split the `/api/issues` group into two — one un-gated for POST, one gated for everything else.

- [ ] **Step 8.1: Read current shape**

```bash
grep -n -A 30 'r.Route\("/api/issues"' /Users/jimmy.mills/Developer/multica-gitlab-phase-3a/server/cmd/server/router.go
```

You'll see `r.Route("/api/issues", func(r chi.Router) { r.Use(middleware.GitlabWritesBlocked(queries)) ... })`.

- [ ] **Step 8.2: Refactor**

Replace the existing block. Mount POST `/api/issues` BEFORE the route group (no middleware), then keep the rest of the routes inside a route group with the middleware:

```go
// POST /api/issues now uses GitLab write-through (Phase 3a) — no 501 stopgap.
r.Post("/api/issues", h.CreateIssue)

// Other issue write/read endpoints — writes still 501 while connected
// (Phase 3b will refactor each).
r.Route("/api/issues", func(r chi.Router) {
    r.Use(middleware.GitlabWritesBlocked(queries))
    r.Get("/search", h.SearchIssues)
    r.Get("/child-progress", h.ChildIssueProgress)
    r.Get("/", h.ListIssues)
    // POST is mounted above without middleware.
    r.Post("/batch-update", h.BatchUpdateIssues)
    r.Post("/batch-delete", h.BatchDeleteIssues)
    r.Route("/{id}", func(r chi.Router) {
        r.Get("/", h.GetIssue)
        r.Put("/", h.UpdateIssue)
        r.Delete("/", h.DeleteIssue)
        r.Get("/timeline", h.ListTimeline)
        // … keep every other existing nested route exactly as it was …
    })
})
```

(Keep all existing nested routes — only POST `/api/issues` is moved out. Read the file carefully; some routes you might miss are `attachments`, `subscribers`, `comments`, `reactions`, `children`. All stay inside the gated group except the top-level POST.)

- [ ] **Step 8.3: Verify build**

```bash
cd server && go build ./...
```

- [ ] **Step 8.4: Smoke check via the existing handler test**

The existing `TestGitlabConnectedWorkspace_WriteReturns501` (Phase 2a) tests POST through a tiny chi router that mounts `r.Use(middleware.GitlabWritesBlocked(...))` then `r.Post("/", h.CreateIssue)`. That test is checking the MIDDLEWARE behaviour, not the production router — it should still pass unchanged. Verify:

```bash
DATABASE_URL="…" go test ./internal/handler/ -run TestGitlabConnectedWorkspace_WriteReturns501 -v
```

- [ ] **Step 8.5: Commit**

```bash
git add server/cmd/server/router.go
git commit -m "feat(server): unmount 501 stopgap from POST /api/issues (Phase 3a write-through)"
```

---

## Task 9: Mount the per-user PAT routes

**Files:**
- Modify: `server/cmd/server/router.go`

Mount POST/GET/DELETE `/api/me/gitlab/connect` inside the existing `r.Group(func(r chi.Router) { r.Use(middleware.Auth(queries)) ... })` block (because the user must be authenticated). The handler internally uses `h.requireWorkspaceMember` to check the `X-Workspace-ID` header.

- [ ] **Step 9.1: Add the routes**

In the protected `r.Group(...)` near the existing `/api/me` routes, add:

```go
r.Post("/api/me/gitlab/connect", h.ConnectUserGitlab)
r.Get("/api/me/gitlab/connect", h.GetUserGitlabConnection)
r.Delete("/api/me/gitlab/connect", h.DisconnectUserGitlab)
```

- [ ] **Step 9.2: Verify build + tests**

```bash
cd server && go build ./...
DATABASE_URL="…" go test ./internal/handler/ -run "TestConnectUserGitlab|TestGetUserGitlabConnection|TestDisconnectUserGitlab" -v
```

- [ ] **Step 9.3: Commit**

```bash
git add server/cmd/server/router.go
git commit -m "feat(server): mount /api/me/gitlab/connect endpoints"
```

---

## Task 10: Frontend — user PAT api + queries + mutations

**Files:**
- Create: `packages/core/gitlab/user-types.ts`
- Create: `packages/core/gitlab/user-queries.ts`
- Create: `packages/core/gitlab/user-mutations.ts`
- Create: `packages/core/gitlab/user-mutations.test.tsx`
- Modify: `packages/core/api/client.ts` — three new methods
- Modify: `packages/core/gitlab/index.ts` — re-exports

Mirrors the Phase 1 workspace-PAT structure in `packages/core/gitlab/`.

- [ ] **Step 10.1: Add API methods to `ApiClient`**

In `packages/core/api/client.ts`, near the existing `connectWorkspaceGitlab` etc., add:

```ts
async getUserGitlabConnection(wsId: string): Promise<UserGitlabConnection> {
    return this.fetch(`/api/me/gitlab/connect`, {
        method: "GET",
        headers: { "X-Workspace-ID": wsId },
    });
}

async connectUserGitlab(wsId: string, input: ConnectUserGitlabInput): Promise<UserGitlabConnection> {
    return this.fetch(`/api/me/gitlab/connect`, {
        method: "POST",
        headers: { "X-Workspace-ID": wsId },
        body: JSON.stringify(input),
    });
}

async disconnectUserGitlab(wsId: string): Promise<void> {
    await this.fetch(`/api/me/gitlab/connect`, {
        method: "DELETE",
        headers: { "X-Workspace-ID": wsId },
    });
}
```

Add the type imports at the top of `client.ts`:

```ts
import type { UserGitlabConnection, ConnectUserGitlabInput } from "../gitlab/user-types";
```

- [ ] **Step 10.2: Create `packages/core/gitlab/user-types.ts`**

```ts
export interface UserGitlabConnection {
  connected: boolean;
  gitlab_user_id?: number;
  gitlab_username?: string;
}

export interface ConnectUserGitlabInput {
  token: string;
}
```

- [ ] **Step 10.3: Create `packages/core/gitlab/user-queries.ts`**

```ts
import { useQuery, type UseQueryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { UserGitlabConnection } from "./user-types";

export const userGitlabKeys = {
  all: (wsId: string) => ["gitlab", "user", wsId] as const,
  connection: (wsId: string) => [...userGitlabKeys.all(wsId), "connection"] as const,
};

export function userGitlabConnectionOptions(wsId: string) {
  return {
    queryKey: userGitlabKeys.connection(wsId),
    queryFn: () => api.getUserGitlabConnection(wsId),
    retry: false,
  } satisfies UseQueryOptions<UserGitlabConnection>;
}

export function useUserGitlabConnection(wsId: string) {
  return useQuery(userGitlabConnectionOptions(wsId));
}
```

- [ ] **Step 10.4: Create `packages/core/gitlab/user-mutations.ts`**

```ts
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { userGitlabKeys } from "./user-queries";
import type { ConnectUserGitlabInput } from "./user-types";

export function useConnectUserGitlabMutation(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ConnectUserGitlabInput) => api.connectUserGitlab(wsId, input),
    onSuccess: (data) => {
      qc.setQueryData(userGitlabKeys.connection(wsId), data);
    },
  });
}

export function useDisconnectUserGitlabMutation(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.disconnectUserGitlab(wsId),
    onSuccess: () => {
      qc.setQueryData(userGitlabKeys.connection(wsId), { connected: false });
    },
  });
}
```

- [ ] **Step 10.5: Update `packages/core/gitlab/index.ts`**

```ts
export * from "./types";
export * from "./queries";
export * from "./mutations";
export * from "./user-types";
export * from "./user-queries";
export * from "./user-mutations";
```

- [ ] **Step 10.6: Add subpath exports in `packages/core/package.json`**

In the `"exports"` map, add:

```json
"./gitlab/user-types": "./gitlab/user-types.ts",
"./gitlab/user-queries": "./gitlab/user-queries.ts",
"./gitlab/user-mutations": "./gitlab/user-mutations.ts"
```

- [ ] **Step 10.7: Test**

Create `packages/core/gitlab/user-mutations.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, act } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import {
  useConnectUserGitlabMutation,
  useDisconnectUserGitlabMutation,
} from "./user-mutations";

vi.mock("../api", () => ({
  api: {
    getUserGitlabConnection: vi.fn(),
    connectUserGitlab: vi.fn(),
    disconnectUserGitlab: vi.fn(),
  },
}));

import { api } from "../api";

function wrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
}

describe("user gitlab mutations", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("connectUserGitlab calls api and caches the response", async () => {
    const conn = { connected: true, gitlab_user_id: 555, gitlab_username: "alice" };
    (api.connectUserGitlab as ReturnType<typeof vi.fn>).mockResolvedValue(conn);

    const { result } = renderHook(() => useConnectUserGitlabMutation("ws-1"), { wrapper: wrapper() });
    await act(async () => {
      await result.current.mutateAsync({ token: "glpat-x" });
    });
    expect(api.connectUserGitlab).toHaveBeenCalledWith("ws-1", { token: "glpat-x" });
    expect(result.current.data).toEqual(conn);
  });

  it("disconnectUserGitlab calls api and clears cache to {connected:false}", async () => {
    (api.disconnectUserGitlab as ReturnType<typeof vi.fn>).mockResolvedValue(undefined);
    const { result } = renderHook(() => useDisconnectUserGitlabMutation("ws-1"), { wrapper: wrapper() });
    await act(async () => {
      await result.current.mutateAsync();
    });
    expect(api.disconnectUserGitlab).toHaveBeenCalledWith("ws-1");
  });
});
```

Run:

```bash
cd /Users/jimmy.mills/Developer/multica-gitlab-phase-3a && \
  pnpm --filter @multica/core exec vitest run gitlab/user-mutations.test.tsx
```

Expected: 2/2 PASS.

- [ ] **Step 10.8: Typecheck**

```bash
cd /Users/jimmy.mills/Developer/multica-gitlab-phase-3a && pnpm typecheck
```
Expected: clean.

- [ ] **Step 10.9: Commit**

```bash
git add packages/core/api/client.ts \
        packages/core/gitlab/user-types.ts \
        packages/core/gitlab/user-queries.ts \
        packages/core/gitlab/user-mutations.ts \
        packages/core/gitlab/user-mutations.test.tsx \
        packages/core/gitlab/index.ts \
        packages/core/package.json
git commit -m "feat(core): user gitlab pat api + queries + mutations"
```

---

## Task 11: Frontend — extend GitlabTab with personal-PAT section

**Files:**
- Modify: `packages/views/settings/components/gitlab-tab.tsx`
- Modify: `packages/views/settings/components/gitlab-tab.test.tsx`

The existing tab shows the workspace-level connection (admin-only). Add a second section below it, visible to every member, where the user can paste their own PAT. Layout: card with a heading "Your personal GitLab connection", a brief explainer, and a form (or a connected-state card with disconnect button).

- [ ] **Step 11.1: Read existing tab**

```bash
cat /Users/jimmy.mills/Developer/multica-gitlab-phase-3a/packages/views/settings/components/gitlab-tab.tsx
```

The current file has a single component with `useWorkspaceGitlabConnection` + connect/disconnect mutations. Add a sibling `<UserGitlabSection workspaceId={wsId} />` rendered below the workspace section. Use Card-like layout consistent with the existing section.

- [ ] **Step 11.2: Append UserGitlabSection to the file**

In `packages/views/settings/components/gitlab-tab.tsx`, after the existing `GitlabTab` function, add a new component (and render it inside `GitlabTab`):

```tsx
import { useUserGitlabConnection } from "@multica/core/gitlab/user-queries";
import {
  useConnectUserGitlabMutation,
  useDisconnectUserGitlabMutation,
} from "@multica/core/gitlab/user-mutations";

function UserGitlabSection({ workspaceId }: { workspaceId: string }) {
  const { data, isLoading } = useUserGitlabConnection(workspaceId);
  const connectMu = useConnectUserGitlabMutation(workspaceId);
  const disconnectMu = useDisconnectUserGitlabMutation(workspaceId);
  const [token, setToken] = useState("");

  if (isLoading) {
    return <div className="text-muted-foreground text-sm">Loading…</div>;
  }

  if (data?.connected) {
    return (
      <Card>
        <CardContent className="space-y-3 pt-6">
          <div>
            <span className="text-muted-foreground">Connected as: </span>
            <span className="font-medium">@{data.gitlab_username}</span>
          </div>
          <Button
            variant="destructive"
            disabled={disconnectMu.isPending}
            onClick={() => disconnectMu.mutate()}
          >
            {disconnectMu.isPending ? "Disconnecting…" : "Disconnect"}
          </Button>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardContent className="space-y-3 pt-6">
        <p className="text-sm text-muted-foreground">
          Connect your personal GitLab account so your writes (issue creation, comments,
          status changes) post as you instead of as the workspace service account.
        </p>
        <form
          className="space-y-2"
          onSubmit={(e) => {
            e.preventDefault();
            connectMu.mutate({ token });
          }}
        >
          <Label htmlFor="user-gitlab-token">Personal access token</Label>
          <Input
            id="user-gitlab-token"
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="glpat-…"
            required
          />
          {connectMu.isError ? (
            <div className="text-destructive text-sm">
              {connectMu.error instanceof ApiError
                ? connectMu.error.message
                : "Connection failed"}
            </div>
          ) : null}
          <Button type="submit" disabled={connectMu.isPending || !token}>
            {connectMu.isPending ? "Connecting…" : "Connect personal account"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
```

Then render it inside `GitlabTab` after the existing workspace-section card. The existing section is wrapped in `<div className="space-y-4">…</div>`; add a sub-heading and the new component:

Find where the GitlabTab returns its main JSX (the connected-state branch and the form-state branch). Wrap each in a layout where the workspace-section UI comes first, then a `<div>` heading "Your personal GitLab connection", then the `<UserGitlabSection workspaceId={workspaceId} />`. Both states (workspace connected, workspace disconnected) should show the personal section, since a user might want to manage their PAT independently.

(For brevity, do NOT enumerate every state combination — render the personal section unconditionally below the workspace section.)

- [ ] **Step 11.3: Update tests**

In `packages/views/settings/components/gitlab-tab.test.tsx`, the Phase 1 test already mocks `@multica/core/api` and uses `(api.connectWorkspaceGitlab as any).mockResolvedValue(...)` style assertions. Extend that mock to also stub the three user-side methods:

```tsx
vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return {
    ...actual,
    api: {
      getWorkspaceGitlabConnection: vi.fn(),
      connectWorkspaceGitlab: vi.fn(),
      disconnectWorkspaceGitlab: vi.fn(),
      getUserGitlabConnection: vi.fn(),
      connectUserGitlab: vi.fn(),
      disconnectUserGitlab: vi.fn(),
    },
  };
});

import { api } from "@multica/core/api";
```

(If the existing mock uses a different shape, adapt — the goal is that all 6 methods are mocked.)

Then add tests:

```tsx
it("renders the personal connection form when user is not connected", async () => {
  (api.getWorkspaceGitlabConnection as any).mockRejectedValue({ status: 404 });
  (api.getUserGitlabConnection as any).mockResolvedValue({ connected: false });
  renderPage();
  expect(await screen.findByText(/connect your personal gitlab/i)).toBeInTheDocument();
  expect(screen.getByLabelText(/personal access token/i)).toBeInTheDocument();
});

it("renders 'connected as @username' when user has connected", async () => {
  (api.getWorkspaceGitlabConnection as any).mockRejectedValue({ status: 404 });
  (api.getUserGitlabConnection as any).mockResolvedValue({
    connected: true, gitlab_username: "alice",
  });
  renderPage();
  expect(await screen.findByText(/@alice/)).toBeInTheDocument();
});
```

Existing tests in this file should continue to pass — they were already using `api.getWorkspaceGitlabConnection` / `api.connectWorkspaceGitlab` style; your mock extension is purely additive.

- [ ] **Step 11.4: Run tests**

```bash
pnpm --filter @multica/views exec vitest run settings/components/gitlab-tab.test.tsx
```

- [ ] **Step 11.5: Commit**

```bash
git add packages/views/settings/components/gitlab-tab.tsx packages/views/settings/components/gitlab-tab.test.tsx
git commit -m "feat(views): personal-PAT section in workspace gitlab settings tab"
```

---

## Task 12: Frontend — workspace banner prompting PAT connection

**Files:**
- Create: `packages/views/workspace/banners/gitlab-pat-banner.tsx`
- Create: `packages/views/workspace/banners/gitlab-pat-banner.test.tsx`
- Modify: the existing workspace dashboard layout (identify in Step 12.1).

The banner shows when:
- The workspace has GitLab connected (via `useWorkspaceGitlabConnection`).
- The current user has NOT connected their personal PAT (via `useUserGitlabConnection`).
- The user is on a workspace page.

When all three are true, render a small dismissable info banner at the top of the dashboard.

- [ ] **Step 12.1: Find the workspace dashboard layout**

The banner needs to render exactly once per workspace page (NOT per route — once per dashboard chrome). The most likely homes for this in priority order:
1. A shared layout component in `packages/views/layout/` (e.g. `DashboardGuard.tsx` per CLAUDE.md hints).
2. A workspace-shell component in `packages/views/workspace/`.
3. The web app's `apps/web/app/[workspaceSlug]/(dashboard)/layout.tsx` and the desktop's equivalent — least preferred since it duplicates per-app.

Inspect:
```bash
ls packages/views/layout/ packages/views/workspace/ 2>/dev/null
grep -rln "DashboardGuard\|WorkspaceShell\|WorkspaceProvider" packages/views/ apps/web/app/\[workspaceSlug\] apps/desktop/src/renderer/src 2>/dev/null | head -10
```

If `packages/views/layout/DashboardGuard.tsx` (or similar) exists and wraps every workspace-scoped page, mount the banner inside it. Otherwise mount in the per-app layout files (web AND desktop — the banner's component lives in `packages/views/`, but the mounting site is platform-specific in that fallback case).

Whichever you pick: add ONE mount point. Don't render the banner twice.

Report which file you chose in your final report.

- [ ] **Step 12.2: Create the banner**

Create `packages/views/workspace/banners/gitlab-pat-banner.tsx`:

```tsx
import { useState } from "react";
import { useWorkspaceGitlabConnection } from "@multica/core/gitlab/queries";
import { useUserGitlabConnection } from "@multica/core/gitlab/user-queries";
import { useNavigation } from "@multica/core/navigation";
import { Button } from "@multica/ui/components/ui/button";
import { X } from "lucide-react";

interface Props {
  workspaceId: string;
  workspaceSlug: string;
}

const STORAGE_KEY_PREFIX = "multica.gitlab-pat-banner-dismissed:";

export function GitlabPatBanner({ workspaceId, workspaceSlug }: Props) {
  const { data: wsConn } = useWorkspaceGitlabConnection(workspaceId);
  const { data: userConn } = useUserGitlabConnection(workspaceId);
  const { push } = useNavigation();

  const [dismissed, setDismissed] = useState(() => {
    try {
      return localStorage.getItem(STORAGE_KEY_PREFIX + workspaceId) === "1";
    } catch {
      return false;
    }
  });

  if (dismissed) return null;
  if (!wsConn?.gitlab_project_id) return null;
  if (userConn?.connected) return null;

  return (
    <div className="bg-muted/50 border-b border-border px-6 py-3 flex items-center justify-between gap-4">
      <p className="text-sm">
        Your writes are posting to GitLab as the workspace service account.{" "}
        <a
          className="underline cursor-pointer"
          onClick={() => push(`/${workspaceSlug}/settings`)}
        >
          Connect your GitLab account
        </a>{" "}
        so they're attributed to you.
      </p>
      <Button
        size="sm"
        variant="ghost"
        onClick={() => {
          setDismissed(true);
          try {
            localStorage.setItem(STORAGE_KEY_PREFIX + workspaceId, "1");
          } catch {
            /* ignore quota / private mode */
          }
        }}
        aria-label="Dismiss"
      >
        <X className="h-4 w-4" />
      </Button>
    </div>
  );
}
```

(`useNavigation` is the cross-app routing hook from Phase 1's NavigationAdapter. Verify the import path matches the existing usage; if it's `@multica/core/navigation` use that.)

- [ ] **Step 12.3: Create the test**

Create `packages/views/workspace/banners/gitlab-pat-banner.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { GitlabPatBanner } from "./gitlab-pat-banner";

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return {
    ...actual,
    api: {
      getWorkspaceGitlabConnection: vi.fn(),
      getUserGitlabConnection: vi.fn(),
    },
  };
});

vi.mock("@multica/core/navigation", () => ({
  useNavigation: () => ({ push: vi.fn() }),
}));

import { api } from "@multica/core/api";

function renderBanner() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <GitlabPatBanner workspaceId="ws-1" workspaceSlug="my-team" />
    </QueryClientProvider>,
  );
}

describe("GitlabPatBanner", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
  });

  it("renders when workspace connected + user not connected", async () => {
    (api.getWorkspaceGitlabConnection as any).mockResolvedValue({
      gitlab_project_id: 7,
    });
    (api.getUserGitlabConnection as any).mockResolvedValue({ connected: false });
    renderBanner();
    expect(await screen.findByText(/connect your gitlab account/i)).toBeInTheDocument();
  });

  it("hides when user is already connected", async () => {
    (api.getWorkspaceGitlabConnection as any).mockResolvedValue({
      gitlab_project_id: 7,
    });
    (api.getUserGitlabConnection as any).mockResolvedValue({ connected: true });
    const { container } = renderBanner();
    expect(container).toBeEmptyDOMElement();
  });

  it("hides when workspace is not connected", async () => {
    (api.getWorkspaceGitlabConnection as any).mockRejectedValue({ status: 404 });
    (api.getUserGitlabConnection as any).mockResolvedValue({ connected: false });
    const { container } = renderBanner();
    expect(container).toBeEmptyDOMElement();
  });
});
```

- [ ] **Step 12.4: Mount in workspace layout**

In the layout component identified in Step 12.1, add:

```tsx
import { GitlabPatBanner } from "../banners/gitlab-pat-banner";

// At the top of the dashboard chrome, before the main content:
<GitlabPatBanner workspaceId={workspaceId} workspaceSlug={workspaceSlug} />
```

- [ ] **Step 12.5: Tests + typecheck**

```bash
pnpm --filter @multica/views exec vitest run workspace/banners/gitlab-pat-banner.test.tsx
pnpm typecheck
```

- [ ] **Step 12.6: Commit**

```bash
git add packages/views/workspace/banners/gitlab-pat-banner.tsx \
        packages/views/workspace/banners/gitlab-pat-banner.test.tsx \
        packages/views/workspace/  # the layout file you modified
git commit -m "feat(views): banner prompting users to connect their personal GitLab PAT"
```

---

## Task 13: Final verification

```bash
cd /Users/jimmy.mills/Developer/multica-gitlab-phase-3a && \
  pnpm typecheck && pnpm test && \
  cd server && \
  psql "$DATABASE_URL" -c "DELETE FROM workspace WHERE slug LIKE 'gl-sync-test-%';" && \
  go test ./... 2>&1 | tail -20
```

Expected: TypeScript + Vitest clean. Go: every gitlab-related package passes; only the two pre-existing date-bucket failures from Phase 1 (`TestGetRuntimeUsage_BucketsByUsageTime`, `TestWorkspaceUsage_BucketsByUsageTime`) remain.

---

## Out of scope (Phase 3b)

- All other write endpoints (PUT/DELETE issues, batch-update/delete, comments, reactions, subscribe, attachments).
- Backfill migration for NULL `creator_id`/`author_id`/`actor_id` on existing cached rows.
- Member assignee → GitLab user_id resolution (uses `user_gitlab_connection` rows that Phase 3a creates).
- Removing the 501 stopgap for the remaining endpoints.

## Definition of done

Phase 3a is complete when:

1. A workspace member can connect their personal GitLab PAT via the new section in the workspace settings tab. The PAT is encrypted at rest.
2. A non-dismissed banner appears for users who haven't connected their PAT in workspaces that have GitLab connected.
3. `POST /api/issues` against a connected workspace creates the issue in GitLab using the user's PAT (or falls back to the service PAT when the user hasn't connected). The cache reflects the GitLab response. The UI sees the new issue immediately via the WS event Multica already publishes.
4. `POST /api/issues` against a non-connected workspace works exactly as before (legacy direct-DB path).
5. Other write endpoints continue to return 501 when the workspace is connected.
6. `make check` is green (modulo the two pre-existing date-bucket failures).
