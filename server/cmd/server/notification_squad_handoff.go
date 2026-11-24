package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// maxSquadLineageHops bounds the delegated_from_task_id walk from a comment's
// run back to the squad leader run that started the chain.
const maxSquadLineageHops = 8

// isSquadHandoff reports whether a comment is one agent of a squad handing
// work to another (MUL-10008): the leader dispatching with an @agent or @squad
// mention, or a worker reporting back. A member's inbox drops these rows when
// they mute inbox.SquadHandoffsPref.
//
// The squad is the one whose leader run the comment's run descends from, so a
// squad called in with an @squad mention on an issue it is not assigned to
// counts too (MUL-10054). Without such a run it is the squad the issue is
// assigned to.
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
	// The run that posted the comment, when it is the author's own.
	var task *db.AgentTaskQueue
	if comment.SourceTaskID.Valid {
		t, err := queries.GetAgentTask(ctx, comment.SourceTaskID)
		if err != nil {
			return false
		}
		if t.AgentID == comment.AuthorID {
			task = &t
		}
	}

	squadID, ok := handoffSquadID(ctx, queries, comment.IssueID, task)
	if !ok {
		return false
	}
	squad, err := queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
		ID:          squadID,
		WorkspaceID: wsID,
	})
	if err != nil || squad.ArchivedAt.Valid {
		return false
	}

	if comment.AuthorID == squad.LeaderID {
		return addressesAgent
	}

	if task != nil {
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

	onRoster, err := queries.IsSquadMember(ctx, db.IsSquadMemberParams{
		SquadID:    squad.ID,
		MemberType: "agent",
		MemberID:   comment.AuthorID,
	})
	return err == nil && onRoster
}

// handoffSquadID is the squad a comment's run works for: the squad of the
// leader run on the comment's issue that the run descends from, else the squad
// the issue is assigned to. Runs on other issues are not followed, so an agent
// working its own issue is never counted for a squad that dispatched it
// elsewhere.
func handoffSquadID(ctx context.Context, queries *db.Queries, issueID pgtype.UUID, task *db.AgentTaskQueue) (pgtype.UUID, bool) {
	if task != nil {
		run := *task
		for range maxSquadLineageHops {
			if run.IssueID != issueID {
				break
			}
			if run.IsLeaderTask && run.SquadID.Valid {
				return run.SquadID, true
			}
			if !run.DelegatedFromTaskID.Valid {
				break
			}
			next, err := queries.GetAgentTask(ctx, run.DelegatedFromTaskID)
			if err != nil {
				break
			}
			run = next
		}
	}
	issue, err := queries.GetIssue(ctx, issueID)
	if err != nil || !issue.AssigneeID.Valid || issue.AssigneeType.String != "squad" {
		return pgtype.UUID{}, false
	}
	return issue.AssigneeID, true
}
