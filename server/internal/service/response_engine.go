package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ResponseEngineMode string

const (
	ResponseEngineLegacy  ResponseEngineMode = "LEGACY"
	ResponseEngineObserve ResponseEngineMode = "OBSERVE"
	ResponseEngineEnforce ResponseEngineMode = "ENFORCE"
)

var (
	ErrResponseFinalizerUnavailable = errors.New("response engine finalizer unavailable")
	ErrResponseSnapshotStale        = errors.New("response engine authoritative snapshot became stale")
)

type ResponseIssueSnapshot struct {
	ID         string         `json:"id"`
	Identifier string         `json:"identifier"`
	Title      string         `json:"title"`
	Status     string         `json:"status"`
	Revision   int64          `json:"revision"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type TaskResponseFinalizationInput struct {
	SchemaVersion string                  `json:"schema_version"`
	ResponseID    string                  `json:"response_id"`
	TaskID        string                  `json:"task_id"`
	WorkspaceID   string                  `json:"workspace_id"`
	Attempt       int32                   `json:"attempt"`
	TaskStatus    string                  `json:"task_status"`
	RawOutput     string                  `json:"raw_output"`
	Issue         *ResponseIssueSnapshot  `json:"issue,omitempty"`
	Children      []ResponseIssueSnapshot `json:"children,omitempty"`
}

type TaskResponseFinalizationResult struct {
	Rendered string `json:"rendered"`
}

// TaskResponseCompletionFence is minted only from Multica's trusted DB snapshot.
// It is never accepted from daemon/model payloads. ENFORCE passes it into the
// terminal transaction so the issue revision remains pinned through the task CAS.
type TaskResponseCompletionFence struct {
	IssueID     pgtype.UUID
	WorkspaceID pgtype.UUID
	Revision    int64
}

type FinalizedTaskResponse struct {
	Output string
	Fence  *TaskResponseCompletionFence
}

type TaskResponseFinalizer interface {
	FinalizeTaskCompletion(context.Context, TaskResponseFinalizationInput) (TaskResponseFinalizationResult, error)
}

func ParseResponseEngineMode(enabledRaw, enforceRaw string) (ResponseEngineMode, error) {
	enabled, err := parseResponseEngineBool(enabledRaw)
	if err != nil {
		return "", fmt.Errorf("RESPONSE_ENGINE_V1_ENABLED: %w", err)
	}
	enforce, err := parseResponseEngineBool(enforceRaw)
	if err != nil {
		return "", fmt.Errorf("RESPONSE_ENGINE_V1_ENFORCE: %w", err)
	}
	if enforce && !enabled {
		return "", errors.New("RESPONSE_ENGINE_V1_ENFORCE requires RESPONSE_ENGINE_V1_ENABLED")
	}
	if !enabled {
		return ResponseEngineLegacy, nil
	}
	if enforce {
		return ResponseEngineEnforce, nil
	}
	return ResponseEngineObserve, nil
}

func parseResponseEngineBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "0", "false", "no":
		return false, nil
	case "1", "true", "yes":
		return true, nil
	default:
		return false, fmt.Errorf("must be boolean-like, got %q", raw)
	}
}

func effectiveResponseEngineMode(mode ResponseEngineMode) ResponseEngineMode {
	if mode == "" {
		return ResponseEngineLegacy
	}
	return mode
}

func (s *TaskService) FinalizeCompletionOutput(
	ctx context.Context,
	task db.AgentTaskQueue,
	workspaceID string,
	rawOutput string,
) (string, error) {
	finalized, err := s.FinalizeCompletionOutputWithFence(ctx, task, workspaceID, rawOutput)
	return finalized.Output, err
}

func (s *TaskService) FinalizeCompletionOutputWithFence(
	ctx context.Context,
	task db.AgentTaskQueue,
	workspaceID string,
	rawOutput string,
) (FinalizedTaskResponse, error) {
	mode := effectiveResponseEngineMode(s.ResponseEngineMode)
	if mode == ResponseEngineLegacy {
		return FinalizedTaskResponse{Output: rawOutput}, nil
	}

	if s.ResponseFinalizer == nil {
		if mode == ResponseEngineObserve {
			return FinalizedTaskResponse{Output: rawOutput}, nil
		}
		return FinalizedTaskResponse{}, ErrResponseFinalizerUnavailable
	}

	input, err := s.buildTaskResponseFinalizationInput(ctx, task, workspaceID, rawOutput)
	if err != nil {
		if mode == ResponseEngineObserve {
			slog.Warn("response engine observe snapshot failed; keeping legacy output",
				"task_id", util.UUIDToString(task.ID), "error", err)
			return FinalizedTaskResponse{Output: rawOutput}, nil
		}
		return FinalizedTaskResponse{}, err
	}

	result, err := s.ResponseFinalizer.FinalizeTaskCompletion(ctx, input)
	if mode == ResponseEngineObserve {
		if err != nil {
			slog.Warn("response engine observe finalization failed; keeping legacy output",
				"task_id", input.TaskID, "response_id", input.ResponseID, "error", err)
		}
		return FinalizedTaskResponse{Output: rawOutput}, nil
	}
	if err != nil {
		return FinalizedTaskResponse{}, fmt.Errorf("response engine finalization: %w", err)
	}
	if strings.TrimSpace(result.Rendered) == "" {
		return FinalizedTaskResponse{}, errors.New("response engine finalization returned empty rendered response")
	}
	if err := s.validateTaskResponseSnapshotCurrent(ctx, task, input); err != nil {
		return FinalizedTaskResponse{}, err
	}
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil || !workspaceUUID.Valid {
		return FinalizedTaskResponse{}, errors.New("response engine workspace id is invalid")
	}
	if input.Issue == nil {
		return FinalizedTaskResponse{}, fmt.Errorf("%w: issue snapshot missing", ErrResponseSnapshotStale)
	}
	return FinalizedTaskResponse{
		Output: result.Rendered,
		Fence: &TaskResponseCompletionFence{
			IssueID:     task.IssueID,
			WorkspaceID: workspaceUUID,
			Revision:    input.Issue.Revision,
		},
	}, nil
}

func (s *TaskService) buildTaskResponseFinalizationInput(
	ctx context.Context,
	task db.AgentTaskQueue,
	workspaceID string,
	rawOutput string,
) (TaskResponseFinalizationInput, error) {
	taskID := util.UUIDToString(task.ID)
	if taskID == "" {
		return TaskResponseFinalizationInput{}, errors.New("response engine task id is required")
	}
	if workspaceID == "" {
		return TaskResponseFinalizationInput{}, errors.New("response engine workspace id is required")
	}
	if !task.IssueID.Valid {
		return TaskResponseFinalizationInput{}, errors.New("response engine issue-backed completion is required")
	}

	issue, err := s.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return TaskResponseFinalizationInput{}, fmt.Errorf("load response engine issue: %w", err)
	}
	prefix := s.getIssuePrefix(issue.WorkspaceID)
	root, err := responseIssueSnapshot(issue, prefix)
	if err != nil {
		return TaskResponseFinalizationInput{}, err
	}
	childrenRows, err := s.Queries.ListChildIssues(ctx, issue.ID)
	if err != nil {
		return TaskResponseFinalizationInput{}, fmt.Errorf("list response engine child issues: %w", err)
	}
	children := make([]ResponseIssueSnapshot, 0, len(childrenRows))
	for _, child := range childrenRows {
		snapshot, snapshotErr := responseIssueSnapshot(child, prefix)
		if snapshotErr != nil {
			return TaskResponseFinalizationInput{}, snapshotErr
		}
		children = append(children, snapshot)
	}

	return TaskResponseFinalizationInput{
		SchemaVersion: "1",
		ResponseID:    fmt.Sprintf("multica:%s:terminal:r%d", taskID, issue.Revision),
		TaskID:        taskID,
		WorkspaceID:   workspaceID,
		Attempt:       task.Attempt,
		TaskStatus:    task.Status,
		RawOutput:     rawOutput,
		Issue:         &root,
		Children:      children,
	}, nil
}

func responseIssueSnapshot(issue db.Issue, prefix string) (ResponseIssueSnapshot, error) {
	metadata := map[string]any{}
	if len(issue.Metadata) > 0 {
		if err := json.Unmarshal(issue.Metadata, &metadata); err != nil {
			return ResponseIssueSnapshot{}, fmt.Errorf("decode response engine issue metadata: %w", err)
		}
	}
	return ResponseIssueSnapshot{
		ID:         util.UUIDToString(issue.ID),
		Identifier: IssueIdentifier(prefix, issue.Number),
		Title:      issue.Title,
		Status:     issue.Status,
		Revision:   issue.Revision,
		Metadata:   metadata,
	}, nil
}

func (s *TaskService) validateTaskResponseSnapshotCurrent(
	ctx context.Context,
	originalTask db.AgentTaskQueue,
	input TaskResponseFinalizationInput,
) error {
	currentTask, err := s.Queries.GetAgentTask(ctx, originalTask.ID)
	if err != nil {
		return fmt.Errorf("%w: reload task: %v", ErrResponseSnapshotStale, err)
	}
	if currentTask.Status != originalTask.Status || currentTask.Attempt != originalTask.Attempt {
		return fmt.Errorf("%w: task status/attempt changed", ErrResponseSnapshotStale)
	}
	if input.Issue == nil || !originalTask.IssueID.Valid {
		return fmt.Errorf("%w: issue snapshot missing", ErrResponseSnapshotStale)
	}
	currentIssue, err := s.Queries.GetIssue(ctx, originalTask.IssueID)
	if err != nil {
		return fmt.Errorf("%w: reload issue: %v", ErrResponseSnapshotStale, err)
	}
	if currentIssue.Revision != input.Issue.Revision {
		return fmt.Errorf("%w: issue revision changed from %d to %d",
			ErrResponseSnapshotStale, input.Issue.Revision, currentIssue.Revision)
	}
	return nil
}
