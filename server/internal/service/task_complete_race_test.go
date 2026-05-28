package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// mockRow implements pgx.Row, returning either a scanned task or pgx.ErrNoRows.
type mockRow struct {
	task    *db.AgentTaskQueue
	issue   *db.Issue
	comment *db.Comment
	ws      *db.Workspace
	agent   *db.Agent
	err     error
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
	if r.comment != nil {
		c := r.comment
		ptrs := []any{
			&c.ID, &c.IssueID, &c.AuthorType, &c.AuthorID, &c.Content,
			&c.Type, &c.CreatedAt, &c.UpdatedAt, &c.ParentID, &c.WorkspaceID,
			&c.DeletedAt, &c.ResolvedAt, &c.ResolvedByType, &c.ResolvedByID,
		}
		for i, p := range ptrs {
			if i >= len(dest) {
				break
			}
			assignScannedValue(dest[i], p)
		}
		return nil
	}
	if r.ws != nil {
		w := r.ws
		ptrs := []any{
			&w.ID, &w.Name, &w.Slug, &w.Description, &w.Settings,
			&w.CreatedAt, &w.UpdatedAt, &w.Context, &w.Repos,
			&w.IssuePrefix, &w.IssueCounter, &w.WikiContent,
		}
		for i, p := range ptrs {
			if i >= len(dest) {
				break
			}
			assignScannedValue(dest[i], p)
		}
		return nil
	}
	if r.agent != nil {
		a := r.agent
		ptrs := []any{
			&a.ID, &a.WorkspaceID, &a.Name, &a.AvatarUrl, &a.RuntimeMode,
			&a.RuntimeConfig, &a.Visibility, &a.Status, &a.MaxConcurrentTasks,
			&a.OwnerID, &a.CreatedAt, &a.UpdatedAt, &a.Description, &a.RuntimeID,
			&a.Instructions, &a.ArchivedAt, &a.ArchivedBy, &a.CustomEnv,
			&a.CustomArgs, &a.McpConfig, &a.Model, &a.CustomEnvCopiedPending,
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
		&t.ChatSessionID, &t.AutopilotRunID, &t.Attempt,
		&t.MaxAttempts, &t.ParentTaskID, &t.FailureReason,
		&t.TriggerSummary, &t.ForceFreshSession, &t.IsLeaderTask,
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
	case *bool:
		*d = *(src.(*bool))
	}
}

// mockDBTX routes QueryRow calls for task service unit tests.
type mockDBTX struct {
	task              db.AgentTaskQueue
	issue             db.Issue
	agent             db.Agent
	updateIssueStatus int
	comments          []db.Comment
	commentedSince    bool
}

func (m *mockDBTX) Exec(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(""), nil
}

func (m *mockDBTX) Query(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
	return nil, pgx.ErrNoRows
}

func (m *mockDBTX) QueryRow(_ context.Context, sql string, args ...interface{}) pgx.Row {
	if strings.Contains(sql, "-- name: UpdateIssueStatus") {
		m.issue.Status = "blocked"
		m.updateIssueStatus++
		return &mockRow{issue: &m.issue}
	}
	if strings.Contains(sql, "-- name: GetIssue") {
		return &mockRow{issue: &m.issue}
	}
	if strings.Contains(sql, "-- name: GetComment :one") {
		for i := range m.comments {
			if m.comments[i].ID == args[0].(pgtype.UUID) {
				comment := m.comments[i]
				return &mockRow{comment: &comment}
			}
		}
		comment := db.Comment{
			ID:          args[0].(pgtype.UUID),
			IssueID:     m.issue.ID,
			WorkspaceID: m.issue.WorkspaceID,
		}
		return &mockRow{comment: &comment}
	}
	if strings.Contains(sql, "-- name: GetWorkspace :one") {
		return &mockRow{ws: &db.Workspace{ID: m.issue.WorkspaceID, IssuePrefix: "OPE"}}
	}
	if strings.Contains(sql, "-- name: GetAgent :one") {
		agent := m.agent
		if !agent.ID.Valid {
			agent.ID = m.task.AgentID
		}
		if !agent.WorkspaceID.Valid {
			agent.WorkspaceID = m.issue.WorkspaceID
		}
		if agent.Name == "" {
			agent.Name = "Test Agent"
		}
		return &mockRow{agent: &agent}
	}
	if strings.Contains(sql, "-- name: RefreshAgentStatusFromTasks") {
		return &mockRow{err: pgx.ErrNoRows}
	}
	if strings.Contains(sql, "-- name: HasAgentCommentedSince") {
		return scanFuncRow(func(dest ...any) error {
			*(dest[0].(*bool)) = m.commentedSince
			return nil
		})
	}
	if strings.Contains(sql, "-- name: HasSquadLeaderNoActionEvaluationForTask") {
		return scanFuncRow(func(dest ...any) error {
			*(dest[0].(*bool)) = false
			return nil
		})
	}
	if strings.Contains(sql, "-- name: CreateComment") {
		return scanFuncRow(func(dest ...any) error {
			comment := db.Comment{
				ID:          testUUID(byte(len(m.comments) + 50)),
				IssueID:     args[0].(pgtype.UUID),
				WorkspaceID: args[1].(pgtype.UUID),
				AuthorType:  args[2].(string),
				AuthorID:    args[3].(pgtype.UUID),
				Content:     args[4].(string),
				Type:        args[5].(string),
				ParentID:    args[6].(pgtype.UUID),
			}
			now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
			comment.CreatedAt = now
			comment.UpdatedAt = now
			m.comments = append(m.comments, comment)

			assignScannedValue(dest[0], &comment.ID)
			assignScannedValue(dest[1], &comment.IssueID)
			assignScannedValue(dest[2], &comment.AuthorType)
			assignScannedValue(dest[3], &comment.AuthorID)
			assignScannedValue(dest[4], &comment.Content)
			assignScannedValue(dest[5], &comment.Type)
			assignScannedValue(dest[6], &comment.CreatedAt)
			assignScannedValue(dest[7], &comment.UpdatedAt)
			assignScannedValue(dest[8], &comment.ParentID)
			assignScannedValue(dest[9], &comment.WorkspaceID)
			assignScannedValue(dest[10], &comment.DeletedAt)
			return nil
		})
	}
	// CompleteAgentTask and FailAgentTask SQL contain "SET status ="
	if strings.Contains(sql, "SET status =") {
		if strings.Contains(sql, "-- name: CompleteAgentTask") {
			if m.task.Status != "running" {
				return &mockRow{err: pgx.ErrNoRows}
			}
			m.task.Status = "completed"
		}
		if strings.Contains(sql, "-- name: FailAgentTask") {
			if m.task.Status != "running" && m.task.Status != "dispatched" {
				return &mockRow{err: pgx.ErrNoRows}
			}
			m.task.Status = "failed"
		}
		return &mockRow{task: &m.task}
	}
	// GetAgentTask — return the existing task
	return &mockRow{task: &m.task}
}

type scanFuncRow func(dest ...any) error

func (f scanFuncRow) Scan(dest ...any) error { return f(dest...) }

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
		{
			name:        "quick-create produced issue is not polluted",
			taskIssueID: issueID,
			issueStatus: "todo",
			wantChanged: false,
			wantPrev:    "todo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := db.Issue{
				ID:          issueID,
				WorkspaceID: workspaceID,
				Status:      tt.issueStatus,
			}
			if tt.name == "quick-create produced issue is not polluted" {
				issue.OriginType = pgtype.Text{String: "quick_create", Valid: true}
				issue.OriginID = taskID
			}
			mock := &mockDBTX{
				task: db.AgentTaskQueue{
					ID:      taskID,
					IssueID: tt.taskIssueID,
				},
				issue: issue,
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

func TestFailTask_QuickCreateProducedIssueDoesNotWriteIssueFailure(t *testing.T) {
	issueID := testUUID(11)
	agentID := testUUID(12)
	workspaceID := testUUID(14)
	taskID := testUUID(15)
	startedAt := pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true}
	mock := &mockDBTX{
		task: db.AgentTaskQueue{
			ID:        taskID,
			IssueID:   issueID,
			AgentID:   agentID,
			Status:    "running",
			StartedAt: startedAt,
		},
		issue: db.Issue{
			ID:          issueID,
			WorkspaceID: workspaceID,
			Title:       "Issue created by quick-create",
			Status:      "todo",
			OriginType:  pgtype.Text{String: "quick_create", Valid: true},
			OriginID:    taskID,
		},
	}
	svc := &TaskService{
		Queries: db.New(mock),
		Bus:     events.New(),
	}

	got, err := svc.FailTask(context.Background(), taskID, "Missing environment variable: `OPENAI_API_KEY`.", "", "", "")
	if err != nil {
		t.Fatalf("FailTask returned error: %v", err)
	}
	if got == nil || got.Status != "failed" {
		t.Fatalf("expected failed task, got %#v", got)
	}
	if mock.updateIssueStatus != 0 {
		t.Fatalf("expected no issue status update, got %d", mock.updateIssueStatus)
	}
	if len(mock.comments) != 0 {
		t.Fatalf("expected no issue failure comment, got %d", len(mock.comments))
	}
	if mock.issue.Status != "todo" {
		t.Fatalf("expected issue to remain todo, got %q", mock.issue.Status)
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

func TestCompleteTask_FallbackCommentIsTopLevel(t *testing.T) {
	issueID := testUUID(1)
	agentID := testUUID(2)
	triggerCommentID := testUUID(3)
	workspaceID := testUUID(4)
	startedAt := pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true}
	result, err := json.Marshal(protocol.TaskCompletedPayload{Output: "completed with a visible summary"})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	mock := &mockDBTX{
		task: db.AgentTaskQueue{
			ID:               testUUID(5),
			IssueID:          issueID,
			AgentID:          agentID,
			Status:           "running",
			StartedAt:        startedAt,
			TriggerCommentID: triggerCommentID,
		},
		issue: db.Issue{
			ID:          issueID,
			WorkspaceID: workspaceID,
			Title:       "Test issue",
			Status:      "in_progress",
		},
	}
	svc := &TaskService{
		Queries: db.New(mock),
		Bus:     events.New(),
	}

	got, err := svc.CompleteTask(context.Background(), mock.task.ID, result, "", "")
	if err != nil {
		t.Fatalf("CompleteTask returned error: %v", err)
	}
	if got == nil || got.Status != "completed" {
		t.Fatalf("expected completed task, got %#v", got)
	}
	if len(mock.comments) != 1 {
		t.Fatalf("expected 1 fallback comment, got %d", len(mock.comments))
	}
	if !mock.comments[0].ParentID.Valid || mock.comments[0].ParentID != triggerCommentID {
		t.Fatalf("expected fallback completion comment parent_id=%v, got %v", triggerCommentID, mock.comments[0].ParentID)
	}
	if mock.comments[0].Type != "comment" {
		t.Fatalf("expected comment type 'comment', got %q", mock.comments[0].Type)
	}
}

func TestFailTask_FallbackCommentIsTopLevel(t *testing.T) {
	issueID := testUUID(11)
	agentID := testUUID(12)
	triggerCommentID := testUUID(13)
	workspaceID := testUUID(14)
	startedAt := pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true}
	mock := &mockDBTX{
		task: db.AgentTaskQueue{
			ID:               testUUID(15),
			IssueID:          issueID,
			AgentID:          agentID,
			Status:           "running",
			StartedAt:        startedAt,
			TriggerCommentID: triggerCommentID,
		},
		issue: db.Issue{
			ID:          issueID,
			WorkspaceID: workspaceID,
			Title:       "Test issue",
			Status:      "in_progress",
		},
	}
	svc := &TaskService{
		Queries: db.New(mock),
		Bus:     events.New(),
	}

	got, err := svc.FailTask(context.Background(), mock.task.ID, "agent crashed", "", "", "")
	if err != nil {
		t.Fatalf("FailTask returned error: %v", err)
	}
	if got == nil || got.Status != "failed" {
		t.Fatalf("expected failed task, got %#v", got)
	}
	if len(mock.comments) != 1 {
		t.Fatalf("expected 1 failure comment, got %d", len(mock.comments))
	}
	if !mock.comments[0].ParentID.Valid || mock.comments[0].ParentID != triggerCommentID {
		t.Fatalf("expected fallback failure comment parent_id=%v, got %v", triggerCommentID, mock.comments[0].ParentID)
	}
	if mock.comments[0].Type != "system" {
		t.Fatalf("expected comment type 'system', got %q", mock.comments[0].Type)
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
		{reason: "codex_semantic_inactivity", wantType: "agent_error", wantResumeOK: false, wantRetry: false},
		{reason: "runtime_recovery", wantType: "runtime", wantResumeOK: true, wantRetry: true},
		{reason: "iteration_limit", wantType: "agent_output", wantResumeOK: false, wantRetry: false},
		{reason: "api_invalid_request", wantType: "upstream", wantResumeOK: false, wantRetry: false},
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
