package lark

import (
	"encoding/json"
	"strings"
)

// flattenContent renders a Lark message's body.content — the raw,
// JSON-encoded string Lark double-encodes — into plain text, dispatching
// on msg_type. It is the shared structural step used by BOTH ingress
// paths:
//
//   - the inbound decoder, for the user's own text / post message, and
//   - the enricher, for the quoted-reply parent and merge_forward child
//     messages it pulls back over the IM REST API.
//
// Mention placeholders (@_user_N) are preserved verbatim; the caller is
// responsible for resolving them against the message's mentions[] array
// via resolveMentions. The two ingress shapes (WS receive event vs IM
// REST item) carry the mentions array differently — only the caller
// knows which one applies — so flattening stays mention-agnostic.
//
// Non-text media types render as a stable bracketed placeholder so the
// agent sees that *something* was attached without us downloading the
// binary. Attachment ingestion is explicitly out of scope (tracked as a
// separate attachment-pipeline issue), and merge_forward is intercepted
// by the enricher before it reaches here (expanding it needs an HTTP
// round-trip); the inline placeholder is only a fallback for a forward
// nested inside another forward.
func flattenContent(msgType, rawContent string) string {
	switch msgType {
	case "text":
		return extractTextBody(rawContent)
	case "post":
		return flattenPostContent(rawContent)
	case "image":
		return "[Image]"
	case "file":
		return "[File]"
	case "audio":
		return "[Audio]"
	case "media":
		return "[Video]"
	case "sticker":
		return "[Sticker]"
	case "interactive":
		return "[interactive card]"
	case "share_chat":
		return "[Shared Chat]"
	case "share_user":
		return "[Shared User Card]"
	case "system":
		return "[System Message]"
	case "merge_forward":
		return "[forwarded messages]"
	default:
		return ""
	}
}

// larkPostContent mirrors the RECEIVE-side shape of a `post` rich-text
// body.content. Crucially this is NOT the locale-wrapped form the SEND
// API takes ({"zh_cn": {...}}): an inbound post body.content unmarshals
// directly into {title, content}. content is a 2-D array — the outer
// array is the ordered list of paragraphs, each inner array the ordered
// spans of that paragraph; the newline between paragraphs is implicit in
// the array boundary, not a span.
type larkPostContent struct {
	Title   string           `json:"title"`
	Content [][]larkPostSpan `json:"content"`
}

// larkPostSpan is one node inside a post paragraph. Only the fields that
// carry renderable text are modelled; the tag set is extensible, so the
// flattener emits `text` for any unrecognized tag and skips it otherwise
// rather than failing.
type larkPostSpan struct {
	Tag      string `json:"tag"`
	Text     string `json:"text"`
	Href     string `json:"href"`
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
}

// flattenPostContent flattens a received `post` body.content into plain
// text: the title (when present) on its own first line, then one line
// per paragraph. Within a paragraph spans are joined with a single space
// — this matches Lark's own rendering, where logically separate chunks
// ("Lark 集成", then a link "PR #3277") read as space-separated words.
//
// A link span renders as "text (href)" so the URL survives into the
// agent's context; an `at` span renders as its @_user_N placeholder (or
// the inline user_name when Lark already resolved it) so a downstream
// resolveMentions pass can substitute the display name. Media spans
// degrade to the same bracketed placeholders flattenContent uses.
func flattenPostContent(raw string) string {
	if raw == "" {
		return ""
	}
	var doc larkPostContent
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return ""
	}

	var b strings.Builder
	write := func(line string) {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	if doc.Title != "" {
		write(doc.Title)
	}
	for _, para := range doc.Content {
		write(flattenPostParagraph(para))
	}
	return strings.TrimRight(b.String(), "\n")
}

func flattenPostParagraph(spans []larkPostSpan) string {
	parts := make([]string, 0, len(spans))
	for _, s := range spans {
		switch s.Tag {
		case "text", "code_block":
			if s.Text != "" {
				parts = append(parts, s.Text)
			}
		case "a":
			switch {
			case s.Text != "" && s.Href != "":
				parts = append(parts, s.Text+" ("+s.Href+")")
			case s.Text != "":
				parts = append(parts, s.Text)
			case s.Href != "":
				parts = append(parts, s.Href)
			}
		case "at":
			// Prefer an already-resolved display name; otherwise emit
			// the user_id, which on the receive side is the @_user_N
			// placeholder a later resolveMentions pass maps to a name.
			switch {
			case s.UserName != "":
				parts = append(parts, "@"+s.UserName)
			case s.UserID != "":
				parts = append(parts, s.UserID)
			}
		case "img":
			parts = append(parts, "[Image]")
		case "media":
			parts = append(parts, "[Video]")
		case "emotion":
			// emoji_type is an enum key (e.g. "SMILE"), not display
			// text — skip it rather than leak the key.
		case "hr":
			parts = append(parts, "---")
		default:
			if s.Text != "" {
				parts = append(parts, s.Text)
			}
		}
	}
	return strings.Join(parts, " ")
}
