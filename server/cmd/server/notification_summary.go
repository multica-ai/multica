package main

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// RenderMode controls how a notification is rendered for a given channel.
type RenderMode string

const (
	RenderModeAuto    RenderMode = "auto"
	RenderModeCompact RenderMode = "compact"
	RenderModeDetail  RenderMode = "detail"
)

// defaultIMSummaryMaxChars is the maximum rune count for a compact IM summary.
const defaultIMSummaryMaxChars = 80

// eventTypeLabels maps internal event types to Chinese UI labels for IM notifications.
var eventTypeLabels = map[string]string{
	"task_completed": "任务完成",
	"task_failed":    "任务失败",
	"mentioned":      "被@",
	"new_comment":    "新回复",
	"replied":        "新回复",
}

// EventTypeLabel returns the Chinese label for a notification event type.
func EventTypeLabel(eventType string) string {
	if label, ok := eventTypeLabels[eventType]; ok {
		return label
	}
	return eventType
}

// --- Summary extractor (deterministic, no LLM) ---

var (
	// Patterns to match conclusion-like first lines
	conclusionPrefixes = []string{
		"结论：", "结论:", "验收结论：", "验收结论:",
		"实现完成", "任务完成", "BLOCKED:", "FAIL:",
		"## 实现完成", "## 验收结论", "## 结论",
	}

	// Markdown patterns to detect complex content
	codeBlockRe   = regexp.MustCompile("(?s)```.*?```")
	tableRowRe    = regexp.MustCompile(`\|.*\|.*\|`)
	headingRe     = regexp.MustCompile(`(?m)^#{1,6}\s`)
	mentionLinkRe = regexp.MustCompile(`\[@([^\]]+)\]\(mention://[^)]+\)`)
	issueLinkRe   = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)]+)\)`)
	imageLinkRe   = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)
	attachmentRe  = regexp.MustCompile(`(?m)^[-*]\s*(附件|Attachment|attachment|文件|File).*$`)
)

// ExtractSummary extracts a short, deterministic summary from notification body.
// Priority: explicit notification_summary > first conclusion line > first sentence > truncated title.
func ExtractSummary(notificationSummary, body, title string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = defaultIMSummaryMaxChars
	}

	// Priority 1: explicit notification_summary from task payload
	if s := strings.TrimSpace(notificationSummary); s != "" {
		return truncateSummary(cleanSummaryMarkdown(s), maxChars)
	}

	// Priority 2: extract from body
	if s := extractSummaryFromBody(body, maxChars); s != "" {
		return s
	}

	// Priority 3: use title
	if t := strings.TrimSpace(title); t != "" {
		return truncateSummary(t, maxChars)
	}

	// Priority 4: fallback
	return "查看详情"
}

// extractSummaryFromBody tries to find a high-signal conclusion line from the body.
func extractSummaryFromBody(body string, maxChars int) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}

	// Clean markdown artifacts first
	cleaned := cleanSummaryMarkdown(body)
	lines := strings.Split(cleaned, "\n")

	// Try to find a conclusion line
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Strip heading markers for matching
		normalized := trimmed
		for strings.HasPrefix(normalized, "#") {
			normalized = strings.TrimPrefix(normalized, "#")
		}
		normalized = strings.TrimSpace(normalized)

		for _, prefix := range conclusionPrefixes {
			// Also strip heading markers from prefix for comparison
			cleanPrefix := prefix
			for strings.HasPrefix(cleanPrefix, "#") {
				cleanPrefix = strings.TrimPrefix(cleanPrefix, "#")
			}
			cleanPrefix = strings.TrimSpace(cleanPrefix)

			if strings.HasPrefix(normalized, cleanPrefix) {
				result := strings.TrimPrefix(normalized, cleanPrefix)
				result = strings.TrimSpace(result)
				if result == "" || len([]rune(result)) < 3 {
					// Too short after stripping prefix; use the full normalized line
					result = normalized
				}
				return truncateSummary(result, maxChars)
			}
		}
		// Use first non-empty, non-heading, non-table, non-codeblock line as fallback
		if !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "|") && !strings.HasPrefix(trimmed, "```") {
			return truncateSummary(trimmed, maxChars)
		}
	}

	return ""
}

// cleanSummaryMarkdown strips markdown formatting not suitable for IM display.
func cleanSummaryMarkdown(text string) string {
	// Remove code blocks
	text = codeBlockRe.ReplaceAllString(text, "")
	// Remove image links
	text = imageLinkRe.ReplaceAllString(text, "")
	// Convert mention links to readable names
	text = mentionLinkRe.ReplaceAllString(text, "@$1")
	// Remove attachment lines
	text = attachmentRe.ReplaceAllString(text, "")
	// Collapse multiple whitespace/newlines
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}

// truncateSummary truncates text to maxChars runes without breaking markdown links.
func truncateSummary(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	// Replace newlines with spaces for single-line summary
	text = strings.ReplaceAll(text, "\n", " ")
	text = regexp.MustCompile(`\s{2,}`).ReplaceAllString(text, " ")

	if utf8.RuneCountInString(text) <= maxChars {
		return text
	}

	// Find a safe truncation point that doesn't break markdown links
	runes := []rune(text)
	if maxChars <= 3 {
		return string(runes[:maxChars])
	}

	// Try to find a clean break before maxChars-3 (for "...")
	candidate := string(runes[:maxChars-3])
	// If we're inside a markdown link, back up to before the link
	if openBracket := strings.LastIndex(candidate, "["); openBracket >= 0 {
		closeBracket := strings.LastIndex(candidate, "]")
		closeParen := strings.LastIndex(candidate, ")")
		if closeBracket < openBracket || closeParen < openBracket {
			// We're inside an incomplete link, truncate before it
			candidate = strings.TrimSpace(candidate[:openBracket])
			if candidate == "" {
				candidate = string(runes[:maxChars-3])
			}
		}
	}

	return candidate + "..."
}

// --- Auto mode detection ---

// ShouldRenderCompact determines if the notification should render in compact mode.
// Returns true if compact rendering is recommended for IM channels.
func ShouldRenderCompact(body string) bool {
	body = strings.TrimSpace(body)
	if body == "" {
		return true
	}

	runeCount := utf8.RuneCountInString(body)
	lineCount := strings.Count(body, "\n") + 1

	// Body exceeds 160 Chinese characters → compact
	if runeCount > 160 {
		return true
	}

	// Body exceeds 4 lines → compact
	if lineCount > 4 {
		return true
	}

	// Contains code blocks → compact
	if codeBlockRe.MatchString(body) {
		return true
	}

	// Contains table rows → compact
	if tableRowRe.MatchString(body) {
		return true
	}

	// Contains multiple headings → compact
	headings := headingRe.FindAllString(body, -1)
	if len(headings) >= 2 {
		return true
	}

	// Over 500 characters force compact
	if runeCount > 500 {
		return true
	}

	return false
}

// ResolveRenderMode resolves the effective render mode for a delivery.
// userPref is the user's configured preference (may be empty → defaults to "auto").
// channelDefault is the channel's default when user has no preference.
func ResolveRenderMode(userPref, channelDefault RenderMode, body string) RenderMode {
	mode := userPref
	if mode == "" {
		mode = channelDefault
	}
	switch mode {
	case RenderModeCompact:
		return RenderModeCompact
	case RenderModeDetail:
		return RenderModeDetail
	default: // auto
		if ShouldRenderCompact(body) {
			return RenderModeCompact
		}
		return RenderModeDetail
	}
}

// --- Compact renderer for IM channels ---

// BuildCompactIMNotification builds a single-line compact notification for IM channels.
// Format: [{事件类型}] {actor_name} [{issue_identifier}]({issue_link}): {summary}
func BuildCompactIMNotification(eventType, actorName, issueIdentifier, issueLink, summary string) string {
	label := EventTypeLabel(eventType)
	actorName = strings.TrimSpace(actorName)
	issueIdentifier = strings.TrimSpace(issueIdentifier)
	issueLink = strings.TrimSpace(issueLink)
	summary = strings.TrimSpace(summary)

	var parts []string
	parts = append(parts, "["+label+"]")

	if actorName != "" {
		parts = append(parts, actorName)
	}

	if issueIdentifier != "" && issueLink != "" {
		parts = append(parts, "["+issueIdentifier+"]("+issueLink+")")
	} else if issueIdentifier != "" {
		parts = append(parts, issueIdentifier)
	}

	prefix := strings.Join(parts, " ")

	if summary != "" {
		return prefix + ": " + summary
	}
	return prefix
}
