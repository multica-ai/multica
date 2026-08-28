package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const testResolverSlug = "middleware-resolver-test"

// openPool returns a connected pgxpool, or skips the test if the database is
// unreachable. Mirrors the handler package's fixture approach so tests don't
// require a DB in environments where one isn't available.
func openPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("skipping: could not connect to database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("skipping: database not reachable: %v", err)
	}
	return pool
}

// setupResolverFixture inserts a workspace with a known slug and returns its
// UUID. The caller is responsible for calling the returned cleanup func.
func setupResolverFixture(t *testing.T, pool *pgxpool.Pool) (workspaceID string, cleanup func()) {
	return setupResolverFixtureWithSlug(t, pool, testResolverSlug)
}

func setupResolverFixtureWithSlug(t *testing.T, pool *pgxpool.Pool, slug string) (workspaceID string, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	// Pre-cleanup in case a previous run didn't finish.
	_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	if err := pool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1, $2, '', 'MRT') RETURNING id`,
		"Middleware Resolver Test", slug,
	).Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	return workspaceID, func() {
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	}
}

// TestResolveWorkspaceIDFromRequest pins down the priority order of the
// shared resolver. Every handler-level lookup of workspace identity — whether
// a route sits inside or outside the workspace middleware — must produce
// identical results, in the same priority, across all five supported
// mechanisms. Breaking any row here is a behavioral regression.
func TestResolveWorkspaceIDFromRequest(t *testing.T) {
	pool := openPool(t)
	defer pool.Close()
	queries := db.New(pool)

	workspaceID, cleanup := setupResolverFixture(t, pool)
	defer cleanup()

	const (
		uuidA = "00000000-0000-0000-0000-000000000001"
		uuidB = "00000000-0000-0000-0000-000000000002"
	)

	cases := []struct {
		name      string
		setup     func(r *http.Request)
		want      string
		wantEmpty bool
	}{
		{
			name: "context UUID wins over everything else",
			setup: func(r *http.Request) {
				ctx := context.WithValue(r.Context(), ctxKeyWorkspaceID, uuidA)
				*r = *r.WithContext(ctx)
				r.Header.Set("X-Workspace-Slug", testResolverSlug)
				r.Header.Set("X-Workspace-ID", uuidB)
			},
			want: uuidA,
		},
		{
			name: "X-Workspace-Slug header resolves to UUID via DB lookup",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-Slug", testResolverSlug)
			},
			want: workspaceID,
		},
		{
			name: "X-Workspace-Slug wins over X-Workspace-ID (post-refactor priority)",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-Slug", testResolverSlug)
				r.Header.Set("X-Workspace-ID", uuidB)
			},
			want: workspaceID,
		},
		{
			name: "unknown X-Workspace-Slug falls through to UUID header",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-Slug", "does-not-exist")
				r.Header.Set("X-Workspace-ID", uuidB)
			},
			want: uuidB,
		},
		{
			name: "?workspace_slug query resolves to UUID via DB lookup",
			setup: func(r *http.Request) {
				q := r.URL.Query()
				q.Set("workspace_slug", testResolverSlug)
				r.URL.RawQuery = q.Encode()
			},
			want: workspaceID,
		},
		{
			name: "X-Workspace-ID header is returned when no slug provided",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-ID", uuidA)
			},
			want: uuidA,
		},
		{
			name: "?workspace_id query is the last-resort fallback",
			setup: func(r *http.Request) {
				q := r.URL.Query()
				q.Set("workspace_id", uuidA)
				r.URL.RawQuery = q.Encode()
			},
			want: uuidA,
		},
		{
			name:      "no identifier at all returns empty",
			setup:     func(r *http.Request) {},
			wantEmpty: true,
		},
		{
			name: "unknown slug with no UUID fallback returns empty",
			setup: func(r *http.Request) {
				r.Header.Set("X-Workspace-Slug", "does-not-exist")
			},
			wantEmpty: true,
		},
		{
			// MUL-2600: a mat_ task token authenticates the request and
			// the auth middleware writes the token-bound workspace into
			// X-Workspace-ID along with X-Actor-Source=task_token. Any
			// other workspace identifier the agent puts on the wire — a
			// slug pointing at a sibling workspace, a different
			// workspace_id — must be ignored. Otherwise an agent could
			// route owner-token traffic at any workspace its host is
			// also a member of.
			name: "task_token actor: client-supplied slug/id cannot override token-bound workspace",
			setup: func(r *http.Request) {
				r.Header.Set("X-Actor-Source", "task_token")
				r.Header.Set("X-Workspace-ID", uuidA)
				// All of these should be ignored under task_token.
				r.Header.Set("X-Workspace-Slug", testResolverSlug)
				q := r.URL.Query()
				q.Set("workspace_slug", testResolverSlug)
				q.Set("workspace_id", uuidB)
				r.URL.RawQuery = q.Encode()
			},
			want: uuidA,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/anything", nil)
			tc.setup(req)

			got := ResolveWorkspaceIDFromRequest(req, queries)

			if tc.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty, got %q", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestWorkspaceSlugCacheExpiresEntries(t *testing.T) {
	cache := newWorkspaceSlugCache(time.Minute)
	cache.set("acme", "00000000-0000-0000-0000-000000000001")

	if got, ok := cache.get("acme"); !ok || got == "" {
		t.Fatalf("expected cached entry, got %q ok=%v", got, ok)
	}

	cache.mu.Lock()
	entry := cache.entries["acme"]
	entry.expiresAt = time.Now().Add(-time.Second)
	cache.entries["acme"] = entry
	cache.mu.Unlock()

	if got, ok := cache.get("acme"); ok {
		t.Fatalf("expected expired entry to miss, got %q", got)
	}
	if _, ok := cache.entries["acme"]; ok {
		t.Fatal("expected expired entry to be removed")
	}
}

func TestResolveWorkspaceUUIDCachesSlugLookup(t *testing.T) {
	pool := openPool(t)
	defer pool.Close()
	queries := db.New(pool)

	const slug = "middleware-resolver-cache-hit"
	workspaceID, cleanup := setupResolverFixtureWithSlug(t, pool, slug)
	defer cleanup()

	cache := newWorkspaceSlugCache(time.Minute)
	resolve := resolveWorkspaceUUIDWithCache(queries, cache)

	req := httptest.NewRequest("GET", "/api/anything", nil)
	q := req.URL.Query()
	q.Set("workspace_slug", slug)
	req.URL.RawQuery = q.Encode()

	got, err := resolve(req)
	if err != nil {
		t.Fatalf("first resolve returned error: %v", err)
	}
	if got != workspaceID {
		t.Fatalf("expected %q, got %q", workspaceID, got)
	}

	_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE slug = $1`, slug)

	got, err = resolve(req)
	if err != nil {
		t.Fatalf("cached resolve returned error: %v", err)
	}
	if got != workspaceID {
		t.Fatalf("expected cached %q, got %q", workspaceID, got)
	}
}

func TestResolveWorkspaceUUIDDoesNotCacheMiss(t *testing.T) {
	pool := openPool(t)
	defer pool.Close()
	queries := db.New(pool)

	const slug = "middleware-resolver-cache-miss"
	_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE slug = $1`, slug)

	cache := newWorkspaceSlugCache(time.Minute)
	resolve := resolveWorkspaceUUIDWithCache(queries, cache)

	req := httptest.NewRequest("GET", "/api/anything", nil)
	req.Header.Set("X-Workspace-Slug", slug)

	if got, err := resolve(req); err != errWorkspaceNotFound {
		t.Fatalf("expected workspace not found, got id %q error %v", got, err)
	}

	workspaceID, cleanup := setupResolverFixtureWithSlug(t, pool, slug)
	defer cleanup()

	got, err := resolve(req)
	if err != nil {
		t.Fatalf("resolve after creating workspace returned error: %v", err)
	}
	if got != workspaceID {
		t.Fatalf("expected %q, got %q", workspaceID, got)
	}
}

func TestResolveWorkspaceUUIDTaskTokenBypassesSlugCache(t *testing.T) {
	const slug = "middleware-resolver-task-token-cache"

	cache := newWorkspaceSlugCache(time.Minute)
	resolve := resolveWorkspaceUUIDWithCache(nil, cache)

	const boundWorkspaceID = "00000000-0000-0000-0000-000000000001"
	req := httptest.NewRequest("GET", "/api/anything", nil)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Workspace-ID", boundWorkspaceID)
	req.Header.Set("X-Workspace-Slug", slug)

	got, err := resolve(req)
	if err != nil {
		t.Fatalf("task token resolve returned error: %v", err)
	}
	if got != boundWorkspaceID {
		t.Fatalf("expected bound workspace %q, got %q", boundWorkspaceID, got)
	}
	if cached, ok := cache.get(slug); ok {
		t.Fatalf("task token path should not populate slug cache, got %q", cached)
	}
}
