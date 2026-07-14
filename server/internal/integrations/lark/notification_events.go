package lark

import "fmt"

var supportedNotificationEventTypes = [...]string{
	"issue_assigned", "unassigned", "assignee_changed",
	"mentioned", "new_comment", "reaction_added",
	"status_changed", "priority_changed", "start_date_changed", "due_date_changed",
	"task_failed",
	"quick_create_failed", "quick_create_done",
	"autopilot_paused", "issue_subscribed",
}

var defaultNotificationEventTypes = [...]string{
	"issue_assigned", "mentioned", "task_failed", "quick_create_failed", "autopilot_paused",
}

// SupportedNotificationEvents returns a copy of the ordered event catalog.
func SupportedNotificationEvents() []string {
	return append([]string{}, supportedNotificationEventTypes[:]...)
}

// DefaultNotificationEvents returns a copy of the default event selection.
func DefaultNotificationEvents() []string {
	return append([]string{}, defaultNotificationEventTypes[:]...)
}

// ValidateNotificationEventTypes validates, deduplicates, and orders event types.
func ValidateNotificationEventTypes(input []string) ([]string, error) {
	supported := make(map[string]struct{}, len(supportedNotificationEventTypes))
	for _, eventType := range supportedNotificationEventTypes {
		supported[eventType] = struct{}{}
	}

	selected := make(map[string]struct{}, len(input))
	for _, eventType := range input {
		if _, ok := supported[eventType]; !ok {
			return nil, fmt.Errorf("unsupported notification event type %q", eventType)
		}
		selected[eventType] = struct{}{}
	}

	result := make([]string, 0, len(selected))
	for _, eventType := range supportedNotificationEventTypes {
		if _, ok := selected[eventType]; ok {
			result = append(result, eventType)
		}
	}
	return result, nil
}
