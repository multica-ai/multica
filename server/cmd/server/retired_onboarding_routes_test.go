package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

func TestRetiredOnboardingRoutesAreNotRegistered(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)

	retired := []struct {
		method string
		path   string
	}{
		{http.MethodPatch, "/api/me/onboarding"},
		{http.MethodPost, "/api/me/onboarding/complete"},
		{http.MethodPost, "/api/me/onboarding/cloud-waitlist"},
		{http.MethodPost, "/api/me/onboarding/runtime-bootstrap"},
		{http.MethodPost, "/api/me/onboarding/no-runtime-bootstrap"},
		{http.MethodPost, "/api/agents/mika"},
		{http.MethodPost, "/api/chat/sessions/00000000-0000-0000-0000-000000000000/onboarding"},
	}

	if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		for _, candidate := range retired {
			pattern := strings.ReplaceAll(candidate.path, "00000000-0000-0000-0000-000000000000", "{sessionId}")
			if method == candidate.method && route == pattern {
				t.Errorf("retired route is still registered: %s %s", method, route)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("walk router: %v", err)
	}

	for _, route := range retired {
		t.Run(route.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(route.method, route.path, nil)
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s status = %d, want %d", route.path, rec.Code, http.StatusNotFound)
			}
		})
	}
}
