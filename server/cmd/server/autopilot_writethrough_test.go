package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	gitlabsync "github.com/multica-ai/multica/server/internal/gitlab"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/gitlab"
	"github.com/multica-ai/multica/server/pkg/secrets"
)

// seedAutopilotWriteThroughFixture prepares a workspace_gitlab_connection row
// on a fresh per-test workspace/user/agent — isolated from the shared
// integration-test workspace so we don't race the global testWorkspaceID
// across parallel write-through tests. Returns wsID, userID, agentID.
func seedAutopilotWriteThroughFixture(t *testing.T, ctx context.Context, h *handler.Handler, projectID int64, slug string) (string, string, string) {
	t.Helper()

	email := slug + "@multica.ai"
	name := "Autopilot WT Tester " + slug

	// Cleanup any leftover rows.
	cleanup := func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	}
	cleanup()
	t.Cleanup(cleanup)

	var userID, wsID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		name, email,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, 'WT')
		RETURNING id
	`, "Autopilot WT "+slug, slug, "autopilot write-through test workspace").Scan(&wsID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		wsID, userID,
	); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	// Runtime + agent so autopilot has an assignee target.
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, now())
		RETURNING id
	`, wsID, "Autopilot WT Runtime "+slug, "autopilot_wt_runtime", "wt runtime").Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'autopilot-test-agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, wsID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	// Workspace GitLab connection (service PAT only — autopilot resolves to
	// service PAT because it acts as an agent).
	encrypted, encErr := h.Secrets.Encrypt([]byte("svc-pat-autopilot-wt"))
	if encErr != nil {
		t.Fatalf("encrypt: %v", encErr)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, $2, $3, $4, 1, 'connected')
	`, wsID, projectID, "team/"+slug, encrypted); err != nil {
		t.Fatalf("insert workspace_gitlab_connection: %v", err)
	}

	return wsID, userID, agentID
}

// seedAutopilot creates an autopilot row for the given workspace. The
// autopilot_run is created by DispatchAutopilot itself (so we deliberately
// don't pre-seed one — that would leave an orphaned run in the DB).
// Returns the autopilot UUID.
func seedAutopilot(t *testing.T, ctx context.Context, wsID, userID, agentID string) string {
	t.Helper()

	var autopilotID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (
			workspace_id, title, description, assignee_id,
			priority, status, execution_mode,
			created_by_type, created_by_id
		) VALUES (
			$1, 'Autopilot WT run', 'autopilot WT description', $2,
			'medium', 'active', 'create_issue',
			'member', $3
		)
		RETURNING id
	`, wsID, agentID, userID).Scan(&autopilotID); err != nil {
		t.Fatalf("insert autopilot: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM autopilot_issue WHERE autopilot_run_id IN (SELECT id FROM autopilot_run WHERE autopilot_id = $1)`, autopilotID)
		testPool.Exec(ctx, `DELETE FROM autopilot_run WHERE autopilot_id = $1`, autopilotID)
		testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
	})

	return autopilotID
}

// buildAutopilotHandler returns a Handler with its AutopilotService wired to
// use the handler itself as the IssueCreator. Mirrors the production wiring
// in main.go (NewRouterWithHandler + SetIssueCreator).
func buildAutopilotHandler(t *testing.T, fakeGitlabURL string) *handler.Handler {
	t.Helper()
	key := make([]byte, 32)
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	client := gitlab.NewClient(fakeGitlabURL, &http.Client{Timeout: 5 * time.Second})
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	emailSvc := service.NewEmailService()
	h := handler.New(
		db.New(testPool), testPool, hub, bus, emailSvc, nil, nil,
		cipher, client, true,
	)
	h.SetGitlabResolver(gitlabsync.NewResolver(h.Queries, func(_ context.Context, b []byte) (string, error) {
		plain, decErr := h.Secrets.Decrypt(b)
		if decErr != nil {
			return "", decErr
		}
		return string(plain), nil
	}))
	// Production wiring: autopilot service gets the handler as its
	// IssueCreator so dispatchCreateIssue flows through GitLab.
	h.AutopilotService.SetIssueCreator(h)
	return h
}

// TestAutopilot_CreateIssueGoesThroughGitlab verifies the Phase 4 refactor:
// when autopilot dispatches a create_issue run on a GitLab-connected
// workspace, the issue is created on GitLab first (one POST to
// /api/v4/projects/:id/issues), the cache row carries the resulting
// gitlab_iid, and a row in autopilot_issue maps the run to that IID.
func TestAutopilot_CreateIssueGoesThroughGitlab(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	var gitlabHits int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues") {
			atomic.AddInt32(&gitlabHits, 1)
			_, _ = w.Write([]byte(`{
				"id": 990011,
				"iid": 600,
				"title": "Autopilot WT run",
				"description": "autopilot",
				"state": "opened",
				"labels": ["status::todo", "priority::medium", "agent::autopilot-test-agent"],
				"updated_at": "2026-04-17T15:00:00Z"
			}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer fake.Close()

	h := buildAutopilotHandler(t, fake.URL)

	wsID, userID, agentID := seedAutopilotWriteThroughFixture(t, ctx, h, 7001, "autopilot-wt-happy")
	autopilotUUID := seedAutopilot(t, ctx, wsID, userID, agentID)

	ap, err := h.Queries.GetAutopilot(ctx, parseUUID(autopilotUUID))
	if err != nil {
		t.Fatalf("load autopilot: %v", err)
	}

	// Dispatch the autopilot. This must route through CreateIssueForAutopilot
	// (which calls the GitLab write-through). The run is created inside
	// DispatchAutopilot — we use its ID for the mapping assertion.
	run, err := h.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err != nil {
		t.Fatalf("DispatchAutopilot: %v", err)
	}
	runID := run.ID

	// Clean up the newly created issue row (seedAutopilotWriteThroughFixture
	// drops the whole workspace, which cascades — but be explicit).
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1 AND gitlab_iid = 600`, wsID)
	})

	if got := atomic.LoadInt32(&gitlabHits); got != 1 {
		t.Fatalf("fake GitLab POST hits = %d, want 1 — autopilot didn't go through write-through", got)
	}

	// Mapping exists keyed by workspace_id + gitlab_iid.
	var count int
	if err := testPool.QueryRow(ctx, `
		SELECT COUNT(*) FROM autopilot_issue WHERE autopilot_run_id = $1 AND gitlab_iid = 600
	`, runID).Scan(&count); err != nil {
		t.Fatalf("count autopilot_issue: %v", err)
	}
	if count != 1 {
		t.Fatalf("autopilot_issue mapping rows = %d, want 1", count)
	}

	// The cache row has gitlab_iid = 600.
	var cachedIID int32
	if err := testPool.QueryRow(ctx, `
		SELECT gitlab_iid FROM issue WHERE workspace_id = $1 AND gitlab_iid = 600
	`, wsID).Scan(&cachedIID); err != nil {
		t.Fatalf("load cache row: %v", err)
	}
	if cachedIID != 600 {
		t.Fatalf("cache gitlab_iid = %d, want 600", cachedIID)
	}

	// The run is linked to the created issue (UpdateAutopilotRunIssueCreated).
	var linkedIssueID string
	if err := testPool.QueryRow(ctx, `SELECT issue_id::text FROM autopilot_run WHERE id = $1`, runID).Scan(&linkedIssueID); err != nil {
		t.Fatalf("read run.issue_id: %v", err)
	}
	if linkedIssueID == "" {
		t.Fatalf("autopilot_run.issue_id not set")
	}
}

// TestAutopilot_GitLabFailureDoesNotOrphanRun verifies that a 500 from GitLab
// on the create-issue call propagates back out of DispatchAutopilot, the run
// is marked failed (via failRun), no cache row is created, and no
// autopilot_issue mapping is recorded. Guards against the worst-case
// regression where a GitLab failure half-creates the run / issue / mapping.
func TestAutopilot_GitLabFailureDoesNotOrphanRun(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"gitlab is sad"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer fake.Close()

	h := buildAutopilotHandler(t, fake.URL)

	wsID, userID, agentID := seedAutopilotWriteThroughFixture(t, ctx, h, 7002, "autopilot-wt-fail")
	autopilotUUID := seedAutopilot(t, ctx, wsID, userID, agentID)

	ap, err := h.Queries.GetAutopilot(ctx, parseUUID(autopilotUUID))
	if err != nil {
		t.Fatalf("load autopilot: %v", err)
	}

	// DispatchAutopilot returns the run it created even on error, so we can
	// inspect its ID for mapping / status assertions.
	run, err := h.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
	if err == nil {
		t.Fatalf("DispatchAutopilot: expected error on GitLab failure, got nil")
	}
	if run == nil {
		t.Fatalf("DispatchAutopilot: expected non-nil run on GitLab failure (failRun needs the ID)")
	}
	runID := run.ID

	// No cache row.
	var issueCount int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM issue WHERE workspace_id = $1`, wsID).Scan(&issueCount); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if issueCount != 0 {
		t.Fatalf("cache issue rows = %d, want 0 (GitLab failed — we must not orphan a row)", issueCount)
	}

	// No autopilot_issue mapping.
	var mappingCount int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM autopilot_issue WHERE autopilot_run_id = $1`, runID).Scan(&mappingCount); err != nil {
		t.Fatalf("count autopilot_issue: %v", err)
	}
	if mappingCount != 0 {
		t.Fatalf("autopilot_issue rows = %d, want 0 on GitLab failure", mappingCount)
	}

	// The run is marked failed.
	var runStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM autopilot_run WHERE id = $1`, runID).Scan(&runStatus); err != nil {
		t.Fatalf("read run status: %v", err)
	}
	if runStatus != "failed" {
		t.Fatalf("run status = %q, want 'failed' (failRun should fire when dispatchCreateIssue errors)", runStatus)
	}
}

