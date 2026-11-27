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

// inboxMarkdownMaxLen is the budget this card is written to, in RUNES.
//
// It is not the platform's cap and never was. The documented cap on one
// aibot_send_msg markdown body is 20480 utf8 bytes — sendMsgContentLimit in
// ws_frame.go, sourced to
// https://developer.work.weixin.qq.com/document/path/101138 — and a body past
// it is refused whole with errcode 45002. This constant used to be justified
// by a "~4096 chars" figure with no source behind it, which is both a
// different number and a different unit: 4000 runes of Chinese is around
// 12000 bytes, comfortably inside the real ceiling.
//
// So what this bounds is the card's length as a product choice, not as a
// protocol limit. It stays where it is because an inbox card is a pointer to
// the item and not the item, and truncating it here keeps the whole card —
// prefix, body and the "view detail" link — inside the frame with room to
// spare. Raising it would be a deliberate decision about how long a
// notification should be, and the ceiling that decision has to respect is
// sendMsgContentLimit measured in bytes, not this number.
const inboxMarkdownMaxLen = 4000

// The notification type labels and the deep link's anchor text live in the
// copy pack (strings.go), read through copyPack.label / InboxDetailLink. The
// pack's zh-Hans labels are the same table this file used to hold, character
// for character; the card is a 1:1 push to a named Multica member, so it is
// their own profile language that picks the pack (outbound.go).

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
//	[{detail link}]({appURL}/{slug|workspaceID}/inbox?issue={issueID})
//
// The type label and the link's anchor text come from the reader's copyPack.
// The link segment is omitted entirely when no appURL is configured — we
// would rather send a title-only card than a broken link.
func buildInboxMarkdown(item map[string]any, workspaceID, slug string, c copyPack) string {
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
	b.WriteString(c.label(typeStr))
	b.WriteString("] ")
	b.WriteString(title)
	b.WriteString("**")
	if body != "" {
		b.WriteString("\n")
		b.WriteString(body)
	}
	if link != "" {
		b.WriteString("\n[" + c.InboxDetailLink + "](")
		b.WriteString(link)
		b.WriteString(")")
	}
	result := b.String()
	if utf8.RuneCountInString(result) <= inboxMarkdownMaxLen {
		return result
	}
	// Truncate the body only. Prefix + link must survive intact so the
	// user still gets the "view detail" affordance.
	prefix := "**[" + c.label(typeStr) + "] " + title + "**"
	suffix := ""
	if link != "" {
		suffix = "\n[" + c.InboxDetailLink + "](" + link + ")"
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
	return "**[" + c.label(typeStr) + "] " +
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
