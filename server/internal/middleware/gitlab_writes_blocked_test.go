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

// fakeQueries implements just the one method the middleware needs.
type fakeQueries struct {
	hasConn bool
}

func (q *fakeQueries) GetWorkspaceGitlabConnection(ctx context.Context, _ pgtype.UUID) (db.WorkspaceGitlabConnection, error) {
	if q.hasConn {
		return db.WorkspaceGitlabConnection{}, nil
	}
	return db.WorkspaceGitlabConnection{}, pgx.ErrNoRows
}

func withURLParamMiddleware(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestGitlabWritesBlocked_AllowsAllMethodsForUnconnectedWorkspaces(t *testing.T) {
	var nextCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nextCalled = true; w.WriteHeader(http.StatusOK) })

	h := GitlabWritesBlocked(&fakeQueries{hasConn: false})(next)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		nextCalled = false
		req := httptest.NewRequest(method, "/api/issues", nil)
		req = withURLParamMiddleware(req, "id", "00000000-0000-0000-0000-000000000000")
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
		req = withURLParamMiddleware(req, "id", "00000000-0000-0000-0000-000000000000")
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
	req = withURLParamMiddleware(req, "id", "00000000-0000-0000-0000-000000000000")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !nextCalled {
		t.Errorf("GET should pass through even with connection, got %d", rr.Code)
	}
}
