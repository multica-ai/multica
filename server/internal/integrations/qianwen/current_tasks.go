package qianwen

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	defaultCurrentTaskLimit = 10
	maxCurrentTaskLimit     = 20
)

// CurrentTaskSummary is the deliberately small, privacy-safe task projection
// returned to a Qianwen Skill. It never carries task result/error/context,
// transcript content, filesystem paths, credentials, or opaque provider IDs.
type CurrentTaskSummary struct {
	TaskID       string     `json:"task_id"`
	RequestID    string     `json:"request_id,omitempty"`
	DisplayTitle string     `json:"display_title"`
	Source       string     `json:"source"`
	AgentName    string     `json:"agent_name"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
}

// CurrentTaskList is a live discovery page, not a snapshot and not a
// replacement for request-id status polling. Only Qianwen-originated tasks
// that can be tied back to this installation carry RequestID.
type CurrentTaskList struct {
	Tasks      []CurrentTaskSummary `json:"tasks"`
	HasMore    bool                 `json:"has_more"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

// ListCurrentTasks returns the bound member's currently visible work. Agent
// visibility follows Multica's VIEW rules; chat tasks add a creator-only fence
// so sharing an agent never exposes another member's private conversation.
func (s *Service) ListCurrentTasks(ctx context.Context, connectionID, token string, invocation TaskListInvocation) (CurrentTaskList, error) {
	if err := VerifyTaskListInvocationSignature(token, invocation, s.now()); err != nil {
		return CurrentTaskList{}, err
	}
	cursorCreatedAt, cursorID, err := decodeCurrentTaskCursor(invocation.Request.Cursor)
	if err != nil {
		return CurrentTaskList{}, fmt.Errorf("%w: invalid task-list cursor", ErrInvalidRequest)
	}
	if s.dbq == nil {
		return CurrentTaskList{}, errors.New("qianwen: current-task list requires database queries")
	}
	installation, err := s.authenticate(ctx, connectionID, token)
	if err != nil {
		return CurrentTaskList{}, err
	}
	boundUserID, err := s.resolveInvocationUser(ctx, installation, connectionID, token, invocation.Identity)
	if err != nil {
		return CurrentTaskList{}, err
	}

	rows, err := s.dbq.ListQianwenVisibleCurrentTasks(ctx, db.ListQianwenVisibleCurrentTasksParams{
		InstallationID:  installation.ID,
		ConnectionID:    connectionID,
		AccessTokenHash: hashAccessToken(token),
		MulticaUserID:   boundUserID,
		OpenUserID:      invocation.Identity.OpenUserID,
		OpenUuid:        invocation.Identity.OpenUUID,
		CursorValid:     invocation.Request.Cursor != "",
		CursorCreatedAt: cursorCreatedAt,
		CursorID:        cursorID,
		PageSize:        int32(invocation.Request.Limit + 1),
	})
	if err != nil {
		return CurrentTaskList{}, fmt.Errorf("list qianwen current tasks: %w", err)
	}
	// The SQL projection emits one NULL sentinel when the caller is still
	// authorized but has no tasks. No rows means the binding/credential/grant
	// disappeared between the preliminary resolution and this authoritative read.
	if len(rows) == 0 {
		return CurrentTaskList{}, ErrPairingAccessDenied
	}

	type projectedTask struct {
		summary   CurrentTaskSummary
		createdAt time.Time
		taskID    pgtype.UUID
	}
	projected := make([]projectedTask, 0, len(rows))
	for _, row := range rows {
		if !row.HasTask {
			continue
		}
		if !row.TaskID.Valid || !row.CreatedAt.Valid || !row.DisplayTitle.Valid || !row.Source.Valid || !row.AgentName.Valid || !row.TaskStatus.Valid {
			return CurrentTaskList{}, errors.New("qianwen: invalid current-task projection")
		}
		summary := CurrentTaskSummary{
			TaskID:       util.UUIDToString(row.TaskID),
			DisplayTitle: row.DisplayTitle.String,
			Source:       row.Source.String,
			AgentName:    row.AgentName.String,
			Status:       currentTaskPublicStatus(row.TaskStatus.String),
			CreatedAt:    row.CreatedAt.Time,
		}
		if row.RequestID.Valid {
			summary.RequestID = util.UUIDToString(row.RequestID)
		}
		if row.StartedAt.Valid {
			startedAt := row.StartedAt.Time
			summary.StartedAt = &startedAt
		}
		projected = append(projected, projectedTask{
			summary:   summary,
			createdAt: row.CreatedAt.Time,
			taskID:    row.TaskID,
		})
	}

	result := CurrentTaskList{Tasks: make([]CurrentTaskSummary, 0, min(len(projected), invocation.Request.Limit))}
	if len(projected) > invocation.Request.Limit {
		result.HasMore = true
		projected = projected[:invocation.Request.Limit]
	}
	for _, task := range projected {
		result.Tasks = append(result.Tasks, task.summary)
	}
	if result.HasMore && len(projected) > 0 {
		last := projected[len(projected)-1]
		result.NextCursor = encodeCurrentTaskCursor(last.createdAt, last.taskID)
	}
	return result, nil
}

func currentTaskPublicStatus(raw string) string {
	switch raw {
	case "queued", "deferred":
		return "queued"
	case "dispatched", "running", "waiting_local_directory":
		return "running"
	default:
		return "unknown"
	}
}

func encodeCurrentTaskCursor(createdAt time.Time, taskID pgtype.UUID) string {
	payload := createdAt.UTC().Format(time.RFC3339Nano) + "\x00" + util.UUIDToString(taskID)
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func decodeCurrentTaskCursor(raw string) (pgtype.Timestamptz, pgtype.UUID, error) {
	if raw == "" {
		return pgtype.Timestamptz{}, pgtype.UUID{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != raw {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ErrInvalidRequest
	}
	createdRaw, idRaw, ok := strings.Cut(string(decoded), "\x00")
	if !ok || strings.Contains(idRaw, "\x00") {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ErrInvalidRequest
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdRaw)
	if err != nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ErrInvalidRequest
	}
	id, err := uuid.Parse(idRaw)
	if err != nil || id == uuid.Nil {
		return pgtype.Timestamptz{}, pgtype.UUID{}, ErrInvalidRequest
	}
	return pgtype.Timestamptz{Time: createdAt, Valid: true}, pgtype.UUID{Bytes: id, Valid: true}, nil
}
