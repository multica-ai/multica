package wakeup

// Integration tests for TECH-3487: a fired wakeup must restart the agent from
// platform activity, not by wrapping the wakeup as a user-visible comment. The
// queued task carries wakeup context while trigger_comment_id points at the
// original thread so the agent's visible reply lands there.
//
// Tests skip cleanly when no test DB is reachable, same pattern as
// sprints/sweeper_db_test.go and feature_flags/store_test.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	wkTestEmail         = "wakeup-origin-test@multica.ai"
	wkTestName          = "Wakeup Origin Test"
	wkTestWorkspaceSlug = "wakeup-origin-tests"
)

var (
	wkPool        *pgxpool.Pool
	wkWorkspaceID pgtype.UUID
	wkUserID      pgtype.UUID
	wkAgentID     pgtype.UUID
	wkRuntimeID   pgtype.UUID
	wkIssueID     pgtype.UUID
	wkOtherIssue  pgtype.UUID
	wkRootID      pgtype.UUID // root comment on wkIssueID
	wkReplyID     pgtype.UUID // reply under wkRootID
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping wakeup origin integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping wakeup origin integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}
	_ = cleanupWkFixture(ctx, pool)
	if err := setupWkFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to set up wakeup origin fixture: %v\n", err)
		_ = cleanupWkFixture(ctx, pool)
		pool.Close()
		os.Exit(1)
	}
	wkPool = pool
	code := m.Run()
	if err := cleanupWkFixture(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean wakeup origin fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupWkFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if err := pool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		wkTestName, wkTestEmail).Scan(&wkUserID); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1, $2, $3, $4) RETURNING id`,
		"Wakeup Origin Tests", wkTestWorkspaceSlug, "Temporary workspace", "WOT").Scan(&wkWorkspaceID); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`,
		wkWorkspaceID, wkUserID); err != nil {
		return fmt.Errorf("create member: %w", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status)
		 VALUES ($1, $2, 'local', 'claude_code', 'online') RETURNING id`,
		wkWorkspaceID, "Wakeup Origin Runtime").Scan(&wkRuntimeID); err != nil {
		return fmt.Errorf("create runtime: %w", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		 VALUES ($1, $2, 'local', $3) RETURNING id`,
		wkWorkspaceID, "Wakeup Origin Agent", wkRuntimeID).Scan(&wkAgentID); err != nil {
		return fmt.Errorf("create agent: %w", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		 VALUES ($1, $2, 'todo', 'none', 'member', $3, 1) RETURNING id`,
		wkWorkspaceID, "Origin issue", wkUserID).Scan(&wkIssueID); err != nil {
		return fmt.Errorf("create issue: %w", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		 VALUES ($1, $2, 'todo', 'none', 'member', $3, 2) RETURNING id`,
		wkWorkspaceID, "Other issue", wkUserID).Scan(&wkOtherIssue); err != nil {
		return fmt.Errorf("create other issue: %w", err)
	}
	// Original conversation: a root comment plus a reply under it.
	if err := pool.QueryRow(ctx,
		`INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, type)
		 VALUES ($1, $2, 'member', $3, 'original request', 'comment') RETURNING id`,
		wkWorkspaceID, wkIssueID, wkUserID).Scan(&wkRootID); err != nil {
		return fmt.Errorf("create root comment: %w", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, type, parent_id)
		 VALUES ($1, $2, 'member', $3, 'a reply in the thread', 'comment', $4) RETURNING id`,
		wkWorkspaceID, wkIssueID, wkUserID, wkRootID).Scan(&wkReplyID); err != nil {
		return fmt.Errorf("create reply comment: %w", err)
	}
	return nil
}

func cleanupWkFixture(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		`DELETE FROM cerebro_agent_wakeup WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1))`,
		`DELETE FROM activity_log WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1))`,
		`DELETE FROM comment WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM issue WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM agent WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM agent_runtime WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM member WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM workspace WHERE slug = $1`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt, wkTestWorkspaceSlug); err != nil {
			return fmt.Errorf("cleanup %q: %w", stmt, err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, wkTestEmail); err != nil {
		return fmt.Errorf("cleanup user: %w", err)
	}
	return nil
}

// newWakeup inserts a time wakeup with the given origin comment (zero = none) and
// returns the persisted row for resolveWakeupParent to act on.
func newWakeup(t *testing.T, ctx context.Context, svc *Service, issueID, origin pgtype.UUID) cerebrodb.CerebroAgentWakeup {
	t.Helper()
	row, err := svc.Cerebro.CreateCerebroAgentWakeup(ctx, cerebrodb.CreateCerebroAgentWakeupParams{
		WorkspaceID:     wkWorkspaceID,
		AgentID:         wkAgentID,
		IssueID:         issueID,
		Prompt:          "check back later",
		TriggerType:     TriggerTime,
		FireAt:          pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
		OriginCommentID: origin,
	})
	if err != nil {
		t.Fatalf("create wakeup: %v", err)
	}
	return row
}

func wkService() *Service {
	queries := db.New(wkPool)
	bus := events.New()
	return &Service{
		Cerebro: cerebrodb.New(wkPool),
		Queries: queries,
		Tasks:   service.NewTaskService(queries, nil, nil, bus),
		Bus:     bus,
	}
}

func TestResolveWakeupParent_RootOrigin(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	row := newWakeup(t, ctx, svc, wkIssueID, wkRootID)

	got := svc.resolveWakeupParent(ctx, row, wkIssueID)
	if util.UUIDToString(got) != util.UUIDToString(wkRootID) {
		t.Fatalf("parent = %s, want root %s", util.UUIDToString(got), util.UUIDToString(wkRootID))
	}
}

func TestResolveWakeupParent_ReplyOriginNormalizesToRoot(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	// Origin is a reply deep in the thread; the note must still anchor to the
	// thread root so it (and the agent's reply under it) resolve into the thread.
	row := newWakeup(t, ctx, svc, wkIssueID, wkReplyID)

	got := svc.resolveWakeupParent(ctx, row, wkIssueID)
	if util.UUIDToString(got) != util.UUIDToString(wkRootID) {
		t.Fatalf("parent = %s, want normalized root %s", util.UUIDToString(got), util.UUIDToString(wkRootID))
	}
}

func TestResolveWakeupParent_CrossIssueRejected(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	// Wakeup fires on a different issue than the origin comment lives on — never
	// cross threads. wkOtherIssue has no human comment, so there is no visible
	// fallback thread.
	row := newWakeup(t, ctx, svc, wkOtherIssue, wkRootID)

	got := svc.resolveWakeupParent(ctx, row, wkOtherIssue)
	if got.Valid {
		t.Fatalf("parent = %s, want zero (cross-issue rejected, no fallback)", util.UUIDToString(got))
	}
}

func TestResolveWakeupParent_NoOriginFallsBackToLatestMemberThread(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	row := newWakeup(t, ctx, svc, wkIssueID, pgtype.UUID{})

	got := svc.resolveWakeupParent(ctx, row, wkIssueID)
	if util.UUIDToString(got) != util.UUIDToString(wkRootID) {
		t.Fatalf("parent = %s, want latest member thread root %s", util.UUIDToString(got), util.UUIDToString(wkRootID))
	}
}

func TestCreateRecordsWakeupScheduledActivity(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	fireAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	if _, err := wkPool.Exec(ctx, `DELETE FROM cerebro_agent_wakeup WHERE issue_id = $1 AND agent_id = $2`, wkIssueID, wkAgentID); err != nil {
		t.Fatalf("clear wakeups before activity create test: %v", err)
	}

	row, err := svc.Create(ctx, wkWorkspaceID, CreateRequest{
		AgentID:         wkAgentID,
		IssueID:         wkIssueID,
		Prompt:          "visible activity test",
		TriggerType:     TriggerTime,
		FireAt:          pgtype.Timestamptz{Time: fireAt, Valid: true},
		OriginCommentID: wkReplyID,
	})
	if err != nil {
		t.Fatalf("create wakeup: %v", err)
	}

	var action string
	var rawDetails []byte
	if err := wkPool.QueryRow(ctx, `
		SELECT action, details
		FROM activity_log
		WHERE issue_id = $1 AND actor_id = $2 AND action = 'wakeup_scheduled'
		ORDER BY created_at DESC
		LIMIT 1
	`, wkIssueID, wkAgentID).Scan(&action, &rawDetails); err != nil {
		t.Fatalf("load wakeup activity: %v", err)
	}
	var details map[string]string
	if err := json.Unmarshal(rawDetails, &details); err != nil {
		t.Fatalf("decode activity details: %v", err)
	}
	if action != "wakeup_scheduled" {
		t.Fatalf("activity action = %q, want wakeup_scheduled", action)
	}
	if details["wakeup_id"] != util.UUIDToString(row.ID) {
		t.Fatalf("activity wakeup_id = %q, want %s", details["wakeup_id"], util.UUIDToString(row.ID))
	}
	if details["fire_at"] != fireAt.Format(time.RFC3339) {
		t.Fatalf("activity fire_at = %q, want %s", details["fire_at"], fireAt.Format(time.RFC3339))
	}
	if details["origin_comment_id"] != util.UUIDToString(wkReplyID) {
		t.Fatalf("activity origin_comment_id = %q, want %s", details["origin_comment_id"], util.UUIDToString(wkReplyID))
	}
}

func TestResolveOriginComment_FallsBackToLatestMemberComment(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	handler := &Handler{Service: wkService()}

	var taskID pgtype.UUID
	if err := wkPool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		 VALUES ($1, $2, $3, 'running', 0) RETURNING id`,
		wkAgentID, wkRuntimeID, wkIssueID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() {
		wkPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	req := httptest.NewRequest("POST", "/api/cerebro/wakeups", nil)
	req.Header.Set("X-Task-ID", util.UUIDToString(taskID))

	got := handler.resolveOriginComment(req, wkIssueID)
	if util.UUIDToString(got) != util.UUIDToString(wkReplyID) {
		t.Fatalf("origin comment = %s, want latest member reply %s", util.UUIDToString(got), util.UUIDToString(wkReplyID))
	}
}

func TestResolveOriginComment_TriggerCommentWinsOverLatestMemberComment(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	handler := &Handler{Service: wkService()}

	var taskID pgtype.UUID
	if err := wkPool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, trigger_comment_id)
		 VALUES ($1, $2, $3, 'running', 0, $4) RETURNING id`,
		wkAgentID, wkRuntimeID, wkIssueID, wkRootID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() {
		wkPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	req := httptest.NewRequest("POST", "/api/cerebro/wakeups", nil)
	req.Header.Set("X-Task-ID", util.UUIDToString(taskID))

	got := handler.resolveOriginComment(req, wkIssueID)
	if util.UUIDToString(got) != util.UUIDToString(wkRootID) {
		t.Fatalf("origin comment = %s, want task trigger %s", util.UUIDToString(got), util.UUIDToString(wkRootID))
	}
}

// TestDispatchCreatesWakeupTaskWithoutSyntheticComment proves the product
// behavior end to end: dispatch creates an agent task directly, stores the
// wakeup note in task context, and does not add a hidden wakeup comment.
func TestDispatchCreatesWakeupTaskWithoutSyntheticComment(t *testing.T) {
	if wkPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	svc := wkService()
	row := newWakeup(t, ctx, svc, wkIssueID, wkReplyID)

	var before int
	if err := wkPool.QueryRow(ctx,
		`SELECT count(*) FROM comment WHERE issue_id = $1 AND type = 'wakeup'`,
		wkIssueID,
	).Scan(&before); err != nil {
		t.Fatalf("count wakeup comments before dispatch: %v", err)
	}

	if err := svc.dispatch(ctx, row); err != nil {
		t.Fatalf("dispatch wakeup: %v", err)
	}
	t.Cleanup(func() {
		wkPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1 AND context->>'type' = 'wakeup'`, wkIssueID)
	})

	var after int
	if err := wkPool.QueryRow(ctx,
		`SELECT count(*) FROM comment WHERE issue_id = $1 AND type = 'wakeup'`,
		wkIssueID,
	).Scan(&after); err != nil {
		t.Fatalf("count wakeup comments after dispatch: %v", err)
	}
	if after != before {
		t.Fatalf("wakeup comments = %d after dispatch, want unchanged %d", after, before)
	}

	var triggerCommentID pgtype.UUID
	var contextType, prompt string
	if err := wkPool.QueryRow(ctx, `
		SELECT trigger_comment_id, context->>'type', context->>'prompt'
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND context->>'type' = 'wakeup'
		ORDER BY created_at DESC
		LIMIT 1
	`, wkIssueID, wkAgentID).Scan(&triggerCommentID, &contextType, &prompt); err != nil {
		t.Fatalf("load wakeup task: %v", err)
	}
	if util.UUIDToString(triggerCommentID) != util.UUIDToString(wkRootID) {
		t.Fatalf("task trigger_comment_id = %s, want thread root %s", util.UUIDToString(triggerCommentID), util.UUIDToString(wkRootID))
	}
	if contextType != service.WakeupTaskContextType {
		t.Fatalf("task context type = %q, want %q", contextType, service.WakeupTaskContextType)
	}
	if prompt != row.Prompt {
		t.Fatalf("task context prompt = %q, want %q", prompt, row.Prompt)
	}
}
