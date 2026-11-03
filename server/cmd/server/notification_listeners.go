package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/inbox"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// mention represents a parsed @mention from markdown content (local alias).
type mention struct {
	Type string // "member", "agent", "issue", or "all"
	ID   string // user_id, agent_id, issue_id, or "all"
}

// statusLabels maps DB status values to human-readable labels for notifications.
var statusLabels = map[string]string{
	"backlog":     "Backlog",
	"todo":        "Todo",
	"in_progress": "In Progress",
	"in_review":   "In Review",
	"done":        "Done",
	"blocked":     "Blocked",
	"cancelled":   "Cancelled",
}

// priorityLabels maps DB priority values to human-readable labels for notifications.
var priorityLabels = map[string]string{
	"urgent": "Urgent",
	"high":   "High",
	"medium": "Medium",
	"low":    "Low",
	"none":   "No priority",
}

func statusLabel(s string) string {
	if l, ok := statusLabels[s]; ok {
		return l
	}
	return s
}

func priorityLabel(p string) string {
	if l, ok := priorityLabels[p]; ok {
		return l
	}
	return p
}

var emptyDetails = []byte("{}")

// parseMentions extracts mentions from markdown content.
// Delegates to the shared util.ParseMentions and converts to the local type.
func parseMentions(content string) []mention {
	parsed := util.ParseMentions(content)
	result := make([]mention, len(parsed))
	for i, m := range parsed {
		result[i] = mention{Type: m.Type, ID: m.ID}
	}
	return result
}

// parentBubbleNotifTypes is the allowlist of inbox notification types that
// bubble up from a sub-issue to subscribers of its parent. Other event types
// only notify subscribers of the sub-issue itself, to keep parent watchers'
// inboxes focused on the signal that matters most: status transitions.
var parentBubbleNotifTypes = map[string]bool{
	"status_changed": true,
}

// delegatedAlwaysNotifTypes are the events a DELEGATED subscriber (reason=
// 'delegated' — an agent created this issue on their behalf, MUL-5483) receives
// unconditionally: they are either addressed at the human directly, or they are
// exceptions that stall the work until a human looks.
var delegatedAlwaysNotifTypes = map[string]bool{
	"mentioned":     true,
	"task_failed":   true,
	"agent_blocked": true,
}

// issueStatusIsHandoff reports whether ARRIVING on this status hands the issue
// back to a human. It is the one predicate behind both the delegated-subscriber
// tier and the stale task_failed dismissal, which used to be two hand-kept
// allowlists that drifted apart (MUL-7379).
//
// It decides on LIFECYCLE plus one exact key, the split MUL-7364 established:
//
//   - done / closed are terminal, so arriving there always ends someone's wait;
//   - started is a handoff EXCEPT on the fixed in_progress key. in_progress is
//     the one status the platform itself writes to mean "an agent is working"
//     — the brief tells agents to write it, and failure recovery, the sweeper
//     and the webhook repair all key off it — so arriving there is routine
//     forward progress, not a handoff;
//   - unstarted is never a handoff: nobody is waiting on queued or parked work.
//
// That makes a CUSTOM started status a handoff. This is deliberate. A workspace
// does not mint "Awaiting Response" or "Code Review" to describe an agent
// typing; it mints one to mark the point where a person has to look. Before
// MUL-7240 those statuses carried the in_review/blocked category and notified
// for that reason; collapsing to four categories silently stopped them, which
// is the regression this restores. A custom status that really is an
// in_progress synonym now notifies where it did not — an accepted cost, since a
// missed handoff strands the issue while a spare inbox row costs one dismissal.
//
// Triage never reaches this predicate: it is not a status but a column of its
// own (MUL-7213), so an entry waiting in Triage carries whatever status the
// triager has proposed and is answered on that. Suppressing notifications for
// Triage entries is a separate rule and belongs to MUL-7219.
//
// The error is preserved rather than swallowed so each caller can choose its
// own failure direction; built-ins answer without touching the catalog, so only
// custom statuses can fail here at all.
func issueStatusIsHandoff(
	ctx context.Context,
	queries *db.Queries,
	workspaceID pgtype.UUID,
	status string,
) (bool, error) {
	category, err := issuestatus.CategoryWithError(ctx, queries, workspaceID, status)
	if err != nil {
		return false, err
	}
	switch category {
	case issuestatus.CategoryDone, issuestatus.CategoryClosed:
		return true, nil
	case issuestatus.CategoryStarted:
		return status != issuestatus.InProgress, nil
	default:
		return false, nil
	}
}

// deliverToSubscriber reports whether a subscriber row should receive this
// notification type. Direct subscriptions (creator / assignee / commenter /
// mentioned / manual / autopilot) are unchanged — they opted in to this issue,
// explicitly or by acting on it. Only the delegated tier is narrowed, and only
// to drop churn.
//
// A child finishing is NOT churn: "sub-issue X is ready for review" is exactly
// the signal a delegated watcher needs, and there is one per piece of real work.
// An earlier cut suppressed those and synthesized a single "the whole batch
// finished" roll-up from sibling state instead. That was both the wrong shape
// (see the MUL-5483 thread) and redundant: the human is subscribed to the PARENT
// as well, so when the agent moves the parent to in_review/done that transition
// delivers here — which is the natural "the tree is done" signal. Deriving it
// from children was reinventing a notification the platform already sends.
//
// statusIsHandoff is the resolved issueStatusIsHandoff answer for the issue's
// current status, so the catalog is read once per notification rather than once
// per subscriber row.
func deliverToSubscriber(reason, notifType string, statusIsHandoff bool) bool {
	if reason != "delegated" {
		return true
	}
	if delegatedAlwaysNotifTypes[notifType] {
		return true
	}
	if notifType != "status_changed" {
		return false
	}
	return statusIsHandoff
}

// loadUserPrefs loads notification preferences for a set of user IDs in a
// workspace. Returns a map from user_id string to parsed preferences.
func loadUserPrefs(
	ctx context.Context,
	queries *db.Queries,
	workspaceID string,
	userIDs []string,
) map[string]map[string]string {
	if len(userIDs) == 0 {
		return nil
	}

	uuids := make([]pgtype.UUID, len(userIDs))
	for i, id := range userIDs {
		uuids[i] = parseUUID(id)
	}

	rows, err := queries.ListNotificationPreferencesByUsers(ctx, db.ListNotificationPreferencesByUsersParams{
		WorkspaceID: parseUUID(workspaceID),
		UserIds:     uuids,
	})
	if err != nil {
		slog.Error("failed to load notification preferences", "error", err)
		return nil
	}

	result := make(map[string]map[string]string, len(rows))
	for _, row := range rows {
		var prefs map[string]string
		if err := json.Unmarshal(row.Preferences, &prefs); err != nil {
			continue
		}
		result[util.UUIDToString(row.UserID)] = prefs
	}
	return result
}

// archiveStaleTaskFailedInbox archives all task_failed inbox rows for the
// given issue and notifies each affected member recipient via
// inbox:batch-archived so connected clients self-heal.
func archiveStaleTaskFailedInbox(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	workspaceID string,
	issueID string,
) {
	rows, err := queries.ArchiveInboxByIssueAndType(ctx, db.ArchiveInboxByIssueAndTypeParams{
		WorkspaceID: parseUUID(workspaceID),
		IssueID:     parseUUID(issueID),
		Type:        "task_failed",
	})
	if err != nil {
		slog.Error("auto-archive task_failed inbox: query failed",
			"workspace_id", workspaceID, "issue_id", issueID, "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	// Dedupe recipients: the listener creates one row per failure event per
	// subscriber, so a long-running issue can yield several rows for the
	// same recipient.
	counts := map[string]int{}
	for _, row := range rows {
		// Inbox rows for task_failed only target member recipients today
		// (notifySubscribers skips agent subscribers), but defend the WS
		// layer against future widening — only members get a personal feed.
		if row.RecipientType != "member" {
			continue
		}
		counts[util.UUIDToString(row.RecipientID)]++
	}

	for recipientID, count := range counts {
		bus.Publish(events.Event{
			Type:        protocol.EventInboxBatchArchived,
			WorkspaceID: workspaceID,
			Payload: map[string]any{
				"recipient_id": recipientID,
				"count":        int64(count),
				"issue_id":     issueID,
				"reason":       "issue_status_terminal",
			},
		})
	}

	slog.Info("auto-archive task_failed inbox: archived stale rows",
		"workspace_id", workspaceID, "issue_id", issueID,
		"row_count", len(rows), "recipient_count", len(counts))
}

// subscriberRoute decides which Inbox section a notification lands in for one
// member subscriber, given their preferences. ok is false when they muted it.
type subscriberRoute func(memberID string, prefs map[string]string) (section string, ok bool)

// routeByType routes on the notification type alone, which is enough for
// every kind except comments.
func routeByType(notifType string) subscriberRoute {
	return func(_ string, prefs map[string]string) (string, bool) {
		return inbox.Route(prefs, notifType)
	}
}

// notifySubscribers queries the subscriber table for an issue, excludes the
// actor and any extra IDs, and creates inbox items for each remaining member
// subscriber. Publishes an inbox:new event for each notification.
// If the issue has a parent and the notification type is in the bubble
// allowlist, parent issue subscribers are also notified (deduplicated
// against direct subscribers).
func notifySubscribers(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	issueID string,
	issueStatus string,
	workspaceID string,
	e events.Event,
	exclude map[string]bool,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
) {
	notifySubscribersRouted(ctx, queries, bus, issueID, issueStatus, workspaceID, e, exclude,
		notifType, severity, title, body, details, routeByType(notifType))
}

// notifySubscribersRouted is notifySubscribers with a caller-chosen route per
// subscriber. Comments use it: where a comment lands depends on whether it
// @mentions the subscriber, someone else, or nobody.
func notifySubscribersRouted(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	issueID string,
	issueStatus string,
	workspaceID string,
	e events.Event,
	exclude map[string]bool,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
	route subscriberRoute,
) {
	notified, tierSuppressed := notifyIssueSubscribers(ctx, queries, bus,
		issueID, issueID, issueStatus, workspaceID, e, exclude,
		notifType, severity, title, body, details, route)

	// Only a small allowlist of event types bubbles to parent subscribers.
	if !parentBubbleNotifTypes[notifType] {
		return
	}

	// Also notify parent issue subscribers if this is a sub-issue.
	issue, err := queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		slog.Error("failed to get issue for parent notification",
			"issue_id", issueID, "error", err)
		return
	}
	if !issue.ParentIssueID.Valid {
		return
	}

	// Merge already-notified IDs into exclude set for parent subscribers.
	parentExclude := make(map[string]bool, len(exclude)+len(notified)+len(tierSuppressed))
	for id := range exclude {
		parentExclude[id] = true
	}
	for id := range notified {
		parentExclude[id] = true
	}
	// Recipients the delegated tier just filtered out for THIS child must not
	// get the same event smuggled back in through the parent's subscriber list.
	// The common shape is exactly that: the human directly created the parent
	// (reason='creator', full delivery) while their agent filed the children
	// (reason='delegated', reduced). Without this the tier suppresses nothing
	// in the one case it exists for — an agent-built tree under a parent the
	// human is watching (MUL-5483).
	for id := range tierSuppressed {
		parentExclude[id] = true
	}

	// Query subscribers from the parent issue, but the inbox item still
	// points to the sub-issue so the user navigates to the actual change.
	parentID := util.UUIDToString(issue.ParentIssueID)
	notifyIssueSubscribers(ctx, queries, bus,
		parentID, issueID, issueStatus, workspaceID, e, parentExclude,
		notifType, severity, title, body, details, route)
}

// notifyIssueSubscribers sends inbox notifications to subscribers of
// subscriberIssueID, but creates inbox items pointing to targetIssueID.
// This allows querying subscribers from a parent issue while the notification
// links to the sub-issue where the change actually occurred.
//
// Returns two sets of member IDs: those that were notified, and those the
// delegated delivery tier deliberately filtered out. The caller propagates the
// second set into the parent bubble so a suppressed event cannot be
// re-delivered through an ancestor subscription (see notifySubscribers).
func notifyIssueSubscribers(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	subscriberIssueID string,
	targetIssueID string,
	issueStatus string,
	workspaceID string,
	e events.Event,
	exclude map[string]bool,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
	route subscriberRoute,
) (map[string]bool, map[string]bool) {
	notified := map[string]bool{}
	tierSuppressed := map[string]bool{}

	// Resolve the handoff question once for the whole subscriber list, rather
	// than per row. A built-in key answers without a query, so the common path
	// still performs no I/O. (MUL-7379)
	//
	// FAIL OPEN. A catalog read can only fail for a CUSTOM status, and the two
	// outcomes are not symmetric: a spare inbox row costs one dismissal, while
	// a dropped one silently strands the issue on a status whose whole purpose
	// is to be waited on, with no other signal to the delegated subscriber.
	statusIsHandoff, err := issueStatusIsHandoff(ctx, queries, parseUUID(workspaceID), issueStatus)
	if err != nil {
		slog.Warn("resolve issue status handoff for delegated delivery; delivering",
			"issue_id", subscriberIssueID, "status", issueStatus, "error", err)
		statusIsHandoff = true
	}

	subs, err := queries.ListIssueSubscribers(ctx, parseUUID(subscriberIssueID))
	if err != nil {
		slog.Error("failed to list subscribers for notification",
			"issue_id", subscriberIssueID, "error", err)
		return notified, tierSuppressed
	}

	// Batch-load notification preferences for all member subscribers.
	var memberIDs []string
	for _, sub := range subs {
		if sub.UserType == "member" {
			memberIDs = append(memberIDs, util.UUIDToString(sub.UserID))
		}
	}
	userPrefs := loadUserPrefs(ctx, queries, workspaceID, memberIDs)

	for _, sub := range subs {
		// Only notify member-type subscribers (not agents)
		if sub.UserType != "member" {
			continue
		}

		subID := util.UUIDToString(sub.UserID)

		// Skip the actor
		if subID == e.ActorID {
			continue
		}

		// Skip any extra excluded IDs
		if exclude[subID] {
			continue
		}

		// The recipient decides which Inbox section this lands in, or mutes it.
		section, routed := route(subID, userPrefs[subID])
		if !routed {
			continue
		}

		// Delegated subscriptions deliver a narrower event set than direct
		// ones — see deliverToSubscriber.
		if !deliverToSubscriber(sub.Reason, notifType, statusIsHandoff) {
			tierSuppressed[subID] = true
			continue
		}

		item, inserted, err := inbox.CreateIfEligible(ctx, queries, db.CreateInboxItemParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   parseUUID(workspaceID),
			RecipientType: "member",
			RecipientID:   sub.UserID,
			Type:          notifType,
			Severity:      severity,
			IssueID:       parseUUID(targetIssueID),
			Title:         title,
			Body:          util.StrToText(body),
			ActorType:     util.StrToText(e.ActorType),
			ActorID:       optionalUUID(e.ActorID),
			Details:       details,
			Section:       inbox.SectionText(section),
		})
		if err != nil {
			slog.Error("subscriber notification creation failed",
				"subscriber_id", subID, "type", notifType, "error", err)
			continue
		}
		if !inserted {
			continue
		}

		notified[subID] = true
		resp := inboxItemToResponse(item)
		resp["issue_status"] = issueStatus
		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: workspaceID,
			ActorType:   e.ActorType,
			ActorID:     e.ActorID,
			Payload:     map[string]any{"item": resp},
		})
	}

	return notified, tierSuppressed
}

// notifyDirect creates an inbox item for a specific recipient. Skips if the
// recipient is the actor. Publishes an inbox:new event on success.
func notifyDirect(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	recipientType string,
	recipientID string,
	workspaceID string,
	e events.Event,
	issueID string,
	issueStatus string,
	notifType string,
	severity string,
	title string,
	body string,
	details []byte,
) {
	// Skip if recipient is the actor
	if recipientID == e.ActorID {
		return
	}

	// Member recipients route the notification through their preferences.
	section := inbox.DefaultSection(notifType)
	if recipientType == "member" {
		prefs := loadUserPrefs(ctx, queries, workspaceID, []string{recipientID})
		var routed bool
		section, routed = inbox.Route(prefs[recipientID], notifType)
		if !routed {
			return
		}
	}

	item, inserted, err := inbox.CreateIfEligible(ctx, queries, db.CreateInboxItemParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   parseUUID(workspaceID),
		RecipientType: recipientType,
		RecipientID:   parseUUID(recipientID),
		Type:          notifType,
		Severity:      severity,
		IssueID:       parseUUID(issueID),
		Title:         title,
		Body:          util.StrToText(body),
		ActorType:     util.StrToText(e.ActorType),
		ActorID:       optionalUUID(e.ActorID),
		Details:       details,
		Section:       inbox.SectionText(section),
	})
	if err != nil {
		slog.Error("direct notification creation failed",
			"issue_id", issueID, "recipient_id", recipientID, "type", notifType, "error", err)
		return
	}
	if !inserted {
		return
	}

	resp := inboxItemToResponse(item)
	resp["issue_status"] = issueStatus
	bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: workspaceID,
		ActorType:   e.ActorType,
		ActorID:     e.ActorID,
		Payload:     map[string]any{"item": resp},
	})
}

// mentionedMembers is the set of workspace members a piece of content
// @mentions: by name, through a squad they belong to, or through @all.
type mentionedMembers struct {
	ids map[string]bool
	// addressesPeople is true when the content @mentions anyone at all — a
	// member, an agent, a squad or @all. Issue links do not count.
	addressesPeople bool
	// complete is false when a squad or @all could not be expanded, so a
	// member missing from ids may still have been mentioned.
	complete bool
}

// expandMentionedMembers resolves mentions to the members they reach. Agent
// members of a squad are reached via comment-trigger / assignment paths, not
// the mention-inbox path, so only member-typed squad members are included.
func expandMentionedMembers(ctx context.Context, queries *db.Queries, workspaceID string, mentions []mention) mentionedMembers {
	out := mentionedMembers{ids: map[string]bool{}, complete: true}

	hasAll := false
	var squadIDs []pgtype.UUID
	for _, m := range mentions {
		switch m.Type {
		case "all":
			hasAll = true
			out.addressesPeople = true
		case "member":
			memberUUID, err := util.ParseUUID(m.ID)
			if err != nil {
				continue
			}
			// Keyed by the canonical form, which is how subscriber and actor
			// IDs read: the parser also accepts uppercase or unhyphenated
			// UUIDs, and those must still match the member they name.
			out.ids[util.UUIDToString(memberUUID)] = true
			out.addressesPeople = true
		case "agent":
			if _, err := util.ParseUUID(m.ID); err != nil {
				continue
			}
			out.addressesPeople = true
		case "squad":
			squadUUID, err := util.ParseUUID(m.ID)
			if err != nil {
				continue
			}
			squadIDs = append(squadIDs, squadUUID)
			out.addressesPeople = true
		}
	}

	for _, squadUUID := range squadIDs {
		members, err := queries.ListSquadMembers(ctx, squadUUID)
		if err != nil {
			slog.Error("failed to list squad members for @squad mention",
				"squad_id", util.UUIDToString(squadUUID), "error", err)
			out.complete = false
			continue
		}
		for _, sm := range members {
			if sm.MemberType == "member" {
				out.ids[util.UUIDToString(sm.MemberID)] = true
			}
		}
	}

	if hasAll {
		members, err := queries.ListMembers(ctx, parseUUID(workspaceID))
		if err != nil {
			slog.Error("failed to list members for @all mention", "workspace_id", workspaceID, "error", err)
			out.complete = false
		} else {
			for _, m := range members {
				out.ids[util.UUIDToString(m.UserID)] = true
			}
		}
	}

	return out
}

// commentAudience is who a comment is written to, as one subscriber sees it
// (see inbox.CommentAudience). When a squad or @all could not be expanded, a
// subscriber the comment may have reached is treated as if nobody was
// mentioned: a comment meant for them then still waits in Needs action
// instead of slipping into Activity.
func (m mentionedMembers) commentAudience(memberID string) inbox.CommentAudience {
	switch {
	case m.ids[memberID]:
		return inbox.CommentToRecipient
	case !m.addressesPeople || !m.complete:
		return inbox.CommentToNobody
	default:
		return inbox.CommentToOthers
	}
}

// notifyMentionedMembers creates inbox items for each @mentioned member,
// excluding the actor and any IDs in the skip set. When an @all mention is
// present, all workspace members are notified (excluding agents).
func notifyMentionedMembers(
	bus *events.Bus,
	queries *db.Queries,
	e events.Event,
	mentions []mention,
	issueID string,
	issueTitle string,
	issueStatus string,
	title string,
	skip map[string]bool,
	details []byte,
) {
	mentioned := expandMentionedMembers(context.Background(), queries, e.WorkspaceID, mentions)
	notifyMentionedMemberIDs(bus, queries, e, mentioned.ids, issueID, issueStatus, title, skip, details)
}

// notifyMentionedMemberIDs creates a "mentioned" inbox item for each member in
// recipientIDs, excluding the actor and any IDs in the skip set.
func notifyMentionedMemberIDs(
	bus *events.Bus,
	queries *db.Queries,
	e events.Event,
	recipientIDs map[string]bool,
	issueID string,
	issueStatus string,
	title string,
	skip map[string]bool,
	details []byte,
) {
	// Batch-load notification preferences for all mention recipients.
	var mentionUserIDs []string
	for id := range recipientIDs {
		if id != e.ActorID && !skip[id] {
			mentionUserIDs = append(mentionUserIDs, id)
		}
	}
	mentionPrefs := loadUserPrefs(context.Background(), queries, e.WorkspaceID, mentionUserIDs)

	for id := range recipientIDs {
		if id == e.ActorID || skip[id] {
			continue
		}
		// Mentions route on their own key, deliberately apart from comments:
		// muting comment volume must not silence someone asking for you by name.
		section, routed := inbox.Route(mentionPrefs[id], "mentioned")
		if !routed {
			continue
		}
		item, inserted, err := inbox.CreateIfEligible(context.Background(), queries, db.CreateInboxItemParams{
			ID:            dbid.NewV7(),
			WorkspaceID:   parseUUID(e.WorkspaceID),
			RecipientType: "member",
			RecipientID:   parseUUID(id),
			Type:          "mentioned",
			Severity:      "info",
			IssueID:       parseUUID(issueID),
			Title:         title,
			ActorType:     util.StrToText(e.ActorType),
			ActorID:       optionalUUID(e.ActorID),
			Details:       details,
			Section:       inbox.SectionText(section),
		})
		if err != nil {
			slog.Error("mention inbox creation failed", "mentioned_id", id, "error", err)
			continue
		}
		if !inserted {
			continue
		}
		resp := inboxItemToResponse(item)
		resp["issue_status"] = issueStatus
		bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: e.WorkspaceID,
			ActorType:   e.ActorType,
			ActorID:     e.ActorID,
			Payload:     map[string]any{"item": resp},
		})
	}
}

// registerNotificationListeners wires up event bus listeners that create inbox
// notifications using the subscriber table. This replaces the old hardcoded
// notification logic from inbox_listeners.go.
//
// NOTE: uses context.Background() because the event bus dispatches synchronously
// within the HTTP request goroutine. Adding per-handler timeouts is a bus-level
// concern — see events.Bus for future improvements.
func registerNotificationListeners(bus *events.Bus, queries *db.Queries) {
	ctx := context.Background()

	// issue:created — Direct notification to assignee if assignee != actor
	bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}

		// Track who already got notified to avoid duplicates
		skip := map[string]bool{e.ActorID: true}

		// Direct notification to assignees that own an inbox.
		if issue.AssigneeType != nil && issue.AssigneeID != nil && isAssignmentRecipientType(*issue.AssigneeType) {
			skip[*issue.AssigneeID] = true
			notifyDirect(ctx, queries, bus,
				*issue.AssigneeType, *issue.AssigneeID,
				issue.WorkspaceID, e, issue.ID, issue.Status,
				"issue_assigned", "action_required",
				issue.Title,
				"",
				emptyDetails,
			)
		}

		// Notify @mentions in description
		if issue.Description != nil && *issue.Description != "" {
			mentions := parseMentions(*issue.Description)
			notifyMentionedMembers(bus, queries, e, mentions, issue.ID, issue.Title, issue.Status,
				issue.Title, skip, emptyDetails)
		}
	})

	// issue:updated — handle assignee changes, status changes, priority, due date
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issue, ok := payload["issue"].(handler.IssueResponse)
		if !ok {
			return
		}
		assigneeChanged, _ := payload["assignee_changed"].(bool)
		statusChanged, _ := payload["status_changed"].(bool)
		descriptionChanged, _ := payload["description_changed"].(bool)
		prevAssigneeType, _ := payload["prev_assignee_type"].(*string)
		prevAssigneeID, _ := payload["prev_assignee_id"].(*string)
		prevDescription, _ := payload["prev_description"].(*string)

		if assigneeChanged {
			// Build structured details for assignee change.
			//
			// map[string]string, not map[string]any: every client parses inbox
			// `details` as a string->string map, and because the inbox endpoint
			// returns an ARRAY, one non-string value fails the whole parse and
			// blanks the entire list rather than one row. `any` let that be a
			// convention a reviewer had to notice; the concrete type makes it a
			// compile error. This is the only details map in this file that was
			// not already string-typed.
			detailsMap := map[string]string{}
			if prevAssigneeType != nil {
				detailsMap["prev_assignee_type"] = *prevAssigneeType
			}
			if prevAssigneeID != nil {
				detailsMap["prev_assignee_id"] = *prevAssigneeID
			}
			if issue.AssigneeType != nil {
				detailsMap["new_assignee_type"] = *issue.AssigneeType
			}
			if issue.AssigneeID != nil {
				detailsMap["new_assignee_id"] = *issue.AssigneeID
			}
			assigneeDetails, _ := json.Marshal(detailsMap)

			// Direct: notify new assignee about assignment when it owns an inbox.
			if issue.AssigneeType != nil && issue.AssigneeID != nil && isAssignmentRecipientType(*issue.AssigneeType) {
				notifyDirect(ctx, queries, bus,
					*issue.AssigneeType, *issue.AssigneeID,
					e.WorkspaceID, e, issue.ID, issue.Status,
					"issue_assigned", "action_required",
					issue.Title,
					"",
					assigneeDetails,
				)
			}

			// Direct: notify only a previous member assignee about unassignment.
			// This is intentionally narrower than isAssignmentRecipientType: agents
			// do not receive unassigned notifications.
			if prevAssigneeType != nil && prevAssigneeID != nil && *prevAssigneeType == "member" {
				notifyDirect(ctx, queries, bus,
					"member", *prevAssigneeID,
					e.WorkspaceID, e, issue.ID, issue.Status,
					"unassigned", "info",
					issue.Title,
					"",
					assigneeDetails,
				)
			}

			// Subscriber: notify remaining subscribers about assignee change,
			// excluding actor, old assignee, and new assignee
			exclude := map[string]bool{}
			if prevAssigneeID != nil {
				exclude[*prevAssigneeID] = true
			}
			if issue.AssigneeID != nil {
				exclude[*issue.AssigneeID] = true
			}
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				exclude, "assignee_changed", "info",
				issue.Title, "",
				assigneeDetails)
		}

		if statusChanged {
			prevStatus, _ := payload["prev_status"].(string)
			statusDetails, _ := json.Marshal(map[string]string{
				"from": prevStatus,
				"to":   issue.Status,
			})
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				nil, "status_changed", "info",
				issue.Title, "",
				statusDetails)

			// When the issue progresses past the failure, retire any stale
			// task_failed inbox rows so the inbox reflects the current state of
			// the work, not its history. The activity log keeps the full
			// failure history for audit.
			//
			// Same handoff predicate as the delegated tier, minus Blocked:
			// blocked hands the issue to a human, but it hands over work that
			// is still stuck, so the past failures remain worth triaging.
			// Blocked is excluded by its exact key — a custom started status is
			// a review gate, not a stall. (MUL-7379)
			//
			// FAIL CLOSED here, unlike delivery: hiding a failure notice on a
			// guess is worse than leaving a stale one the user can dismiss.
			handoff, handoffErr := issueStatusIsHandoff(ctx, queries, parseUUID(e.WorkspaceID), issue.Status)
			switch {
			case handoffErr != nil:
				slog.Warn("resolve issue status handoff for task_failed dismissal; keeping rows",
					"issue_id", issue.ID, "status", issue.Status, "error", handoffErr)
			case handoff && issue.Status != issuestatus.Blocked:
				archiveStaleTaskFailedInbox(ctx, queries, bus, e.WorkspaceID, issue.ID)
			}
		}

		if priorityChanged, _ := payload["priority_changed"].(bool); priorityChanged {
			prevPriority, _ := payload["prev_priority"].(string)
			priorityDetails, _ := json.Marshal(map[string]string{
				"from": prevPriority,
				"to":   issue.Priority,
			})
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				nil, "priority_changed", "info",
				issue.Title, "",
				priorityDetails)
		}

		if startDateChanged, _ := payload["start_date_changed"].(bool); startDateChanged {
			prevStartDateStr := ""
			if prevStartDate, ok := payload["prev_start_date"].(*string); ok && prevStartDate != nil {
				prevStartDateStr = *prevStartDate
			}
			newStartDateStr := ""
			if issue.StartDate != nil {
				newStartDateStr = *issue.StartDate
			}
			startDateDetails, _ := json.Marshal(map[string]string{
				"from": prevStartDateStr,
				"to":   newStartDateStr,
			})
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				nil, "start_date_changed", "info",
				issue.Title, "",
				startDateDetails)
		}

		if dueDateChanged, _ := payload["due_date_changed"].(bool); dueDateChanged {
			prevDueDateStr := ""
			if prevDueDate, ok := payload["prev_due_date"].(*string); ok && prevDueDate != nil {
				prevDueDateStr = *prevDueDate
			}
			newDueDateStr := ""
			if issue.DueDate != nil {
				newDueDateStr = *issue.DueDate
			}
			dueDateDetails, _ := json.Marshal(map[string]string{
				"from": prevDueDateStr,
				"to":   newDueDateStr,
			})
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				nil, "due_date_changed", "info",
				issue.Title, "",
				dueDateDetails)
		}

		// Notify NEW @mentions in description
		if descriptionChanged && issue.Description != nil {
			newMentions := parseMentions(*issue.Description)
			if len(newMentions) > 0 {
				prevMentioned := map[string]bool{}
				if prevDescription != nil {
					for _, m := range parseMentions(*prevDescription) {
						prevMentioned[m.Type+":"+m.ID] = true
					}
				}
				var added []mention
				for _, m := range newMentions {
					if !prevMentioned[m.Type+":"+m.ID] {
						added = append(added, m)
					}
				}
				skip := map[string]bool{e.ActorID: true}
				notifyMentionedMembers(bus, queries, e, added, issue.ID, issue.Title, issue.Status,
					issue.Title, skip, emptyDetails)
			}
		}
	})

	// comment:created — notify all subscribers except the commenter
	bus.Subscribe(protocol.EventCommentCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		// The comment payload can come as handler.CommentResponse from the
		// HTTP handler, or as map[string]any from the agent comment path in
		// task.go. Handle both.
		var issueID, commentID, commentContent, authorType string
		switch c := payload["comment"].(type) {
		case handler.CommentResponse:
			issueID = c.IssueID
			commentID = c.ID
			commentContent = c.Content
			authorType = c.AuthorType
		case map[string]any:
			issueID, _ = c["issue_id"].(string)
			commentID, _ = c["id"].(string)
			commentContent, _ = c["content"].(string)
			authorType, _ = c["author_type"].(string)
		default:
			return
		}

		// Platform-authored system comments (MUL-2538 child-done parent
		// notify) must NOT create inbox rows or parse mentions from their
		// body — the comment is a controlled platform signal, not a human
		// commenter. Mention parsing is the dangerous bit: if the body
		// transcluded a child title containing `mention://member/<uuid>`,
		// the parent's assignee inbox would light up via the generic path.
		// Skip the listener entirely; the WS broadcast still delivers the
		// comment to the issue timeline.
		if authorType == "system" {
			return
		}

		issueTitle, _ := payload["issue_title"].(string)
		issueStatus, _ := payload["issue_status"].(string)

		commentDetails := emptyDetails
		if commentID != "" {
			commentDetails, _ = json.Marshal(map[string]string{
				"comment_id": commentID,
			})
		}

		// Where a comment lands depends on who it is written to, not who wrote
		// it: one that @mentions the subscriber or nobody waits on them, one
		// that @mentions only others is activity for them.
		mentioned := expandMentionedMembers(ctx, queries, e.WorkspaceID, parseMentions(commentContent))
		notifySubscribersRouted(ctx, queries, bus, issueID, issueStatus, e.WorkspaceID, e,
			nil, "new_comment", "info",
			issueTitle, commentContent,
			commentDetails,
			func(memberID string, prefs map[string]string) (string, bool) {
				return inbox.RouteComment(prefs, mentioned.commentAudience(memberID))
			})

		// Notify @mentions in comment content.
		if len(mentioned.ids) > 0 {
			skip := map[string]bool{e.ActorID: true}
			notifyMentionedMemberIDs(bus, queries, e, mentioned.ids, issueID, issueStatus,
				issueTitle, skip, commentDetails)
		}
	})

	// issue_reaction:added — notify the issue creator
	bus.Subscribe(protocol.EventIssueReactionAdded, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		reaction, ok := payload["reaction"].(handler.IssueReactionResponse)
		if !ok {
			return
		}

		creatorType, _ := payload["creator_type"].(string)
		creatorID, _ := payload["creator_id"].(string)
		issueID, _ := payload["issue_id"].(string)
		issueTitle, _ := payload["issue_title"].(string)
		issueStatus, _ := payload["issue_status"].(string)

		if creatorType == "" || creatorID == "" {
			return
		}

		details, _ := json.Marshal(map[string]string{
			"emoji": reaction.Emoji,
		})

		notifyDirect(ctx, queries, bus,
			creatorType, creatorID,
			e.WorkspaceID, e, issueID, issueStatus,
			"reaction_added", "info",
			issueTitle, "",
			details,
		)
	})

	// reaction:added — notify the comment author
	bus.Subscribe(protocol.EventReactionAdded, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}

		reaction, ok := payload["reaction"].(handler.ReactionResponse)
		if !ok {
			return
		}

		commentAuthorType, _ := payload["comment_author_type"].(string)
		commentAuthorID, _ := payload["comment_author_id"].(string)
		commentID, _ := payload["comment_id"].(string)
		issueID, _ := payload["issue_id"].(string)
		issueTitle, _ := payload["issue_title"].(string)
		issueStatus, _ := payload["issue_status"].(string)

		if commentAuthorType == "" || commentAuthorID == "" {
			return
		}

		detailsMap := map[string]string{
			"emoji": reaction.Emoji,
		}
		if commentID != "" {
			detailsMap["comment_id"] = commentID
		}
		details, _ := json.Marshal(detailsMap)

		notifyDirect(ctx, queries, bus,
			commentAuthorType, commentAuthorID,
			e.WorkspaceID, e, issueID, issueStatus,
			"reaction_added", "info",
			issueTitle, "",
			details,
		)
	})

	// task:completed — no inbox notification (completion is visible from status change)

	// task:failed — notify all subscribers except the agent
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		agentID, _ := payload["agent_id"].(string)
		issueID, _ := payload["issue_id"].(string)
		if issueID == "" {
			return
		}

		issue, err := queries.GetIssue(ctx, parseUUID(issueID))
		if err != nil {
			slog.Error("task:failed notification: failed to get issue", "issue_id", issueID, "error", err)
			return
		}

		exclude := map[string]bool{}
		if agentID != "" {
			exclude[agentID] = true
		}

		notifySubscribers(ctx, queries, bus, issueID, issue.Status, e.WorkspaceID,
			events.Event{
				Type:        e.Type,
				WorkspaceID: e.WorkspaceID,
				ActorType:   "agent",
				ActorID:     agentID,
			},
			exclude, "task_failed", "action_required",
			issue.Title, "",
			emptyDetails)
	})
}

// inboxItemToResponse converts a db.InboxItem into a map suitable for
// JSON-serializable event payloads (mirrors handler.inboxToResponse fields).
func inboxItemToResponse(item db.InboxItem) map[string]any {
	return map[string]any{
		"id":             util.UUIDToString(item.ID),
		"workspace_id":   util.UUIDToString(item.WorkspaceID),
		"recipient_type": item.RecipientType,
		"recipient_id":   util.UUIDToString(item.RecipientID),
		"type":           item.Type,
		"severity":       item.Severity,
		"section":        inbox.ItemSection(item.Section, item.Type),
		"issue_id":       util.UUIDToPtr(item.IssueID),
		"title":          item.Title,
		"body":           util.TextToPtr(item.Body),
		"read":           item.Read,
		"archived":       item.Archived,
		"created_at":     util.TimestampToString(item.CreatedAt),
		"actor_type":     util.TextToPtr(item.ActorType),
		"actor_id":       util.UUIDToPtr(item.ActorID),
		"details":        json.RawMessage(item.Details),
	}
}
