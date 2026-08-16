package service

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// noteRuntimeUnusable leaves the refusal on the issue when an assignment cannot
// be enqueued because the assignee's agent CLI cannot run on its machine.
//
// Assignment is the trigger with no reply anyone reads: the API returns the
// updated issue, and nothing in it says "and by the way, nothing will run".
// Without this the user gets exactly the silence this change exists to remove
// (MUL-6164).
//
// Best-effort: the refusal is correct whether or not the note lands, so a
// failure here is logged rather than returned.
func (s *IssueService) noteRuntimeUnusable(ctx context.Context, issue db.Issue, verdict AgentVerdict) {
	name := ""
	if agent, err := s.Queries.GetAgent(ctx, issue.AssigneeID); err == nil {
		name = agent.Name
	}
	// author_type='system', author_id=zero UUID. The zero UUID is a valid 16
	// byte value and the column is NOT NULL; clients branch on author_type.
	created, err := s.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     RuntimeUnusableNotice(name, verdict),
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		slog.Warn("runtime unusable notice: create system comment failed",
			"error", err, "issue_id", util.UUIDToString(issue.ID))
		return
	}
	comment := created.Comment()
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: util.UUIDToString(issue.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"comment": map[string]any{
				"id":          util.UUIDToString(comment.ID),
				"issue_id":    util.UUIDToString(comment.IssueID),
				"author_type": comment.AuthorType,
				"author_id":   util.UUIDToString(comment.AuthorID),
				"content":     comment.Content,
				"type":        comment.Type,
				"revision":    comment.Revision,
			},
			"issue_title":    issue.Title,
			"issue_revision": created.IssueRevision,
		},
	})
}
