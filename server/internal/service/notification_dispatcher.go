package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// NotificationDispatcher subscribes to notification:* events on the event bus
// and pushes real-time alerts (sound + inbox) to member users via WebSocket.
//
// Design (OXY-583 §6.2 / OXY-588):
//   - Listens for all notification:* events
//   - Resolves target users (issue creator + member-type subscribers)
//   - Checks user notification preferences (server-side gate)
//   - Applies per-user rate limiting
//   - Creates inbox items and pushes WS frames via Broadcaster.SendToUser
type NotificationDispatcher struct {
	Queries     *db.Queries
	Bus         *events.Bus
	Broadcaster realtime.Broadcaster
	RateLimiter *NotificationRateLimiter
}

// NewNotificationDispatcher creates and wires a NotificationDispatcher.
// Caller must call Start() to subscribe to events.
func NewNotificationDispatcher(q *db.Queries, bus *events.Bus, bc realtime.Broadcaster, rl *NotificationRateLimiter) *NotificationDispatcher {
	return &NotificationDispatcher{
		Queries:     q,
		Bus:         bus,
		Broadcaster: bc,
		RateLimiter: rl,
	}
}

// Start subscribes to all notification:* event types on the event bus.
// Handlers run synchronously; long-running work should be offloaded.
func (d *NotificationDispatcher) Start() {
	d.Bus.Subscribe(protocol.EventNotificationIssueDone, d.handleIssueDone)
	d.Bus.Subscribe(protocol.EventNotificationChildBlocked, d.handleChildBlocked)
	d.Bus.Subscribe(protocol.EventNotificationIssueBlocked, d.handleIssueBlocked)
	d.Bus.Subscribe(protocol.EventNotificationInReview, d.handleInReview)
	d.Bus.Subscribe(protocol.EventNotificationParentChainDone, d.handleParentChainDone)
	d.Bus.Subscribe(protocol.EventNotificationTaskFailed, d.handleTaskFailed)
	d.Bus.Subscribe(protocol.EventNotificationStageClosed, d.handleStageClosed)
	d.Bus.Subscribe(protocol.EventNotificationBlockedTimeout, d.handleBlockedTimeout)
	d.Bus.Subscribe(protocol.EventNotificationMentionDecision, d.handleMentionDecision)
}

// ---- event handlers ----

func (d *NotificationDispatcher) handleIssueDone(e events.Event) {
	d.dispatch(context.Background(), e, protocol.EventNotificationIssueDone)
}

func (d *NotificationDispatcher) handleChildBlocked(e events.Event) {
	d.dispatch(context.Background(), e, protocol.EventNotificationChildBlocked)
}

func (d *NotificationDispatcher) handleIssueBlocked(e events.Event) {
	d.dispatch(context.Background(), e, protocol.EventNotificationIssueBlocked)
}

func (d *NotificationDispatcher) handleInReview(e events.Event) {
	d.dispatch(context.Background(), e, protocol.EventNotificationInReview)
}

func (d *NotificationDispatcher) handleParentChainDone(e events.Event) {
	d.dispatch(context.Background(), e, protocol.EventNotificationParentChainDone)
}

func (d *NotificationDispatcher) handleTaskFailed(e events.Event) {
	d.dispatch(context.Background(), e, protocol.EventNotificationTaskFailed)
}

func (d *NotificationDispatcher) handleStageClosed(e events.Event) {
	d.dispatch(context.Background(), e, protocol.EventNotificationStageClosed)
}

func (d *NotificationDispatcher) handleBlockedTimeout(e events.Event) {
	d.dispatch(context.Background(), e, protocol.EventNotificationBlockedTimeout)
}

func (d *NotificationDispatcher) handleMentionDecision(e events.Event) {
	// OXY-583 §实现注意事项 #4: PR reviewer scenario registers handler but
	// returns empty until GitHub reviewer→Multica user mapping is ready.
	// TODO: pending GitHub reviewer→Multica user mapping
}

// ---- core dispatch logic ----

// dispatch resolves target users, checks preferences, applies rate limiting,
// creates inbox items, and pushes WS frames to each eligible member user.
func (d *NotificationDispatcher) dispatch(ctx context.Context, e events.Event, eventType string) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		slog.Warn("notification dispatch: invalid payload type", "event_type", eventType)
		return
	}

	// Determine which issue to use for resolving users.
	issueID, _ := payload["issue_id"].(string)
	if issueID == "" {
		// Stage closed has parent_id and child_id instead of issue_id.
		// Use parent_id as the anchor for user resolution.
		issueID, _ = payload["parent_id"].(string)
		if issueID == "" {
			slog.Warn("notification dispatch: no issue_id or parent_id", "event_type", eventType)
			return
		}
	}

	issueUUID, err := util.ParseUUID(issueID)
	if err != nil {
		slog.Warn("notification dispatch: invalid issue UUID", "event_type", eventType, "issue_id", issueID)
		return
	}

	// Load the issue for metadata (title, identifier, workspace).
	issue, err := d.Queries.GetIssue(ctx, issueUUID)
	if err != nil {
		slog.Warn("notification dispatch: failed to load issue", "event_type", eventType, "issue_id", issueID, "error", err)
		return
	}

	workspaceIDStr := util.UUIDToString(issue.WorkspaceID)

	// Resolve target users: creator (if member) + member-type subscribers.
	targetUserIDs := d.resolveTargetUsers(ctx, issue, eventType, payload)

	if len(targetUserIDs) == 0 {
		return
	}

	// Batch-load notification preferences for all target users.
	prefs := d.loadNotificationPrefs(ctx, workspaceIDStr, targetUserIDs)

	// Build the NotificationData that will go into the WS payload.
	data := buildNotificationData(issue, eventType, payload)

	severity := protocol.NotificationSeverity(eventType)
	sound := protocol.NotificationSound(eventType)

	for _, userID := range targetUserIDs {
		userPrefs := prefs[userID]

		// Server-side preference gate: if the user muted this notification
		// type, skip entirely (no inbox item, no WS push).
		if !protocol.IsNotificationEnabled(userPrefs, eventType) {
			continue
		}

		// Rate limiting: skip if this user has exceeded the per-second limit.
		if d.RateLimiter != nil && !d.RateLimiter.Allow(userID) {
			slog.Debug("notification rate limited", "user_id", userID, "event_type", eventType)
			continue
		}

		// Determine if this user should receive sound.
		shouldSound := protocol.IsSoundEnabled(userPrefs, eventType)

		// Create inbox item.
		title := buildNotificationTitle(issue.Title, eventType)
		body := buildNotificationBody(issue, eventType, payload)

		var inboxItem *protocol.NotificationInbox
		item, err := d.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   issue.WorkspaceID,
			RecipientType: "member",
			RecipientID:   util.MustParseUUID(userID),
			Type:          mapNotificationTypeToInboxType(eventType),
			Severity:      severity,
			IssueID:       issue.ID,
			Title:         title,
			Body:          util.StrToText(body),
			ActorType:     util.StrToText("system"),
			ActorID:       util.MustParseUUID("00000000-0000-0000-0000-000000000000"),
			Details:       mustMarshalJSON(data),
		})
		if err != nil {
			slog.Warn("notification dispatch: failed to create inbox item",
				"user_id", userID, "event_type", eventType, "error", err)
			// Continue without inbox item — still push WS frame for sound.
		} else {
			inboxItem = &protocol.NotificationInbox{
				ID:       util.UUIDToString(item.ID),
				Title:    item.Title,
				Severity: item.Severity,
			}
		}

		// Build the push payload.
		pushPayload := protocol.NotificationPushPayload{
			Type:             eventType,
			ShowNotification: true,
			InboxItem:        inboxItem,
			Data:             data,
		}
		if shouldSound {
			pushPayload.Sound = sound
		}

		// Marshal and push via WebSocket.
		frame, err := json.Marshal(pushPayload)
		if err != nil {
			slog.Warn("notification dispatch: failed to marshal push payload", "error", err)
			continue
		}
		d.Broadcaster.SendToUser(userID, frame)
	}
}

// resolveTargetUsers returns the set of member-type user IDs that should
// receive a notification for the given issue and event type.
func (d *NotificationDispatcher) resolveTargetUsers(ctx context.Context, issue db.Issue, eventType string, payload map[string]any) []string {
	seen := map[string]bool{}
	var users []string

	// 1. Issue creator (only if member type).
	if issue.CreatorType == "member" {
		uid := util.UUIDToString(issue.CreatorID)
		seen[uid] = true
		users = append(users, uid)
	}

	// 2. Member-type subscribers of the issue.
	issueUUID := issue.ID
	// For stage_closed, the parent is the anchor issue (already resolved above).
	subs, err := d.Queries.ListIssueSubscribers(ctx, issueUUID)
	if err != nil {
		slog.Warn("notification dispatch: failed to list subscribers", "issue_id", util.UUIDToString(issueUUID), "error", err)
	} else {
		for _, sub := range subs {
			if sub.UserType != "member" {
				continue
			}
			uid := util.UUIDToString(sub.UserID)
			if !seen[uid] {
				seen[uid] = true
				users = append(users, uid)
			}
		}
	}

	// 3. For child_blocked, also include the parent's creator + subscribers.
	if eventType == protocol.EventNotificationChildBlocked {
		parentID, _ := payload["parent_id"].(string)
		if parentID != "" {
			parentUUID, err := util.ParseUUID(parentID)
			if err == nil {
				parent, err := d.Queries.GetIssue(ctx, parentUUID)
				if err == nil && parent.CreatorType == "member" {
					uid := util.UUIDToString(parent.CreatorID)
					if !seen[uid] {
						seen[uid] = true
						users = append(users, uid)
					}
				}
				// Also include parent subscribers.
				parentSubs, err := d.Queries.ListIssueSubscribers(ctx, parentUUID)
				if err == nil {
					for _, sub := range parentSubs {
						if sub.UserType != "member" {
							continue
						}
						uid := util.UUIDToString(sub.UserID)
						if !seen[uid] {
							seen[uid] = true
							users = append(users, uid)
						}
					}
				}
			}
		}
	}

	// 4. For stage_closed, also include the child issue subscribers.
	if eventType == protocol.EventNotificationStageClosed {
		childID, _ := payload["child_id"].(string)
		if childID != "" {
			childUUID, err := util.ParseUUID(childID)
			if err == nil {
				childSubs, err := d.Queries.ListIssueSubscribers(ctx, childUUID)
				if err == nil {
					for _, sub := range childSubs {
						if sub.UserType != "member" {
							continue
						}
						uid := util.UUIDToString(sub.UserID)
						if !seen[uid] {
							seen[uid] = true
							users = append(users, uid)
						}
					}
				}
			}
		}
	}

	return users
}

// loadNotificationPrefs loads notification preferences for a batch of user IDs
// in the given workspace. Returns a map from user_id string to parsed preferences.
func (d *NotificationDispatcher) loadNotificationPrefs(ctx context.Context, workspaceID string, userIDs []string) map[string]map[string]string {
	if len(userIDs) == 0 {
		return nil
	}

	// Load preferences one by one via GetNotificationPreference.
	// This is acceptable because notification dispatch is not on the hot path.
	result := make(map[string]map[string]string, len(userIDs))
	for _, uid := range userIDs {
		u, err := util.ParseUUID(uid)
		if err != nil {
			continue
		}
		pref, err := d.Queries.GetNotificationPreference(ctx, db.GetNotificationPreferenceParams{
			WorkspaceID: util.MustParseUUID(workspaceID),
			UserID:      u,
		})
		if err != nil {
			// No preference row → all defaults (enabled).
			result[uid] = map[string]string{}
			continue
		}
		var prefs map[string]string
		if err := json.Unmarshal(pref.Preferences, &prefs); err != nil {
			prefs = map[string]string{}
		}
		result[uid] = prefs
	}
	return result
}

// ---- helpers ----

func buildNotificationData(issue db.Issue, eventType string, payload map[string]any) protocol.NotificationData {
	data := protocol.NotificationData{
		IssueID:   util.UUIDToString(issue.ID),
		IssueTitle: issue.Title,
	}

	if v, ok := payload["parent_id"].(string); ok {
		data.ParentID = v
	}
	if v, ok := payload["child_id"].(string); ok {
		data.ChildID = v
	}
	if v, ok := payload["blocked_reason"].(string); ok {
		data.BlockedReason = v
	}
	if v, ok := payload["waiting_on"].(string); ok {
		data.WaitingOn = v
	}
	if v, ok := payload["stage"].(float64); ok {
		data.Stage = int32(v)
	} else if v, ok := payload["stage"].(int32); ok {
		data.Stage = v
	}

	// TODO: populate IssueIdentifier and WorkspaceSlug when available.
	// These require additional DB lookups that can be added later.

	return data
}

func buildNotificationTitle(issueTitle string, eventType string) string {
	switch eventType {
	case protocol.EventNotificationIssueDone:
		return "Issue completed: " + issueTitle
	case protocol.EventNotificationChildBlocked:
		return "Sub-issue blocked: " + issueTitle
	case protocol.EventNotificationIssueBlocked:
		return "Issue blocked: " + issueTitle
	case protocol.EventNotificationInReview:
		return "Ready for review: " + issueTitle
	case protocol.EventNotificationParentChainDone:
		return "All sub-issues complete: " + issueTitle
	case protocol.EventNotificationTaskFailed:
		return "Task failed: " + issueTitle
	case protocol.EventNotificationStageClosed:
		return "Stage completed: " + issueTitle
	case protocol.EventNotificationBlockedTimeout:
		return "Issue still blocked: " + issueTitle
	case protocol.EventNotificationMentionDecision:
		return "Decision requested: " + issueTitle
	default:
		return issueTitle
	}
}

func buildNotificationBody(issue db.Issue, eventType string, payload map[string]any) string {
	switch eventType {
	case protocol.EventNotificationIssueDone:
		return "The issue \"" + issue.Title + "\" has been marked as done."
	case protocol.EventNotificationChildBlocked:
		parentID, _ := payload["parent_id"].(string)
		return "A sub-issue of " + parentID + " is blocked and may need your attention."
	case protocol.EventNotificationIssueBlocked:
		reason, _ := payload["blocked_reason"].(string)
		if reason != "" {
			return "Reason: " + reason
		}
		return "This issue has been blocked."
	case protocol.EventNotificationInReview:
		return "This issue is ready for your review."
	case protocol.EventNotificationParentChainDone:
		return "All sub-issues have been completed."
	case protocol.EventNotificationTaskFailed:
		return "An agent task for this issue has failed."
	case protocol.EventNotificationStageClosed:
		stage := int32(0)
		if v, ok := payload["stage"].(float64); ok {
			stage = int32(v)
		} else if v, ok := payload["stage"].(int32); ok {
			stage = v
		}
		if stage > 0 {
			return "Stage " + strconv.Itoa(int(stage)) + " has been completed."
		}
		return "All sub-issues in this stage have been completed."
	case protocol.EventNotificationBlockedTimeout:
		return "This issue has been blocked for over 4 hours."
	default:
		return ""
	}
}

func mapNotificationTypeToInboxType(eventType string) string {
	switch eventType {
	case protocol.EventNotificationIssueDone:
		return "issue_done"
	case protocol.EventNotificationChildBlocked:
		return "child_blocked"
	case protocol.EventNotificationIssueBlocked:
		return "issue_blocked"
	case protocol.EventNotificationInReview:
		return "in_review"
	case protocol.EventNotificationParentChainDone:
		return "parent_chain_done"
	case protocol.EventNotificationTaskFailed:
		return "task_failed"
	case protocol.EventNotificationStageClosed:
		return "stage_closed"
	case protocol.EventNotificationBlockedTimeout:
		return "blocked_timeout"
	case protocol.EventNotificationMentionDecision:
		return "mention_decision"
	default:
		return eventType
	}
}



func mustMarshalJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
