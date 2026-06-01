package cost_optimization

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests exercise the cerebro-specific validation paths, which all return
// before touching the DB, so a zero-value handler is sufficient (mirrors
// handler_test.go).

func TestUpsertHoldoutRejectsOutOfRange(t *testing.T) {
	for _, body := range []string{`{"holdout_pct":-1}`, `{"holdout_pct":101}`} {
		h := &Handler{}
		req := withParams(
			httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)),
			map[string]string{"id": "11111111-1111-1111-1111-111111111111", "key": "model_routing"},
		)
		rec := httptest.NewRecorder()
		h.UpsertHoldout(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: got status %d, want 400", body, rec.Code)
		}
	}
}

func TestUpsertHoldoutRejectsInvalidBody(t *testing.T) {
	h := &Handler{}
	req := withParams(
		httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`not-json`)),
		map[string]string{"id": "11111111-1111-1111-1111-111111111111", "key": "model_routing"},
	)
	rec := httptest.NewRecorder()
	h.UpsertHoldout(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid body: got status %d, want 400", rec.Code)
	}
}

func TestUpsertHoldoutRejectsMissingKey(t *testing.T) {
	h := &Handler{}
	req := withParams(
		httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"holdout_pct":20}`)),
		map[string]string{"id": "11111111-1111-1111-1111-111111111111"},
	)
	rec := httptest.NewRecorder()
	h.UpsertHoldout(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing key: got status %d, want 400", rec.Code)
	}
}

func TestUpsertHoldoutRejectsInvalidWorkspaceID(t *testing.T) {
	h := &Handler{}
	req := withParams(
		httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"holdout_pct":20}`)),
		map[string]string{"id": "not-a-uuid", "key": "model_routing"},
	)
	rec := httptest.NewRecorder()
	h.UpsertHoldout(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid workspace id: got status %d, want 400", rec.Code)
	}
}

func TestListHoldoutRejectsInvalidWorkspaceID(t *testing.T) {
	h := &Handler{}
	req := withParams(
		httptest.NewRequest(http.MethodGet, "/", nil),
		map[string]string{"id": "not-a-uuid"},
	)
	rec := httptest.NewRecorder()
	h.ListHoldout(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid workspace id: got status %d, want 400", rec.Code)
	}
}

func TestDeleteHoldoutRejectsMissingKey(t *testing.T) {
	h := &Handler{}
	req := withParams(
		httptest.NewRequest(http.MethodDelete, "/", nil),
		map[string]string{"id": "11111111-1111-1111-1111-111111111111"},
	)
	rec := httptest.NewRecorder()
	h.DeleteHoldout(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing key: got status %d, want 400", rec.Code)
	}
}

func TestDeleteHoldoutRejectsInvalidWorkspaceID(t *testing.T) {
	h := &Handler{}
	req := withParams(
		httptest.NewRequest(http.MethodDelete, "/", nil),
		map[string]string{"id": "not-a-uuid", "key": "model_routing"},
	)
	rec := httptest.NewRecorder()
	h.DeleteHoldout(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid workspace id: got status %d, want 400", rec.Code)
	}
}
