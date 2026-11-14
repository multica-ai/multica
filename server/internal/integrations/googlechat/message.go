package googlechat

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	googleChatMessageRuneBudget = 4000
	notificationFieldRuneLimit  = 200
)

// NotificationItem is one inbox item rendered into an out-of-app message.
type NotificationItem struct {
	IssueIdentifier string
	Title           string
	Body            string
	IssueURL        string
	Type            string
	Actor           string
	Details         map[string]string
}

// EscapeText prevents user-authored text from becoming Google Chat markup.
func EscapeText(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	return strings.ReplaceAll(value, ">", "&gt;")
}

// FormatNotifications renders one Google Chat message with one line per item.
func FormatNotifications(workspaceName string, items []NotificationItem) string {
	if len(items) == 0 {
		return ""
	}

	var message strings.Builder
	if len(items) != 1 {
		fmt.Fprintf(&message, "*%d new notifications in ", len(items))
	} else {
		message.WriteString("*")
	}
	fmt.Fprintf(&message, "%s*", EscapeText(truncateCompactText(workspaceName)))

	for index, item := range items {
		line := formatNotificationLine(item)
		remainingAfter := len(items) - index - 1
		candidateRunes := utf8.RuneCountInString(message.String()) +
			utf8.RuneCountInString(line)
		if candidateRunes <= googleChatMessageRuneBudget {
			if remainingAfter == 0 ||
				candidateRunes+utf8.RuneCountInString(omittedItemsLine(remainingAfter)) <= googleChatMessageRuneBudget {
				message.WriteString(line)
				continue
			}
		}
		message.WriteString(omittedItemsLine(len(items) - index))
		break
	}
	return message.String()
}

func formatNotificationLine(item NotificationItem) string {
	label := EscapeText(truncateCompactText(itemLabel(item)))
	if item.IssueURL != "" {
		label = "<" + item.IssueURL + "|" + label + ">"
	}
	line := "\n• " + label
	if action := notificationAction(item); action != "" {
		line += " — " + EscapeText(truncateCompactText(action))
	}
	if body := truncateCompactText(item.Body); body != "" {
		line += " — " + EscapeText(body)
	}
	return line
}

func notificationAction(item NotificationItem) string {
	actor := item.Actor
	if actor == "" {
		actor = "Someone"
	}
	switch item.Type {
	case "issue_assigned":
		return actor + " assigned you"
	case "unassigned":
		return actor + " unassigned you"
	case "assignee_changed":
		return actor + " changed the assignee"
	case "mentioned":
		return actor + " mentioned you"
	case "new_comment":
		return actor + " commented"
	case "issue_created":
		return actor + " created an issue"
	case "issue_subscribed":
		return actor + " subscribed you"
	case "review_requested":
		return actor + " requested your review"
	case "status_changed", "priority_changed", "start_date_changed", "due_date_changed":
		field := strings.TrimSuffix(strings.ReplaceAll(item.Type, "_", " "), " changed")
		from, to := item.Details["from"], item.Details["to"]
		if from == "" {
			from = "unset"
		}
		if to == "" {
			to = "unset"
		}
		return fmt.Sprintf("%s changed %s: %s → %s", actor, field, from, to)
	case "reaction_added":
		return actor + " reacted " + item.Details["emoji"]
	case "task_failed", "quick_create_failed":
		return actor + "'s run failed"
	case "task_completed", "agent_completed", "quick_create_done":
		return actor + " completed the run"
	case "agent_blocked":
		return actor + " is blocked"
	case "app_action_requested", "app_action_reminder":
		return actor + " needs your approval"
	case "app_connect_requested":
		return actor + " needs you to connect an app"
	case "children_done":
		return "All sub-issues are done"
	case "quick_create_unconfirmed":
		return "Issue creation could not be confirmed"
	case "autopilot_paused":
		return "Autopilot paused"
	case "autopilot_quota_exceeded":
		return "Autopilot quota exceeded"
	case "app_action_result":
		return actor + " reported an app action result"
	default:
		return strings.ReplaceAll(item.Type, "_", " ")
	}
}

func omittedItemsLine(count int) string {
	if count <= 0 {
		return ""
	}
	return fmt.Sprintf("\n…and %d more in Multica", count)
}

func truncateCompactText(value string) string {
	value = compactText(value)
	runes := []rune(value)
	if len(runes) <= notificationFieldRuneLimit {
		return value
	}
	return string(runes[:notificationFieldRuneLimit-1]) + "…"
}

// FormatConfirmation is sent before a newly supplied webhook is stored.
func FormatConfirmation(workspaceName string) string {
	return fmt.Sprintf(
		"Multica notifications are connected for *%s*.",
		EscapeText(truncateCompactText(workspaceName)),
	)
}

func itemLabel(item NotificationItem) string {
	switch {
	case item.IssueIdentifier != "" && item.Title != "":
		return item.IssueIdentifier + ": " + item.Title
	case item.IssueIdentifier != "":
		return item.IssueIdentifier
	case item.Title != "":
		return item.Title
	default:
		return "New inbox notification"
	}
}

func compactText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
