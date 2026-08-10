package main

// FIR-4930 — moving the inbox when an issue's human origin is corrected.
//
// Setting the stamp at create time is enough for a new issue. Correcting an
// existing one is not: the wrong human already has an auto-added
// 'triggered_agent' subscription, and leaving it in place means they keep
// getting notified about work they don't own — the exact symptom the explicit
// stamp exists to remove.
//
// Only rows the platform added itself (reason='triggered_agent') are touched.
// A subscription someone added by hand carries a different reason and survives.

import (
	"context"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// swapTriggeredAgentSubscriber drops every stale triggered_agent subscriber on
// the issue and subscribes the newly stamped member instead. An empty
// stampedUserID means the stamp was cleared: the stale rows go, nothing is
// added, and the derived chain governs any future issue.
func swapTriggeredAgentSubscriber(bus *events.Bus, queries *db.Queries, workspaceID, issueID, stampedUserID string) {
	ctx := context.Background()

	keep := db.ClearStaleTriggeredAgentSubscribersParams{IssueID: parseUUID(issueID)}
	if stampedUserID != "" {
		keep.KeepUserID = parseUUID(stampedUserID)
	}
	if err := queries.ClearStaleTriggeredAgentSubscribers(ctx, keep); err != nil {
		slog.Error("failed to clear stale triggered_agent subscribers",
			"issue_id", issueID, "error", err)
		return
	}

	// Notify the UI that the subscriber list changed even when nothing new is
	// added — otherwise an open issue page keeps showing the removed human.
	bus.Publish(events.Event{
		Type:        protocol.EventSubscriberRemoved,
		WorkspaceID: workspaceID,
		Payload:     map[string]any{"issue_id": issueID, "reason": "triggered_agent"},
	})

	if stampedUserID != "" {
		maybeAddSubscriber(bus, queries, workspaceID, issueID, "member", stampedUserID, "triggered_agent")
	}
}
