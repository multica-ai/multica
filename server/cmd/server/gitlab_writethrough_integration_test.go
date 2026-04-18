package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/pkg/gitlab"
	"github.com/multica-ai/multica/server/pkg/secrets"
)

// TestGitlabWriteThrough_RealProductionWiring verifies that when the server is
// booted with gitlabEnabled=true (the way main.go wires it), the write-through
// path in POST /api/issues actually makes a REST call to GitLab. This is the
// regression test for the blocker where NewRouter was called with a nil
// resolver, so every POST /api/issues silently fell through to the legacy
// direct-DB path in production even though unit tests (which manually wire
// the resolver via SetGitlabResolver) passed.
//
// The assertion is deliberately observational: we spy on the fake GitLab
// server and confirm it received POST /api/v4/projects/<id>/issues. A local
// issue row without that HTTP call means we've regressed back to the legacy
// path.
func TestGitlabWriteThrough_RealProductionWiring(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	// ---- Fake GitLab REST server ----
	var gitlabHits int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v4/projects/4242/issues" && r.Method == http.MethodPost:
			atomic.AddInt32(&gitlabHits, 1)
			w.Write([]byte(`{
				"id": 990001,
				"iid": 777,
				"title": "Production wiring test",
				"description": "",
				"state": "opened",
				"labels": ["status::todo", "priority::medium"],
				"updated_at": "2026-04-17T15:00:00Z"
			}`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer fake.Close()

	// ---- Per-test fixture: its own workspace + user + member ----
	const (
		email = "gitlab-writethrough-wiring@multica.ai"
		name  = "GitLab Writethrough Tester"
		slug  = "gitlab-writethrough-wiring"
	)

	// Clean any leftover rows from a previous run before we start.
	cleanup := func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	}
	cleanup()
	t.Cleanup(cleanup)

	var userID, workspaceID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		name, email,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, $3)
		RETURNING id
	`, "GitLab Writethrough Wiring", slug, "regression test workspace").Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}

	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		workspaceID, userID,
	); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	// ---- Seed a connected workspace_gitlab_connection. No user PAT for the
	// caller → resolver must pick the service PAT, and the write-through path
	// must actually fire (i.e. we must see an outbound /api/v4 call). ----
	key := make([]byte, 32) // fixed zero key is fine in tests
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	svcEnc, err := cipher.Encrypt([]byte("service-pat-prod-wiring"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 4242, 'team/prod-wiring', $2, 1, 'connected')
	`, workspaceID, svcEnc); err != nil {
		t.Fatalf("insert workspace_gitlab_connection: %v", err)
	}

	// ---- Boot the real router EXACTLY the way main.go does. This is the
	// production wiring check: we don't touch SetGitlabResolver manually. If
	// NewRouterWithHandler forgets to wire it, the test MUST fail. ----
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	registerListeners(bus, hub)

	gitlabClient := gitlab.NewClient(fake.URL, &http.Client{Timeout: 5 * time.Second})
	router, _ := NewRouterWithHandler(testPool, hub, bus, cipher, gitlabClient, true, context.Background(), "")
	srv := httptest.NewServer(router)
	defer srv.Close()

	// Generate a JWT for our per-test user (the global testToken belongs to a
	// different user and workspace).
	token, err := generateTestJWT(userID, email, name)
	if err != nil {
		t.Fatalf("generateTestJWT: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"title":    "Production wiring test",
		"status":   "todo",
		"priority": "medium",
	})
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/issues", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Workspace-ID", workspaceID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/issues: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		t.Fatalf("POST /api/issues: status = %d, body = %s", resp.StatusCode, buf.String())
	}

	// The critical assertion: the write-through path must actually have fired.
	// If the resolver was nil (the blocker this test guards against), the
	// handler takes the legacy direct-DB branch and we never talk to GitLab.
	if got := atomic.LoadInt32(&gitlabHits); got != 1 {
		t.Fatalf("fake GitLab POST /api/v4/projects/4242/issues hits = %d, want 1 — write-through path did not fire, handler fell through to legacy path (resolver not wired?)", got)
	}
}

