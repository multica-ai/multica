package lark

// inbox_message.go — the card body the Lark bot pushes on inbox:new. Kept
// in its own file so the outbound Patcher stays focused on delivery and this
// module owns the wording + link building. Mirrors the WeCom smart-bot inbox
// path (internal/integrations/wecom/inbox_message.go); the presentation
// differs deliberately: the notification lands in the same conversation as
// the agent's chat replies, which are schema-2.0 markdown cards carrying
// their full body — so this card renders the body verbatim too, instead of
// WeCom's flattened plain text. A squeezed excerpt next to fully-rendered
// reply cards reads as a different, worse product.

import (
	"net/url"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	// inboxTitleMaxRunes bounds the header line; titles are one line by
	// nature and 120 runes is beyond any real one.
	inboxTitleMaxRunes = 120
	// inboxBodyMaxRunes is a safety valve, not a presentation choice: the
	// body ships verbatim, and only a pathological one is cut — on a line
	// boundary, with the full text still one click away.
	inboxBodyMaxRunes = 3000
)

// inboxTypeLabels are the Chinese display names used in the notification
// preamble. Kept locally so lark does not reach into cmd/server for it;
// the lists agree with wecom's by convention.
var inboxTypeLabels = map[string]string{
	"issue_assigned":     "任务指派",
	"mentioned":          "提及你",
	"status_changed":     "状态变更",
	"comment_added":      "新评论",
	"new_comment":        "新评论",
	"reaction_added":     "表情反应",
	"task_failed":        "任务失败",
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

// inboxAppURL resolves the frontend origin used to build the detail link.
// Priority: LARK_APP_URL → MULTICA_APP_URL → FRONTEND_ORIGIN.
//
// Unlike the WeCom path this accepts http:// as well. That guard exists
// there to keep a misconfigured env from putting an http link in front of a
// user; here it would instead silence the link for every self-hosted
// deployment served over plain http on an internal network, which is the
// common shape for this integration. The value is operator-set, never
// member-authored, and the message says where it points.
func inboxAppURL() string {
	for _, name := range []string{"LARK_APP_URL", "MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
		v := strings.TrimSpace(os.Getenv(name))
		if v == "" {
			continue
		}
		if !strings.HasPrefix(v, "https://") && !strings.HasPrefix(v, "http://") {
			continue
		}
		return strings.TrimRight(v, "/")
	}
	return ""
}

// buildInboxCard builds the markdown card body and the chat-list summary
// line from an inbox_item map. Card shape:
//
//	**【{type}】{title}**
//	{body, verbatim markdown}
//	[查看详情]({appURL}/{slug|workspaceID}/inbox?issue={issueID})
//
// The body renders exactly as the agent-reply cards in the same chat do.
// The link line is omitted entirely when no appURL is configured — better a
// link-less card than a broken link. The summary is what Lark shows in the
// chat list and the OS notification banner.
func buildInboxCard(item map[string]any, workspaceID, slug string) (markdown, summary string) {
	parts, ok := buildInboxParts(item, workspaceID, slug)
	if !ok {
		return "", ""
	}
	return composeInboxCard(parts.header, parts.body, nil, parts.link), parts.summary
}

// inboxCardParts are the composable pieces of one notification; a
// standalone card is header + body + link, and the blocks parameter of
// composeInboxCard leaves room for stitching follow-up events into the
// same card.
type inboxCardParts struct {
	label   string
	header  string // **【label】title**
	body    string
	link    string
	summary string
}

func buildInboxParts(item map[string]any, workspaceID, slug string) (inboxCardParts, bool) {
	title, _ := item["title"].(string)
	typeStr, _ := item["type"].(string)
	if title == "" && typeStr == "" {
		return inboxCardParts{}, false
	}
	label := inboxTypeLabel(typeStr)
	// The title is the one member-authored field spliced into the bot's own
	// bold line, where "[x](url)" would render as a link the bot appears to
	// vouch for. Separating "](" is sufficient — markdown only forms a link
	// when the two are adjacent. The body is NOT rewritten: it renders
	// verbatim like the agent-reply cards beside it, links included.
	title = breakLinkAdjacency(title)
	title = truncateRunesEllipsis(title, inboxTitleMaxRunes)
	return inboxCardParts{
		label:   label,
		header:  "**【" + label + "】" + title + "**",
		body:    clampBody(strings.TrimSpace(inboxItemBody(item))),
		link:    inboxItemLink(item, workspaceID, slug),
		summary: "【" + label + "】" + title,
	}, true
}

// composeInboxCard renders the card markdown: the first event's header and
// body, any follow-up blocks separated by rules, and one 查看详情
// line at the bottom.
// Every seam is a blank line: markdown folds a single newline into the
// previous paragraph, and a line directly above "---" turns into a setext
// heading — the title-and-description-as-one-giant-bold-block bug.
func composeInboxCard(header, body string, blocks []string, link string) string {
	var b strings.Builder
	b.WriteString(header)
	if body != "" {
		b.WriteString("\n\n")
		b.WriteString(body)
	}
	for _, blk := range blocks {
		b.WriteString("\n\n---\n\n")
		b.WriteString(blk)
	}
	if link != "" {
		b.WriteString("\n\n[查看详情](")
		b.WriteString(link)
		b.WriteString(")")
	}
	return b.String()
}

// clampBody cuts a pathologically long body on a line boundary and closes
// an unbalanced code fence so the cut cannot swallow the rest of the card
// into a code block. Anything cut is still one click away.
func clampBody(s string) string {
	if utf8.RuneCountInString(s) <= inboxBodyMaxRunes {
		return s
	}
	runes := []rune(s)
	cut := string(runes[:inboxBodyMaxRunes])
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	if strings.Count(cut, "```")%2 == 1 {
		cut += "\n```"
	}
	return cut + "\n……(已截断,完整内容见下方链接)"
}

// breakLinkAdjacency inserts a space between "](" pairs so member-authored
// text cannot form a markdown link inside the bot's own header line.
// CommonMark requires the link text to be followed immediately by "(" —
// separation is enough, and every character stays visible.
func breakLinkAdjacency(s string) string {
	return strings.ReplaceAll(s, "](", "] (")
}

// inboxItemBody extracts the body string from an inbox_item map. Body may
// arrive as *string (nil-able JSON field), string, or be missing.
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
// deep link. Returns "" when no appURL is configured — the caller drops the
// whole link line on that signal.
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
// have none and return "", which drops the query param.
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

// truncateRunesEllipsis trims s to at most maxRunes runes, appending "…"
// when anything was cut. Rune-based rather than byte-based so truncation
// never splits a Chinese character.
func truncateRunesEllipsis(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	i := 0
	for pos := range s {
		if i == maxRunes {
			return s[:pos] + "…"
		}
		i++
	}
	return s
}
