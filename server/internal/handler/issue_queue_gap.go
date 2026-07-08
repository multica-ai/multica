package handler

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issueguard"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const queueGapInboxType = "queue_gap"

func (h *Handler) notifyQueueGapAfterStatusChange(ctx context.Context, prev, issue db.Issue) {
	h.notifyParentQueueGap(ctx, prev, issue)
	h.notifyProjectQueueGap(ctx, prev, issue)
}

func (h *Handler) notifyParentQueueGap(ctx context.Context, prev, issue db.Issue) {
	if !issue.ParentIssueID.Valid {
		return
	}

	parent, err := h.Queries.GetIssue(ctx, issue.ParentIssueID)
	if err != nil {
		slog.Warn("queue gap: failed to load parent",
			"error", err,
			"child_id", uuidToString(issue.ID),
			"parent_id", uuidToString(issue.ParentIssueID))
		return
	}

	children, err := h.Queries.ListChildIssues(ctx, issue.ParentIssueID)
	if err != nil {
		slog.Warn("queue gap: failed to list child issues",
			"error", err,
			"parent_id", uuidToString(issue.ParentIssueID))
		return
	}
	childStates := make([]issueguard.QueueGapChildState, 0, len(children))
	for _, child := range children {
		childStates = append(childStates, issueguard.QueueGapChildState{Status: child.Status, Metadata: child.Metadata})
	}

	if !issueguard.ShouldEmitParentQueueGap(prev.Status, issue.Status, parent.Status, parent.Metadata, childStates) {
		return
	}
	if h.hasOpenQueueGapInbox(ctx, parent.ID) {
		return
	}

	h.createQueueGapSystemComment(ctx, parent, issue)
	h.createQueueGapInboxItems(ctx, parent, issue, "parent_issue", h.queueGapRecipientsForIssue(ctx, parent))
}

func (h *Handler) notifyProjectQueueGap(ctx context.Context, prev, issue db.Issue) {
	if issue.ParentIssueID.Valid || !issue.ProjectID.Valid {
		return
	}
	if prev.Status != "todo" && prev.Status != "in_progress" {
		return
	}
	if issue.Status != "in_review" {
		return
	}

	project, err := h.Queries.GetProject(ctx, issue.ProjectID)
	if err != nil {
		slog.Warn("queue gap: failed to load project",
			"error", err,
			"issue_id", uuidToString(issue.ID),
			"project_id", uuidToString(issue.ProjectID))
		return
	}
	if project.Status != "in_progress" {
		return
	}
	if h.projectHasActiveIssue(ctx, issue.ProjectID) || h.projectHasExplicitWait(ctx, issue.ProjectID) {
		return
	}
	if h.hasOpenQueueGapInbox(ctx, issue.ID) {
		return
	}

	h.createQueueGapInboxItems(ctx, issue, issue, "project", h.queueGapRecipientsForProject(ctx, project, issue))
}

func (h *Handler) createQueueGapSystemComment(ctx context.Context, parent, trigger db.Issue) {
	content := "Queue gap detected: this parent issue is still `in_progress`, but its sub-issues now have no `todo` or `in_progress` work. Choose one next action: create or promote the next `todo`; mark the parent `blocked` and set `waiting_on` / `blocked_reason`; or close it as `done`/`cancelled`."

	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     parent.ID,
		WorkspaceID: parent.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     content,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		slog.Warn("queue gap: create system comment failed",
			"error", err,
			"parent_id", uuidToString(parent.ID),
			"trigger_issue_id", uuidToString(trigger.ID))
		return
	}

	h.publish(protocol.EventCommentCreated, uuidToString(parent.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         parent.Title,
		"issue_assignee_type": textToPtr(parent.AssigneeType),
		"issue_assignee_id":   uuidToPtr(parent.AssigneeID),
		"issue_status":        parent.Status,
	})
}

func (h *Handler) createQueueGapInboxItems(ctx context.Context, target, trigger db.Issue, scope string, recipients []pgtype.UUID) {
	if len(recipients) == 0 {
		return
	}

	targetID := uuidToString(target.ID)
	triggerID := uuidToString(trigger.ID)
	body := "No runnable work remains for this active scope. Choose one: create/promote the next `todo`; mark it `blocked` with `waiting_on` / `blocked_reason`; or close it as `done`/`cancelled`."
	details, _ := json.Marshal(map[string]any{
		"reason":           "queue_gap",
		"scope":            scope,
		"target_issue_id":  targetID,
		"trigger_issue_id": triggerID,
		"required_actions": []string{
			"create_or_promote_next_todo",
			"mark_blocked_with_waiting_on_or_blocked_reason",
			"close_as_done_or_cancelled",
		},
	})

	title := "Queue needs a next step: " + target.Title
	workspaceID := uuidToString(target.WorkspaceID)
	for _, recipientID := range recipients {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   target.WorkspaceID,
			RecipientType: "member",
			RecipientID:   recipientID,
			Type:          queueGapInboxType,
			Severity:      "action_required",
			IssueID:       target.ID,
			Title:         title,
			Body:          strToText(body),
			ActorType:     strToText("system"),
			ActorID:       pgtype.UUID{},
			Details:       details,
		})
		if err != nil {
			slog.Warn("queue gap: inbox write failed",
				"target_issue_id", targetID,
				"recipient_id", uuidToString(recipientID),
				"error", err)
			continue
		}

		h.publish(protocol.EventInboxNew, workspaceID, "system", "", map[string]any{
			"item": queueGapInboxPayload(item, target.Status),
		})
	}
}

func queueGapInboxPayload(item db.InboxItem, issueStatus string) map[string]any {
	return map[string]any{
		"id":             uuidToString(item.ID),
		"workspace_id":   uuidToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   uuidToString(item.RecipientID),
		"type":           item.Type,
		"severity":       item.Severity,
		"issue_id":       uuidToPtr(item.IssueID),
		"title":          item.Title,
		"body":           textToPtr(item.Body),
		"read":           item.Read,
		"archived":       item.Archived,
		"created_at":     timestampToString(item.CreatedAt),
		"actor_type":     textToPtr(item.ActorType),
		"actor_id":       uuidToPtr(item.ActorID),
		"details":        json.RawMessage(item.Details),
		"issue_status":   issueStatus,
	}
}

func (h *Handler) queueGapRecipientsForIssue(ctx context.Context, issue db.Issue) []pgtype.UUID {
	recipients := map[string]pgtype.UUID{}
	h.addQueueGapActorRecipients(ctx, issue.WorkspaceID, issue.AssigneeType, issue.AssigneeID, recipients)
	h.addQueueGapActorRecipients(ctx, issue.WorkspaceID, pgtype.Text{String: issue.CreatorType, Valid: true}, issue.CreatorID, recipients)

	if issue.ProjectID.Valid {
		if project, err := h.Queries.GetProject(ctx, issue.ProjectID); err == nil {
			h.addQueueGapActorRecipients(ctx, issue.WorkspaceID, project.LeadType, project.LeadID, recipients)
		}
	}

	subs, err := h.Queries.ListIssueSubscribers(ctx, issue.ID)
	if err == nil {
		for _, sub := range subs {
			if sub.UserType == "member" {
				recipients[uuidToString(sub.UserID)] = sub.UserID
			}
		}
	}
	return queueGapRecipientList(recipients)
}

func (h *Handler) queueGapRecipientsForProject(ctx context.Context, project db.Project, trigger db.Issue) []pgtype.UUID {
	recipients := map[string]pgtype.UUID{}
	h.addQueueGapActorRecipients(ctx, project.WorkspaceID, project.LeadType, project.LeadID, recipients)
	h.addQueueGapActorRecipients(ctx, trigger.WorkspaceID, trigger.AssigneeType, trigger.AssigneeID, recipients)
	h.addQueueGapActorRecipients(ctx, trigger.WorkspaceID, pgtype.Text{String: trigger.CreatorType, Valid: true}, trigger.CreatorID, recipients)
	return queueGapRecipientList(recipients)
}

func queueGapRecipientList(recipients map[string]pgtype.UUID) []pgtype.UUID {
	result := make([]pgtype.UUID, 0, len(recipients))
	for _, id := range recipients {
		result = append(result, id)
	}
	return result
}

func (h *Handler) addQueueGapActorRecipients(ctx context.Context, workspaceID pgtype.UUID, actorType pgtype.Text, actorID pgtype.UUID, recipients map[string]pgtype.UUID) {
	if !actorType.Valid || !actorID.Valid {
		return
	}

	switch actorType.String {
	case "member":
		recipients[uuidToString(actorID)] = actorID
	case "agent":
		h.addQueueGapAgentOwner(ctx, workspaceID, actorID, recipients)
	case "squad":
		squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          actorID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return
		}
		h.addQueueGapAgentOwner(ctx, workspaceID, squad.LeaderID, recipients)
		members, err := h.Queries.ListSquadMembers(ctx, squad.ID)
		if err != nil {
			return
		}
		for _, member := range members {
			if member.MemberType == "member" {
				recipients[uuidToString(member.MemberID)] = member.MemberID
			}
			if member.MemberType == "agent" {
				h.addQueueGapAgentOwner(ctx, workspaceID, member.MemberID, recipients)
			}
		}
	}
}

func (h *Handler) addQueueGapAgentOwner(ctx context.Context, workspaceID, agentID pgtype.UUID, recipients map[string]pgtype.UUID) {
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err != nil || !agent.OwnerID.Valid {
		return
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      agent.OwnerID,
		WorkspaceID: workspaceID,
	}); err != nil {
		return
	}
	recipients[uuidToString(agent.OwnerID)] = agent.OwnerID
}

func (h *Handler) hasOpenQueueGapInbox(ctx context.Context, issueID pgtype.UUID) bool {
	var exists bool
	if err := h.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			  FROM inbox_item
			 WHERE issue_id = $1
			   AND type = $2
			   AND archived = false
		)
	`, issueID, queueGapInboxType).Scan(&exists); err != nil {
		slog.Warn("queue gap: failed to check existing inbox", "issue_id", uuidToString(issueID), "error", err)
		return false
	}
	return exists
}

func (h *Handler) projectHasActiveIssue(ctx context.Context, projectID pgtype.UUID) bool {
	var exists bool
	if err := h.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			  FROM issue
			 WHERE project_id = $1
			   AND status IN ('todo', 'in_progress')
		)
	`, projectID).Scan(&exists); err != nil {
		slog.Warn("queue gap: failed to check active project issues", "project_id", uuidToString(projectID), "error", err)
		return true
	}
	return exists
}

func (h *Handler) projectHasExplicitWait(ctx context.Context, projectID pgtype.UUID) bool {
	var exists bool
	if err := h.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			  FROM issue
			 WHERE project_id = $1
			   AND status NOT IN ('done', 'cancelled')
			   AND (
			    status = 'blocked'
			    OR metadata ? 'waiting_on'
			    OR metadata ? 'blocked_reason'
			   )
		)
	`, projectID).Scan(&exists); err != nil {
		slog.Warn("queue gap: failed to check project waits", "project_id", uuidToString(projectID), "error", err)
		return true
	}
	return exists
}
