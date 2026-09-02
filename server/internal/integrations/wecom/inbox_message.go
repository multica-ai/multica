package wecom

// inbox_message.go — the markdown body wecom smart-bot pushes on inbox:new.
// Uses markdown so it renders cleanly through aibot_send_msg (which does not
// accept msgtype=text). Kept in a separate file so the outbound handler stays
// focused on delivery and this module owns the wording + link building.

import (
	"net/url"
	"os"
	"strings"
	"unicode/utf8"
)

// aibot markdown size cap. WeCom rejects the whole frame if we
// push more than ~4096 chars, so we truncate the body on that budget. We
// use 4000 to leave headroom for the prefix + link suffix.
const inboxMarkdownMaxLen = 4000

// inboxTypeLabels are the Chinese display names used in the notification
// preamble. Kept locally so wecom does not reach into cmd/server for it;
// the two lists agree by convention.
var inboxTypeLabels = map[string]string{
	"issue_assigned":     "任务指派",
	"mentioned":          "提及你",
	"status_changed":     "状态变更",
	"comment_added":      "新评论",
	"new_comment":        "新评论",
	"reaction_added":     "表情反应",
	"task_failed":        "task 失败",
	"unassigned":         "取消指派",
	"assignee_changed":   "指派人变更",
	"priority_changed":   "优先级变更",
	"due_date_changed":   "截止日期变更",
	"start_date_changed": "开始日期变更",
}

func inboxTypeLabel(t string) string {
	if label, ok := inboxTypeLabels[t]; ok {
		return label
	}
	return "新消息"
}

// inboxAppURL resolves the frontend URL for building the "view detail" link.
// Priority: WECOM_APP_URL → MULTICA_APP_URL → FRONTEND_ORIGIN. Only HTTPS
// values are accepted; a non-HTTPS override is silently dropped so a
// misconfigured env cannot leak an http:// URL into a user chat.
func inboxAppURL() string {
	for _, name := range []string{"WECOM_APP_URL", "MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
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

// buildInboxMarkdown builds the aibot-friendly markdown body from an
// inbox_item map. Format:
//
//	**[{type}] {title}**
//	{body}
//	[查看详情]({appURL}/{slug|workspaceID}/inbox?issue={issueID})
//
// The link segment is omitted entirely when no appURL is configured — we
// would rather send a title-only card than a broken link.
func buildInboxMarkdown(item map[string]any, workspaceID, slug string) string {
	title, _ := item["title"].(string)
	typeStr, _ := item["type"].(string)
	if title == "" && typeStr == "" {
		return ""
	}
	body := inboxItemBody(item)
	link := inboxItemLink(item, workspaceID, slug)

	// title and body are written by a member. They land inside an aibot
	// markdown card the recipient has every reason to trust — it comes from
	// the bot, not from the author — so link syntax in them must not render.
	// An issue titled "[click here](http://evil.example)" arrived as a
	// working link, and a body carrying "[重置密码]: https://evil.example"
	// plus "[重置密码]" arrived as one too.
	//
	// Per field rather than over the finished card, which is what the length
	// budget below needs — and it costs no coverage. Every line of member
	// text begins where the card's own line begins, bar the title's first,
	// and there the bot's "**[" prefix keeps the member out of the block
	// position a definition has to start in. The scan reads each field's
	// start as a line start anyway, so that one is guarded twice over rather
	// than not at all.
	//
	// Done here, before anything below measures a length: each break inserts
	// a space, and a body dense in "](" grows by half. Budget the grown text
	// or the card goes out over the cap, which WeCom refuses whole while
	// telling the sender it was delivered.
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
	// Truncate the body only. Prefix + link must survive intact so the
	// user still gets the "view detail" affordance.
	prefix := "**[" + inboxTypeLabel(typeStr) + "] " + title + "**"
	suffix := ""
	if link != "" {
		suffix = "\n[查看详情](" + link + ")"
	}
	// Neither cut below can put a "]" back next to a "(" or a ":".
	// truncateRunes only drops a suffix, so it cannot recreate a pair
	// breakMemberLinks already separated, and every literal spliced in after
	// member text here starts with ".", "*" or "\n" — never "(" or ":".
	// Nor can dropping a suffix complete a reference definition: it can only
	// cut the destination off one. Pinned by the seam cases in
	// TestInboxCardNeverPutsCloseBracketNextToOpenParen and
	// TestInboxCardDefinesNoResolvableLinkReference.
	room := inboxMarkdownMaxLen - utf8.RuneCountInString(prefix) - utf8.RuneCountInString(suffix) - 4 // "\n...\n"
	if room > 0 {
		return prefix + "\n" + truncateRunes(body, room) + "..." + suffix
	}
	// No room for any body means the prefix itself is the problem — a long
	// enough title pushes prefix+suffix past the cap on its own, and
	// returning it unchanged means WeCom refuses the whole frame while the
	// sender is told it was delivered. Truncate the title so what goes out
	// always fits.
	fixed := utf8.RuneCountInString(prefix) - utf8.RuneCountInString(title)
	titleRoom := inboxMarkdownMaxLen - fixed - utf8.RuneCountInString(suffix) - 3 // "..."
	if titleRoom < 0 {
		// Clamping here still returns fixed+3+len(suffix) runes with no cap
		// check, so in principle this branch can hand back a card over the
		// cap. Reaching it needs a link suffix past ~3987 runes, i.e. an
		// appURL plus workspace slug of that length; both are operator- and
		// DB-constrained, never member-authored. Left as-is deliberately:
		// the alternative is dropping the link to make room, and no input
		// that exists can ask for it.
		titleRoom = 0
	}
	return "**[" + inboxTypeLabel(typeStr) + "] " +
		truncateRunes(title, titleRoom) + "...**" + suffix
}

// inboxItemBody extracts the body/description string from an inbox_item map.
// Body may arrive as *string (nil-able JSON field), string, or missing.
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

// inboxItemLink builds the {appURL}/{slug|wsUUID}/inbox?issue={issueID}
// deep link. Returns "" when no appURL is configured — the caller uses that
// as a signal to drop the entire link segment.
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
	// Optional ?issue=... — chat-only inbox items have no issue.
	if issueID := inboxItemIssueID(item); issueID != "" {
		b.WriteString("?issue=")
		b.WriteString(url.QueryEscape(issueID))
	}
	return b.String()
}

// inboxItemIssueID extracts issue_id when present. Chat-type notifications
// have no issue_id and we return "" — the link then omits the query param.
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

// truncateRunes trims s to at most maxRunes runes. Rune-based rather than
// byte-based so the truncation never splits a Chinese character.
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
