package approvals

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

var (
	ErrAgentRequired    = errors.New("request_approval requires agent context")
	ErrCapabilityNeeded = errors.New("request_approval: capability is required")
	ErrReasonNeeded     = errors.New("request_approval: reason is required")
)

// IntakeRequester is the shared write seam used by agent-callable approval tools.
type IntakeRequester interface {
	Intake(context.Context, IntakeParams) (cerebrodb.CerebroApprovalRequest, error)
}

// AgentRequest contains server-owned identity and origin context plus the
// model-supplied approval description.
type AgentRequest struct {
	WorkspaceID      pgtype.UUID
	AgentID          pgtype.UUID
	TaskID           pgtype.UUID
	IssueID          pgtype.UUID
	ChatSessionID    pgtype.UUID
	TriggerCommentID pgtype.UUID
	Capability       string
	Resource         string
	Reason           string
	Surface          string
	AuditSurface     string
}

// RequestFromAgent validates one approval request and records a pending row.
func RequestFromAgent(ctx context.Context, requester IntakeRequester, req AgentRequest) (cerebrodb.CerebroApprovalRequest, error) {
	if requester == nil || !req.WorkspaceID.Valid || !req.AgentID.Valid {
		return cerebrodb.CerebroApprovalRequest{}, ErrAgentRequired
	}
	capability := strings.TrimSpace(req.Capability)
	if capability == "" {
		return cerebrodb.CerebroApprovalRequest{}, ErrCapabilityNeeded
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return cerebrodb.CerebroApprovalRequest{}, ErrReasonNeeded
	}
	surface := normaliseOriginSurface(req.Surface)
	origin := map[string]any{"surface": surface}
	putOriginUUID(origin, "task_id", req.TaskID)
	putOriginUUID(origin, "issue_id", req.IssueID)
	putOriginUUID(origin, "chat_session_id", req.ChatSessionID)
	putOriginUUID(origin, "trigger_comment_id", req.TriggerCommentID)

	return requester.Intake(ctx, IntakeParams{
		WorkspaceID:   req.WorkspaceID,
		RequesterType: RequesterAgent,
		RequesterID:   req.AgentID,
		Agent:         req.AgentID,
		Capability:    capability,
		Resource:      strings.TrimSpace(req.Resource),
		Reason:        reason,
		Context:       origin,
		Surface:       req.AuditSurface,
	})
}

func normaliseOriginSurface(surface string) string {
	switch strings.ToLower(strings.TrimSpace(surface)) {
	case "chat", "issue", "channel", "dm", "autopilot":
		return strings.ToLower(strings.TrimSpace(surface))
	default:
		return "issue"
	}
}

func putOriginUUID(dst map[string]any, key string, id pgtype.UUID) {
	if id.Valid {
		dst[key] = util.UUIDToString(id)
	}
}
