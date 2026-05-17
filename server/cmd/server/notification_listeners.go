package main

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// pushNotifier is set once at register time. Used by notify* helpers to fan
// inbox items out to the recipient's web-push subscriptions in addition to
// the existing in-app inbox/realtime delivery. Nil-safe — Web Push is opt-in
// per device, and not every deployment has VAPID keys configured.
var pushNotifier *service.PushService

// Apple Web Push enforces a hard ~4 KB ceiling on the encrypted payload.
// After AES-128-GCM padding the plaintext budget shrinks to roughly 3 KB —
// and full comment bodies routinely exceeded that, which made Apple reject
// the message ("payload has exceeded the maximum length") and triggered
// iOS Safari's silent-fallback behaviour: a generic "Multica" notification
// AND an auto-incremented icon badge that the SW never got to fix. Cap
// title and body to lock-screen-readable lengths so payloads stay well
// under the limit.
const (
	pushTitleMaxRunes = 100
	pushBodyMaxRunes  = 200
)

func truncateForPush(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

// pushItemToMember sends a Web Push to recipientID's subscribed devices
// describing the just-created inbox item. Only members get pushes — agents
// don't have devices.
//
// Badge and Silent are derived from the recipient's mobile-channel transport
// preferences (notifications.channels.mobile.{badge,sound}) so the icon
// counter and notification sound respect the per-user "Visning" toggles.
//
// CEREBRO-PATCH(push-deep-link): URL is built as `/<slug>/inbox?issue=<id>`
// when the workspace lookup succeeds, so tapping the notification opens the
// inbox with the right item selected. JEH-737. Falls back to `/?issue=…`
// if the lookup fails — that path doesn't deep-link (the landing-page
// redirect drops query params) but is no worse than the previous behaviour.
func pushItemToMember(ctx context.Context, queries *db.Queries, recipientType, recipientID, workspaceID, issueID, notifType, title, body string) {
	if pushNotifier == nil || !pushNotifier.Enabled() {
		return
	}
	if recipientType != "member" || recipientID == "" {
		return
	}
	transport := resolveChannelTransport(ctx, queries, recipientType, recipientID, channelMobile)
	pushNotifier.SendToUser(ctx, recipientID, service.Payload{
		Title:   truncateForPush(title, pushTitleMaxRunes),
		Body:    truncateForPush(body, pushBodyMaxRunes),
		URL:     pushDeepLinkURL(ctx, queries, workspaceID, issueID),
		Tag:     "issue:" + issueID,
		IssueID: issueID,
		Type:    notifType,
		Badge:   transport.Badge,
		Silent:  !transport.Sound,
	})
}

// pushDeepLinkURL resolves the workspace slug from workspaceID and returns a
// deep link of the form `/<slug>/inbox?issue=<issueID>` so a tap on the
// notification lands on the inbox row that triggered it. The inbox page
// matches `?issue=<value>` against the inbox item's `issue_id` (UUID), so
// the issue UUID we already have is the right value to stamp.
//
// On lookup failure we fall back to `/?issue=<issueID>` rather than dropping
// the push — the landing page won't preserve the query through its
// post-auth redirect, but the user still ends up logged in and can find the
// item manually.
//
// CEREBRO-PATCH(push-deep-link): see JEH-737 conversation.
func pushDeepLinkURL(ctx context.Context, queries *db.Queries, workspaceID, issueID string) string {
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return "/?issue=" + issueID
	}
	ws, err := queries.GetWorkspace(ctx, wsUUID)
	if err != nil || ws.Slug == "" {
		return "/?issue=" + issueID
	}
	return "/" + ws.Slug + "/inbox?issue=" + issueID
}

// inboxItemDraft holds the fields needed to create one inbox item. Used by
// dispatchToMember to fan a single notification out across the channels the
// user has chosen.
type inboxItemDraft struct {
	WorkspaceID   string
	RecipientType string
	RecipientID   string
	IssueID       string
	IssueStatus   string
	NotifType     string
	Severity      string
	Title         string
	Body          string
	Details       []byte
	Actor         events.Event
	IsAssignee    bool
}

// dispatchToMember resolves the recipient's per-channel preferences for this
// notification and creates one inbox_item per active "storage" channel (Inbox
// and/or Notifications). Returns true if at least one item was created.
//
// Mobile and Desktop are push channels — they don't create rows in inbox_item.
// Both are gated on the same row-level on/off plus the existence of an
// inbox/notifications item (we don't push something the user can't open).
//
// Agents always behave as inbox-only: resolveChannelChoice returns true for
// channelInbox and false for everything else, regardless of preferences.
func dispatchToMember(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	d inboxItemDraft,
) bool {
	key := routeKey(d.NotifType, d.IsAssignee)

	inboxOn := resolveChannelChoice(ctx, queries, d.RecipientType, d.RecipientID, channelInbox, key)
	notifOn := resolveChannelChoice(ctx, queries, d.RecipientType, d.RecipientID, channelNotifications, key)
	if !inboxOn && !notifOn {
		return false
	}

	created := false
	if inboxOn {
		if createInboxItemForChannel(ctx, queries, bus, d, routeInbox) {
			created = true
		}
	}
	if notifOn {
		if createInboxItemForChannel(ctx, queries, bus, d, routeNotifications) {
			created = true
		}
	}

	// Mobile push fires only when something landed in the user's inbox or
	// notifications page — push is a bell-ring on top of an existing item,
	// not a standalone notification.
	if created && resolveChannelChoice(ctx, queries, d.RecipientType, d.RecipientID, channelMobile, key) {
		pushItemToMember(ctx, queries, d.RecipientType, d.RecipientID, d.WorkspaceID, d.IssueID, d.NotifType, d.Title, d.Body)
	}

	// Desktop banner fires under the same "must have a real item to open"
	// rule as mobile. It rides the WS hub via EventDesktopNotify and is
	// scoped to the recipient by listeners.go.
	if created && resolveChannelChoice(ctx, queries, d.RecipientType, d.RecipientID, channelDesktop, key) {
		bus.Publish(events.Event{
			Type:        protocol.EventDesktopNotify,
			WorkspaceID: d.WorkspaceID,
			ActorType:   d.Actor.ActorType,
			ActorID:     d.Actor.ActorID,
			Payload: map[string]any{
				"recipient_id": d.RecipientID,
				"type":         d.NotifType,
				"severity":     d.Severity,
				"title":        d.Title,
				"body":         d.Body,
				"issue_id":     d.IssueID,
				"issue_status": d.IssueStatus,
			},
		})
	}

	return created
}

func createInboxItemForChannel(
	ctx context.Context,
	queries *db.Queries,
	bus *events.Bus,
	d inboxItemDraft,
	route string,
) bool {
	item, err := queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		WorkspaceID:   parseUUID(d.WorkspaceID),
		RecipientType: d.RecipientType,
		RecipientID:   parseUUID(d.RecipientID),
		Type:          d.NotifType,
		Severity:      d.Severity,
		IssueID:       parseUUID(d.IssueID),
		Title:         d.Title,
		Body:          util.StrToText(d.Body),
		ActorType:     util.StrToText(d.Actor.ActorType),
		ActorID:       parseUUID(d.Actor.ActorID),
		Details:       d.Details,
		Route:         route,
	})
	if err != nil {
		slog.Error("inbox item creation failed",
			"recipient_id", d.RecipientID, "type", d.NotifType, "route", route, "error", err)
		return false
	}
	resp := inboxItemToResponse(item)
	if d.IssueStatus != "" {
		resp["issue_status"] = d.IssueStatus
	}
	bus.Publish(events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: d.WorkspaceID,
		ActorType:   d.Actor.ActorType,
		ActorID:     d.Actor.ActorID,
		Payload:     map[string]any{"item": resp},
	})
	return true
}

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

// notifTypeToGroup maps each InboxItemType to a user-configurable preference
// group. Types not in this map are always delivered (not configurable).
var notifTypeToGroup = map[string]string{
	"issue_assigned":     "assignments",
	"unassigned":         "assignments",
	"assignee_changed":   "assignments",
	"status_changed":     "status_changes",
	"new_comment":        "comments",
	"mentioned":          "comments",
	"priority_changed":   "updates",
	"start_date_changed": "updates",
	"due_date_changed":   "updates",
	"task_completed":     "agent_activity",
	"task_failed":        "agent_activity",
	"agent_blocked":      "agent_activity",
	"agent_completed":    "agent_activity",
}

// isNotifMuted returns true if the given notification type is muted for a user
// based on their parsed preferences map.
func isNotifMuted(prefs map[string]string, notifType string) bool {
	group, ok := notifTypeToGroup[notifType]
	if !ok {
		return false // unconfigurable types are always delivered
	}
	return prefs[group] == "muted"
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

// terminalStatusForTaskFailedDismiss is the set of issue statuses that mark
// the issue as "the user no longer needs to triage past failures." When a
// status change lands on one of these, any pre-existing task_failed inbox
// rows for the issue are archived so the inbox stays a fresh-signal surface.
// `in_review` is included because in Multica's agent flow that's the most
// reliable "work delivered" handoff — and a status flip back to in_progress
// will simply produce new task_failed rows that surface normally.
var terminalStatusForTaskFailedDismiss = map[string]bool{
	"in_review": true,
	"done":      true,
	"cancelled": true,
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

// notifySubscribers queries the subscriber table for an issue, excludes the
// actor and any extra IDs, and creates inbox items for each remaining member
// subscriber. Publishes an inbox:new event for each notification.
// If the issue has a parent and the notification type is in the bubble
// allowlist (parentBubbleNotifTypes), parent issue subscribers are also
// notified (deduplicated against direct subscribers).
//
// assigneeMemberID is the user_id of the (member) assignee on the changed
// issue, used to disambiguate split notification types (priority_changed,
// due_date_changed) — assignees get a stronger default routing than passive
// subscribers. Pass "" when the issue has no member assignee.
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
	assigneeMemberID string,
) {
	notified := notifyIssueSubscribers(ctx, queries, bus,
		issueID, issueID, issueStatus, workspaceID, e, exclude,
		notifType, severity, title, body, details, assigneeMemberID)

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
	parentExclude := make(map[string]bool, len(exclude)+len(notified))
	for id := range exclude {
		parentExclude[id] = true
	}
	for id := range notified {
		parentExclude[id] = true
	}

	// Query subscribers from the parent issue, but the inbox item still
	// points to the sub-issue so the user navigates to the actual change.
	parentID := util.UUIDToString(issue.ParentIssueID)
	notifyIssueSubscribers(ctx, queries, bus,
		parentID, issueID, issueStatus, workspaceID, e, parentExclude,
		notifType, severity, title, body, details, assigneeMemberID)
}

// notifyIssueSubscribers sends inbox notifications to subscribers of
// subscriberIssueID, but creates inbox items pointing to targetIssueID.
// This allows querying subscribers from a parent issue while the notification
// links to the sub-issue where the change actually occurred.
// Returns the set of member IDs that were notified (excluding subscribers
// whose preference resolved to 'off').
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
	assigneeMemberID string,
) map[string]bool {
	notified := map[string]bool{}

	subs, err := queries.ListIssueSubscribers(ctx, parseUUID(subscriberIssueID))
	if err != nil {
		slog.Error("failed to list subscribers for notification",
			"issue_id", subscriberIssueID, "error", err)
		return notified
	}

	// CEREBRO-PATCH(channel-routing-supersedes-prefs): cerebro routes mute
	// decisions through resolveChannelChoice in dispatchToMember. The legacy
	// batch loadUserPrefs/isNotifMuted path is unused on this fork.

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

		isAssignee := assigneeMemberID != "" && subID == assigneeMemberID
		ok := dispatchToMember(ctx, queries, bus, inboxItemDraft{
			WorkspaceID:   workspaceID,
			RecipientType: "member",
			RecipientID:   subID,
			IssueID:       targetIssueID,
			IssueStatus:   issueStatus,
			NotifType:     notifType,
			Severity:      severity,
			Title:         title,
			Body:          body,
			Details:       details,
			Actor:         e,
			IsAssignee:    isAssignee,
		})
		if ok {
			notified[subID] = true
		}
	}

	return notified
}

// notifyDirect creates an inbox item for a specific recipient. Skips if the
// recipient is the actor or if their routing preference resolved to 'off'.
// Publishes an inbox:new event on success.
//
// recipientIsAssignee disambiguates split notification types
// (priority_changed, due_date_changed). For non-split types it has no effect
// — pass false when not applicable.
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
	recipientIsAssignee bool,
) {
	// Skip if recipient is the actor
	if recipientID == e.ActorID {
		return
	}

	dispatchToMember(ctx, queries, bus, inboxItemDraft{
		WorkspaceID:   workspaceID,
		RecipientType: recipientType,
		RecipientID:   recipientID,
		IssueID:       issueID,
		IssueStatus:   issueStatus,
		NotifType:     notifType,
		Severity:      severity,
		Title:         title,
		Body:          body,
		Details:       details,
		Actor:         e,
		IsAssignee:    recipientIsAssignee,
	})
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
	// Collect the set of member IDs to notify.
	recipientIDs := map[string]bool{}

	hasAll := false
	for _, m := range mentions {
		if m.Type == "all" {
			hasAll = true
			continue
		}
		if m.Type == "member" {
			recipientIDs[m.ID] = true
		}
	}

	// If @all is present, expand to all workspace members.
	if hasAll {
		members, err := queries.ListMembers(context.Background(), parseUUID(e.WorkspaceID))
		if err != nil {
			slog.Error("failed to list members for @all mention", "workspace_id", e.WorkspaceID, "error", err)
		} else {
			for _, m := range members {
				recipientIDs[util.UUIDToString(m.UserID)] = true
			}
		}
	}

	ctx := context.Background()
	for id := range recipientIDs {
		if id == e.ActorID || skip[id] {
			continue
		}
		dispatchToMember(ctx, queries, bus, inboxItemDraft{
			WorkspaceID:   e.WorkspaceID,
			RecipientType: "member",
			RecipientID:   id,
			IssueID:       issueID,
			IssueStatus:   issueStatus,
			NotifType:     "mentioned",
			Severity:      "info",
			Title:         title,
			Body:          "",
			Details:       details,
			Actor:         e,
			// "mentioned" is not a split type; IsAssignee has no effect.
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
func registerNotificationListeners(bus *events.Bus, queries *db.Queries, pushSvc *service.PushService) {
	pushNotifier = pushSvc
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

		// Direct notification to assignee
		if issue.AssigneeType != nil && issue.AssigneeID != nil {
			skip[*issue.AssigneeID] = true
			notifyDirect(ctx, queries, bus,
				*issue.AssigneeType, *issue.AssigneeID,
				issue.WorkspaceID, e, issue.ID, issue.Status,
				"issue_assigned", "action_required",
				issue.Title,
				"",
				emptyDetails,
				true, // recipient IS the new assignee
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
			// Build structured details for assignee change
			detailsMap := map[string]any{}
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

			// Direct: notify new assignee about assignment
			if issue.AssigneeType != nil && issue.AssigneeID != nil {
				notifyDirect(ctx, queries, bus,
					*issue.AssigneeType, *issue.AssigneeID,
					e.WorkspaceID, e, issue.ID, issue.Status,
					"issue_assigned", "action_required",
					issue.Title,
					"",
					assigneeDetails,
					true, // recipient IS the new assignee
				)
			}

			// Direct: notify old assignee about unassignment
			if prevAssigneeType != nil && prevAssigneeID != nil && *prevAssigneeType == "member" {
				notifyDirect(ctx, queries, bus,
					"member", *prevAssigneeID,
					e.WorkspaceID, e, issue.ID, issue.Status,
					"unassigned", "info",
					issue.Title,
					"",
					assigneeDetails,
					false, // recipient is no longer assignee
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
			currentAssigneeMemberID := responseAssigneeMemberID(issue.AssigneeType, issue.AssigneeID)
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				exclude, "assignee_changed", "info",
				issue.Title, "",
				assigneeDetails, currentAssigneeMemberID)
		}

		assigneeMemberID := responseAssigneeMemberID(issue.AssigneeType, issue.AssigneeID)

		if statusChanged {
			prevStatus, _ := payload["prev_status"].(string)
			statusDetails, _ := json.Marshal(map[string]string{
				"from": prevStatus,
				"to":   issue.Status,
			})
			notifySubscribers(ctx, queries, bus, issue.ID, issue.Status, e.WorkspaceID, e,
				nil, "status_changed", "info",
				issue.Title, "",
				statusDetails, assigneeMemberID)

			// When the issue progresses past the failure (in_review / done /
			// cancelled), retire any stale task_failed inbox rows so the
			// inbox reflects the current state of the work, not its history.
			// The activity log keeps the full failure history for audit.
			if terminalStatusForTaskFailedDismiss[issue.Status] {
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
				priorityDetails, assigneeMemberID)
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
				startDateDetails, assigneeMemberID)
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
				dueDateDetails, assigneeMemberID)
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
		var issueID, commentID, commentContent string
		switch c := payload["comment"].(type) {
		case handler.CommentResponse:
			issueID = c.IssueID
			commentID = c.ID
			// CEREBRO-PATCH(comment-content-nullable): JEH-1215 — Content
			// is *string so a redacted comment reads as nil; deref safely
			// so notification body falls back to "".
			if c.Content != nil {
				commentContent = *c.Content
			}
		case map[string]any:
			issueID, _ = c["issue_id"].(string)
			commentID, _ = c["id"].(string)
			commentContent, _ = c["content"].(string)
		default:
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

		// Look up the issue to know who the current assignee is — needed for
		// split-type routing on subscribers (new_comment isn't split, but we
		// pass it for consistency and so future split types Just Work).
		commentAssigneeMemberID := ""
		if issueID != "" {
			if iss, err := queries.GetIssue(ctx, parseUUID(issueID)); err == nil {
				commentAssigneeMemberID = issueAssigneeMemberID(iss)
			}
		}

		notifySubscribers(ctx, queries, bus, issueID, issueStatus, e.WorkspaceID, e,
			nil, "new_comment", "info",
			issueTitle, commentContent,
			commentDetails, commentAssigneeMemberID)

		// Notify @mentions in comment content.
		mentions := parseMentions(commentContent)
		if len(mentions) > 0 {
			skip := map[string]bool{e.ActorID: true}
			notifyMentionedMembers(bus, queries, e, mentions, issueID, issueTitle, issueStatus,
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
			false,
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
			false,
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
			emptyDetails,
			issueAssigneeMemberID(issue))
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
		"route":          item.Route,
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
