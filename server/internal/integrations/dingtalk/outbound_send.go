package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// This file is the OUTBOUND send path shared by the EventChatDone subscriber
// (outbound.go) and the OutboundReplier (replier.go). It turns a reply body into
// one or more DingTalk Markdown messages using the installation's robot endpoints.

const (
	// sampleMarkdown is the documented Markdown template for both robot send
	// endpoints. Keep this aligned with DingTalk's endpoint contract instead of
	// inferring a template migration from received interactive-card payloads.
	msgKeyMarkdown = "sampleMarkdown"

	// p2p (1:1) proactive send; group send.
	pathSendP2P   = "/v1.0/robot/oToMessages/batchSend"
	pathSendGroup = "/v1.0/robot/groupMessages/send"
)

// sendTarget is the resolved DingTalk destination for a reply. ConversationType
// selects the endpoint; StaffID is the recipient for a 1:1 send; ConversationID
// is the group's
// openConversationId for a group send.
type sendTarget struct {
	ConversationType string
	ConversationID   string
	StaffID          string
	SourceMessageID  string
	QuoteText        string
}

// sender posts replies for one installation. The robotCode + credentials come
// from the installation; the shared Client owns the token cache and transport.
type sender struct {
	client    *Client
	robotCode string
	appKey    string
	appSecret string
}

// markdownParam is the msgParam payload for a sampleMarkdown message.
type markdownParam struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// send delivers one or more bounded Markdown messages. A proactive-send 401
// triggers one token refresh and retry.
func (s *sender) send(ctx context.Context, target sendTarget, text string) (string, error) {
	if text == "" {
		return "", nil
	}
	quote := ""
	if target.ConversationType != convTypeP2P {
		quote = target.QuoteText
	}
	chunks, err := replyMarkdownChunks(text, quote)
	if err != nil {
		return "", err
	}
	var lastKey string
	for _, chunk := range chunks {
		param, err := json.Marshal(markdownParam{Title: chunk.title, Text: chunk.text})
		if err != nil {
			return "", fmt.Errorf("marshal msgParam: %w", err)
		}
		lastKey, err = s.sendOne(ctx, target, string(param))
		if err != nil {
			return "", err
		}
	}
	return lastKey, nil
}

type replyMarkdownChunk struct{ text, title string }

// Budget serialized JSON before any provider call. Escapes can expand a body
// substantially; re-render smaller chunks to keep Markdown fences intact.
func replyMarkdownChunks(text, quote string) ([]replyMarkdownChunk, error) {
	for budget := markdownByteBudget; budget >= 512; budget /= 2 {
		chunks := replyMarkdownChunksWithBudget(text, quote, budget)
		fits := true
		for _, chunk := range chunks {
			payload, err := json.Marshal(markdownParam{Title: chunk.title, Text: chunk.text})
			if err != nil {
				return nil, fmt.Errorf("marshal Markdown payload: %w", err)
			}
			if len(payload) > markdownPayloadByteBudget {
				fits = false
				break
			}
		}
		if fits {
			return chunks, nil
		}
	}
	return nil, errors.New("dingtalk: Markdown metadata exceeds payload byte budget")
}

func replyMarkdownChunksWithBudget(text, quote string, byteBudget int) []replyMarkdownChunk {
	var chunks []replyMarkdownChunk
	if prefix := prependMarkdownQuote("", quote); prefix != "" {
		for _, chunk := range chunkMarkdownWithBudget(prefix, byteBudget) {
			chunks = append(chunks, replyMarkdownChunk{text: chunk, title: defaultMarkdownTitle})
		}
	}
	for i, body := range chunkMarkdownWithBudget(text, byteBudget) {
		// Derive the title from the answer, independently of the source quote.
		chunk := replyMarkdownChunk{text: body, title: markdownTitle(body)}
		if i == 0 {
			if last := len(chunks) - 1; last >= 0 && len(chunks[last].text)+len(body) <= byteBudget {
				chunk.text = chunks[last].text + body
				chunks[last] = chunk
				continue
			}
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

// prependMarkdownQuote renders the triggering message as the visual quote
// block supported by DingTalk Markdown. DingTalk's outbound robot message
// contract has no native reply/quote field, so the horizontal rule is part of
// the Markdown as well. Task replies supply the sealed input; immediate command
// feedback supplies the current callback text. Neither persists a quote copy.
func prependMarkdownQuote(text, quote string) string {
	quote = quotePreview(quote)
	if quote == "" {
		return text
	}
	var b strings.Builder
	b.WriteString("> ")
	// Plain newlines are Markdown soft breaks. Preserve source line breaks and
	// the separate ellipsis line with hard breaks in the outbound quote only.
	b.WriteString(strings.ReplaceAll(escapeMarkdownQuoteText(quote), "\n", "  \n> "))
	b.WriteString("\n\n---\n\n")
	b.WriteString(text)
	return b.String()
}

func escapeMarkdownQuoteText(text string) string {
	var b strings.Builder
	start := 0
	for _, span := range webURLSpans(text) {
		b.WriteString(escapeMarkdownText(text[start:span[0]]))
		b.WriteString(text[span[0]:span[1]])
		start = span[1]
	}
	b.WriteString(escapeMarkdownText(text[start:]))
	return b.String()
}

func escapeMarkdownText(text string) string {
	// Escaping brackets already prevents Markdown links. Leave parentheses
	// literal: DingTalk can include a trailing escape in its automatic URL link.
	return strings.NewReplacer(
		`\`, `\\`, "`", "\\`", "*", "\\*", "_", "\\_",
		"[", "\\[", "]", "\\]",
		"#", "\\#", "+", "\\+", "-", "\\-", "!", "\\!",
		">", "\\>", "|", "\\|",
	).Replace(text)
}

// sendOne posts a single rendered message, refreshing the token once on 401.
func (s *sender) sendOne(ctx context.Context, target sendTarget, msgParam string) (string, error) {
	path, body, err := s.request(target, msgParam)
	if err != nil {
		return "", err
	}
	var resp struct {
		ProcessQueryKey string `json:"processQueryKey"`
	}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := s.client.accessToken(ctx, s.appKey, s.appSecret)
		if err != nil {
			return "", fmt.Errorf("access token: %w", err)
		}
		err = s.client.postJSON(ctx, path, token, body, &resp)
		if err == nil {
			return resp.ProcessQueryKey, nil
		}
		if errors.Is(err, errUnauthorized) && attempt == 0 {
			s.client.invalidate(s.appKey)
			continue
		}
		return "", err
	}
	return "", errUnauthorized
}

// request builds the endpoint + body for a target. A 1:1 send needs a recipient
// staff id; a group send needs the group's openConversationId.
func (s *sender) request(target sendTarget, msgParam string) (string, map[string]any, error) {
	if target.ConversationType == convTypeP2P {
		if target.StaffID == "" {
			return "", nil, errors.New("dingtalk: 1:1 send missing recipient staff id")
		}
		return pathSendP2P, map[string]any{
			"robotCode": s.robotCode,
			"userIds":   []string{target.StaffID},
			"msgKey":    msgKeyMarkdown,
			"msgParam":  msgParam,
		}, nil
	}
	if target.ConversationID == "" {
		return "", nil, errors.New("dingtalk: group send missing conversation id")
	}
	body := map[string]any{
		"robotCode":          s.robotCode,
		"openConversationId": target.ConversationID,
		"msgKey":             msgKeyMarkdown,
		"msgParam":           msgParam,
	}
	return pathSendGroup, body, nil
}
