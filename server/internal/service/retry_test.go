package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var retryTestPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err == nil {
		if err := pool.Ping(context.Background()); err != nil {
			pool.Close()
			pool = nil
		}
	}
	retryTestPool = pool
	code := m.Run()
	if retryTestPool != nil {
		retryTestPool.Close()
	}
	os.Exit(code)
}

func skipIfNoDB(t *testing.T) {
	if retryTestPool == nil {
		t.Skip("database not available")
	}
}

func TestComputeDelay(t *testing.T) {
	re := &RetryExecutor{}

	cases := []struct {
		attempt    int
		retryAfter time.Duration
		minExpected time.Duration
		maxExpected time.Duration
	}{
		// First retry (attempt=1): cap = 1s * 2^1 = 2s
		{attempt: 1, retryAfter: 0, minExpected: 0, maxExpected: 2 * time.Second},
		// Second retry (attempt=2): cap = 1s * 2^2 = 4s
		{attempt: 2, retryAfter: 0, minExpected: 0, maxExpected: 4 * time.Second},
		// Third retry (attempt=3): cap = 1s * 2^3 = 8s
		{attempt: 3, retryAfter: 0, minExpected: 0, maxExpected: 8 * time.Second},
		// retryAfter dominates when larger than jitter
		{attempt: 1, retryAfter: 10 * time.Second, minExpected: 10 * time.Second, maxExpected: 10 * time.Second},
		// retryAfter loses when jitter is larger
		{attempt: 1, retryAfter: 1 * time.Second, minExpected: 1 * time.Second, maxExpected: 2 * time.Second},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("attempt_%d_retryAfter_%s", tc.attempt, tc.retryAfter), func(t *testing.T) {
			delay := re.computeDelay(tc.attempt, tc.retryAfter)
			if delay < tc.minExpected || delay > tc.maxExpected {
				t.Fatalf("delay %s not in range [%s, %s]", delay, tc.minExpected, tc.maxExpected)
			}
		})
	}
}

func TestIsRetryable(t *testing.T) {
	re := &RetryExecutor{}

	if !re.isRetryable(FailureReasonTimeout) {
		t.Error("expected timeout to be retryable")
	}
	if !re.isRetryable(FailureReasonRuntimeOffline) {
		t.Error("expected runtime_offline to be retryable")
	}
	if !re.isRetryable(FailureReasonInfraFailure) {
		t.Error("expected infra_failure to be retryable")
	}
	if re.isRetryable(FailureReasonPermanentError) {
		t.Error("expected permanent_error to NOT be retryable")
	}
	if re.isRetryable(FailureReasonAgentError) {
		t.Error("expected agent_error to NOT be retryable")
	}
}

func TestRetryExecutor_LegacyPath_WhenDisabled(t *testing.T) {
	skipIfNoDB(t)
	ctx := context.Background()
	queries := db.New(retryTestPool)

	re := NewRetryExecutor(queries, nil, nil, false)

	// Create a minimal workspace + agent + issue + task.
	wsID := mustInsertWorkspace(t, ctx, queries, "retry-legacy-"+randHex(8))
	agentID := mustInsertAgent(t, ctx, queries, wsID)
	issueID := mustInsertIssue(t, ctx, queries, wsID, agentID)
	taskID := mustInsertTask(t, ctx, queries, agentID, issueID, "failed", "timeout", 1, 2)

	parent, err := queries.GetAgentTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}

	child, err := re.MaybeRetry(ctx, parent, "timeout", 0)
	if err != nil {
		t.Fatalf("MaybeRetry error: %v", err)
	}
	if child == nil {
		t.Fatal("expected retry task to be created")
	}
	if child.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", child.Attempt)
	}
	if child.Status != "queued" {
		t.Fatalf("expected status queued, got %s", child.Status)
	}

	cleanupTask(t, ctx, taskID)
	cleanupTask(t, ctx, child.ID)
	cleanupIssue(t, ctx, issueID)
	cleanupAgent(t, ctx, agentID)
	cleanupWorkspace(t, ctx, wsID)
}

func TestRetryExecutor_Exhaustion(t *testing.T) {
	skipIfNoDB(t)
	ctx := context.Background()
	queries := db.New(retryTestPool)
	bus := events.New()
	metrics := metrics.NewRetryMetrics()

	re := NewRetryExecutor(queries, bus, metrics, true)

	wsID := mustInsertWorkspace(t, ctx, queries, "retry-exhaust-"+randHex(8))
	agentID := mustInsertAgent(t, ctx, queries, wsID)
	issueID := mustInsertIssue(t, ctx, queries, wsID, agentID)
	// attempt=3, max_attempts irrelevant — global MaxRetryAttempts=3 means no more retries.
	taskID := mustInsertTask(t, ctx, queries, agentID, issueID, "failed", "timeout", 3, 5)

	parent, err := queries.GetAgentTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}

	child, err := re.MaybeRetry(ctx, parent, "timeout", 0)
	if err != nil {
		t.Fatalf("MaybeRetry error: %v", err)
	}
	if child != nil {
		t.Fatal("expected no retry when exhausted")
	}

	// Verify failure_reason was updated to retry_exhausted.
	updated, err := queries.GetAgentTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get task after exhaustion: %v", err)
	}
	if !updated.FailureReason.Valid || updated.FailureReason.String != FailureReasonRetryExhausted {
		t.Fatalf("expected failure_reason=%s, got %v", FailureReasonRetryExhausted, updated.FailureReason)
	}

	cleanupTask(t, ctx, taskID)
	cleanupIssue(t, ctx, issueID)
	cleanupAgent(t, ctx, agentID)
	cleanupWorkspace(t, ctx, wsID)
}

func TestRetryExecutor_BackoffScheduling(t *testing.T) {
	skipIfNoDB(t)
	ctx := context.Background()
	queries := db.New(retryTestPool)

	re := NewRetryExecutor(queries, nil, nil, true)

	wsID := mustInsertWorkspace(t, ctx, queries, "retry-backoff-"+randHex(8))
	agentID := mustInsertAgent(t, ctx, queries, wsID)
	issueID := mustInsertIssue(t, ctx, queries, wsID, agentID)
	taskID := mustInsertTask(t, ctx, queries, agentID, issueID, "failed", "timeout", 1, 5)

	parent, err := queries.GetAgentTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}

	child, err := re.MaybeRetry(ctx, parent, "timeout", 5*time.Second)
	if err != nil {
		t.Fatalf("MaybeRetry error: %v", err)
	}
	if child == nil {
		t.Fatal("expected retry task")
	}

	// scheduled_at should be at least 5s in the future because retryAfter dominates.
	minScheduled := time.Now().UTC().Add(4 * time.Second)
	if !child.ScheduledAt.Valid || child.ScheduledAt.Time.Before(minScheduled) {
		t.Fatalf("expected scheduled_at >= %s, got %v", minScheduled, child.ScheduledAt)
	}

	cleanupTask(t, ctx, taskID)
	cleanupTask(t, ctx, child.ID)
	cleanupIssue(t, ctx, issueID)
	cleanupAgent(t, ctx, agentID)
	cleanupWorkspace(t, ctx, wsID)
}

func TestRetryExecutor_PermanentErrorNotRetried(t *testing.T) {
	skipIfNoDB(t)
	ctx := context.Background()
	queries := db.New(retryTestPool)

	re := NewRetryExecutor(queries, nil, nil, true)

	wsID := mustInsertWorkspace(t, ctx, queries, "retry-perm-"+randHex(8))
	agentID := mustInsertAgent(t, ctx, queries, wsID)
	issueID := mustInsertIssue(t, ctx, queries, wsID, agentID)
	taskID := mustInsertTask(t, ctx, queries, agentID, issueID, "failed", FailureReasonPermanentError, 1, 5)

	parent, err := queries.GetAgentTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}

	child, err := re.MaybeRetry(ctx, parent, FailureReasonPermanentError, 0)
	if err != nil {
		t.Fatalf("MaybeRetry error: %v", err)
	}
	if child != nil {
		t.Fatal("expected no retry for permanent error")
	}

	cleanupTask(t, ctx, taskID)
	cleanupIssue(t, ctx, issueID)
	cleanupAgent(t, ctx, agentID)
	cleanupWorkspace(t, ctx, wsID)
}

func TestRetryExecutor_InfraRetryCollision(t *testing.T) {
	skipIfNoDB(t)
	ctx := context.Background()
	queries := db.New(retryTestPool)

	re := NewRetryExecutor(queries, nil, nil, true)

	wsID := mustInsertWorkspace(t, ctx, queries, "retry-infra-"+randHex(8))
	agentID := mustInsertAgent(t, ctx, queries, wsID)
	issueID := mustInsertIssue(t, ctx, queries, wsID, agentID)
	taskID := mustInsertTask(t, ctx, queries, agentID, issueID, "failed", FailureReasonInfraFailure, 1, 5)

	parent, err := queries.GetAgentTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}

	child, err := re.MaybeRetry(ctx, parent, FailureReasonInfraFailure, 0)
	if err != nil {
		t.Fatalf("MaybeRetry error: %v", err)
	}
	if child == nil {
		t.Fatal("expected retry for infra_failure")
	}
	if child.Attempt != 2 {
		t.Fatalf("expected attempt 2, got %d", child.Attempt)
	}

	cleanupTask(t, ctx, taskID)
	cleanupTask(t, ctx, child.ID)
	cleanupIssue(t, ctx, issueID)
	cleanupAgent(t, ctx, agentID)
	cleanupWorkspace(t, ctx, wsID)
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func randHex(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + (i % 26))
	}
	return string(b)
}

func mustInsertWorkspace(t *testing.T, ctx context.Context, q *db.Queries, slug string) pgtype.UUID {
	t.Helper()
	row, err := q.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Slug:        slug,
		Name:        slug,
		IssuePrefix: "TST",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return row.ID
}

func cleanupWorkspace(t *testing.T, ctx context.Context, id pgtype.UUID) {
	t.Helper()
	if retryTestPool != nil {
		_, _ = retryTestPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, id)
	}
}

func mustInsertAgent(t *testing.T, ctx context.Context, q *db.Queries, wsID pgtype.UUID) pgtype.UUID {
	t.Helper()
	// Need a runtime first.
	var runtimeID pgtype.UUID
	err := retryTestPool.QueryRow(ctx, `INSERT INTO agent_runtime (workspace_id, name, device_name) VALUES ($1, 'test', 'test') RETURNING id`, wsID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	row, err := q.CreateAgent(ctx, db.CreateAgentParams{
		WorkspaceID: wsID,
		RuntimeID:   runtimeID,
		Name:        "test-agent",
		Description: "test",
		RuntimeMode: "local",
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return row.ID
}

func cleanupAgent(t *testing.T, ctx context.Context, id pgtype.UUID) {
	t.Helper()
	if retryTestPool != nil {
		_, _ = retryTestPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, id)
	}
}

func mustInsertIssue(t *testing.T, ctx context.Context, q *db.Queries, wsID, agentID pgtype.UUID) pgtype.UUID {
	t.Helper()
	row, err := q.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID:   wsID,
		Title:         "test issue",
		Description:   pgtype.Text{String: "desc", Valid: true},
		Status:        "todo",
		Priority:      "medium",
		AssigneeID:    agentID,
		AssigneeType:  pgtype.Text{String: "agent", Valid: true},
		CreatorID:     agentID,
		CreatorType:   "agent",
		Position:      0,
	})
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return row.ID
}

func cleanupIssue(t *testing.T, ctx context.Context, id pgtype.UUID) {
	t.Helper()
	if retryTestPool != nil {
		_, _ = retryTestPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id)
	}
}

func mustInsertTask(t *testing.T, ctx context.Context, q *db.Queries, agentID, issueID pgtype.UUID, status, reason string, attempt, maxAttempts int32) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	err := retryTestPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, attempt, max_attempts, failure_reason)
		VALUES ($1, $1, $2, $3, $4, $5, $6)
		RETURNING id
	`, agentID, issueID, status, attempt, maxAttempts, reason).Scan(&id)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return id
}

func cleanupTask(t *testing.T, ctx context.Context, id pgtype.UUID) {
	t.Helper()
	if retryTestPool != nil {
		_, _ = retryTestPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, id)
		_, _ = retryTestPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE parent_task_id = $1`, id)
	}
}


// TestIsRetryableProviderErrors verifies that each provider-level error code
// is correctly classified as retryable or permanent.
func TestIsRetryableProviderErrors(t *testing.T) {
	re := &RetryExecutor{Enabled: true}

	// Transient provider errors should be retryable.
	transientCases := []string{
		string(agent.ErrRateLimited),
		string(agent.ErrServiceUnavailable),
		string(agent.ErrGatewayError),
		string(agent.ErrTimeout),
	}
	for _, code := range transientCases {
		t.Run(code, func(t *testing.T) {
			if !re.isRetryable(code) {
				t.Errorf("expected %q to be retryable", code)
			}
		})
	}

	// Permanent provider errors should NOT be retryable.
	permanentCases := []string{
		string(agent.ErrContextExceeded),
		string(agent.ErrQuotaExhausted),
	}
	for _, code := range permanentCases {
		t.Run(code, func(t *testing.T) {
			if re.isRetryable(code) {
				t.Errorf("expected %q to NOT be retryable", code)
			}
		})
	}
}

