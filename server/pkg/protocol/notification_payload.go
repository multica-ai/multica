package protocol

// NotificationPushPayload is the WebSocket frame the NotificationDispatcher
// pushes to member users via Broadcaster.SendToUser. It mirrors the shape
// defined in the notification design (OXY-583 §6.3).
type NotificationPushPayload struct {
	// Type is the notification event type (e.g. "notification:issue_done").
	Type string `json:"type"`

	// Sound is the suggested sound type for the frontend to play.
	// Empty string means no sound. Valid values: "complete", "blocked",
	// "attention", "action_required".
	Sound string `json:"sound,omitempty"`

	// ShowNotification indicates whether the frontend should display a
	// native OS notification banner for this event.
	ShowNotification bool `json:"show_notification,omitempty"`

	// InboxItem contains the server-created inbox item (nil when the
	// user's preferences suppress inbox creation).
	InboxItem *NotificationInbox `json:"inbox_item,omitempty"`

	// Data carries the scene-specific fields (issue details, parent/child
	// ids, blocked reason, etc).
	Data NotificationData `json:"data"`
}

// NotificationInbox is the inbox item embedded in the push payload.
type NotificationInbox struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Body     string `json:"body,omitempty"`
	Severity string `json:"severity"` // "action_required" | "attention" | "info"
}

// NotificationData carries scene-specific fields for each notification type.
type NotificationData struct {
	IssueID         string `json:"issue_id"`
	IssueIdentifier string `json:"issue_identifier"`
	IssueTitle      string `json:"issue_title"`
	WorkspaceSlug   string `json:"workspace_slug"`

	// Scene-specific fields (zero-value omitted by omitempty)
	ParentID     string `json:"parent_id,omitempty"`
	ChildID      string `json:"child_id,omitempty"`
	BlockedReason string `json:"blocked_reason,omitempty"`
	WaitingOn    string `json:"waiting_on,omitempty"`
	Stage        int32  `json:"stage,omitempty"`
}

// NotificationSeverity maps each notification event type to a display severity.
func NotificationSeverity(eventType string) string {
	switch eventType {
	case EventNotificationIssueBlocked,
		EventNotificationChildBlocked,
		EventNotificationInReview,
		EventNotificationMentionDecision,
		EventNotificationTaskFailed:
		return "action_required"
	case EventNotificationBlockedTimeout:
		return "attention"
	case EventNotificationIssueDone,
		EventNotificationParentChainDone,
		EventNotificationStageClosed:
		return "info"
	default:
		return "info"
	}
}

// NotificationSound returns the suggested sound type for an event type.
func NotificationSound(eventType string) string {
	switch eventType {
	case EventNotificationIssueDone,
		EventNotificationParentChainDone,
		EventNotificationStageClosed:
		return "complete"
	case EventNotificationIssueBlocked,
		EventNotificationChildBlocked,
		EventNotificationBlockedTimeout:
		return "blocked"
	case EventNotificationInReview,
		EventNotificationMentionDecision,
		EventNotificationTaskFailed:
		return "action_required"
	default:
		return ""
	}
}

// NotificationPreferenceKey returns the preference key a user would toggle
// to control notifications for the given event type. Empty string means
// the event type has no preference toggle (always delivered).
func NotificationPreferenceKey(eventType string) string {
	switch eventType {
	case EventNotificationIssueDone:
		return "notify_issue_done"
	case EventNotificationChildBlocked:
		return "notify_child_blocked"
	case EventNotificationIssueBlocked:
		return "notify_issue_blocked"
	case EventNotificationInReview:
		return "notify_in_review"
	case EventNotificationParentChainDone:
		return "notify_parent_chain_done"
	case EventNotificationTaskFailed:
		return "notify_task_failed"
	case EventNotificationStageClosed:
		return "notify_child_blocked" // reuse child_blocked pref for stage_closed
	case EventNotificationBlockedTimeout:
		return "notify_issue_blocked" // reuse blocked pref for timeout variant
	case EventNotificationMentionDecision:
		return "notify_mention_decision"
	default:
		return ""
	}
}

// SoundPreferenceKey returns the sound-specific preference key for an event type.
func SoundPreferenceKey(eventType string) string {
	switch eventType {
	case EventNotificationIssueDone:
		return "sound_issue_done"
	case EventNotificationChildBlocked, EventNotificationStageClosed:
		return "sound_child_blocked"
	case EventNotificationIssueBlocked, EventNotificationBlockedTimeout:
		return "sound_blocked"
	case EventNotificationInReview:
		return "sound_in_review"
	case EventNotificationMentionDecision:
		return "sound_mention_decision"
	case EventNotificationTaskFailed:
		return "sound_task_failed"
	default:
		return ""
	}
}

// IsSoundEnabled checks whether the user has globally enabled sound and
// has not muted the specific sound key for this event type.
// prefs is the parsed notification_preference JSONB map.
func IsSoundEnabled(prefs map[string]string, eventType string) bool {
	// Global sound must be on.
	if v, ok := prefs["sound_enabled"]; ok && v == "false" {
		return false
	}
	// Check the per-scene sound key (default: enabled when absent).
	key := SoundPreferenceKey(eventType)
	if key == "" {
		return false
	}
	if v, ok := prefs[key]; ok && v == "muted" {
		return false
	}
	return true
}

// IsNotificationEnabled checks whether the user has not muted the given
// notification type. Returns true (default enabled) when no preference is set.
func IsNotificationEnabled(prefs map[string]string, eventType string) bool {
	key := NotificationPreferenceKey(eventType)
	if key == "" {
		return true // unconfigurable types are always delivered
	}
	if v, ok := prefs[key]; ok && v == "muted" {
		return false
	}
	return true
}
