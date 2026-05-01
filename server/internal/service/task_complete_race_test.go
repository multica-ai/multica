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
	task  *db.AgentTaskQueue
	issue *db.Issue
	err   error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.issue != nil {
		i := r.issue
		ptrs := []any{
			&i.ID, &i.WorkspaceID, &i.Title, &i.Description, &i.Status,
			&i.Priority, &i.AssigneeType, &i.AssigneeID, &i.CreatorType,
			&i.CreatorID, &i.ParentIssueID, &i.AcceptanceCriteria,
			&i.ContextRefs, &i.Position, &i.DueDate, &i.CreatedAt,
			&i.UpdatedAt, &i.Number, &i.ProjectID, &i.OriginType,
			&i.OriginID, &i.FirstExecutedAt,
		}
		for i, p := range ptrs {
			if i >= len(dest) {
				break
			}
			assignScannedValue(dest[i], p)
		}
		return nil
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
		assignScannedValue(dest[i], p)
	}
	return nil
}

func assignScannedValue(dest, src any) {
	switch d := dest.(type) {
	case *pgtype.UUID:
		*d = *(src.(*pgtype.UUID))
	case *string:
		*d = *(src.(*string))
	case *int32:
		*d = *(src.(*int32))
	case *int64:
		*d = *(src.(*int64))
	case *float64:
		*d = *(src.(*float64))
	case *pgtype.Timestamptz:
		*d = *(src.(*pgtype.Timestamptz))
	case *[]byte:
		*d = *(src.(*[]byte))
	case *pgtype.Text:
		*d = *(src.(*pgtype.Text))
	}
}

// mockDBTX routes QueryRow calls: complete/fail queries return ErrNoRows,
// getAgentTask returns the stored task.
type mockDBTX struct {
	task              db.AgentTaskQueue
	issue             db.Issue
	updateIssueStatus int
}

func (m *mockDBTX) Exec(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), nil
}

func (m *mockDBTX) Query(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
	return nil, pgx.ErrNoRows
}

func (m *mockDBTX) QueryRow(_ context.Context, sql string, _ ...interface{}) pgx.Row {
	if strings.Contains(sql, "-- name: UpdateIssueStatus") {
		m.issue.Status = "blocked"
		m.updateIssueStatus++
		return &mockRow{issue: &m.issue}
	}
	if strings.Contains(sql, "-- name: GetIssue") {
		return &mockRow{issue: &m.issue}
	}
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

func TestBlockIssueForFailedTask(t *testing.T) {
	taskID := testUUID(1)
	issueID := testUUID(2)
	workspaceID := testUUID(3)

	tests := []struct {
		name        string
		taskIssueID pgtype.UUID
		issueStatus string
		wantChanged bool
		wantPrev    string
	}{
		{
			name:        "blocks active issue",
			taskIssueID: issueID,
			issueStatus: "in_progress",
			wantChanged: true,
			wantPrev:    "in_progress",
		},
		{
			name:        "chat task without issue is ignored",
			taskIssueID: pgtype.UUID{},
			issueStatus: "in_progress",
			wantChanged: false,
			wantPrev:    "",
		},
		{
			name:        "already blocked is idempotent",
			taskIssueID: issueID,
			issueStatus: "blocked",
			wantChanged: false,
			wantPrev:    "blocked",
		},
		{
			name:        "done issue is not overwritten",
			taskIssueID: issueID,
			issueStatus: "done",
			wantChanged: false,
			wantPrev:    "done",
		},
		{
			name:        "cancelled issue is not overwritten",
			taskIssueID: issueID,
			issueStatus: "cancelled",
			wantChanged: false,
			wantPrev:    "cancelled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockDBTX{
				task: db.AgentTaskQueue{
					ID:      taskID,
					IssueID: tt.taskIssueID,
				},
				issue: db.Issue{
					ID:          issueID,
					WorkspaceID: workspaceID,
					Status:      tt.issueStatus,
				},
			}
			svc := &TaskService{Queries: db.New(mock)}

			got, prevStatus, changed, err := svc.blockIssueForFailedTask(context.Background(), svc.Queries, mock.task)
			if err != nil {
				t.Fatalf("blockIssueForFailedTask returned error: %v", err)
			}
			if changed != tt.wantChanged {
				t.Fatalf("changed: got %v, want %v", changed, tt.wantChanged)
			}
			if prevStatus != tt.wantPrev {
				t.Fatalf("prevStatus: got %q, want %q", prevStatus, tt.wantPrev)
			}
			if tt.wantChanged {
				if got.Status != "blocked" {
					t.Fatalf("issue status: got %q, want blocked", got.Status)
				}
				if mock.updateIssueStatus != 1 {
					t.Fatalf("UpdateIssueStatus calls: got %d, want 1", mock.updateIssueStatus)
				}
			} else if mock.updateIssueStatus != 0 {
				t.Fatalf("UpdateIssueStatus calls: got %d, want 0", mock.updateIssueStatus)
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

			got, err := svc.FailTask(context.Background(), taskID, "agent crashed", "", "")
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
