package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	notifyutil "github.com/multica-ai/multica/server/internal/notify"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

func buildNotificationLink(ctx context.Context, queries *db.Queries, workspaceID, issueID, commentID string) string {
	return buildNotificationContext(ctx, queries, workspaceID, issueID, commentID, "").Link
}

type notificationContext struct {
	Link            string
	IssueIdentifier string
}

func resolveNotificationActorName(ctx context.Context, queries *db.Queries, actorType, actorID string) string {
	actorType = strings.TrimSpace(actorType)
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return ""
	}

	switch actorType {
	case "member":
		user, err := queries.GetUser(ctx, parseUUID(actorID))
		if err != nil {
			return ""
		}
		return firstValue(user.Name, user.Email)
	case "agent":
		agent, err := queries.GetAgent(ctx, parseUUID(actorID))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(agent.Name)
	default:
		return ""
	}
}

func buildNotificationContext(ctx context.Context, queries *db.Queries, workspaceID, issueID, commentID, appOrigin string) notificationContext {
	if workspaceID == "" || issueID == "" {
		return notificationContext{}
	}

	workspace, err := queries.GetWorkspace(ctx, parseUUID(workspaceID))
	if err != nil {
		slog.Error("failed to resolve workspace for notification link",
			"workspace_id", workspaceID,
			"issue_id", issueID,
			"comment_id", commentID,
			"error", err,
		)
		return notificationContext{}
	}

	identifier := ""
	if issue, err := queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          parseUUID(issueID),
		WorkspaceID: parseUUID(workspaceID),
	}); err == nil {
		identifier = fmt.Sprintf("%s-%d", workspace.IssuePrefix, issue.Number)
	} else {
		slog.Error("failed to resolve issue identifier for notification",
			"workspace_id", workspaceID,
			"issue_id", issueID,
			"comment_id", commentID,
			"error", err,
		)
	}

	issueRef := issueID
	if identifier != "" {
		issueRef = identifier
	}
	baseURL := notificationBaseURL(appOrigin)
	if commentID != "" {
		return notificationContext{
			Link:            notifyutil.CommentURL(baseURL, workspace.Slug, issueRef, commentID),
			IssueIdentifier: identifier,
		}
	}
	return notificationContext{
		Link:            notifyutil.IssueURL(baseURL, workspace.Slug, issueRef),
		IssueIdentifier: identifier,
	}
}

func notificationBaseURL(appOrigin string) string {
	if normalized := normalizeNotificationBaseURL(appOrigin); normalized != "" {
		return normalized
	}
	return notifyutil.AppURL()
}

func normalizeNotificationBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func recordMentionNotification(
	ctx context.Context,
	queries *db.Queries,
	e events.Event,
	recipientID string,
	issueID string,
	commentID string,
	title string,
	body string,
	link string,
	issueIdentifier string,
	actorName string,
	details []byte,
) {
	recordNotification(ctx, queries, e, recipientID, "mentioned", "info", issueID, commentID, title, body, link, issueIdentifier, actorName, details)
}

func recordNotification(
	ctx context.Context,
	queries *db.Queries,
	e events.Event,
	recipientID string,
	eventType string,
	severity string,
	issueID string,
	commentID string,
	title string,
	body string,
	link string,
	issueIdentifier string,
	actorName string,
	details []byte,
) {
	if len(details) == 0 {
		details = emptyDetails
	}

	// Compute summary for IM compact rendering
	summary := ExtractSummary("", body, title, defaultIMSummaryMaxChars)

	payloadSnapshot, err := json.Marshal(map[string]any{
		"type":             eventType,
		"severity":         severity,
		"title":            title,
		"summary":          summary,
		"body":             body,
		"link":             link,
		"issue_id":         issueID,
		"issue_identifier": issueIdentifier,
		"comment_id":       commentID,
		"actor_type":       e.ActorType,
		"actor_id":         e.ActorID,
		"actor_name":       actorName,
		"render_mode":      "auto",
		"details":          json.RawMessage(details),
	})
	if err != nil {
		payloadSnapshot = emptyDetails
	}

	// commentID may be empty for non-comment events (e.g. issue:created).
	var commentUUID pgtype.UUID
	if commentID != "" {
		commentUUID = parseUUID(commentID)
	}

	event, err := queries.CreateNotificationEvent(ctx, db.CreateNotificationEventParams{
		WorkspaceID:     parseUUID(e.WorkspaceID),
		RecipientUserID: parseUUID(recipientID),
		Type:            eventType,
		Severity:        severity,
		IssueID:         parseUUID(issueID),
		CommentID:       commentUUID,
		ActorType:       util.StrToText(e.ActorType),
		ActorID:         parseUUID(e.ActorID),
		Title:           title,
		Body:            util.StrToText(body),
		Link:            util.StrToText(link),
		Details:         details,
	})
	if err != nil {
		slog.Error("failed to create canonical notification",
			"recipient_id", recipientID,
			"issue_id", issueID,
			"comment_id", commentID,
			"type", eventType,
			"error", err,
		)
		return
	}

	_, err = queries.CreateNotificationDelivery(ctx, db.CreateNotificationDeliveryParams{
		NotificationEventID: event.ID,
		Channel:             "inbox",
		Status:              "sent",
		AttemptCount:        1,
		LastError:           pgtype.Text{},
		PayloadSnapshot:     payloadSnapshot,
		SentAt:              pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	})
	if err != nil {
		slog.Error("failed to create inbox delivery record for mention notification",
			"notification_event_id", util.UUIDToString(event.ID),
			"recipient_id", recipientID,
			"error", err,
		)
	}

	if eventType == "mentioned" {
		recordMentionDingTalkDelivery(ctx, queries, recipientID, event, payloadSnapshot)
		recordMentionEmailDelivery(ctx, queries, recipientID, event, payloadSnapshot)
	}
	recordCustomWebhookDeliveries(ctx, queries, recipientID, event, payloadSnapshot)
	recordOpenclawWeixinDelivery(ctx, queries, recipientID, event, payloadSnapshot)
}

func recordMentionDingTalkDelivery(
	ctx context.Context,
	queries *db.Queries,
	recipientID string,
	event db.NotificationEvent,
	payloadSnapshot []byte,
) {
	if event.CommentID.Valid {
		exists, err := queries.ExistsDeliveryByCommentAndChannel(ctx, db.ExistsDeliveryByCommentAndChannelParams{
			RecipientUserID: event.RecipientUserID,
			CommentID:       event.CommentID,
			Channel:         "dingtalk",
		})
		if err != nil {
			slog.Error("failed to check dingtalk dedup",
				"recipient_id", recipientID,
				"comment_id", util.UUIDToString(event.CommentID),
				"error", err,
			)
		}
		if exists {
			return
		}
	}

	prefs, err := queries.ListNotificationChannelPreferencesByUser(ctx, parseUUID(recipientID))
	if err != nil {
		slog.Error("failed to load notification preferences for dingtalk delivery",
			"recipient_id", recipientID,
			"notification_event_id", util.UUIDToString(event.ID),
			"error", err,
		)
		return
	}

	var dingtalkPref *db.NotificationChannelPreference
	for i := range prefs {
		pref := &prefs[i]
		if pref.Channel == "dingtalk" && pref.EventType == "mentioned" && pref.Enabled {
			dingtalkPref = pref
			break
		}
	}
	if dingtalkPref == nil {
		return
	}

	bindings, err := queries.ListExternalAccountBindingsByUser(ctx, parseUUID(recipientID))
	if err != nil {
		slog.Error("failed to load external account bindings for dingtalk delivery",
			"recipient_id", recipientID,
			"notification_event_id", util.UUIDToString(event.ID),
			"error", err,
		)
		return
	}

	var binding *db.ExternalAccountBinding
	for i := range bindings {
		candidate := &bindings[i]
		if candidate.Provider != "dingtalk" || candidate.Status != "active" {
			continue
		}
		if dingtalkPref.BindingID.Valid && util.UUIDToString(dingtalkPref.BindingID) != util.UUIDToString(candidate.ID) {
			continue
		}
		binding = candidate
		break
	}
	if binding == nil {
		return
	}

	payload := map[string]any{
		"binding_id":         util.UUIDToString(binding.ID),
		"provider":           binding.Provider,
		"external_user_id":   binding.ExternalUserID,
		"notification_event": json.RawMessage(payloadSnapshot),
	}
	dingtalkPayload, err := json.Marshal(payload)
	if err != nil {
		dingtalkPayload = payloadSnapshot
	}

	if _, err := queries.CreateNotificationDelivery(ctx, db.CreateNotificationDeliveryParams{
		NotificationEventID: event.ID,
		Channel:             "dingtalk",
		Status:              "pending",
		AttemptCount:        0,
		LastError:           pgtype.Text{},
		PayloadSnapshot:     dingtalkPayload,
		SentAt:              pgtype.Timestamptz{},
	}); err != nil {
		slog.Error("failed to create dingtalk delivery record for mention notification",
			"notification_event_id", util.UUIDToString(event.ID),
			"recipient_id", recipientID,
			"error", err,
		)
	}
}

// recordDingTalkTaskDelivery creates a pending DingTalk delivery for task events
// (task_completed, task_failed) if the recipient has a dingtalk preference enabled
// for the specific task event type (dingtalk + task_completed / dingtalk + task_failed).
func recordDingTalkTaskDelivery(
	ctx context.Context,
	queries *db.Queries,
	recipientID string,
	event db.NotificationEvent,
	payloadSnapshot []byte,
) {
	if event.CommentID.Valid {
		exists, err := queries.ExistsDeliveryByCommentAndChannel(ctx, db.ExistsDeliveryByCommentAndChannelParams{
			RecipientUserID: event.RecipientUserID,
			CommentID:       event.CommentID,
			Channel:         "dingtalk",
		})
		if err != nil {
			slog.Error("failed to check dingtalk task dedup",
				"recipient_id", recipientID,
				"comment_id", util.UUIDToString(event.CommentID),
				"error", err,
			)
		}
		if exists {
			return
		}
	}

	prefs, err := queries.ListNotificationChannelPreferencesByUser(ctx, parseUUID(recipientID))
	if err != nil {
		slog.Error("failed to load notification preferences for dingtalk task delivery",
			"recipient_id", recipientID,
			"notification_event_id", util.UUIDToString(event.ID),
			"error", err,
		)
		return
	}

	// DingTalk task notifications use independent per-event-type preferences:
	// "dingtalk + task_completed" or "dingtalk + task_failed".
	// Determine the event type from the notification event.
	taskEventType := event.Type // "task_completed" or "task_failed"
	var dingtalkPref *db.NotificationChannelPreference
	for i := range prefs {
		pref := &prefs[i]
		if pref.Channel == "dingtalk" && pref.EventType == taskEventType && pref.Enabled {
			dingtalkPref = pref
			break
		}
	}
	if dingtalkPref == nil {
		return
	}

	bindings, err := queries.ListExternalAccountBindingsByUser(ctx, parseUUID(recipientID))
	if err != nil {
		return
	}

	var binding *db.ExternalAccountBinding
	for i := range bindings {
		candidate := &bindings[i]
		if candidate.Provider != "dingtalk" || candidate.Status != "active" {
			continue
		}
		if dingtalkPref.BindingID.Valid && util.UUIDToString(dingtalkPref.BindingID) != util.UUIDToString(candidate.ID) {
			continue
		}
		binding = candidate
		break
	}
	if binding == nil {
		return
	}

	payload := map[string]any{
		"binding_id":         util.UUIDToString(binding.ID),
		"provider":           binding.Provider,
		"external_user_id":   binding.ExternalUserID,
		"notification_event": json.RawMessage(payloadSnapshot),
	}
	dingtalkPayload, err := json.Marshal(payload)
	if err != nil {
		dingtalkPayload = payloadSnapshot
	}

	if _, err := queries.CreateNotificationDelivery(ctx, db.CreateNotificationDeliveryParams{
		NotificationEventID: event.ID,
		Channel:             "dingtalk",
		Status:              "pending",
		AttemptCount:        0,
		LastError:           pgtype.Text{},
		PayloadSnapshot:     dingtalkPayload,
		SentAt:              pgtype.Timestamptz{},
	}); err != nil {
		slog.Error("failed to create dingtalk delivery record for task notification",
			"notification_event_id", util.UUIDToString(event.ID),
			"recipient_id", recipientID,
			"error", err,
		)
	}
}

func recordMentionEmailDelivery(
	ctx context.Context,
	queries *db.Queries,
	recipientID string,
	event db.NotificationEvent,
	payloadSnapshot []byte,
) {
	prefs, err := queries.ListNotificationChannelPreferencesByUser(ctx, parseUUID(recipientID))
	if err != nil {
		slog.Error("failed to load notification preferences for email delivery",
			"recipient_id", recipientID,
			"notification_event_id", util.UUIDToString(event.ID),
			"error", err,
		)
		return
	}

	var emailPref *db.NotificationChannelPreference
	for i := range prefs {
		pref := &prefs[i]
		if pref.Channel == "email" && pref.EventType == "mentioned" && pref.Enabled {
			emailPref = pref
			break
		}
	}
	if emailPref == nil {
		return
	}

	bindings, err := queries.ListExternalAccountBindingsByUser(ctx, parseUUID(recipientID))
	if err != nil {
		slog.Error("failed to load external account bindings for email delivery",
			"recipient_id", recipientID,
			"notification_event_id", util.UUIDToString(event.ID),
			"error", err,
		)
		return
	}

	var binding *db.ExternalAccountBinding
	for i := range bindings {
		candidate := &bindings[i]
		if candidate.Provider != "email" || candidate.Status != "active" {
			continue
		}
		if emailPref.BindingID.Valid && util.UUIDToString(emailPref.BindingID) != util.UUIDToString(candidate.ID) {
			continue
		}
		binding = candidate
		break
	}
	if binding == nil {
		return
	}

	payload := map[string]any{
		"binding_id":         util.UUIDToString(binding.ID),
		"provider":           binding.Provider,
		"external_user_id":   binding.ExternalUserID,
		"notification_event": json.RawMessage(payloadSnapshot),
	}
	emailPayload, err := json.Marshal(payload)
	if err != nil {
		emailPayload = payloadSnapshot
	}

	if _, err := queries.CreateNotificationDelivery(ctx, db.CreateNotificationDeliveryParams{
		NotificationEventID: event.ID,
		Channel:             "email",
		Status:              "pending",
		AttemptCount:        0,
		LastError:           pgtype.Text{},
		PayloadSnapshot:     emailPayload,
		SentAt:              pgtype.Timestamptz{},
	}); err != nil {
		slog.Error("failed to create email delivery record for mention notification",
			"notification_event_id", util.UUIDToString(event.ID),
			"recipient_id", recipientID,
			"error", err,
		)
	}
}

// openclawWeixinPreferenceEventType maps internal notification types to the
// openclaw_weixin preference event types configured in supportedNotificationPreferences.
func openclawWeixinPreferenceEventType(notificationType string) string {
	switch notificationType {
	case "mentioned":
		return "mentioned"
	case "task_completed":
		return "task_completed"
	case "task_failed":
		return "task_failed"
	case "new_comment":
		return "replied"
	default:
		return ""
	}
}

// recordOpenclawWeixinDelivery creates a pending delivery record for the
// openclaw_weixin channel if the recipient has the preference enabled and
// an active binding. Follows the same pattern as recordMentionDingTalkDelivery.
func recordOpenclawWeixinDelivery(
	ctx context.Context,
	queries *db.Queries,
	recipientID string,
	event db.NotificationEvent,
	payloadSnapshot []byte,
) {
	preferenceEventType := openclawWeixinPreferenceEventType(event.Type)
	if preferenceEventType == "" {
		return
	}

	prefs, err := queries.ListNotificationChannelPreferencesByUser(ctx, parseUUID(recipientID))
	if err != nil {
		slog.Error("failed to load notification preferences for openclaw_weixin delivery",
			"recipient_id", recipientID,
			"notification_event_id", util.UUIDToString(event.ID),
			"error", err,
		)
		return
	}

	var weixinPref *db.NotificationChannelPreference
	for i := range prefs {
		pref := &prefs[i]
		if pref.Channel == "openclaw_weixin" && pref.EventType == preferenceEventType && pref.Enabled {
			weixinPref = pref
			break
		}
	}
	if weixinPref == nil {
		return
	}

	if event.CommentID.Valid {
		exists, err := queries.ExistsOpenclawWeixinDeliveryByComment(ctx, db.ExistsOpenclawWeixinDeliveryByCommentParams{
			RecipientUserID: event.RecipientUserID,
			CommentID:       event.CommentID,
		})
		if err != nil {
			slog.Error("failed to check openclaw_weixin dedup",
				"recipient_id", recipientID,
				"comment_id", util.UUIDToString(event.CommentID),
				"error", err,
			)
		}
		if exists {
			return
		}
	}

	bindings, err := queries.ListExternalAccountBindingsByUser(ctx, parseUUID(recipientID))
	if err != nil {
		slog.Error("failed to load external account bindings for openclaw_weixin delivery",
			"recipient_id", recipientID,
			"notification_event_id", util.UUIDToString(event.ID),
			"error", err,
		)
		return
	}

	var binding *db.ExternalAccountBinding
	for i := range bindings {
		candidate := &bindings[i]
		if candidate.Provider != "openclaw_weixin" || candidate.Status != "active" {
			continue
		}
		if weixinPref.BindingID.Valid && util.UUIDToString(weixinPref.BindingID) != util.UUIDToString(candidate.ID) {
			continue
		}
		binding = candidate
		break
	}
	if binding == nil {
		return
	}

	// Extract channel from binding metadata (defaults to "openclaw-weixin")
	channel := "openclaw-weixin"
	if len(binding.Metadata) > 0 {
		var meta map[string]string
		if err := json.Unmarshal(binding.Metadata, &meta); err == nil {
			if ch := strings.TrimSpace(meta["channel"]); ch != "" {
				channel = ch
			}
		}
	}

	payload := map[string]any{
		"binding_id":         util.UUIDToString(binding.ID),
		"provider":           binding.Provider,
		"wechat_id":          binding.ExternalUserID,
		"channel":            channel,
		"notification_event": json.RawMessage(payloadSnapshot),
	}
	weixinPayload, err := json.Marshal(payload)
	if err != nil {
		weixinPayload = payloadSnapshot
	}

	if _, err := queries.CreateNotificationDelivery(ctx, db.CreateNotificationDeliveryParams{
		NotificationEventID: event.ID,
		Channel:             "openclaw_weixin",
		Status:              "pending",
		AttemptCount:        0,
		LastError:           pgtype.Text{},
		PayloadSnapshot:     weixinPayload,
		SentAt:              pgtype.Timestamptz{},
	}); err != nil {
		slog.Error("failed to create openclaw_weixin delivery record",
			"notification_event_id", util.UUIDToString(event.ID),
			"recipient_id", recipientID,
			"error", err,
		)
	}
}

func latestAgentCommentForTask(
	ctx context.Context,
	queries *db.Queries,
	workspaceID string,
	issueID string,
	agentID string,
	task db.AgentTaskQueue,
	taskLoaded bool,
) (string, string) {
	if !taskLoaded || !task.StartedAt.Valid || issueID == "" || workspaceID == "" || agentID == "" {
		return "", ""
	}
	comment, err := queries.GetLatestAgentCommentSince(ctx, db.GetLatestAgentCommentSinceParams{
		IssueID:     parseUUID(issueID),
		WorkspaceID: parseUUID(workspaceID),
		AuthorID:    parseUUID(agentID),
		CreatedAt:   task.StartedAt,
	})
	if err != nil {
		return "", ""
	}
	return comment.Content, util.UUIDToString(comment.ID)
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

func recordCustomWebhookDeliveries(
	ctx context.Context,
	queries *db.Queries,
	recipientID string,
	event db.NotificationEvent,
	payloadSnapshot []byte,
) {
	preferenceEventType := customWebhookPreferenceEventType(event.Type)
	if preferenceEventType == "" {
		return
	}

	prefs, err := queries.ListNotificationChannelPreferencesByUser(ctx, parseUUID(recipientID))
	if err != nil {
		slog.Error("failed to load notification preferences for custom webhook delivery",
			"recipient_id", recipientID,
			"notification_event_id", util.UUIDToString(event.ID),
			"error", err,
		)
		return
	}

	enabled := false
	for i := range prefs {
		pref := &prefs[i]
		if pref.Channel == "custom_webhook" && pref.EventType == preferenceEventType && pref.Enabled {
			enabled = true
			break
		}
	}
	if !enabled {
		return
	}

	endpoints, err := queries.ListEnabledNotificationWebhookEndpointsByUser(ctx, parseUUID(recipientID))
	if err != nil {
		slog.Error("failed to load custom webhook endpoints",
			"recipient_id", recipientID,
			"notification_event_id", util.UUIDToString(event.ID),
			"error", err,
		)
		return
	}

	for _, endpoint := range endpoints {
		payload := map[string]any{
			"webhook_endpoint_id": util.UUIDToString(endpoint.ID),
			"notification_event":  json.RawMessage(payloadSnapshot),
		}
		webhookPayload, err := json.Marshal(payload)
		if err != nil {
			webhookPayload = payloadSnapshot
		}

		if _, err := queries.CreateTargetedNotificationDelivery(ctx, db.CreateTargetedNotificationDeliveryParams{
			NotificationEventID: event.ID,
			Channel:             "custom_webhook",
			TargetType:          "webhook_endpoint",
			TargetID:            endpoint.ID,
			Status:              "pending",
			AttemptCount:        0,
			LastError:           pgtype.Text{},
			PayloadSnapshot:     webhookPayload,
			SentAt:              pgtype.Timestamptz{},
		}); err != nil {
			slog.Error("failed to create custom webhook delivery record",
				"notification_event_id", util.UUIDToString(event.ID),
				"recipient_id", recipientID,
				"webhook_endpoint_id", util.UUIDToString(endpoint.ID),
				"error", err,
			)
		}
	}
}

func customWebhookPreferenceEventType(notificationType string) string {
	switch notificationType {
	case "mentioned":
		return "mentioned"
	case "issue_assigned":
		return "issue_assigned"
	case "new_comment", "assignee_changed", "status_changed", "priority_changed", "due_date_changed", "task_failed":
		return "subscribed_issue_updated"
	default:
		return ""
	}
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
// allowlist, parent issue subscribers are also notified (deduplicated
// against direct subscribers).
// Returns the set of member IDs that were notified.
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
	commentID string,
	details []byte,
) map[string]bool {
	notified := notifyIssueSubscribers(ctx, queries, bus,
		issueID, issueID, issueStatus, workspaceID, e, exclude,
		notifType, severity, title, body, commentID, details)

	// Only a small allowlist of event types bubbles to parent subscribers.
	if !parentBubbleNotifTypes[notifType] {
		return notified
	}

	// Also notify parent issue subscribers if this is a sub-issue.
	issue, err := queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		slog.Error("failed to get issue for parent notification",
			"issue_id", issueID, "error", err)
		return notified
	}
	if !issue.ParentIssueID.Valid {
		return notified
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
	parentNotified := notifyIssueSubscribers(ctx, queries, bus,
		parentID, issueID, issueStatus, workspaceID, e, parentExclude,
		notifType, severity, title, body, commentID, details)
	for id := range parentNotified {
		notified[id] = true
	}
	return notified
}

// notifyIssueSubscribers sends inbox notifications to subscribers of
// subscriberIssueID, but creates inbox items pointing to targetIssueID.
// This allows querying subscribers from a parent issue while the notification
// links to the sub-issue where the change actually occurred.
// Returns the set of member IDs that were notified.
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
	commentID string,
	details []byte,
) map[string]bool {
	notified := map[string]bool{}

	subs, err := queries.ListIssueSubscribers(ctx, parseUUID(subscriberIssueID))
	if err != nil {
		slog.Error("failed to list subscribers for notification",
			"issue_id", subscriberIssueID, "error", err)
		return notified
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

		// Skip if this notification type is muted by the user
		if prefs, ok := userPrefs[subID]; ok && isNotifMuted(prefs, notifType) {
			continue
		}

		item, err := queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
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
		})
		if err != nil {
			slog.Error("subscriber notification creation failed",
				"subscriber_id", subID, "type", notifType, "error", err)
			continue
		}

		notified[subID] = true
		notificationCtx := buildNotificationContext(ctx, queries, workspaceID, targetIssueID, commentID, "")
		recordNotification(ctx, queries, e, subID, notifType, severity, targetIssueID, commentID, title, body, notificationCtx.Link, notificationCtx.IssueIdentifier, "", details)

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

	return notified
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

	// Check notification preferences for member recipients.
	if recipientType == "member" {
		prefs := loadUserPrefs(ctx, queries, workspaceID, []string{recipientID})
		if p, ok := prefs[recipientID]; ok && isNotifMuted(p, notifType) {
			return
		}
	}

	item, err := queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
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
	})
	if err != nil {
		slog.Error("direct notification creation failed",
			"recipient_id", recipientID, "type", notifType, "error", err)
		return
	}

	notificationCtx := buildNotificationContext(ctx, queries, workspaceID, issueID, "", "")
	if recipientType == "member" {
		recordNotification(ctx, queries, e, recipientID, notifType, severity, issueID, "", title, body, notificationCtx.Link, notificationCtx.IssueIdentifier, "", details)
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

// notifyMentionedMembers creates inbox items for each @mentioned member,
// excluding any IDs in the skip set. The actor is skipped for expanded @all
// mentions, but an explicit self-mention still produces a notification.
// When an @all mention is present, all workspace members are notified
// (excluding agents).
func notifyMentionedMembers(
	bus *events.Bus,
	queries *db.Queries,
	e events.Event,
	mentions []mention,
	issueID string,
	issueStatus string,
	title string,
	body string,
	commentID string,
	appOrigin string,
	skip map[string]bool,
	details []byte,
) {
	ctx := context.Background()

	// Collect the set of member IDs to notify.
	recipientIDs := map[string]bool{}
	explicitRecipientIDs := map[string]bool{}

	hasAll := false
	var squadIDs []string
	for _, m := range mentions {
		if m.Type == "all" {
			hasAll = true
			continue
		}
		if m.Type == "member" {
			recipientIDs[m.ID] = true
			explicitRecipientIDs[m.ID] = true
		}
		if m.Type == "squad" {
			squadIDs = append(squadIDs, m.ID)
		}
	}

	// Expand each @squad mention to its human members. Agent members of a
	// squad are reached via comment-trigger / assignment paths, not the
	// mention-inbox path, so we only seed member-typed recipients here.
	for _, sid := range squadIDs {
		squadUUID, err := util.ParseUUID(sid)
		if err != nil {
			continue
		}
		members, err := queries.ListSquadMembers(context.Background(), squadUUID)
		if err != nil {
			slog.Error("failed to list squad members for @squad mention", "squad_id", sid, "error", err)
			continue
		}
		for _, sm := range members {
			if sm.MemberType == "member" {
				recipientIDs[util.UUIDToString(sm.MemberID)] = true
			}
		}
	}

	// If @all is present, expand to all workspace members.
	if hasAll {
		members, err := queries.ListMembers(ctx, parseUUID(e.WorkspaceID))
		if err != nil {
			slog.Error("failed to list members for @all mention", "workspace_id", e.WorkspaceID, "error", err)
		} else {
			for _, m := range members {
				recipientIDs[util.UUIDToString(m.UserID)] = true
			}
		}
	}

	if len(recipientIDs) == 0 {
		return
	}

	notificationCtx := buildNotificationContext(ctx, queries, e.WorkspaceID, issueID, commentID, appOrigin)
	actorName := resolveNotificationActorName(ctx, queries, e.ActorType, e.ActorID)
	// Batch-load notification preferences for all mention recipients.
	var mentionUserIDs []string
	for id := range recipientIDs {
		if id != e.ActorID && !skip[id] {
			mentionUserIDs = append(mentionUserIDs, id)
		}
	}
	mentionPrefs := loadUserPrefs(context.Background(), queries, e.WorkspaceID, mentionUserIDs)

	for id := range recipientIDs {
		isExplicitSelfMention := id == e.ActorID && explicitRecipientIDs[id]
		if skip[id] && !isExplicitSelfMention {
			continue
		}
		if id == e.ActorID && !explicitRecipientIDs[id] {
			continue
		}
		// Skip if mentions/comments are muted by this user
		if p, ok := mentionPrefs[id]; ok && isNotifMuted(p, "mentioned") {
			continue
		}
		item, err := queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   parseUUID(e.WorkspaceID),
			RecipientType: "member",
			RecipientID:   parseUUID(id),
			Type:          "mentioned",
			Severity:      "info",
			IssueID:       parseUUID(issueID),
			Title:         title,
			Body:          util.StrToText(body),
			ActorType:     util.StrToText(e.ActorType),
			ActorID:       optionalUUID(e.ActorID),
			Details:       details,
		})
		if err != nil {
			slog.Error("mention inbox creation failed", "mentioned_id", id, "error", err)
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

		recordMentionNotification(ctx, queries, e, id, issueID, commentID, title, body, notificationCtx.Link, notificationCtx.IssueIdentifier, actorName, details)
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
		issue, ok := issueEventIssueFromPayload(payload["issue"])
		if !ok {
			return
		}
		appOrigin, _ := payload["app_origin"].(string)

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
			)
		}

		// Notify @mentions in description
		if issue.Description != nil && *issue.Description != "" {
			mentions := parseMentions(*issue.Description)
			notifyMentionedMembers(bus, queries, e, mentions, issue.ID, issue.Status,
				issue.Title, *issue.Description, "", appOrigin, skip, emptyDetails)
		}
	})

	// issue:updated — handle assignee changes, status changes, priority, due date
	bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		issue, ok := issueEventIssueFromPayload(payload["issue"])
		if !ok {
			return
		}
		appOrigin, _ := payload["app_origin"].(string)
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
				"",
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
				"",
				statusDetails)

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
				"",
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
				"",
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
				"",
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
				notifyMentionedMembers(bus, queries, e, added, issue.ID, issue.Status,
					issue.Title, *issue.Description, "", appOrigin, skip, emptyDetails)
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
		var parentID string
		switch c := payload["comment"].(type) {
		case handler.CommentResponse:
			issueID = c.IssueID
			commentID = c.ID
			commentContent = c.Content
			if c.ParentID != nil {
				parentID = *c.ParentID
			}
			authorType = c.AuthorType
		case map[string]any:
			issueID, _ = c["issue_id"].(string)
			commentID, _ = c["id"].(string)
			commentContent, _ = c["content"].(string)
			if pid, ok := c["parent_id"].(string); ok {
				parentID = pid
			}
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
		appOrigin, _ := payload["app_origin"].(string)

		commentDetails := emptyDetails
		if commentID != "" {
			commentDetails, _ = json.Marshal(map[string]string{
				"comment_id": commentID,
			})
		}

		notifySubscribers(ctx, queries, bus, issueID, issueStatus, e.WorkspaceID, e,
			nil, "new_comment", "info",
			issueTitle, commentContent,
			commentID,
			commentDetails)

		// Notify @mentions in comment content.
		mentions := parseMentions(commentContent)
		mentionedIDs := map[string]bool{}
		if len(mentions) > 0 {
			for _, m := range mentions {
				if m.Type == "member" {
					mentionedIDs[m.ID] = true
				}
			}
			skip := map[string]bool{e.ActorID: true}
			notifyMentionedMembers(bus, queries, e, mentions, issueID, issueStatus,
				issueTitle, commentContent, commentID, appOrigin, skip, commentDetails)
		}

		// Notify parent comment author on reply (OPE-856).
		// When a comment has a parent_id, notify the parent's author if:
		// - parent author is a member (not agent)
		// - parent author is not the current actor (no self-notification)
		// - parent author was not already @mentioned (dedup)
		if parentID != "" {
			parentComment, err := queries.GetComment(ctx, parseUUID(parentID))
			if err == nil &&
				parentComment.AuthorType == "member" &&
				util.UUIDToString(parentComment.AuthorID) != e.ActorID &&
				!mentionedIDs[util.UUIDToString(parentComment.AuthorID)] {

				recipientID := util.UUIDToString(parentComment.AuthorID)
				prefs := loadUserPrefs(ctx, queries, e.WorkspaceID, []string{recipientID})
				if p, ok := prefs[recipientID]; !ok || !isNotifMuted(p, "mentioned") {
					item, err := queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
						WorkspaceID:   parseUUID(e.WorkspaceID),
						RecipientType: "member",
						RecipientID:   parentComment.AuthorID,
						Type:          "mentioned",
						Severity:      "info",
						IssueID:       parseUUID(issueID),
						Title:         issueTitle,
						Body:          util.StrToText(commentContent),
						ActorType:     util.StrToText(e.ActorType),
						ActorID:       optionalUUID(e.ActorID),
						Details:       commentDetails,
					})
					if err != nil {
						slog.Error("reply notification inbox creation failed",
							"recipient_id", recipientID, "parent_comment_id", parentID, "error", err)
					} else {
						resp := inboxItemToResponse(item)
						resp["issue_status"] = issueStatus
						bus.Publish(events.Event{
							Type:        protocol.EventInboxNew,
							WorkspaceID: e.WorkspaceID,
							ActorType:   e.ActorType,
							ActorID:     e.ActorID,
							Payload:     map[string]any{"item": resp},
						})

						notificationCtx := buildNotificationContext(ctx, queries, e.WorkspaceID, issueID, commentID, appOrigin)
						actorName := resolveNotificationActorName(ctx, queries, e.ActorType, e.ActorID)
						recordMentionNotification(ctx, queries, e, recipientID, issueID, commentID,
							issueTitle, commentContent, notificationCtx.Link, notificationCtx.IssueIdentifier, actorName, commentDetails)
					}
				}
			}
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
	// but openclaw_weixin delivery is created if the user has it enabled.
	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		agentID, _ := payload["agent_id"].(string)
		issueID, _ := payload["issue_id"].(string)
		taskID, _ := payload["task_id"].(string)
		if issueID == "" {
			return
		}

		issue, err := queries.GetIssue(ctx, parseUUID(issueID))
		if err != nil {
			slog.Error("task:completed openclaw_weixin: failed to get issue", "issue_id", issueID, "error", err)
			return
		}

		// Determine the recipient: issue creator or assignee (member type)
		recipientID := ""
		if issue.AssigneeType.Valid && issue.AssigneeType.String == "member" && issue.AssigneeID.Valid {
			recipientID = util.UUIDToString(issue.AssigneeID)
		}
		if recipientID == "" {
			recipientID = util.UUIDToString(issue.CreatorID)
		}
		if recipientID == "" || recipientID == agentID {
			return
		}

		var task db.AgentTaskQueue
		taskLoaded := false
		if taskID != "" {
			if loadedTask, err := queries.GetAgentTask(ctx, parseUUID(taskID)); err == nil {
				task = loadedTask
				taskLoaded = true
			}
		}

		// Extract notification_summary: prefer event payload, fall back to task result
		notificationSummary, _ := payload["notification_summary"].(string)
		if notificationSummary == "" && taskLoaded && len(task.Result) > 0 {
			var taskResult map[string]any
			if err := json.Unmarshal(task.Result, &taskResult); err == nil {
				if s, ok := taskResult["notification_summary"].(string); ok {
					notificationSummary = s
				}
			}
		}

		// Anchor task notifications only to comments created by this task run.
		// Reusing the issue's historical last agent comment would make comment-level
		// IM dedup suppress future task notifications indefinitely.
		body, taskAgentCommentID := latestAgentCommentForTask(ctx, queries, e.WorkspaceID, issueID, agentID, task, taskLoaded)

		// Build notification context with comment anchor when available
		nCtx := buildNotificationContext(ctx, queries, e.WorkspaceID, issueID, taskAgentCommentID, "")

		// If no explicit summary from task payload, extract from body
		if notificationSummary == "" && body != "" {
			notificationSummary = ExtractSummary("", body, issue.Title, defaultIMSummaryMaxChars)
		}

		// Create a notification event for openclaw_weixin delivery
		payloadSnapshot, _ := json.Marshal(map[string]any{
			"type":             "task_completed",
			"severity":         "info",
			"title":            issue.Title,
			"summary":          notificationSummary,
			"body":             body,
			"link":             nCtx.Link,
			"issue_id":         issueID,
			"issue_identifier": nCtx.IssueIdentifier,
			"actor_type":       "agent",
			"actor_id":         agentID,
			"render_mode":      "auto",
			"details":          json.RawMessage(emptyDetails),
		})

		event, err := queries.CreateNotificationEvent(ctx, db.CreateNotificationEventParams{
			WorkspaceID:     parseUUID(e.WorkspaceID),
			RecipientUserID: parseUUID(recipientID),
			Type:            "task_completed",
			Severity:        "info",
			IssueID:         parseUUID(issueID),
			CommentID:       optionalUUID(taskAgentCommentID),
			ActorType:       util.StrToText("agent"),
			ActorID:         parseUUID(agentID),
			Title:           issue.Title,
			Body:            util.StrToText(body),
			Link:            util.StrToText(nCtx.Link),
			Details:         emptyDetails,
		})
		if err != nil {
			slog.Error("task:completed openclaw_weixin: failed to create notification event",
				"issue_id", issueID, "recipient_id", recipientID, "error", err)
			return
		}

		recordOpenclawWeixinDelivery(ctx, queries, recipientID, event, payloadSnapshot)
		recordDingTalkTaskDelivery(ctx, queries, recipientID, event, payloadSnapshot)
	})

	// task:failed — notify all subscribers except the agent
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		agentID, _ := payload["agent_id"].(string)
		issueID, _ := payload["issue_id"].(string)
		taskID, _ := payload["task_id"].(string)
		if issueID == "" {
			return
		}

		issue, err := queries.GetIssue(ctx, parseUUID(issueID))
		if err != nil {
			slog.Error("task:failed notification: failed to get issue", "issue_id", issueID, "error", err)
			return
		}

		// Extract failure summary: prefer event payload, then task error, then task result
		failureSummary, _ := payload["notification_summary"].(string)
		if failureSummary == "" && taskID != "" {
			if task, err := queries.GetAgentTask(ctx, parseUUID(taskID)); err == nil {
				if task.Error.Valid && task.Error.String != "" {
					failureSummary = ExtractSummary("", task.Error.String, "", defaultIMSummaryMaxChars)
				}
				if failureSummary == "" && len(task.Result) > 0 {
					var taskResult map[string]any
					if err := json.Unmarshal(task.Result, &taskResult); err == nil {
						if s, ok := taskResult["notification_summary"].(string); ok {
							failureSummary = s
						}
					}
				}
			}
		}

		exclude := map[string]bool{}
		if agentID != "" {
			exclude[agentID] = true
		}

		// Determine recipient for IM delivery (same as task_completed)
		recipientID := ""
		if issue.AssigneeType.Valid && issue.AssigneeType.String == "member" && issue.AssigneeID.Valid {
			recipientID = util.UUIDToString(issue.AssigneeID)
		}
		if recipientID == "" {
			recipientID = util.UUIDToString(issue.CreatorID)
		}

		var task db.AgentTaskQueue
		taskLoaded := false
		if taskID != "" {
			if loadedTask, err := queries.GetAgentTask(ctx, parseUUID(taskID)); err == nil {
				task = loadedTask
				taskLoaded = true
			}
		}

		// Find last agent comment for comment anchor link (needed by both
		// notifySubscribers for dedup and the explicit IM delivery below), but
		// only if this task actually created a new agent comment.
		_, taskAgentCommentID := latestAgentCommentForTask(ctx, queries, e.WorkspaceID, issueID, agentID, task, taskLoaded)

		notifiedSubscribers := notifySubscribers(ctx, queries, bus, issueID, issue.Status, e.WorkspaceID,
			events.Event{
				Type:        e.Type,
				WorkspaceID: e.WorkspaceID,
				ActorType:   "agent",
				ActorID:     agentID,
			},
			exclude, "task_failed", "action_required",
			issue.Title, failureSummary,
			taskAgentCommentID,
			emptyDetails)

		// Deliver to openclaw_weixin and dingtalk for task_failed (same as task_completed).
		// If the recipient was already notified via the subscriber path, skip only
		// the explicit openclaw_weixin delivery; dingtalk task preferences are
		// evaluated only on this explicit task event.
		if recipientID != "" && recipientID != agentID {

			nCtx := buildNotificationContext(ctx, queries, e.WorkspaceID, issueID, taskAgentCommentID, "")
			payloadSnapshot, _ := json.Marshal(map[string]any{
				"type":             "task_failed",
				"severity":         "action_required",
				"title":            issue.Title,
				"summary":          failureSummary,
				"body":             failureSummary,
				"link":             nCtx.Link,
				"issue_id":         issueID,
				"issue_identifier": nCtx.IssueIdentifier,
				"actor_type":       "agent",
				"actor_id":         agentID,
				"render_mode":      "auto",
				"details":          json.RawMessage(emptyDetails),
			})

			event, err := queries.CreateNotificationEvent(ctx, db.CreateNotificationEventParams{
				WorkspaceID:     parseUUID(e.WorkspaceID),
				RecipientUserID: parseUUID(recipientID),
				Type:            "task_failed",
				Severity:        "action_required",
				IssueID:         parseUUID(issueID),
				CommentID:       optionalUUID(taskAgentCommentID),
				ActorType:       util.StrToText("agent"),
				ActorID:         parseUUID(agentID),
				Title:           issue.Title,
				Body:            util.StrToText(failureSummary),
				Link:            util.StrToText(nCtx.Link),
				Details:         emptyDetails,
			})
			if err != nil {
				slog.Error("task:failed: failed to create notification event for IM delivery",
					"issue_id", issueID, "recipient_id", recipientID, "error", err)
			} else {
				if !notifiedSubscribers[recipientID] {
					recordOpenclawWeixinDelivery(ctx, queries, recipientID, event, payloadSnapshot)
				}
				recordDingTalkTaskDelivery(ctx, queries, recipientID, event, payloadSnapshot)
			}
		}
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
