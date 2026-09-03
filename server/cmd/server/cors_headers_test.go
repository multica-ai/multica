package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/realtime"
)

func TestRouterCORSContract(t *testing.T) {
	const origin = "https://cors-client.example"
	t.Setenv("CORS_ALLOWED_ORIGINS", origin)
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)

	t.Run("preflight accepts browser request headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/config", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		req.Header.Set("Access-Control-Request-Headers", "X-Client-Capabilities, Idempotency-Key")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("preflight status = %d, want %d", rec.Code, http.StatusOK)
		}
		for _, want := range []string{"X-Client-Capabilities", "Idempotency-Key"} {
			if !headerListContains(rec.Header().Get("Access-Control-Allow-Headers"), want) {
				t.Errorf("Access-Control-Allow-Headers = %q, missing %q", rec.Header().Get("Access-Control-Allow-Headers"), want)
			}
		}
	})

	t.Run("browser can read truncation signals", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		for _, want := range []string{
			handler.HeaderCommentsTruncated,
			handler.HeaderTimelineTruncated,
			handler.HeaderActiveRunsTruncated,
		} {
			if !headerListContains(rec.Header().Get("Access-Control-Expose-Headers"), want) {
				t.Errorf("Access-Control-Expose-Headers = %q, missing %q", rec.Header().Get("Access-Control-Expose-Headers"), want)
			}
		}
	})
}

func headerListContains(header, want string) bool {
	for _, value := range strings.Split(header, ",") {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}
