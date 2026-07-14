package main

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

func TestIssueUpsertExternalRequiresAuthentication(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/issues/upsert-external", strings.NewReader(`{"aliases":[{"namespace":"taskthreads","external_id":"1"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s, want 401", rec.Code, rec.Body.String())
	}
}

func TestAuthorityAttestIsPublicNoCookieNot401(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)
	nonce := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	req := httptest.NewRequest(http.MethodPost, "/api/authority/attest", strings.NewReader(`{"nonce":"`+nonce+`"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	resp := rec.Result()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("attest returned 401 without a session; it is behind Auth. body=%s", body)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s, want disabled public handler 503", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}
