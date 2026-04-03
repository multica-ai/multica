package service

// MULTICA-LOCAL: Rewritten for SQLite — pgtype.UUID → string, pgtype.Text → sql.NullString.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/mention"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

type TaskService struct {
	Queries *db.Queries
	Hub     *realtime.Hub
	Bus     *events.Bus
}

func NewTaskService(q *db.Queries, hub *realtime.Hub, bus *events.Bus) *TaskService {
	return &TaskService{Queries: q, Hub: hub, Bus: bus}
}

// EnqueueTaskForIssue creates a queued task for an agent-assigned issue.
func (s *TaskService) EnqueueTaskForIssue(ctx context.Context, issue db.Issue, triggerCommentID ...sql.NullString) (db.AgentTaskQueue, error) {
	if !issue.AssigneeID.Valid {
		slog.Error("task enqueue failed", "issue_id", issue.ID, "error", "issue has no assignee")
		return db.AgentTaskQueue{}, fmt.Errorf("issue has no assignee")
	}

	agent, err := s.Queries.GetAgent(ctx, issue.AssigneeID.String)
	if err != nil {
		slog.Error("task enqueue failed", "issue_id", issue.ID, "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		slog.Debug("task enqueue skipped: agent is archived", "issue_id", issue.ID, "agent_id", agent.ID)
		return db.AgentTaskQueue{}, fmt.Errorf("agent is archived")
	}

	var commentID sql.NullString
	if len(triggerCommentID) > 0 {
		commentID = triggerCommentID[0]
	}

	task, err := s.Queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{
		ID:               uuid.New().String(),
		AgentID:          agent.ID,
		RuntimeID:        agent.RuntimeID,
		IssueID:          issue.ID,
		Priority:         priorityToInt(issue.Priority),
		TriggerCommentID: commentID,
	})
	if err != nil {
		slog.Error("task enqueue failed", "issue_id", issue.ID, "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("create task: %w", err)
	}

	slog.Info("task enqueued", "task_id", task.ID, "issue_id", issue.ID, "agent_id", issue.AssigneeID.String)
	return task, nil
}

// EnqueueTaskForMention creates a queued task for a mentioned agent on an issue.
func (s *TaskService) EnqueueTaskForMention(ctx context.Context, issue db.Issue, agentID string, triggerCommentID sql.NullString) (db.AgentTaskQueue, error) {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		slog.Error("mention task enqueue failed: agent not found", "issue_id", issue.ID, "agent_id", agentID, "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("load agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		slog.Debug("mention task enqueue skipped: agent is archived", "issue_id", issue.ID, "agent_id", agentID)
		return db.AgentTaskQueue{}, fmt.Errorf("agent is archived")
	}

	task, err := s.Queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{
		ID:               uuid.New().String(),
		AgentID:          agentID,
		RuntimeID:        agent.RuntimeID,
		IssueID:          issue.ID,
		Priority:         priorityToInt(issue.Priority),
		TriggerCommentID: triggerCommentID,
	})
	if err != nil {
		slog.Error("mention task enqueue failed", "issue_id", issue.ID, "agent_id", agentID, "error", err)
		return db.AgentTaskQueue{}, fmt.Errorf("create task: %w", err)
	}

	slog.Info("mention task enqueued", "task_id", task.ID, "issue_id", issue.ID, "agent_id", agentID)
	return task, nil
}

// CancelTasksForIssue cancels all active tasks for an issue.
func (s *TaskService) CancelTasksForIssue(ctx context.Context, issueID string) error {
	return s.Queries.CancelAgentTasksByIssue(ctx, issueID)
}

// CancelTask cancels a single task by ID.
func (s *TaskService) CancelTask(ctx context.Context, taskID string) (*db.AgentTaskQueue, error) {
	task, err := s.Queries.CancelAgentTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("cancel task: %w", err)
	}

	slog.Info("task cancelled", "task_id", task.ID, "issue_id", task.IssueID)
	s.ReconcileAgentStatus(ctx, task.AgentID)
	s.broadcastTaskEvent(ctx, protocol.EventTaskCancelled, task)
	return &task, nil
}

// ClaimTask atomically claims the next queued task for an agent.
func (s *TaskService) ClaimTask(ctx context.Context, agentID string) (*db.AgentTaskQueue, error) {
	agent, err := s.Queries.GetAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}

	running, err := s.Queries.CountRunningTasks(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("count running tasks: %w", err)
	}
	if running >= agent.MaxConcurrentTasks {
		slog.Debug("task claim: no capacity", "agent_id", agentID, "running", running, "max", agent.MaxConcurrentTasks)
		return nil, nil
	}

	task, err := s.Queries.ClaimAgentTask(ctx, agentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			slog.Debug("task claim: no tasks available", "agent_id", agentID)
			return nil, nil
		}
		return nil, fmt.Errorf("claim task: %w", err)
	}

	slog.Info("task claimed", "task_id", task.ID, "agent_id", agentID)
	s.updateAgentStatus(ctx, agentID, "working")
	s.broadcastTaskDispatch(ctx, task)
	return &task, nil
}

// ClaimTaskForRuntime claims the next runnable task for a runtime.
func (s *TaskService) ClaimTaskForRuntime(ctx context.Context, runtimeID string) (*db.AgentTaskQueue, error) {
	tasks, err := s.Queries.ListPendingTasksByRuntime(ctx, runtimeID)
	if err != nil {
		return nil, fmt.Errorf("list pending tasks: %w", err)
	}

	triedAgents := map[string]struct{}{}
	for _, candidate := range tasks {
		if _, seen := triedAgents[candidate.AgentID]; seen {
			continue
		}
		triedAgents[candidate.AgentID] = struct{}{}

		task, err := s.ClaimTask(ctx, candidate.AgentID)
		if err != nil {
			return nil, err
		}
		if task != nil && task.RuntimeID == runtimeID {
			return task, nil
		}
	}

	return nil, nil
}

// StartTask transitions a dispatched task to running.
func (s *TaskService) StartTask(ctx context.Context, taskID string) (*db.AgentTaskQueue, error) {
	task, err := s.Queries.StartAgentTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("start task: %w", err)
	}

	slog.Info("task started", "task_id", task.ID, "issue_id", task.IssueID)
	return &task, nil
}

// CompleteTask marks a task as completed.
func (s *TaskService) CompleteTask(ctx context.Context, taskID string, result []byte, sessionID, workDir string) (*db.AgentTaskQueue, error) {
	task, err := s.Queries.CompleteAgentTask(ctx, db.CompleteAgentTaskParams{
		ID:        taskID,
		Result:    sql.NullString{String: string(result), Valid: len(result) > 0},
		SessionID: sql.NullString{String: sessionID, Valid: sessionID != ""},
		WorkDir:   sql.NullString{String: workDir, Valid: workDir != ""},
	})
	if err != nil {
		if existing, lookupErr := s.Queries.GetAgentTask(ctx, taskID); lookupErr == nil {
			slog.Warn("complete task failed: task not in running state",
				"task_id", taskID, "current_status", existing.Status,
				"issue_id", existing.IssueID, "agent_id", existing.AgentID)
		}
		return nil, fmt.Errorf("complete task: %w", err)
	}

	slog.Info("task completed", "task_id", task.ID, "issue_id", task.IssueID)

	if !task.TriggerCommentID.Valid {
		var payload protocol.TaskCompletedPayload
		if err := json.Unmarshal(result, &payload); err == nil {
			if payload.Output != "" {
				s.createAgentComment(ctx, task.IssueID, task.AgentID, redact.Text(payload.Output), "comment", task.TriggerCommentID)
			}
		}
	}

	s.ReconcileAgentStatus(ctx, task.AgentID)
	s.broadcastTaskEvent(ctx, protocol.EventTaskCompleted, task)
	return &task, nil
}

// FailTask marks a task as failed.
func (s *TaskService) FailTask(ctx context.Context, taskID string, errMsg string) (*db.AgentTaskQueue, error) {
	task, err := s.Queries.FailAgentTask(ctx, db.FailAgentTaskParams{
		ID:    taskID,
		Error: sql.NullString{String: errMsg, Valid: true},
	})
	if err != nil {
		if existing, lookupErr := s.Queries.GetAgentTask(ctx, taskID); lookupErr == nil {
			slog.Warn("fail task failed: task not in dispatched/running state",
				"task_id", taskID, "current_status", existing.Status,
				"issue_id", existing.IssueID, "agent_id", existing.AgentID)
		}
		return nil, fmt.Errorf("fail task: %w", err)
	}

	slog.Warn("task failed", "task_id", task.ID, "issue_id", task.IssueID, "error", errMsg)

	if errMsg != "" {
		s.createAgentComment(ctx, task.IssueID, task.AgentID, redact.Text(errMsg), "system", task.TriggerCommentID)
	}
	s.ReconcileAgentStatus(ctx, task.AgentID)
	s.broadcastTaskEvent(ctx, protocol.EventTaskFailed, task)
	return &task, nil
}

// ReportProgress broadcasts a progress update via the event bus.
func (s *TaskService) ReportProgress(ctx context.Context, taskID string, workspaceID string, summary string, step, total int) {
	s.Bus.Publish(events.Event{
		Type:        protocol.EventTaskProgress,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: protocol.TaskProgressPayload{
			TaskID:  taskID,
			Summary: summary,
			Step:    step,
			Total:   total,
		},
	})
}

// ReconcileAgentStatus checks running task count and sets agent status accordingly.
func (s *TaskService) ReconcileAgentStatus(ctx context.Context, agentID string) {
	running, err := s.Queries.CountRunningTasks(ctx, agentID)
	if err != nil {
		return
	}
	newStatus := "idle"
	if running > 0 {
		newStatus = "working"
	}
	slog.Debug("agent status reconciled", "agent_id", agentID, "status", newStatus, "running_tasks", running)
	s.updateAgentStatus(ctx, agentID, newStatus)
}

func (s *TaskService) updateAgentStatus(ctx context.Context, agentID string, status string) {
	agent, err := s.Queries.UpdateAgentStatus(ctx, db.UpdateAgentStatusParams{
		ID:     agentID,
		Status: status,
	})
	if err != nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventAgentStatus,
		WorkspaceID: agent.WorkspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload:     map[string]any{"agent": agentToMap(agent)},
	})
}

// LoadAgentSkills loads an agent's skills with their files for task execution.
func (s *TaskService) LoadAgentSkills(ctx context.Context, agentID string) []AgentSkillData {
	skills, err := s.Queries.ListAgentSkills(ctx, agentID)
	if err != nil || len(skills) == 0 {
		return nil
	}

	result := make([]AgentSkillData, 0, len(skills))
	for _, sk := range skills {
		data := AgentSkillData{Name: sk.Name, Content: sk.Content}
		files, _ := s.Queries.ListSkillFiles(ctx, sk.ID)
		for _, f := range files {
			data.Files = append(data.Files, AgentSkillFileData{Path: f.Path, Content: f.Content})
		}
		result = append(result, data)
	}
	return result
}

type AgentSkillData struct {
	Name    string               `json:"name"`
	Content string               `json:"content"`
	Files   []AgentSkillFileData `json:"files,omitempty"`
}

type AgentSkillFileData struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func priorityToInt(p string) int64 {
	switch p {
	case "urgent":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func (s *TaskService) broadcastTaskDispatch(ctx context.Context, task db.AgentTaskQueue) {
	var payload map[string]any
	if task.Context.Valid {
		json.Unmarshal([]byte(task.Context.String), &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["task_id"] = task.ID
	payload["runtime_id"] = task.RuntimeID

	workspaceID := ""
	if issue, err := s.Queries.GetIssue(ctx, task.IssueID); err == nil {
		workspaceID = issue.WorkspaceID
	}
	if workspaceID == "" {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventTaskDispatch,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload:     payload,
	})
}

func (s *TaskService) broadcastTaskEvent(ctx context.Context, eventType string, task db.AgentTaskQueue) {
	workspaceID := ""
	if issue, err := s.Queries.GetIssue(ctx, task.IssueID); err == nil {
		workspaceID = issue.WorkspaceID
	}
	if workspaceID == "" {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"task_id":  task.ID,
			"agent_id": task.AgentID,
			"issue_id": task.IssueID,
			"status":   task.Status,
		},
	})
}

func (s *TaskService) broadcastIssueUpdated(issue db.Issue) {
	prefix := s.getIssuePrefix(issue.WorkspaceID)
	s.Bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: issue.WorkspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload:     map[string]any{"issue": issueToMap(issue, prefix)},
	})
}

func (s *TaskService) getIssuePrefix(workspaceID string) string {
	ws, err := s.Queries.GetWorkspace(context.Background(), workspaceID)
	if err != nil {
		return ""
	}
	return ws.IssuePrefix
}

func (s *TaskService) createAgentComment(ctx context.Context, issueID, agentID string, content, commentType string, parentID sql.NullString) {
	if content == "" {
		return
	}
	issue, err := s.Queries.GetIssue(ctx, issueID)
	if err != nil {
		return
	}
	content = mention.ExpandIssueIdentifiers(ctx, s.Queries, issue.WorkspaceID, content)
	comment, err := s.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID:          uuid.New().String(),
		IssueID:     issueID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "agent",
		AuthorID:    agentID,
		Content:     content,
		Type:        commentType,
		ParentID:    parentID,
	})
	if err != nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: issue.WorkspaceID,
		ActorType:   "agent",
		ActorID:     agentID,
		Payload: map[string]any{
			"comment": map[string]any{
				"id":          comment.ID,
				"issue_id":    comment.IssueID,
				"author_type": comment.AuthorType,
				"author_id":   comment.AuthorID,
				"content":     comment.Content,
				"type":        comment.Type,
				"parent_id":   util.NullStringToPtr(comment.ParentID),
				"created_at":  comment.CreatedAt,
			},
			"issue_title":  issue.Title,
			"issue_status": issue.Status,
		},
	})
}

func issueToMap(issue db.Issue, issuePrefix string) map[string]any {
	return map[string]any{
		"id":              issue.ID,
		"workspace_id":    issue.WorkspaceID,
		"number":          issue.Number,
		"identifier":      issuePrefix + "-" + strconv.Itoa(int(issue.Number)),
		"title":           issue.Title,
		"description":     util.NullStringToPtr(issue.Description),
		"status":          issue.Status,
		"priority":        issue.Priority,
		"assignee_type":   util.NullStringToPtr(issue.AssigneeType),
		"assignee_id":     util.NullStringToPtr(issue.AssigneeID),
		"creator_type":    issue.CreatorType,
		"creator_id":      issue.CreatorID,
		"parent_issue_id": util.NullStringToPtr(issue.ParentIssueID),
		"position":        issue.Position,
		"due_date":        util.NullStringToPtr(issue.DueDate),
		"created_at":      issue.CreatedAt,
		"updated_at":      issue.UpdatedAt,
	}
}

func agentToMap(a db.Agent) map[string]any {
	var rc any
	json.Unmarshal([]byte(a.RuntimeConfig), &rc)
	var tools any
	json.Unmarshal([]byte(a.Tools), &tools)
	var triggers any
	json.Unmarshal([]byte(a.Triggers), &triggers)
	return map[string]any{
		"id":                   a.ID,
		"workspace_id":         a.WorkspaceID,
		"runtime_id":           a.RuntimeID,
		"name":                 a.Name,
		"description":          a.Description,
		"avatar_url":           util.NullStringToPtr(a.AvatarUrl),
		"runtime_mode":         a.RuntimeMode,
		"runtime_config":       rc,
		"visibility":           a.Visibility,
		"status":               a.Status,
		"max_concurrent_tasks": a.MaxConcurrentTasks,
		"owner_id":             util.NullStringToPtr(a.OwnerID),
		"skills":               []any{},
		"tools":                tools,
		"triggers":             triggers,
		"created_at":           a.CreatedAt,
		"updated_at":           a.UpdatedAt,
		"archived_at":          util.NullStringToPtr(a.ArchivedAt),
		"archived_by":          util.NullStringToPtr(a.ArchivedBy),
	}
}
