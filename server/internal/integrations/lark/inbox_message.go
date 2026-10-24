package lark

// inbox_message.go — the card body the Lark patcher pushes on inbox:new.
// Kept separate from outbound.go so the delivery handler stays focused on
// binding lookup and this file owns the wording and link building.

import (
	"net/url"
	"os"
	"strings"
	"unicode/utf8"
)

// inboxNotifiableTypes are the notification types that reach a member in
// Lark. Everything else stays in the in-app inbox.
//
// The filter exists because a chat channel and an inbox have different
// noise budgets: the inbox is a place a member goes to catch up, so a
// comment on every issue they touch is useful there; a Lark push
// interrupts, and pushing new_comment would make the channel something
// people mute. What earns an interruption is work that lands on the
// member (issue_assigned), someone addressing them directly (mentioned),
// and an agent failing on their work (task_failed).
var inboxNotifiableTypes = map[string]bool{
	"issue_assigned": true,
	"mentioned":      true,
	"task_failed":    true,
}

// inboxNotifiable reports whether an item of this type is pushed to Lark.
func inboxNotifiable(t string) bool { return inboxNotifiableTypes[t] }

// inboxMarkdownMaxLen bounds the card as a product choice, not a protocol
// limit: an inbox card is a pointer to the item, not the item. Lark's own
// cap on one interactive card body is far larger, and the binding prompt
// template this mirrors is a single short paragraph.
const inboxMarkdownMaxLen = 1200

// inboxAppURL resolves the frontend URL for the "查看详情" link. Only HTTPS
// is accepted so a misconfigured env cannot put an http:// URL in a
// member's chat.
func inboxAppURL() string {
	for _, name := range []string{"LARK_APP_URL", "MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
		v := strings.TrimSpace(os.Getenv(name))
		if v == "" {
			continue
		}
		if !strings.HasPrefix(v, "https://") {
			continue
		}
		return strings.TrimRight(v, "/")
	}
	return ""
}

// inboxTypeLabel is the short Chinese label shown in front of the title.
// Unknown types fall back to the raw type string rather than dropping the
// notification: a new server-side type must still reach the member.
func inboxTypeLabel(t string) string {
	switch t {
	case "issue_assigned":
		return "任务指派"
	case "assignee_changed":
		return "负责人变更"
	case "new_comment":
		return "新评论"
	case "mentioned":
		return "有人提到你"
	case "status_changed":
		return "状态变更"
	case "task_failed":
		return "运行失败"
	case "unassigned":
		return "已取消指派"
	case "issue_subscribed":
		return "订阅更新"
	}
	return t
}

// buildInboxMarkdown renders:
//
//	**[{label}] {title}**
//	{body}
//	[查看详情]({appURL}/{slug|wsID}/inbox?issue={issueID})
//
// The link segment is dropped entirely when no appURL is configured — a
// title-only card beats a broken link.
func buildInboxMarkdown(item map[string]any, workspaceID, slug string) string {
	title, _ := item["title"].(string)
	typeStr, _ := item["type"].(string)
	if title == "" && typeStr == "" {
		return ""
	}
	body := inboxItemBody(item)
	link := inboxItemLink(item, workspaceID, slug)

	// Title and body are member-authored and land inside a card the
	// recipient has every reason to trust, so link syntax in them must not
	// render as a live link. Same defence as the WeCom card.
	title = breakMemberLinks(title)
	body = breakMemberLinks(body)

	var b strings.Builder
	b.WriteString("**[")
	b.WriteString(inboxTypeLabel(typeStr))
	b.WriteString("] ")
	b.WriteString(title)
	b.WriteString("**")
	if body != "" {
		b.WriteString("\n")
		b.WriteString(body)
	}
	if link != "" {
		b.WriteString("\n[查看详情](")
		b.WriteString(link)
		b.WriteString(")")
	}
	result := b.String()
	if utf8.RuneCountInString(result) <= inboxMarkdownMaxLen {
		return result
	}
	prefix := "**[" + inboxTypeLabel(typeStr) + "] " + title + "**"
	suffix := ""
	if link != "" {
		suffix = "\n[查看详情](" + link + ")"
	}
	room := inboxMarkdownMaxLen - utf8.RuneCountInString(prefix) - utf8.RuneCountInString(suffix) - 4
	if room > 0 {
		return prefix + "\n" + truncateRunes(body, room) + "..." + suffix
	}
	fixed := utf8.RuneCountInString(prefix) - utf8.RuneCountInString(title)
	titleRoom := inboxMarkdownMaxLen - fixed - utf8.RuneCountInString(suffix) - 3
	if titleRoom < 0 {
		titleRoom = 0
	}
	return "**[" + inboxTypeLabel(typeStr) + "] " +
		truncateRunes(title, titleRoom) + "...**" + suffix
}

// breakMemberLinks inserts a space between "]" and "(" / ":" so member
// text cannot form a markdown link or a link-reference definition.
func breakMemberLinks(s string) string {
	if !strings.ContainsAny(s, "]") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		b.WriteByte(s[i])
		if s[i] != ']' || i+1 >= len(s) {
			continue
		}
		if next := s[i+1]; next == '(' || next == ':' {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// inboxItemBody extracts body, which may arrive as *string, string or
// missing depending on the JSON encoder that produced the event.
func inboxItemBody(item map[string]any) string {
	switch v := item["body"].(type) {
	case *string:
		if v != nil {
			return *v
		}
	case string:
		return v
	}
	return ""
}

// inboxItemLink builds {appURL}/{slug|wsUUID}/inbox?issue={issueID}, or ""
// when no appURL is configured.
func inboxItemLink(item map[string]any, workspaceID, slug string) string {
	appURL := inboxAppURL()
	if appURL == "" {
		return ""
	}
	seg := slug
	if seg == "" {
		seg = workspaceID
	}
	var b strings.Builder
	b.WriteString(appURL)
	b.WriteString("/")
	b.WriteString(url.PathEscape(seg))
	b.WriteString("/inbox")
	if issueID := inboxItemIssueID(item); issueID != "" {
		b.WriteString("?issue=")
		b.WriteString(url.QueryEscape(issueID))
	}
	return b.String()
}

func inboxItemIssueID(item map[string]any) string {
	switch v := item["issue_id"].(type) {
	case *string:
		if v != nil {
			return *v
		}
	case string:
		return v
	}
	return ""
}

// truncateRunes trims s to at most maxRunes runes, never splitting a
// multi-byte character.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	i := 0
	for pos := range s {
		if i == maxRunes {
			return s[:pos]
		}
		i++
	}
	return s
}
