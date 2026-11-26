package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

func TestDeviceAuthRouteProtection(t *testing.T) {
	for _, path := range []string{"/api/device/inspect", "/api/device/approve"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"user_code":"ABCD2345"}`))
		req.Header.Set("X-User-ID", testUserID)
		rec := httptest.NewRecorder()
		testServer.Config.Handler.ServeHTTP(rec, req)
		if rec.Code != 401 {
			t.Fatalf("%s without authentication: %d", path, rec.Code)
		}
	}
}

func TestDeviceAuthRouteRateLimits(t *testing.T) {
	testDeviceAuthLimits(t, nil)
}

func TestDeviceAuthRouteRedisRateLimits(t *testing.T) {
	raw := os.Getenv("REDIS_TEST_URL")
	if raw == "" {
		t.Skip("REDIS_TEST_URL required")
	}
	opts, err := redis.ParseURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opts)
	defer rdb.Close()
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	testDeviceAuthLimits(t, rdb)
}

func testDeviceAuthLimits(t *testing.T, rdb redis.UniversalClient) {
	t.Setenv("RATE_LIMIT_DEVICE_START", "2")
	t.Setenv("RATE_LIMIT_DEVICE_POLL", "2")
	t.Setenv("RATE_LIMIT_AUTH_VERIFY", "2")
	hub := realtime.NewHub()
	router := NewRouter(testPool, hub, events.New(), analytics.NoopClient{}, rdb)
	for i, path := range []string{"/auth/device/start", "/auth/device/token", "/api/device/inspect", "/api/device/approve"} {
		for n := 0; n < 3; n++ {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.RemoteAddr = "192.0.2." + string(rune('1'+i)) + ":19876"
			req.Header.Set("Authorization", "Bearer "+testToken)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if n < 2 && rec.Code == 429 {
				t.Fatalf("%s prematurely limited", path)
			}
			if n == 2 && rec.Code != 429 {
				t.Fatalf("%s not limited: %d", path, rec.Code)
			}
		}
	}
}

func TestDeviceAuthClientsBehindTrustedProxyHaveSeparateBudgets(t *testing.T) {
	t.Setenv("RATE_LIMIT_DEVICE_START", "2")
	t.Setenv("RATE_LIMIT_DEVICE_POLL", "2")
	t.Setenv("RATE_LIMIT_AUTH_VERIFY", "2")
	t.Setenv("RATE_LIMIT_TRUSTED_PROXIES", "10.20.0.0/24")
	router := NewRouter(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)
	for _, path := range []string{"/auth/device/start", "/auth/device/token"} {
		for _, ip := range []string{"192.0.2.20", "192.0.2.21"} {
			for n := 0; n < 3; n++ {
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
				req.RemoteAddr = "10.20.0.2:12345"
				req.Header.Set("X-Forwarded-For", ip+", 10.20.0.3")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				if (rec.Code == 429) != (n == 2) {
					t.Fatalf("%s client %s attempt %d: %d", path, ip, n, rec.Code)
				}
			}
		}
	}
}
