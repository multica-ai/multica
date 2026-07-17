package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// mockRow implements pgx.Row, returning either a scanned task or pgx.ErrNoRows.
type mockRow struct {
	task *db.AgentTaskQueue
	err  error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	t := r.task
	ptrs := []any{
		&t.ID, &t.AgentID, &t.IssueID, &t.Status, &t.Priority,
		&t.DispatchedAt, &t.StartedAt, &t.CompletedAt, &t.Result,
		&t.Error, &t.CreatedAt, &t.Context, &t.RuntimeID,
		&t.SessionID, &t.WorkDir, &t.TriggerCommentID,
		&t.ChatSessionID, &t.AutopilotRunID,
	}
	for i, p := range ptrs {
		if i >= len(dest) {
			break
		}
		// Copy value from source to dest by assigning through the pointer.
		switch d := dest[i].(type) {
		case *pgtype.UUID:
			*d = *(p.(*pgtype.UUID))
		case *string:
			*d = *(p.(*string))
		case *int32:
			*d = *(p.(*int32))
		case *pgtype.Timestamptz:
			*d = *(p.(*pgtype.Timestamptz))
		case *[]byte:
			*d = *(p.(*[]byte))
		case *pgtype.Text:
			*d = *(p.(*pgtype.Text))
		}
	}
	return nil
}

// mockDBTX routes QueryRow calls: complete/fail queries return ErrNoRows,
// getAgentTask returns the stored task.
type mockDBTX struct {
	task db.AgentTaskQueue
}

func (m *mockDBTX) Exec(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), nil
}

func (m *mockDBTX) Query(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
	return nil, pgx.ErrNoRows
}

func (m *mockDBTX) QueryRow(_ context.Context, sql string, _ ...interface{}) pgx.Row {
	// CompleteAgentTask and FailAgentTask SQL contain "SET status ="
	if strings.Contains(sql, "SET status =") {
		return &mockRow{err: pgx.ErrNoRows}
	}
	// GetAgentTask — return the existing task
	return &mockRow{task: &m.task}
}

func testUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Valid = true
	u.Bytes[0] = b
	return u
}

func TestCompleteTask_AlreadyFinalized(t *testing.T) {
	taskID := testUUID(1)
	agentID := testUUID(2)

	tests := []struct {
		name   string
		status string
	}{
		{"already completed", "completed"},
		{"already cancelled", "cancelled"},
		{"already failed", "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockDBTX{task: db.AgentTaskQueue{
				ID:      taskID,
				AgentID: agentID,
				Status:  tt.status,
			}}
			svc := &TaskService{
				Queries: db.New(mock),
				Bus:     events.New(),
			}

			got, err := svc.CompleteTask(context.Background(), taskID, nil, "", "")
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got == nil {
				t.Fatal("expected task, got nil")
			}
			if got.Status != tt.status {
				t.Errorf("expected status %q, got %q", tt.status, got.Status)
			}
			if got.ID != taskID {
				t.Error("returned task ID doesn't match")
			}
		})
	}
}

func TestFailTask_AlreadyFinalized(t *testing.T) {
	taskID := testUUID(1)
	agentID := testUUID(2)

	tests := []struct {
		name   string
		status string
	}{
		{"already completed", "completed"},
		{"already cancelled", "cancelled"},
		{"already failed", "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockDBTX{task: db.AgentTaskQueue{
				ID:      taskID,
				AgentID: agentID,
				Status:  tt.status,
			}}
			svc := &TaskService{
				Queries: db.New(mock),
				Bus:     events.New(),
			}

			got, err := svc.FailTask(context.Background(), taskID, "agent crashed", "", "", "")
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got == nil {
				t.Fatal("expected task, got nil")
			}
			if got.Status != tt.status {
				t.Errorf("expected status %q, got %q", tt.status, got.Status)
			}
			if got.ID != taskID {
				t.Error("returned task ID doesn't match")
			}
		})
	}
}

// TestProviderNetworkRetrySchedule locks in the three-tier schedule for a
// transient provider stream cut (MUL-4910): first run + immediate retry + one
// retry deferred ~5s, and only for provider_network — other retryable reasons
// keep their generic max_attempts=2 (single, immediate retry).
func TestProviderNetworkRetrySchedule(t *testing.T) {
	const provNet = "agent_error.provider_network"

	// Attempt ceiling: provider_network is raised to 3, but only ever WIDENS the
	// budget and never overrides the max_attempts<=1 "retry disabled" contract.
	ceilingCases := []struct {
		reason string
		max    int32
		want   int32
	}{
		{provNet, 2, providerNetworkMaxAttempts}, // default budget → raised to 3
		{provNet, 1, 1},                          // disabled → stays disabled, not revived
		{provNet, 5, 5},                          // higher configured budget → kept (widen-only)
		{"timeout", 2, 2},                        // unrelated reason → column value untouched
		{"timeout", 1, 1},                        // unrelated + disabled → untouched
	}
	for _, tc := range ceilingCases {
		if got := retryAttemptCeiling(tc.reason, tc.max); got != tc.want {
			t.Errorf("ceiling(%q, %d) = %d, want %d", tc.reason, tc.max, got, tc.want)
		}
	}

	// Backoff: only provider_network's final attempt (after the 2nd failure) is
	// deferred; its first retry and every other reason are immediate.
	delayCases := []struct {
		reason        string
		failedAttempt int32
		want          time.Duration
	}{
		{provNet, 1, 0}, // first failure → immediate retry
		{provNet, 2, providerNetworkFinalRetryWait}, // second failure → 5s-deferred retry
		{"timeout", 2, 0}, // unrelated reason → never deferred
	}
	for _, tc := range delayCases {
		if got := retryDelayForAttempt(tc.reason, tc.failedAttempt); got != tc.want {
			t.Errorf("retryDelayForAttempt(%q, %d) = %s, want %s", tc.reason, tc.failedAttempt, got, tc.want)
		}
	}

	// Eligibility across the whole chain. mkTask has an issue link and no
	// autopilot run so only the reason/attempt/ceiling gate is exercised.
	mkTask := func(attempt, max int32) db.AgentTaskQueue {
		return db.AgentTaskQueue{
			Attempt:     attempt,
			MaxAttempts: max,
			IssueID:     pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		}
	}
	eligCases := []struct {
		name    string
		reason  string
		attempt int32
		max     int32
		want    bool
	}{
		{"provider_network first run retries", provNet, 1, 2, true},
		{"provider_network second run still retries (deferred tier)", provNet, 2, 2, true},
		{"provider_network third run is the ceiling", provNet, 3, 2, false},
		{"provider_network with retry disabled (max_attempts=1) never retries", provNet, 1, 1, false},
		{"timeout keeps single immediate retry", "timeout", 1, 2, true},
		{"timeout exhausts at attempt 2", "timeout", 2, 2, false},
		{"non-retryable reason never retries", "agent_error.unknown", 1, 2, false},
	}
	for _, tc := range eligCases {
		if got := retryEligible(tc.reason, mkTask(tc.attempt, tc.max)); got != tc.want {
			t.Errorf("%s: retryEligible(%q, attempt=%d/max=%d) = %v, want %v", tc.name, tc.reason, tc.attempt, tc.max, got, tc.want)
		}
	}
}

func TestTaskFailureClassifiers(t *testing.T) {
	cases := []struct {
		reason       string
		wantType     string
		wantResumeOK bool
		wantRetry    bool
	}{
		{reason: "timeout", wantType: "timeout", wantResumeOK: true, wantRetry: true},
		{reason: "codex_semantic_inactivity", wantType: "timeout", wantResumeOK: false, wantRetry: true},
		// Transient mid-stream provider disconnect (MUL-4910): retryable, and
		// resume-safe so the retry continues the truncated conversation.
		{reason: "agent_error.provider_network", wantType: "agent_error", wantResumeOK: true, wantRetry: true},
		{reason: "runtime_recovery", wantType: "runtime", wantResumeOK: true, wantRetry: true},
		{reason: "iteration_limit", wantType: "agent_output", wantResumeOK: false, wantRetry: false},
		{reason: "api_invalid_request", wantType: "agent_error", wantResumeOK: false, wantRetry: false},
		{reason: "agent_error.context_overflow", wantType: "agent_error", wantResumeOK: false, wantRetry: false},
		{reason: "agent_error", wantType: "agent_error", wantResumeOK: true, wantRetry: false},
		// Missing terminal result errors classify to agent_error.unknown. Keep
		// that deterministic upstream failure outside the auto-retry allowlist.
		{reason: "agent_error.unknown", wantType: "agent_error", wantResumeOK: true, wantRetry: false},
	}

	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			if got := taskErrorType(tc.reason); got != tc.wantType {
				t.Fatalf("taskErrorType(%q) = %q, want %q", tc.reason, got, tc.wantType)
			}
			if got := !resumeUnsafeFailureReason(tc.reason); got != tc.wantResumeOK {
				t.Fatalf("resume-safe(%q) = %v, want %v", tc.reason, got, tc.wantResumeOK)
			}
			if got := retryableReasons[tc.reason]; got != tc.wantRetry {
				t.Fatalf("retryableReasons[%q] = %v, want %v", tc.reason, got, tc.wantRetry)
			}
		})
	}
}
