package service

import (
	"context"
	"strings"
	"testing"

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
		&t.Attempt, &t.MaxAttempts, &t.ParentTaskID, &t.FailureReason,
		&t.TriggerSummary, &t.ForceFreshSession, &t.IsLeaderTask,
		&t.WaitReason, &t.InitiatorUserID, &t.HandoffNote, &t.PrepareLeaseExpiresAt,
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
		case *bool:
			*d = *(p.(*bool))
		}
	}
	return nil
}

// mockDBTX routes QueryRow calls: complete/fail queries return ErrNoRows unless reclaimable,
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



type mockAgentRow struct{}

func (r *mockAgentRow) Scan(dest ...any) error {
	for _, d := range dest {
		switch v := d.(type) {
		case *pgtype.UUID:
			*v = testUUID(2)
		case *string:
			*v = "test"
		}
	}
	return nil
}

type mockBoolRow struct{ val bool }

func (r *mockBoolRow) Scan(dest ...any) error {
	if len(dest) > 0 {
		if b, ok := dest[0].(*bool); ok {
			*b = r.val
		}
	}
	return nil
}

type mockCommentRow struct{}

func (r *mockCommentRow) Scan(dest ...any) error {
	for _, d := range dest {
		switch v := d.(type) {
		case *pgtype.UUID:
			*v = testUUID(3)
		}
	}
	return nil
}

func (m *mockDBTX) QueryRow(_ context.Context, sql string, args ...interface{}) pgx.Row {
	if strings.Contains(sql, "UPDATE agent AS a") {
		return &mockAgentRow{}
	}
	if strings.Contains(sql, "SELECT count(*) > 0") || strings.Contains(sql, "HasSquadLeaderNoActionEvaluationForTask") || strings.Contains(sql, "HasAgentCommentedSince") {
		return &mockBoolRow{val: false}
	}
	if strings.Contains(sql, "INSERT INTO issue_comment") || strings.Contains(sql, "CreateIssueComment") {
		return &mockCommentRow{}
	}
	if strings.Contains(sql, "SET status = 'completed'") {
		canReclaim := m.task.Status == "running" ||
			(strings.Contains(sql, "status = 'failed'") && m.task.Status == "failed" && (m.task.FailureReason.String == "runtime_offline" || m.task.FailureReason.String == "timeout" || m.task.FailureReason.String == "runtime_recovery" || m.task.FailureReason.String == "queued_expired"))
		if !canReclaim {
			return &mockRow{err: pgx.ErrNoRows}
		}
		m.task.Status = "completed"
		m.task.Error = pgtype.Text{}
		m.task.FailureReason = pgtype.Text{}
		if len(args) > 1 {
			if res, ok := args[1].([]byte); ok {
				m.task.Result = res
			}
		}
		return &mockRow{task: &m.task}
	}
	if strings.Contains(sql, "SET status = 'failed'") {
		canReclaim := m.task.Status == "dispatched" || m.task.Status == "running" || m.task.Status == "waiting_local_directory" ||
			(strings.Contains(sql, "status = 'failed'") && m.task.Status == "failed" && (m.task.FailureReason.String == "runtime_offline" || m.task.FailureReason.String == "timeout" || m.task.FailureReason.String == "runtime_recovery" || m.task.FailureReason.String == "queued_expired"))
		if !canReclaim {
			return &mockRow{err: pgx.ErrNoRows}
		}
		m.task.Status = "failed"
		if len(args) > 1 {
			if errText, ok := args[1].(pgtype.Text); ok && errText.Valid {
				m.task.Error = errText
			}
		}
		if len(args) > 2 {
			if frText, ok := args[2].(pgtype.Text); ok && frText.Valid {
				m.task.FailureReason = frText
			}
		}
		return &mockRow{task: &m.task}
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
		{"already failed with agent error", "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failureReason := pgtype.Text{}
			if tt.status == "failed" {
				failureReason = pgtype.Text{String: "agent_error", Valid: true}
			}
			mock := &mockDBTX{task: db.AgentTaskQueue{
				ID:            taskID,
				AgentID:       agentID,
				Status:        tt.status,
				FailureReason: failureReason,
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

func TestCompleteTask_ReclaimOfflineFailure(t *testing.T) {
	taskID := testUUID(1)
	agentID := testUUID(2)

	reclaimableReasons := []string{"runtime_offline", "timeout", "runtime_recovery", "queued_expired"}

	for _, reason := range reclaimableReasons {
		t.Run("reclaim_"+reason, func(t *testing.T) {
			mock := &mockDBTX{task: db.AgentTaskQueue{
				ID:            taskID,
				AgentID:       agentID,
				Status:        "failed",
				Error:         pgtype.Text{String: "system sweeper killed task", Valid: true},
				FailureReason: pgtype.Text{String: reason, Valid: true},
			}}
			svc := &TaskService{
				Queries: db.New(mock),
				Bus:     events.New(),
			}

			resultPayload := []byte(`{"output": "late success output"}`)
			got, err := svc.CompleteTask(context.Background(), taskID, resultPayload, "sess-123", "/work/dir")
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got == nil {
				t.Fatal("expected task, got nil")
			}
			if got.Status != "completed" {
				t.Errorf("expected status 'completed', got %q", got.Status)
			}
			if got.Error.Valid {
				t.Errorf("expected error cleared, got %q", got.Error.String)
			}
			if got.FailureReason.Valid {
				t.Errorf("expected failure_reason cleared, got %q", got.FailureReason.String)
			}
			if string(got.Result) != string(resultPayload) {
				t.Errorf("expected result %q, got %q", string(resultPayload), string(got.Result))
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
		{"already failed with agent error", "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failureReason := pgtype.Text{}
			if tt.status == "failed" {
				failureReason = pgtype.Text{String: "agent_error", Valid: true}
			}
			mock := &mockDBTX{task: db.AgentTaskQueue{
				ID:            taskID,
				AgentID:       agentID,
				Status:        tt.status,
				FailureReason: failureReason,
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

func TestFailTask_ReclaimOfflineFailure(t *testing.T) {
	taskID := testUUID(1)
	agentID := testUUID(2)

	mock := &mockDBTX{task: db.AgentTaskQueue{
		ID:            taskID,
		AgentID:       agentID,
		Status:        "failed",
		Error:         pgtype.Text{String: "runtime went offline", Valid: true},
		FailureReason: pgtype.Text{String: "runtime_offline", Valid: true},
	}}
	svc := &TaskService{
		Queries: db.New(mock),
		Bus:     events.New(),
	}

	realErrStr := "context_overflow: prompt too long"
	got, err := svc.FailTask(context.Background(), taskID, realErrStr, "sess-123", "/work/dir", "context_overflow")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got == nil {
		t.Fatal("expected task, got nil")
	}
	if got.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", got.Status)
	}
	if !got.Error.Valid || got.Error.String != realErrStr {
		t.Errorf("expected error %q, got %q", realErrStr, got.Error.String)
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
		{reason: "runtime_recovery", wantType: "runtime", wantResumeOK: true, wantRetry: true},
		{reason: "iteration_limit", wantType: "agent_output", wantResumeOK: false, wantRetry: false},
		{reason: "api_invalid_request", wantType: "agent_error", wantResumeOK: false, wantRetry: false},
		{reason: "agent_error", wantType: "agent_error", wantResumeOK: true, wantRetry: false},
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
