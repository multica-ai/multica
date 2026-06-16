package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// registerReferenceListeners wires up event bus listeners that detect issue
// mentions in comments and issue descriptions, and record "referenced_by"
// activity entries on the target issues.
func registerReferenceListeners(bus *events.Bus, queries *db.Queries) {
	ctx := context.Background()

	// comment:created — scan new comment content for issue mentions
	bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		var issueID, commentID, content string
		switch c := payload["comment"].(type) {
		case map[string]any:
			issueID, _ = c["issue_id"].(string)
			commentID, _ = c["id"].(string)
			content, _ = c["content"].(string)
		default:
			// handler.CommentResponse — use reflection-free type assertion via
			// the map encoding path; if the comment was published as a struct,
			// try marshalling and unmarshalling to extract the fields.
			data, err := json.Marshal(payload["comment"])
			if err != nil {
				return
			}
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				return
			}
			issueID, _ = m["issue_id"].(string)
			commentID, _ = m["id"].(string)
			content, _ = m["content"].(string)
		}

		if issueID == "" || commentID == "" || content == "" {
			return
		}

		processIssueReferences(ctx, bus, queries, e.WorkspaceID, issueID, "comment", commentID, e.ActorType, e.ActorID, content)
	})

	// comment:updated — rescan updated comment content for issue mentions
	bus.Subscribe(protocol.EventCommentUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		var issueID, commentID, content string
		data, err := json.Marshal(payload["comment"])
		if err != nil {
			return
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return
		}
		issueID, _ = m["issue_id"].(string)
		commentID, _ = m["id"].(string)
		content, _ = m["content"].(string)

		if issueID == "" || commentID == "" || content == "" {
			return
		}

		processIssueReferences(ctx, bus, queries, e.WorkspaceID, issueID, "comment", commentID, e.ActorType, e.ActorID, content)
	})

	// issue:created — scan description for issue mentions
	bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issue, ok := issueEventIssueFromPayload(payload["issue"])
		if !ok || issue.Description == nil || *issue.Description == "" {
			return
		}
		processIssueReferences(ctx, bus, queries, issue.WorkspaceID, issue.ID, "description", issue.ID, e.ActorType, e.ActorID, *issue.Description)
	})

	// issue:updated — scan description if it changed
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		descriptionChanged, _ := payload["description_changed"].(bool)
		if !descriptionChanged {
			return
		}
		issue, ok := issueEventIssueFromPayload(payload["issue"])
		if !ok || issue.Description == nil || *issue.Description == "" {
			return
		}
		processIssueReferences(ctx, bus, queries, issue.WorkspaceID, issue.ID, "description", issue.ID, e.ActorType, e.ActorID, *issue.Description)
	})

	// channel_message:created — scan channel message content for issue mentions
	bus.Subscribe(protocol.EventChannelMessageCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		msg, ok := payload["message"].(map[string]any)
		if !ok {
			data, err := json.Marshal(payload["message"])
			if err != nil {
				return
			}
			if err := json.Unmarshal(data, &msg); err != nil {
				return
			}
		}
		content, _ := msg["content"].(string)
		messageID, _ := msg["id"].(string)
		channelID, _ := payload["channel_id"].(string)
		if content == "" || messageID == "" {
			return
		}
		processChannelMessageReferences(ctx, bus, queries, e.WorkspaceID, channelID, messageID, e.ActorType, e.ActorID, content)
	})
}

// processIssueReferences parses issue mentions from content and creates
// "referenced_by" activity entries on each target issue (deduped).
func processIssueReferences(
	ctx context.Context,
	bus *events.Bus,
	queries *db.Queries,
	workspaceID, sourceIssueID, sourceType, sourceID,
	actorType, actorID, content string,
) {
	mentions := util.ParseMentions(content)

	// Resolve source issue info once (for the activity details).
	sourceIssue, err := queries.GetIssue(ctx, parseUUID(sourceIssueID))
	if err != nil {
		slog.Error("reference: failed to get source issue",
			"source_issue_id", sourceIssueID, "error", err)
		return
	}

	workspace, err := queries.GetWorkspace(ctx, sourceIssue.WorkspaceID)
	if err != nil {
		slog.Error("reference: failed to get workspace",
			"workspace_id", workspaceID, "error", err)
		return
	}
	sourceIdentifier := fmt.Sprintf("%s-%d", workspace.IssuePrefix, sourceIssue.Number)

	for _, m := range mentions {
		if m.Type != "issue" {
			continue
		}
		targetIssueID := m.ID
		if targetIssueID == sourceIssueID {
			// Skip self-references.
			continue
		}

		// Dedup: skip if this exact reference already exists.
		_, err := queries.CheckReferenceActivityExists(ctx, db.CheckReferenceActivityExistsParams{
			IssueID:       parseUUID(targetIssueID),
			SourceIssueID: sourceIssueID,
			SourceType:    sourceType,
			SourceID:      sourceID,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("reference: failed to check existing reference activity",
				"target_issue_id", targetIssueID, "error", err)
			continue
		}
		if err == nil {
			// Already recorded.
			continue
		}

		details, _ := json.Marshal(map[string]string{
			"source_issue_id":         sourceIssueID,
			"source_issue_identifier": sourceIdentifier,
			"source_issue_title":      sourceIssue.Title,
			"source_type":             sourceType,
			"source_id":               sourceID,
			"actor_type":              actorType,
			"actor_id":                actorID,
		})

		activity, err := queries.CreateActivity(ctx, db.CreateActivityParams{
			WorkspaceID: parseUUID(workspaceID),
			IssueID:     parseUUID(targetIssueID),
			ActorType:   util.StrToText(actorType),
			ActorID:     optionalUUID(actorID),
			Action:      "referenced_by",
			Details:     details,
		})
		if err != nil {
			slog.Error("reference: failed to create referenced_by activity",
				"target_issue_id", targetIssueID,
				"source_issue_id", sourceIssueID,
				"error", err)
			continue
		}

		// Publish activity:created for WS broadcasting.
		dummyEvent := events.Event{
			WorkspaceID: workspaceID,
			ActorType:   actorType,
			ActorID:     actorID,
		}
		publishActivityEvent(bus, dummyEvent, activity)
	}
}

// processChannelMessageReferences parses issue mentions from a channel message
// and creates "referenced_by" activity entries on each target issue.
func processChannelMessageReferences(
	ctx context.Context,
	bus *events.Bus,
	queries *db.Queries,
	workspaceID, channelID, messageID,
	actorType, actorID, content string,
) {
	mentions := util.ParseMentions(content)

	var channelName string
	if channelID != "" {
		if ch, err := queries.GetChannel(ctx, db.GetChannelParams{
			ID:          parseUUID(channelID),
			WorkspaceID: parseUUID(workspaceID),
		}); err == nil {
			channelName = ch.Name
		}
	}

	for _, m := range mentions {
		if m.Type != "issue" {
			continue
		}
		targetIssueID := m.ID

		// Dedup: skip if this exact reference already exists.
		_, err := queries.CheckChannelReferenceActivityExists(ctx, db.CheckChannelReferenceActivityExistsParams{
			IssueID:  parseUUID(targetIssueID),
			SourceID: messageID,
		})
		if err == nil {
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("channel reference: failed to check existing activity",
				"target_issue_id", targetIssueID, "error", err)
			continue
		}

		details, _ := json.Marshal(map[string]string{
			"source_type":         "channel_message",
			"source_id":           messageID,
			"source_channel_id":   channelID,
			"source_channel_name": channelName,
			"actor_type":          actorType,
			"actor_id":            actorID,
		})

		activity, err := queries.CreateActivity(ctx, db.CreateActivityParams{
			WorkspaceID: parseUUID(workspaceID),
			IssueID:     parseUUID(targetIssueID),
			ActorType:   util.StrToText(actorType),
			ActorID:     optionalUUID(actorID),
			Action:      "referenced_by",
			Details:     details,
		})
		if err != nil {
			slog.Error("channel reference: failed to create referenced_by activity",
				"target_issue_id", targetIssueID,
				"message_id", messageID,
				"error", err)
			continue
		}

		dummyEvent := events.Event{
			WorkspaceID: workspaceID,
			ActorType:   actorType,
			ActorID:     actorID,
		}
		publishActivityEvent(bus, dummyEvent, activity)
	}
}
