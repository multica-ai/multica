package handler

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// noteRuntimeUnusable records, on the issue itself, that a trigger was refused
// because the target's agent CLI cannot run on its machine.
//
// Written for EVERY author, not only the unattended ones. The composer chip and
// the post-send toast do reach whoever was typing, but they carry the reason
// code alone — the repair command is too long for a chip and too important to
// put in something that disappears. An agent-authored @mention has nobody
// watching a response at all. So both get the same durable record, and the
// transient surfaces stay a preview of it rather than the only copy (MUL-6164).
//
// Best-effort by construction: the refusal already happened and is correct
// whether or not this note lands, so a failure here is logged, never returned.
func (h *Handler) noteRuntimeUnusable(ctx context.Context, issue db.Issue, agent db.Agent, verdict service.AgentVerdict) {
	content := service.RuntimeUnusableNotice(agent.Name, verdict)
	// author_type='system', author_id=zero UUID — same shape as the sub-issue
	// completion notice; clients branch on author_type, not the UUID value.
	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     content,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		slog.Warn("runtime unusable notice: create system comment failed",
			"error", err,
			"issue_id", uuidToString(issue.ID),
			"agent_id", uuidToString(agent.ID))
		return
	}
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
	})
}
