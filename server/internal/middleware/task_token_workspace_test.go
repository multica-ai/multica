package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fixtures for the GitHub #9403 regression tests. The token is bound to
// workspace A; B is a different workspace the agent's user can see.
const (
	ttTestToken   = "mat_test_9403_token"
	ttWorkspaceA  = "0a0a0a0a-0000-4000-8000-00000000000a"
	ttWorkspaceB  = "0b0b0b0b-0000-4000-8000-00000000000b"
	ttUserID      = "11111111-0000-4000-8000-000000000001"
	ttAgentID     = "22222222-0000-4000-8000-000000000002"
	ttTaskID      = "33333333-0000-4000-8000-000000000003"
	ttSlugA       = "ws-a"
	ttSlugB       = "ws-b"
	ttSlugDBError = "ws-db-error"
)

// fakeTaskTokenDB is a minimal db.DBTX that answers the two queries the
// task-token auth path issues: GetTaskTokenByHash and GetWorkspaceBySlug.
// Anything else fails the test, so an unexpected query on this path is
// caught rather than silently returning zero values.
type fakeTaskTokenDB struct {
	t *testing.T
}

func (f *fakeTaskTokenDB) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	f.t.Errorf("unexpected Exec on task-token auth path")
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (f *fakeTaskTokenDB) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	f.t.Errorf("unexpected Query on task-token auth path")
	return nil, errors.New("unexpected Query")
}

func (f *fakeTaskTokenDB) QueryRow(_ context.Context, sql string, args ...interface{}) pgx.Row {
	switch {
	case strings.Contains(sql, "name: GetTaskTokenByHash"):
		if len(args) != 1 || args[0] != auth.HashToken(ttTestToken) {
			return fakeRow{err: pgx.ErrNoRows}
		}
		return fakeRow{scan: func(dest ...any) {
			*dest[0].(*pgtype.UUID) = util.MustParseUUID("44444444-0000-4000-8000-000000000004")
			*dest[2].(*pgtype.UUID) = util.MustParseUUID(ttTaskID)
			*dest[3].(*pgtype.UUID) = util.MustParseUUID(ttAgentID)
			*dest[4].(*pgtype.UUID) = util.MustParseUUID(ttWorkspaceA)
			*dest[5].(*pgtype.UUID) = util.MustParseUUID(ttUserID)
		}}
	case strings.Contains(sql, "name: GetWorkspaceBySlug"):
		var id string
		switch args[0] {
		case ttSlugA:
			id = ttWorkspaceA
		case ttSlugB:
			id = ttWorkspaceB
		case ttSlugDBError:
			return fakeRow{err: errors.New("connection reset")}
		default:
			return fakeRow{err: pgx.ErrNoRows}
		}
		return fakeRow{scan: func(dest ...any) {
			*dest[0].(*pgtype.UUID) = util.MustParseUUID(id)
		}}
	}
	f.t.Errorf("unexpected QueryRow on task-token auth path: %s", sql)
	return fakeRow{err: errors.New("unexpected QueryRow")}
}

type fakeRow struct {
	err  error
	scan func(dest ...any)
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	r.scan(dest...)
	return nil
}

// TestAuth_TaskTokenWorkspaceSelectors drives the real Auth middleware with a
// mat_ token bound to workspace A and asserts that an explicit selector for
// any other workspace is refused before the handler runs, instead of being
// silently replaced by A (GitHub #9403).
func TestAuth_TaskTokenWorkspaceSelectors(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		target     string
		headers    map[string][]string
		wantStatus int
	}{
		// Allowed: no selector, or selectors that all name A.
		{name: "no selector", method: "GET", target: "/api/projects", wantStatus: http.StatusOK},
		{name: "header id same", method: "GET", target: "/api/projects",
			headers: map[string][]string{"X-Workspace-ID": {ttWorkspaceA}}, wantStatus: http.StatusOK},
		{name: "header id same, uppercase", method: "GET", target: "/api/projects",
			headers: map[string][]string{"X-Workspace-ID": {strings.ToUpper(ttWorkspaceA)}}, wantStatus: http.StatusOK},
		{name: "query id same", method: "GET", target: "/api/projects?workspace_id=" + ttWorkspaceA, wantStatus: http.StatusOK},
		{name: "header slug same", method: "GET", target: "/api/projects",
			headers: map[string][]string{"X-Workspace-Slug": {ttSlugA}}, wantStatus: http.StatusOK},
		{name: "query slug same", method: "GET", target: "/api/projects?workspace_slug=" + ttSlugA, wantStatus: http.StatusOK},
		{name: "all selectors same", method: "POST",
			target:     "/api/issues?workspace_id=" + ttWorkspaceA + "&workspace_slug=" + ttSlugA,
			headers:    map[string][]string{"X-Workspace-ID": {ttWorkspaceA}, "X-Workspace-Slug": {ttSlugA}},
			wantStatus: http.StatusOK},
		{name: "empty query id is no selector", method: "GET", target: "/api/projects?workspace_id=", wantStatus: http.StatusOK},

		// Conflicts: each selector on its own.
		{name: "header id conflict", method: "GET", target: "/api/projects",
			headers: map[string][]string{"X-Workspace-ID": {ttWorkspaceB}}, wantStatus: http.StatusForbidden},
		{name: "query id conflict", method: "GET", target: "/api/projects?workspace_id=" + ttWorkspaceB, wantStatus: http.StatusForbidden},
		{name: "header slug conflict", method: "GET", target: "/api/projects",
			headers: map[string][]string{"X-Workspace-Slug": {ttSlugB}}, wantStatus: http.StatusForbidden},
		{name: "query slug conflict", method: "GET", target: "/api/projects?workspace_slug=" + ttSlugB, wantStatus: http.StatusForbidden},

		// Contradicting selectors and duplicated parameters.
		{name: "header A, query B", method: "GET", target: "/api/projects?workspace_id=" + ttWorkspaceB,
			headers: map[string][]string{"X-Workspace-ID": {ttWorkspaceA}}, wantStatus: http.StatusForbidden},
		{name: "id A, slug B", method: "GET", target: "/api/projects",
			headers: map[string][]string{"X-Workspace-ID": {ttWorkspaceA}, "X-Workspace-Slug": {ttSlugB}}, wantStatus: http.StatusForbidden},
		{name: "duplicate query id A then B", method: "GET",
			target: "/api/projects?workspace_id=" + ttWorkspaceA + "&workspace_id=" + ttWorkspaceB, wantStatus: http.StatusForbidden},
		{name: "duplicate header id A then B", method: "GET", target: "/api/projects",
			headers: map[string][]string{"X-Workspace-ID": {ttWorkspaceA, ttWorkspaceB}}, wantStatus: http.StatusForbidden},
		{name: "duplicate query slug A then B", method: "GET",
			target: "/api/projects?workspace_slug=" + ttSlugA + "&workspace_slug=" + ttSlugB, wantStatus: http.StatusForbidden},

		// Unresolvable selectors never fall back to A.
		{name: "malformed header id", method: "GET", target: "/api/projects",
			headers: map[string][]string{"X-Workspace-ID": {"not-a-uuid"}}, wantStatus: http.StatusBadRequest},
		{name: "malformed query id", method: "GET", target: "/api/projects?workspace_id=nope", wantStatus: http.StatusBadRequest},
		{name: "unknown slug", method: "GET", target: "/api/projects",
			headers: map[string][]string{"X-Workspace-Slug": {"does-not-exist"}}, wantStatus: http.StatusNotFound},
		{name: "slug lookup db error", method: "GET", target: "/api/projects?workspace_slug=" + ttSlugDBError,
			wantStatus: http.StatusInternalServerError},

		// Query strings that url.Query() would partially drop must be
		// refused outright, not read as "no selector" (PR #9422 review).
		{name: "bad escape in workspace_id", method: "POST",
			target: "/api/projects?workspace_id=" + ttWorkspaceB + "%zz", wantStatus: http.StatusBadRequest},
		{name: "bad escape in workspace_slug", method: "POST",
			target: "/api/projects?workspace_slug=ws-b%zz", wantStatus: http.StatusBadRequest},
		{name: "unencoded semicolon", method: "POST",
			target: "/api/projects?workspace_id=" + ttWorkspaceB + ";ignored=1", wantStatus: http.StatusBadRequest},
		{name: "valid A then malformed B", method: "POST",
			target:     "/api/projects?workspace_id=" + ttWorkspaceA + "&workspace_id=" + ttWorkspaceB + "%zz",
			wantStatus: http.StatusBadRequest},
		{name: "malformed non-selector param", method: "POST",
			target: "/api/projects?workspace_id=" + ttWorkspaceA + "&q=%zz", wantStatus: http.StatusBadRequest},
		{name: "encoded values still accepted", method: "GET",
			target: "/api/projects?workspace_slug=ws%2Da&q=a%20b", wantStatus: http.StatusOK},

		// Writes, including an auth-only route that never reaches the
		// workspace middleware.
		{name: "POST conflict", method: "POST", target: "/api/issues",
			headers: map[string][]string{"X-Workspace-ID": {ttWorkspaceB}}, wantStatus: http.StatusForbidden},
		{name: "PATCH conflict", method: "PATCH", target: "/api/issues/x?workspace_slug=" + ttSlugB, wantStatus: http.StatusForbidden},
		{name: "upload conflict", method: "POST", target: "/api/upload-file",
			headers: map[string][]string{"X-Workspace-ID": {ttWorkspaceB}}, wantStatus: http.StatusForbidden},
		{name: "upload same", method: "POST", target: "/api/upload-file",
			headers: map[string][]string{"X-Workspace-ID": {ttWorkspaceA}}, wantStatus: http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			queries := db.New(&fakeTaskTokenDB{t: t})
			var called bool
			var gotWorkspace, gotSource string
			handler := Auth(queries, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				gotWorkspace = r.Header.Get("X-Workspace-ID")
				gotSource = r.Header.Get("X-Actor-Source")
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(tc.method, tc.target, nil)
			req.Header.Set("Authorization", "Bearer "+ttTestToken)
			for k, vs := range tc.headers {
				for _, v := range vs {
					req.Header.Add(k, v)
				}
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantStatus != http.StatusOK {
				if called {
					t.Fatalf("handler must not run when the selector is rejected")
				}
				if tc.wantStatus == http.StatusForbidden && !strings.Contains(w.Body.String(), errTaskTokenWorkspaceMismatch) {
					t.Fatalf("403 body = %s, want %q", w.Body.String(), errTaskTokenWorkspaceMismatch)
				}
				return
			}
			if !called {
				t.Fatalf("handler was not called")
			}
			// Accepted requests still carry the token's binding, in
			// canonical form, as the single X-Workspace-ID value.
			if gotWorkspace != ttWorkspaceA {
				t.Fatalf("X-Workspace-ID = %q, want bound workspace %q", gotWorkspace, ttWorkspaceA)
			}
			if gotSource != "task_token" {
				t.Fatalf("X-Actor-Source = %q, want task_token", gotSource)
			}
		})
	}
}

// TestAuth_JWTWorkspaceSelectorUntouched is the control for the task-token
// check: human JWT sessions keep choosing their workspace through headers,
// and Auth must neither validate nor rewrite that choice.
func TestAuth_JWTWorkspaceSelectorUntouched(t *testing.T) {
	var gotWorkspace string
	handler := Auth(nil, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotWorkspace = r.Header.Get("X-Workspace-ID")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/projects?workspace_slug=anything", nil)
	req.Header.Set("Authorization", "Bearer "+generateToken(validClaims(), auth.JWTSecret()))
	req.Header.Set("X-Workspace-ID", ttWorkspaceB)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", w.Code, w.Body.String())
	}
	if gotWorkspace != ttWorkspaceB {
		t.Fatalf("X-Workspace-ID = %q, want client value %q", gotWorkspace, ttWorkspaceB)
	}
}
