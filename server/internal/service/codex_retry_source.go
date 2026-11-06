package service

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type codexRetrySource struct {
	Route          string `json:"route"`
	CommentID      string `json:"comment_id"`
	Revision       int64  `json:"revision"`
	ParentID       string `json:"parent_id"`
	ParentRevision int64  `json:"parent_revision"`
	RootID         string `json:"root_id"`
	RootRevision   int64  `json:"root_revision"`
}

func retrySourceSnapshot(task db.AgentTaskQueue) (codexRetrySource, bool) {
	var value struct {
		Source codexRetrySource `json:"codex_retry_source"`
	}
	if json.Unmarshal(task.Context, &value) != nil || value.Source.Route == "" {
		return codexRetrySource{}, false
	}
	return value.Source, true
}

func commentHasMention(content, kind string, id pgtype.UUID) bool {
	for _, mention := range util.ParseMentions(content) {
		if mention.Type == kind && mention.ID == util.UUIDToString(id) {
			return true
		}
	}
	return false
}

// codexRetrySourceProblem checks the saved route and live authority for an
// issue-linked overload child. The SQL insert checks structural facts again
// inside the failure transaction; this check also covers invocation rights.
func (s *TaskService) codexRetrySourceProblem(ctx context.Context, task db.AgentTaskQueue, agent db.Agent) string {
	if !task.IssueID.Valid {
		return ""
	}
	issue, err := s.Queries.GetIssue(ctx, task.IssueID)
	if err != nil || (issue.Status != "todo" && issue.Status != "in_progress") || issue.WorkspaceID != agent.WorkspaceID {
		return "Codex retry stopped because the issue is no longer runnable."
	}
	source, ok := retrySourceSnapshot(task)
	if !ok {
		return "Codex retry stopped because its original trigger route cannot be verified."
	}
	if source.Route == "issue_assignee" {
		assigned := issue.AssigneeType.String == "agent" && issue.AssigneeID == task.AgentID
		if issue.AssigneeType.String == "squad" && issue.AssigneeID.Valid && issue.AssigneeID == task.SquadID {
			squad, squadErr := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: issue.AssigneeID, WorkspaceID: issue.WorkspaceID})
			assigned = squadErr == nil && !squad.ArchivedAt.Valid && squad.LeaderID == task.AgentID
		}
		if !assigned {
			return "Codex retry stopped because the issue assignment changed."
		}
	} else if source.Route != "mention_agent" && source.Route != "mention_squad_leader" &&
		source.Route != "thread_parent" && source.Route != "conversation" && source.Route != "conversation_continuation" {
		return "Codex retry stopped because its original trigger route is unknown."
	}
	if !task.TriggerCommentID.Valid {
		if source.Route == "issue_assignee" && source.CommentID == "" {
			return ""
		}
		return "Codex retry stopped because the source comment is unavailable."
	}
	if source.CommentID != util.UUIDToString(task.TriggerCommentID) || source.Revision < 1 {
		return "Codex retry stopped because the source comment changed."
	}
	comment, err := s.Queries.GetComment(ctx, task.TriggerCommentID)
	if err != nil || comment.DeletedAt.Valid || comment.IssueID != task.IssueID || comment.WorkspaceID != issue.WorkspaceID || comment.Revision != source.Revision {
		return "Codex retry stopped because the source comment changed or was deleted."
	}
	if comment.ParentID.Valid && !s.codexRetryParentValid(ctx, comment, source, pgtype.UUID{}) {
		return "Codex retry stopped because the replied-to comment changed or was deleted."
	}
	if comment.AuthorType == "member" {
		if _, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: comment.AuthorID, WorkspaceID: issue.WorkspaceID}); err != nil {
			return "Codex retry stopped because the source author lost issue access."
		}
		if !CanMemberInvokeAgent(ctx, s.Queries, agent, comment.AuthorID, issue.WorkspaceID) {
			return "Codex retry stopped because the source author can no longer invoke the agent."
		}
	} else if comment.AuthorType == "agent" {
		if !comment.SourceTaskID.Valid || !comment.AuthorID.Valid {
			return "Codex retry stopped because agent delegation cannot be verified."
		}
		author, err := s.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: comment.AuthorID, WorkspaceID: issue.WorkspaceID})
		if err != nil || author.ArchivedAt.Valid {
			return "Codex retry stopped because the source agent lost issue access."
		}
		origin, err := s.Queries.GetAgentTask(ctx, comment.SourceTaskID)
		if err != nil || origin.AgentID != comment.AuthorID || origin.IssueID != task.IssueID || origin.OriginatorUserID != task.OriginatorUserID {
			return "Codex retry stopped because agent delegation changed."
		}
		if task.OriginatorUserID.Valid {
			if _, err := s.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: task.OriginatorUserID, WorkspaceID: issue.WorkspaceID}); err != nil {
				return "Codex retry stopped because the delegating member lost issue access."
			}
		}
		if !s.canDelegationInvokeAgent(ctx, agent, task.OriginatorUserID, issue.WorkspaceID) {
			return "Codex retry stopped because agent delegation can no longer invoke the target."
		}
	} else {
		return "Codex retry stopped because the source author is not verifiable."
	}
	if source.Route == "issue_assignee" {
		return ""
	}
	switch source.Route {
	case "mention_agent":
		if commentHasMention(comment.Content, "agent", task.AgentID) {
			return ""
		}
	case "mention_squad_leader":
		if task.SquadID.Valid && commentHasMention(comment.Content, "squad", task.SquadID) {
			squad, err := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: task.SquadID, WorkspaceID: issue.WorkspaceID})
			if err == nil && !squad.ArchivedAt.Valid && squad.LeaderID == task.AgentID {
				return ""
			}
		}
	case "thread_parent":
		if s.codexRetryParentValid(ctx, comment, source, task.AgentID) {
			return ""
		}
	case "conversation", "conversation_continuation":
		if !s.codexRetryParentValid(ctx, comment, source, pgtype.UUID{}) {
			break
		}
		rootID, err := util.ParseUUID(source.RootID)
		if err != nil || !rootID.Valid {
			break
		}
		root, err := s.Queries.GetComment(ctx, rootID)
		if err != nil || root.DeletedAt.Valid || root.IssueID != task.IssueID || root.Revision != source.RootRevision {
			break
		}
		hasExplicitOwner := false
		for _, mention := range util.ParseMentions(root.Content) {
			if mention.Type == "agent" {
				hasExplicitOwner = true
				if mention.ID == util.UUIDToString(task.AgentID) {
					return ""
				}
				break
			}
			if mention.Type == "squad" {
				hasExplicitOwner = true
				if task.SquadID.Valid && mention.ID == util.UUIDToString(task.SquadID) {
					squad, err := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: task.SquadID, WorkspaceID: issue.WorkspaceID})
					if err == nil && !squad.ArchivedAt.Valid && squad.LeaderID == task.AgentID {
						return ""
					}
				}
				break
			}
		}
		if hasExplicitOwner {
			break
		}
		owners, err := s.Queries.ListConversationRootOwners(ctx, db.ListConversationRootOwnersParams{IssueID: task.IssueID, TriggerCommentID: rootID})
		if err == nil {
			for _, owner := range owners {
				if owner.AgentID == task.AgentID {
					return ""
				}
			}
		}
	}
	return "Codex retry stopped because the original comment no longer routes to this agent."
}

func (s *TaskService) codexRetryParentValid(ctx context.Context, comment db.Comment, source codexRetrySource, author pgtype.UUID) bool {
	if !comment.ParentID.Valid || source.ParentID != util.UUIDToString(comment.ParentID) || source.ParentRevision < 1 {
		return false
	}
	parent, err := s.Queries.GetComment(ctx, comment.ParentID)
	return err == nil && !parent.DeletedAt.Valid && parent.IssueID == comment.IssueID &&
		parent.Revision == source.ParentRevision && (!author.Valid || parent.AuthorType == "agent" && parent.AuthorID == author)
}

func (s *TaskService) canDelegationInvokeAgent(ctx context.Context, agent db.Agent, originator, workspaceID pgtype.UUID) bool {
	if originator.Valid && CanMemberInvokeAgent(ctx, s.Queries, agent, originator, workspaceID) {
		return true
	}
	if originator.Valid || agent.PermissionMode != "public_to" {
		return false
	}
	targets, err := s.Queries.ListAgentInvocationTargets(ctx, agent.ID)
	if err != nil {
		return false
	}
	for _, target := range targets {
		if target.TargetType == "workspace" {
			return true
		}
	}
	return false
}
