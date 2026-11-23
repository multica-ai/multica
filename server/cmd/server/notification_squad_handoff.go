package main

import (
	"context"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// isSquadHandoff reports whether a comment is one agent of the squad its issue
// is assigned to handing work to another (MUL-10008): the leader dispatching
// with an @agent or @squad mention, or a worker reporting back. A member's
// inbox drops these rows when they mute inbox.SquadHandoffsPref.
//
// A worker is a roster agent of the squad, or any agent whose run the squad's
// leader dispatched (delegated_from_task_id points at a leader task of this
// squad), since a leader can pull in agents from outside the roster. A run a
// person started directly answers that person, so its comment is not a
// handoff even when the agent is on the roster.
//
// A comment that mentions a member or @all is addressed to people and is never
// a handoff, whatever else it mentions.
//
// FAIL OPEN: every read that fails answers false, so the comment is delivered.
// A spare inbox row costs a dismissal; a dropped one can strand a reply.
func isSquadHandoff(ctx context.Context, queries *db.Queries, workspaceID, commentID string, mentions []mention) bool {
	if commentID == "" {
		return false
	}
	addressesAgent := false
	for _, m := range mentions {
		switch m.Type {
		case "member", "all":
			return false
		case "agent", "squad":
			addressesAgent = true
		}
	}

	wsID := parseUUID(workspaceID)
	comment, err := queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID:          parseUUID(commentID),
		WorkspaceID: wsID,
	})
	if err != nil || comment.AuthorType != "agent" || !comment.AuthorID.Valid {
		return false
	}
	issue, err := queries.GetIssue(ctx, comment.IssueID)
	if err != nil || !issue.AssigneeID.Valid || issue.AssigneeType.String != "squad" {
		return false
	}
	squad, err := queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          issue.AssigneeID,
		WorkspaceID: wsID,
	})
	if err != nil || squad.ArchivedAt.Valid {
		return false
	}

	if comment.AuthorID == squad.LeaderID {
		return addressesAgent
	}

	if comment.SourceTaskID.Valid {
		task, err := queries.GetAgentTask(ctx, comment.SourceTaskID)
		if err != nil {
			return false
		}
		if task.AgentID == comment.AuthorID {
			if task.DelegatedFromTaskID.Valid {
				source, err := queries.GetAgentTask(ctx, task.DelegatedFromTaskID)
				if err == nil && source.IsLeaderTask && source.SquadID == squad.ID {
					return true
				}
			}
			if task.TriggerCommentID.Valid {
				trigger, err := queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
					ID:          task.TriggerCommentID,
					WorkspaceID: wsID,
				})
				if err != nil || trigger.AuthorType == "member" {
					return false
				}
			}
		}
	}

	onRoster, err := queries.IsSquadMember(ctx, db.IsSquadMemberParams{
		SquadID:    squad.ID,
		MemberType: "agent",
		MemberID:   comment.AuthorID,
	})
	return err == nil && onRoster
}
