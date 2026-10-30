package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	publicapiv1 "github.com/multica-ai/multica/server/pkg/publicapi/v1"
)

func TestPluginRateLimitIsPerCredentialAndUsesStableProblem(t *testing.T) {
	rdb := newRedisTestClient(t)
	handler := PluginRateLimit(rdb, 1, time.Minute)(okHandler)

	call := func(token string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/v1/issues/MUL-1", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	if response := call("mpi_first-secret"); response.Code != http.StatusOK {
		t.Fatalf("first credential call status = %d", response.Code)
	}
	limited := call("mpi_first-secret")
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") != "60" {
		t.Fatalf("limited response status=%d retry-after=%q body=%s", limited.Code, limited.Header().Get("Retry-After"), limited.Body.String())
	}
	var problem publicapiv1.Problem
	if err := json.Unmarshal(limited.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "rate_limited" || problem.RequestID == "" || problem.Error == "" {
		t.Fatalf("unexpected problem: %+v", problem)
	}
	if response := call("mpi_second-secret"); response.Code != http.StatusOK {
		t.Fatalf("different credential shared budget: status=%d", response.Code)
	}

	keys, err := rdb.Keys(context.Background(), "mul:ratelimit:plugin:*").Result()
	if err != nil {
		t.Fatalf("list limiter keys: %v", err)
	}
	for _, key := range keys {
		if strings.Contains(key, "first-secret") || strings.Contains(key, "second-secret") {
			t.Fatalf("credential leaked into rate-limit key: %q", key)
		}
	}
}

func TestPluginRateLimitWithoutRedisFailsOpen(t *testing.T) {
	handler := PluginRateLimit(nil, 1, time.Minute)(okHandler)
	request := httptest.NewRequest(http.MethodGet, "/v1/context", nil)
	request.Header.Set("Authorization", "Bearer mpi_local")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("nil Redis status = %d", response.Code)
	}
}
