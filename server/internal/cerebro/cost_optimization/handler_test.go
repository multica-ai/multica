package cost_optimization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// withParams attaches chi URL params to a request so the handler can read them
// via chi.URLParam without a full router. These tests exercise the
// cerebro-specific validation paths, which all return before touching the DB,
// so a zero-value handler is sufficient and no test database is needed.
func withParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestUpsertRejectsUnknownMode(t *testing.T) {
	h := &Handler{}
	req := withParams(
		httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"mode":"maybe"}`)),
		map[string]string{"id": "11111111-1111-1111-1111-111111111111", "key": "snapshot_prompt"},
	)

	rec := httptest.NewRecorder()
	h.Upsert(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown mode: got status %d, want 400", rec.Code)
	}
}

func TestUpsertRejectsInvalidWorkspaceID(t *testing.T) {
	h := &Handler{}
	req := withParams(
		httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"mode":"on"}`)),
		map[string]string{"id": "not-a-uuid", "key": "snapshot_prompt"},
	)

	rec := httptest.NewRecorder()
	h.Upsert(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid workspace id: got status %d, want 400", rec.Code)
	}
}

func TestListRejectsInvalidWorkspaceID(t *testing.T) {
	h := &Handler{}
	req := withParams(
		httptest.NewRequest(http.MethodGet, "/", nil),
		map[string]string{"id": "not-a-uuid"},
	)

	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid workspace id: got status %d, want 400", rec.Code)
	}
}

func TestDeleteRejectsMissingKey(t *testing.T) {
	h := &Handler{}
	req := withParams(
		httptest.NewRequest(http.MethodDelete, "/", nil),
		map[string]string{"id": "11111111-1111-1111-1111-111111111111"},
	)

	rec := httptest.NewRecorder()
	h.Delete(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing key: got status %d, want 400", rec.Code)
	}
}
