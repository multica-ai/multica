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
// agent sees that *something* was attached without this fast path
// downloading the binary; the detached media resolver separately fetches
// the resource and binds it as a chat attachment, with the placeholder
// as the durable fallback. merge_forward is intercepted by the enricher
// before it reaches here (expanding it needs an HTTP round-trip); the
// inline placeholder is only a fallback for a forward nested inside
// another forward.
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
	case "media", "video":
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
// body.content after normalization. This is not the locale-wrapped form
// the SEND API takes ({"zh_cn": {...}}). Content is a 2-D array — the outer
// array is the ordered list of paragraphs, each inner array the ordered
// spans of that paragraph; the newline between paragraphs is implicit in
// the array boundary, not a span.
type larkPostContent struct {
	Title   string           `json:"title"`
	Content [][]larkPostSpan `json:"content"`
}

// decodePostContent normalizes the receive-side variants for both text and
// attachment discovery. Prefer legacy paragraphs when present; content_v2 may
// be a paragraph array or a JSON-encoded paragraph array.
func decodePostContent(raw string) (larkPostContent, error) {
	var payload struct {
		Title     string          `json:"title"`
		Content   json.RawMessage `json:"content"`
		ContentV2 json.RawMessage `json:"content_v2"`
		Files     []larkPostSpan  `json:"files"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return larkPostContent{}, err
	}
	doc := larkPostContent{Title: payload.Title}
	for _, content := range []json.RawMessage{payload.Content, payload.ContentV2} {
		var encoded string
		if json.Unmarshal(content, &encoded) == nil {
			content = json.RawMessage(encoded)
		}
		var paragraphs [][]larkPostSpan
		if json.Unmarshal(content, &paragraphs) == nil && len(paragraphs) > 0 {
			doc.Content = paragraphs
			break
		}
	}
	seen := make(map[string]*larkPostSpan)
	for _, para := range doc.Content {
		for i := range para {
			span := &para[i]
			if (span.Tag == "file" || span.Tag == "media") && !span.IsFolder && seen[span.FileKey] == nil {
				seen[span.FileKey] = span
			}
		}
	}
	for _, file := range payload.Files {
		if file.IsFolder || file.FileKey == "" {
			continue
		}
		if span := seen[file.FileKey]; span != nil {
			span.FileName = firstNonEmpty(span.FileName, span.Name, file.FileName, file.Name)
			span.MimeType = firstNonEmpty(span.MimeType, file.MimeType)
			continue
		}
		file.Tag = "file"
		para := []larkPostSpan{file}
		doc.Content = append(doc.Content, para)
		seen[file.FileKey] = &para[0]
	}
	return doc, nil
}

// larkPostSpan is one node inside a post paragraph. Only the fields that
// carry renderable text are modelled; the tag set is extensible, so the
// flattener emits `text` for any unrecognized tag and skips it otherwise
// rather than failing.
type larkPostSpan struct {
	IsFolder bool   `json:"is_folder"`
	Tag      string `json:"tag"`
	Text     string `json:"text"`
	Href     string `json:"href"`
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
	ImageKey string `json:"image_key"`
	FileKey  string `json:"file_key"`
	FileName string `json:"file_name"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
}

// flattenPostContent flattens a received `post` body.content into plain
// text: the title (when present) on its own first line, then one line
// per paragraph. Within a paragraph spans are joined with a single space
// — this matches Lark's own rendering, where logically separate chunks
// ("Lark 集成", then a link "PR #3277") read as space-separated words.
//
// A link span renders as "text (href)" so the URL survives into the
// agent's context; an `at` span renders as its @_user_N placeholder so
// a downstream resolveMentions pass can substitute the display name
// (falling back to the inline user_name when the placeholder is absent).
// Media spans degrade to the same bracketed placeholders flattenContent
// uses.
func flattenPostContent(raw string) string {
	if raw == "" {
		return ""
	}
	doc, err := decodePostContent(raw)
	if err != nil {
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
			// Prefer the @_user_N placeholder so a later
			// resolveMentions pass can map it to a display name and
			// strip the bot's own mention; fall back to the inline
			// user_name when the placeholder is absent.
			switch {
			case s.UserID != "":
				parts = append(parts, s.UserID)
			case s.UserName != "":
				parts = append(parts, "@"+s.UserName)
			}
		case "img":
			parts = append(parts, "[Image]")
		case "file":
			if s.IsFolder || s.FileKey == "" {
				continue
			}
			if name := firstNonEmpty(s.FileName, s.Name); name != "" {
				parts = append(parts, "[File: "+name+"]")
			} else {
				parts = append(parts, "[File]")
			}
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
