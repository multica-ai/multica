package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// HandlerActionExecutor implements ActionExecutor by calling into the
// server's handler infrastructure.
type HandlerActionExecutor struct {
	Queries *db.Queries
	Callback ActionCallback
}

// ActionCallback is called to perform the actual write operations. It is
// implemented by the server's handler layer, which has access to the full
// HTTP/DB pipeline.
type ActionCallback func(ctx context.Context, action Action) error

// NewHandlerActionExecutor creates an executor backed by a handler callback.
func NewHandlerActionExecutor(queries *db.Queries, cb ActionCallback) *HandlerActionExecutor {
	return &HandlerActionExecutor{
		Queries:  queries,
		Callback: cb,
	}
}

// ExecuteAction dispatches an action to the appropriate handler.
func (e *HandlerActionExecutor) ExecuteAction(ctx context.Context, action Action, actorType, actorID string) error {
	switch action.Kind {
	case ActionPostComment:
		return e.executePostComment(ctx, action, actorType)
	case ActionMentionAgent:
		return e.executeMentionAgent(ctx, action)
	case ActionUpdateStatus:
		return e.executeUpdateStatus(ctx, action)
	case ActionSetMetadata:
		return e.executeSetMetadata(ctx, action)
	case ActionClearMetadata:
		return e.executeClearMetadata(ctx, action)
	case ActionEscalate:
		return e.executeEscalate(ctx, action)
	default:
		return fmt.Errorf("unknown action kind: %s", action.Kind)
	}
}

func (e *HandlerActionExecutor) executePostComment(ctx context.Context, a Action, actorType string) error {
	if a.Template == "" || a.TargetIssueID == "" {
		return fmt.Errorf("post_comment requires Template and TargetIssueID")
	}

	targetUUID := parseUUID(a.TargetIssueID)
	issue, err := e.Queries.GetIssue(ctx, targetUUID)
	if err != nil {
		return fmt.Errorf("get target issue: %w", err)
	}

	// Skip if target is done or cancelled (no point notifying)
	if issue.Status == "done" || issue.Status == "cancelled" {
		return nil
	}

	// Build the mention prefix for the target assignee
	mentionPrefix := e.buildTargetMention(ctx, issue)

	content := mentionPrefix + a.Template

	_, err = e.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     targetUUID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     content,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		return fmt.Errorf("create system comment: %w", err)
	}

	slog.Debug("notification: posted system comment",
		"target", a.TargetIssueID,
		"action_kind", string(a.Kind))
	return nil
}

func (e *HandlerActionExecutor) executeMentionAgent(ctx context.Context, a Action) error {
	if a.AgentID == "" || a.TargetIssueID == "" {
		return fmt.Errorf("mention_agent requires AgentID and TargetIssueID")
	}
	// Agent mentions are embedded in comments (via the post_comment action).
	// This is a no-op at the action level — the mention is added as a prefix
	// in executePostComment via buildTargetMention.
	return nil
}

func (e *HandlerActionExecutor) executeUpdateStatus(ctx context.Context, a Action) error {
	if a.TargetIssueID == "" || a.NewStatus == "" {
		return fmt.Errorf("update_status requires TargetIssueID and NewStatus")
	}

	targetUUID := parseUUID(a.TargetIssueID)
	issue, err := e.Queries.GetIssue(ctx, targetUUID)
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	if issue.Status == a.NewStatus {
		return nil // idempotent
	}

	_, err = e.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          targetUUID,
		Status:      a.NewStatus,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	slog.Debug("notification: updated issue status",
		"issue", a.TargetIssueID,
		"new_status", a.NewStatus)
	return nil
}

func (e *HandlerActionExecutor) executeSetMetadata(ctx context.Context, a Action) error {
	if a.TargetIssueID == "" || a.MetaKey == "" {
		return fmt.Errorf("set_metadata requires TargetIssueID and MetaKey")
	}

	valueJSON, err := json.Marshal(a.MetaValue)
	if err != nil {
		return fmt.Errorf("marshal metadata value: %w", err)
	}

	targetUUID := parseUUID(a.TargetIssueID)
	issue, err := e.Queries.GetIssue(ctx, targetUUID)
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	_, err = e.Queries.SetIssueMetadataKey(ctx, db.SetIssueMetadataKeyParams{
		ID:          targetUUID,
		Key:         a.MetaKey,
		Value:       valueJSON,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return fmt.Errorf("set metadata: %w", err)
	}

	slog.Debug("notification: set issue metadata",
		"issue", a.TargetIssueID,
		"key", a.MetaKey)
	return nil
}

func (e *HandlerActionExecutor) executeClearMetadata(ctx context.Context, a Action) error {
	if a.TargetIssueID == "" || a.MetaKey == "" {
		return fmt.Errorf("clear_metadata requires TargetIssueID and MetaKey")
	}

	targetUUID := parseUUID(a.TargetIssueID)
	issue, err := e.Queries.GetIssue(ctx, targetUUID)
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	_, err = e.Queries.DeleteIssueMetadataKey(ctx, db.DeleteIssueMetadataKeyParams{
		Key:         a.MetaKey,
		ID:          targetUUID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return fmt.Errorf("clear metadata: %w", err)
	}

	slog.Debug("notification: cleared issue metadata",
		"issue", a.TargetIssueID,
		"key", a.MetaKey)
	return nil
}

func (e *HandlerActionExecutor) executeEscalate(ctx context.Context, a Action) error {
	// Escalation creates a new issue assigned to the squad leader or
	// workspace owner. For initial implementation, escalate means a
	// strongly-worded comment + @mention of relevant leaders.
	// Full escalation (creating new issues) will be implemented later.
	slog.Warn("notification: escalate action not fully implemented",
		"target", a.TargetIssueID,
		"message", a.Template)
	return nil
}

// buildTargetMention creates a mention prefix for the target issue's assignee.
// Following the pattern in issue_child_done.go (MUL-2538): member assignees
// get no mention; agent/squad assignees get a mention link.
func (e *HandlerActionExecutor) buildTargetMention(ctx context.Context, issue db.Issue) string {
	if !issue.AssigneeType.Valid || !issue.AssigneeID.Valid {
		return ""
	}
	if issue.AssigneeType.String == "member" {
		return ""
	}

	var label string
	switch issue.AssigneeType.String {
	case "agent":
		agent, err := e.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          issue.AssigneeID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			return ""
		}
		label = sanitizeLabel(agent.Name)
	case "squad":
		squad, err := e.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          issue.AssigneeID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			return ""
		}
		label = sanitizeLabel(squad.Name)
	default:
		return ""
	}

	return fmt.Sprintf("[@%s](mention://%s/%s) ", label, issue.AssigneeType.String, uuidToString(issue.AssigneeID))
}

func sanitizeLabel(name string) string {
	result := ""
	for _, r := range name {
		if r != ']' && r != '[' && r != '(' && r != ')' {
			result += string(r)
		}
	}
	if result == "" {
		return "assignee"
	}
	return result
}
