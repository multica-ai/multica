package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// deliverSquadResult runs when an agent run completes. A squad leader run that
// a handoff woke, and that neither posted a comment, started another agent run
// nor left a wakeup waiting on its issue, is where the squad stopped passing
// the work on: the handoff that woke it is the squad's result (MUL-10054).
// Members who muted squad handoffs never received it, so it is delivered to
// them now as a new_comment row marked inbox.SquadResultDetail; Deliver keeps
// it from everyone else, who already have the handoff's own row.
//
// A leader that dispatched again or is waiting (for CI, say) keeps the chain
// going, and one that wrote to people has delivered its own comment, so none
// of them delivers the handoff.
func deliverSquadResult(ctx context.Context, queries *db.Queries, bus *events.Bus, e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	taskID, _ := payload["task_id"].(string)
	taskUUID, err := util.ParseUUID(taskID)
	if err != nil {
		return
	}
	task, err := queries.GetAgentTask(ctx, taskUUID)
	if err != nil || !task.IsLeaderTask || !task.SquadID.Valid || !task.IssueID.Valid || !task.TriggerCommentID.Valid {
		return
	}
	handoff, err := queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{
		ID:          task.TriggerCommentID,
		WorkspaceID: parseUUID(e.WorkspaceID),
	})
	if err != nil || handoff.DeletedAt.Valid || handoff.AuthorType != "agent" || handoff.AuthorID == task.AgentID || handoff.IssueID != task.IssueID {
		return
	}
	handoffID := util.UUIDToString(handoff.ID)
	if !isSquadHandoff(ctx, queries, e.WorkspaceID, handoffID, parseMentions(handoff.Content)) {
		return
	}

	// FAIL OPEN: a run whose follow-up cannot be read delivers the handoff.
	passedOn, err := queries.TaskPassedWorkOn(ctx, db.TaskPassedWorkOnParams{
		IssueID: task.IssueID,
		TaskID:  task.ID,
	})
	if err != nil {
		slog.Warn("squad result: read leader run follow-up failed; delivering",
			"task_id", taskID, "error", err)
	} else if passedOn {
		return
	}

	issue, err := queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return
	}
	details, _ := json.Marshal(map[string]string{
		"comment_id":            handoffID,
		inbox.SquadResultDetail: "true",
	})
	author := events.Event{
		Type:        e.Type,
		WorkspaceID: e.WorkspaceID,
		ActorType:   "agent",
		ActorID:     util.UUIDToString(handoff.AuthorID),
	}
	notifySubscribers(ctx, queries, bus, util.UUIDToString(issue.ID), issue.Status, e.WorkspaceID, author,
		nil, "new_comment", "info",
		issue.Title, handoff.Content,
		details)
}
