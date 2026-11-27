package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUserRateLimitSeparatesAccountsAndIgnoresAddress(t *testing.T) {
	seen := map[string]int{}
	h := UserRateLimit(func(_ context.Context, key string) bool { seen[key]++; return seen[key] <= 1 })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for _, tc := range []struct {
		user, ip string
		want     int
	}{{"a", "192.0.2.1:12", 200}, {"b", "192.0.2.1:12", 200}, {"a", "192.0.2.2:12", 429}} {
		req := httptest.NewRequest(http.MethodPost, "/api/device/approve", nil)
		req.RemoteAddr = tc.ip
		req.Header.Set("X-User-ID", tc.user)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatal(tc, rec.Code)
		}
	}
}
