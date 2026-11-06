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
	fmt.Fprintf(&message, "*%d new notification", len(items))
	if len(items) != 1 {
		message.WriteString("s")
	}
	fmt.Fprintf(&message, " in %s*", EscapeText(truncateCompactText(workspaceName)))

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
	if body := truncateCompactText(item.Body); body != "" {
		line += " — " + EscapeText(body)
	}
	return line
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
