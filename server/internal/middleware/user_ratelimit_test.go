package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUserRateLimitIsPerUser(t *testing.T) {
	rdb := newRedisTestClient(t)
	handler := UserRateLimit(rdb, "voice", 2, time.Minute)(okHandler)

	send := func(userID string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/voice/transcriptions", nil)
		if userID != "" {
			req.Header.Set("X-User-ID", userID)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	for i := 0; i < 2; i++ {
		if code := send("user-a"); code != http.StatusOK {
			t.Fatalf("request %d for user-a: got %d", i+1, code)
		}
	}
	if code := send("user-a"); code != http.StatusTooManyRequests {
		t.Fatalf("third request for user-a: got %d, want 429", code)
	}
	if code := send("user-b"); code != http.StatusOK {
		t.Fatalf("user-b must have its own budget: got %d", code)
	}
	if code := send(""); code != http.StatusOK {
		t.Fatalf("a request without a user is left to the handler: got %d", code)
	}
}

func TestUserRateLimitNilRedisPassesThrough(t *testing.T) {
	handler := UserRateLimit(nil, "voice", 1, time.Minute)(okHandler)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/voice/transcriptions", nil)
		req.Header.Set("X-User-ID", "user-a")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("nil redis should pass through; got %d", rec.Code)
		}
	}
}
