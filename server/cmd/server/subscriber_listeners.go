package main

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// isAssignmentRecipientType reports whether an assignee can own a subscriber
// or inbox row. Squads are routing objects whose work runs through the leader;
// they are not user identities and have no inbox to consume.
func isAssignmentRecipientType(assigneeType string) bool {
	return assigneeType == "member" || assigneeType == "agent"
}

// registerSubscriberListeners wires up event bus listeners that auto-subscribe
// relevant users to issues. This ensures creators, assignees, and commenters
// are automatically tracked as issue subscribers.
//
// Takes the pool rather than *db.Queries because the delegated rule needs a
// transaction: its eligibility checks and its write must sit inside one
// serialization boundary (see subscribeDelegatedHuman).
func registerSubscriberListeners(bus *events.Bus, pool *pgxpool.Pool) {
	queries := db.New(pool)
	// issue:created — subscribe creator + assignee (if different)
	bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		// Issues created via handler use IssueResponse; autopilot-created issues
		// use map[string]any (see service/autopilot.go → IssueToMap).
		issue, ok := extractIssueFields(payload["issue"])
		if !ok {
			return
		}

		// Subscribe the creator
		addSubscriber(bus, queries, e.WorkspaceID, issue.ID, issue.CreatorType, issue.CreatorID, "creator")

		// Subscribe the assignee if it is a direct recipient and differs from the creator.
		if issue.AssigneeType != nil && issue.AssigneeID != nil &&
			isAssignmentRecipientType(*issue.AssigneeType) &&
			!(*issue.AssigneeType == issue.CreatorType && *issue.AssigneeID == issue.CreatorID) {
			addSubscriber(bus, queries, e.WorkspaceID, issue.ID, *issue.AssigneeType, *issue.AssigneeID, "assignee")
		}

		// Subscribe @mentioned users in description
		if issue.Description != nil && *issue.Description != "" {
			for _, m := range parseMentions(*issue.Description) {
				addSubscriber(bus, queries, e.WorkspaceID, issue.ID, m.Type, m.ID, "mentioned")
			}
		}

		// Subscribe the human this issue was created ON BEHALF OF. Every rule
		// above keys on ACTOR identity, so when an agent creates an issue and
		// assigns it to an agent, every subscriber is an agent — and
		// notifyIssueSubscribers only delivers to members. The result is a full
		// subscriber list with zero recipients (MUL-5483).
		subscribeDelegatedHuman(bus, pool, queries, e.WorkspaceID, issue.ID)
	})

	// issue:updated — subscribe new assignee or @mentioned users
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issue, ok := extractIssueFields(payload["issue"])
		if !ok {
			return
		}

		// Subscribe new assignee if assignee changed
		if assigneeChanged, _ := payload["assignee_changed"].(bool); assigneeChanged {
			if issue.AssigneeType != nil && issue.AssigneeID != nil && isAssignmentRecipientType(*issue.AssigneeType) {
				addSubscriber(bus, queries, e.WorkspaceID, issue.ID, *issue.AssigneeType, *issue.AssigneeID, "assignee")
			}
		}

		// Subscribe newly @mentioned users in description
		if descriptionChanged, _ := payload["description_changed"].(bool); descriptionChanged && issue.Description != nil {
			newMentions := parseMentions(*issue.Description)
			if len(newMentions) > 0 {
				prevMentioned := map[string]bool{}
				if prevDescription, _ := payload["prev_description"].(*string); prevDescription != nil {
					for _, m := range parseMentions(*prevDescription) {
						prevMentioned[m.Type+":"+m.ID] = true
					}
				}
				for _, m := range newMentions {
					if !prevMentioned[m.Type+":"+m.ID] {
						addSubscriber(bus, queries, e.WorkspaceID, issue.ID, m.Type, m.ID, "mentioned")
					}
				}
			}
		}
	})

	// comment:created — subscribe the commenter
	bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		// Comments created via handler use CommentResponse; agent comments from task.go use map[string]any
		var issueID, authorType, authorID string
		if comment, ok := payload["comment"].(handler.CommentResponse); ok {
			issueID = comment.IssueID
			authorType = comment.AuthorType
			authorID = comment.AuthorID
		} else if commentMap, ok := payload["comment"].(map[string]any); ok {
			issueID, _ = commentMap["issue_id"].(string)
			authorType, _ = commentMap["author_type"].(string)
			authorID, _ = commentMap["author_id"].(string)
		} else {
			return
		}
		if issueID == "" || authorID == "" {
			return
		}

		// Platform-authored system comments (MUL-2538 child-done parent notify)
		// have author_type='system' and a zero UUID author. They must NOT
		// add a subscriber row: issue_subscriber.user_type is constrained to
		// ('member','agent'), and a "system" subscriber has no inbox to read
		// anyway. Skip them at the side-effect boundary so the system event
		// stays a pure WS broadcast for the timeline.
		if authorType == "system" {
			return
		}

		addSubscriber(bus, queries, e.WorkspaceID, issueID, authorType, authorID, "commenter")
	})
}

// subscribeDelegatedHuman auto-subscribes the accountable human behind an
// agent-created issue, so a delegated chain keeps the visibility its
// attribution already records (MUL-5483).
//
// The facts are read here and classified by the pure
// attribution.DelegatedSubscriber rule, which is the SAME origin waterfall
// ClassifyDirect uses to decide who a derived run is attributed to. That shared
// rule is the point: the defect being fixed is attribution and notification
// disagreeing about whose behalf an issue exists on.
//
// Attribution answers "whose authority does this run carry", which since MUL-6951
// is a broader population than "who asked for this work" — an armed autopilot
// trigger carries its creator's. Visibility follows the narrower one, so the read
// also walks the chain back to the run that resolved the human and hands the rule
// that root's label (MUL-7051).
//
// Everything is best-effort and logged, never fatal — the issue is already
// committed and a subscription hiccup must not look like a creation failure.
func subscribeDelegatedHuman(bus *events.Bus, pool *pgxpool.Pool, queries *db.Queries, workspaceID, issueID string) {
	ctx := context.Background()
	issueUUID := parseUUID(issueID)

	// The event payload carries no origin columns (they are platform-internal
	// and not part of IssueResponse), so re-read the row.
	issue, err := queries.GetIssue(ctx, issueUUID)
	if err != nil {
		slog.Error("delegated subscribe: load issue failed", "issue_id", issueID, "error", err)
		return
	}
	if issue.CreatorType != "agent" || !issue.OriginType.Valid || !issue.OriginID.Valid {
		return
	}

	// The origin run's human, plus how the chain it belongs to acquired that
	// human — one walk, workspace-scoped at every hop so a foreign origin id can
	// never resolve someone from another tenant (the MUL-4252 guard the comment
	// chain already applies).
	facts, err := queries.GetDelegatedSubscriptionFacts(ctx, db.GetDelegatedSubscriptionFactsParams{
		OriginTaskID: issue.OriginID,
		WorkspaceID:  parseUUID(workspaceID),
	})
	if err != nil {
		// A missing origin task is normal (cancelled/reaped run), not an error
		// worth alerting on; there is simply no human to inherit.
		slog.Debug("delegated subscribe: origin task not resolvable",
			"issue_id", issueID, "origin_type", issue.OriginType.String)
		return
	}

	human, reason, ok := attribution.DelegatedSubscriber(attribution.SubscriptionFacts{
		CreatorType:      issue.CreatorType,
		OriginType:       issue.OriginType.String,
		OriginOriginator: facts.OriginatorUserID,
		OriginRootSource: attribution.Source(facts.RootSource.String),
	})
	if !ok {
		return
	}

	// Two conditions decide whether this subscription may exist, and BOTH can
	// be invalidated by a concurrent request between a check and the write:
	//
	//   * the originator must still be a workspace member — originator_user_id
	//     is stamped at queue time and never revisited, so a revoked member's
	//     still-running tasks would keep filing ghost subscribers;
	//   * no opt-out may cover this issue — otherwise the next child an agent
	//     files silently undoes "stop watching this sub-tree", which is the
	//     escape hatch this whole feature depends on.
	//
	// Both live in AddDelegatedSubscriber as a single statement, under a
	// transaction-scoped lock that member revoke and subtree unsubscribe also
	// take. Checking in one round trip and writing in another cannot be made
	// correct here: the losing interleaving subscribes an issue that did not
	// exist when the opt-out was written, so there is no row for a row lock to
	// protect (MUL-5483 review round 7).
	tx, err := pool.Begin(ctx)
	if err != nil {
		slog.Error("delegated subscribe: begin failed", "issue_id", issueID, "error", err)
		return
	}
	defer tx.Rollback(ctx)
	qtx := queries.WithTx(tx)

	if err := qtx.LockSubscriberWrites(ctx, db.LockSubscriberWritesParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      human,
	}); err != nil {
		slog.Error("delegated subscribe: lock failed", "issue_id", issueID, "error", err)
		return
	}

	affected, err := qtx.AddDelegatedSubscriber(ctx, db.AddDelegatedSubscriberParams{
		IssueID:     issueUUID,
		UserID:      human,
		Reason:      reason,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		slog.Error("delegated subscribe: insert failed", "issue_id", issueID, "error", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Error("delegated subscribe: commit failed", "issue_id", issueID, "error", err)
		return
	}

	// Broadcast only for a write the DB actually accepted, and only AFTER the
	// commit — publishing from inside the transaction would let clients observe
	// a subscriber a rollback then removed.
	if affected == 0 {
		return
	}
	publishSubscriberAdded(bus, workspaceID, issueID, "member", util.UUIDToString(human), reason)
}

// extractIssueFields normalizes an issue payload that may be either a
// handler.IssueResponse struct (HTTP handler path) or a map[string]any
// (autopilot service path) into a common shape.
func extractIssueFields(v any) (handler.IssueResponse, bool) {
	if issue, ok := v.(handler.IssueResponse); ok {
		return issue, true
	}
	m, ok := v.(map[string]any)
	if !ok {
		return handler.IssueResponse{}, false
	}
	issue := handler.IssueResponse{}
	issue.ID, _ = m["id"].(string)
	issue.WorkspaceID, _ = m["workspace_id"].(string)
	issue.CreatorType, _ = m["creator_type"].(string)
	issue.CreatorID, _ = m["creator_id"].(string)
	issue.AssigneeType, _ = m["assignee_type"].(*string)
	issue.AssigneeID, _ = m["assignee_id"].(*string)
	issue.Description, _ = m["description"].(*string)
	if issue.ID == "" || issue.CreatorID == "" {
		return handler.IssueResponse{}, false
	}
	return issue, true
}

// addSubscriber adds a user as an issue subscriber and publishes a
// subscriber:added event for real-time frontend sync.
func addSubscriber(bus *events.Bus, queries *db.Queries, workspaceID, issueID, userType, userID, reason string) {
	affected, err := queries.AddIssueSubscriber(context.Background(), db.AddIssueSubscriberParams{
		IssueID:  parseUUID(issueID),
		UserType: userType,
		UserID:   parseUUID(userID),
		Reason:   reason,
	})
	if err != nil {
		slog.Error("failed to add issue subscriber",
			"issue_id", issueID,
			"user_type", userType,
			"user_id", userID,
			"reason", reason,
			"error", err,
		)
		return
	}
	// Nothing changed — either an identical active row already existed, or the
	// insert hit an opt-out tombstone and was correctly refused. Broadcasting
	// subscriber:added anyway made every open client insert a subscriber the DB
	// does not have: the UI claimed the user was watching while they stayed
	// unsubscribed and kept receiving nothing.
	if affected == 0 {
		return
	}

	publishSubscriberAdded(bus, workspaceID, issueID, userType, userID, reason)
}

// publishSubscriberAdded broadcasts a subscription the DB actually accepted.
// Shared by the plain auto-subscribe rules and the transactional delegated
// path so both describe an added subscriber identically to clients.
func publishSubscriberAdded(bus *events.Bus, workspaceID, issueID, userType, userID, reason string) {
	bus.Publish(events.Event{
		Type:        protocol.EventSubscriberAdded,
		WorkspaceID: workspaceID,
		Payload: map[string]any{
			"issue_id":  issueID,
			"user_type": userType,
			"user_id":   userID,
			"reason":    reason,
		},
	})
}
