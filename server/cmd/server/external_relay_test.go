package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/redis/go-redis/v9"
)

// A second PAT consumer must not turn relay credentials into human authority.
func TestExternalRelayCannotAuthenticateDaemonOrWebSocket(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	raw, err := auth.GeneratePATToken()
	if err != nil {
		t.Fatal(err)
	}
	pat, err := queries.CreatePersonalAccessToken(ctx, db.CreatePersonalAccessTokenParams{UserID: parseUUID(testUserID), Name: "relay-consumer-test", TokenHash: auth.HashToken(raw), TokenPrefix: raw[:12], IsExternalRelay: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM personal_access_token WHERE id=$1`, pat.ID) })
	rdb := redis.NewClient(&redis.Options{Addr: "unused"})
	rdb.AddHook(&relayCacheHook{values: map[string]string{}})
	defer rdb.Close()
	cache := auth.NewPATCache(rdb)
	resolver := &patResolver{queries: queries, cache: cache}
	// Prove the same cache really can serve ordinary human credentials.
	cache.Set(ctx, "positive-control", testUserID, time.Minute)
	if user, ok := cache.Get(ctx, "positive-control"); !ok || user != testUserID {
		t.Fatal("cache fixture is inactive")
	}
	for i := 0; i < 2; i++ {
		if _, ok := resolver.ResolveToken(ctx, raw); ok {
			t.Fatal("relay credential authenticated websocket")
		}
		req := httptest.NewRequest("POST", "/api/daemon/runtimes/x/claim", nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		w := httptest.NewRecorder()
		middleware.DaemonAuth(queries, cache, nil, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("relay credential authenticated daemon") })).ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("daemon: %d", w.Code)
		}
		req = httptest.NewRequest("POST", "/api/issues", nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		w = httptest.NewRecorder()
		middleware.Auth(queries, cache, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("relay credential escaped after alternate auth") })).ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("auth: %d", w.Code)
		}
		if _, ok := cache.Get(ctx, auth.HashToken(raw)); ok {
			t.Fatal("relay poisoned human PAT cache")
		}
	}
}

// Exercise the real PATCache with an in-memory Redis transport; no network or
// deployment infrastructure is required for the cross-auth cache regression.
type relayCacheHook struct{ values map[string]string }

func (h *relayCacheHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return nil, fmt.Errorf("unexpected Redis dial")
	}
}
func (h *relayCacheHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		args := cmd.Args()
		key := fmt.Sprint(args[1])
		switch cmd.Name() {
		case "get":
			val, ok := h.values[key]
			if !ok {
				cmd.SetErr(redis.Nil)
				return redis.Nil
			}
			cmd.(*redis.StringCmd).SetVal(val)
		case "set":
			h.values[key] = fmt.Sprint(args[2])
			cmd.(*redis.StatusCmd).SetVal("OK")
		default:
			return fmt.Errorf("unexpected Redis command %s", cmd.Name())
		}
		return nil
	}
}
func (h *relayCacheHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
