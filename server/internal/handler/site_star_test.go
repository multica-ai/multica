package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func resetSiteStar(t *testing.T) {
	t.Helper()
	clear := func() error {
		_, err := testPool.Exec(context.Background(), `DELETE FROM site_counter WHERE name = $1`, siteStarCounter)
		return err
	}
	if err := clear(); err != nil {
		t.Fatalf("clear site_counter: %v", err)
	}
	t.Cleanup(func() { _ = clear() })
}

func siteStarTurns(t *testing.T, w *httptest.ResponseRecorder) int64 {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp siteStarResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.Turns
}

func addSiteStarTurns(body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/site/star/turns", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	testHandler.AddSiteStarTurns(w, req)
	return w
}

func TestSiteStarStartsAtZeroAndAccumulates(t *testing.T) {
	resetSiteStar(t)

	w := httptest.NewRecorder()
	testHandler.GetSiteStar(w, httptest.NewRequest("GET", "/api/site/star", nil))
	if got := siteStarTurns(t, w); got != 0 {
		t.Fatalf("expected 0 turns before any spin, got %d", got)
	}

	if got := siteStarTurns(t, addSiteStarTurns(`{"turns":3}`)); got != 3 {
		t.Fatalf("expected 3 turns after the first spin, got %d", got)
	}
	if got := siteStarTurns(t, addSiteStarTurns(`{"turns":4}`)); got != 7 {
		t.Fatalf("expected 7 turns after the second spin, got %d", got)
	}

	w = httptest.NewRecorder()
	testHandler.GetSiteStar(w, httptest.NewRequest("GET", "/api/site/star", nil))
	if got := siteStarTurns(t, w); got != 7 {
		t.Fatalf("expected the total to read back as 7, got %d", got)
	}
}

func TestSiteStarRejectsOutOfRangeTurns(t *testing.T) {
	resetSiteStar(t)

	for _, body := range []string{`{"turns":0}`, `{"turns":-5}`, `{"turns":31}`, `{}`, `not json`} {
		if w := addSiteStarTurns(body); w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: expected 400, got %d: %s", body, w.Code, w.Body.String())
		}
	}

	w := httptest.NewRecorder()
	testHandler.GetSiteStar(w, httptest.NewRequest("GET", "/api/site/star", nil))
	if got := siteStarTurns(t, w); got != 0 {
		t.Fatalf("rejected spins must not count, got %d", got)
	}
}
