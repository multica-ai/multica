// Package card builds Feishu interactive card JSON payloads for outbound
// messages. It is a pure rendering layer — no I/O, no SDK imports — so the
// adapter's sendCard path stays testable and the Feishu card schema is
// contained in a single sub-package (DESIGN §8 T16).
//
// Feishu interactive cards use msg_type "interactive" with a JSON body
// following the Open Platform card schema:
//
//	{
//	  "header": { "title": { "tag": "plain_text", "content": "..." } },
//	  "elements": [ ... ]
//	}
//
// This package builds that JSON and returns it as a string ready to be
// passed as SendRequest.Content. Only the Feishu adapter should call this
// package; upstream channel runtime code passes platform-neutral title/body
// messages and never pre-renders provider JSON.
//
// Responsibilities:
//   - Define the (subset of the) Feishu interactive-card schema we use.
//   - Provide composable primitives (NewCard / AddMarkdown / AddPlainText /
//     AddButton / AddHR) and high-level templates (IssueCard / CommentCard
//     / BindPromptCard).
//   - Enforce the 200-rune truncation contract for user-supplied long
//     fields (Summary / Body) so callers cannot accidentally produce a
//     5KB payload that Feishu rejects.
//
// Boundaries:
//   - No I/O, no SDK imports, no logging — stays unit-testable in isolation.
//   - No knowledge of port DTOs — the adapter is responsible for mapping
//     port.OutboundCardMessage to a card.Render() result.
package card

import (
	"encoding/json"
	"unicode/utf8"
)

// defaultTitle is used when callers pass an empty titleText to NewCard.
// Empty plain_text content is undefined behaviour on Feishu's side; a
// neutral fallback keeps the card visible even if upstream data is missing.
const defaultTitle = "Multica 通知"

// summaryMaxRunes is the contractual upper bound on user-supplied long
// text fields rendered into a card body. 200 runes was chosen by DESIGN
// §8 T16 as a comfortable fit for Feishu's preview pane while staying
// well below the 5KB message-body cap.
const summaryMaxRunes = 200

// header is the card header block.
type header struct {
	Title    title  `json:"title"`
	Template string `json:"template,omitempty"`
}

// title is the header title element.
type title struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

// element is a generic card element. Only the fields actually used by this
// package are modelled — the Feishu card schema has many more element
// types (image, table, chart, …) that future tasks can add without
// breaking existing callers.
type element struct {
	Tag     string    `json:"tag"`
	Text    *textNode `json:"text,omitempty"`
	Content string    `json:"content,omitempty"`
	Actions []action  `json:"actions,omitempty"`
}

// textNode is a rich-text or plain-text node inside an element.
type textNode struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

// action is a button or other interactive element.
type action struct {
	Tag  string   `json:"tag"`
	Text textNode `json:"text"`
	Type string   `json:"type"`
	URL  string   `json:"url,omitempty"`
}

// Card is a Feishu interactive card ready to be serialised to JSON.
type Card struct {
	Header   header    `json:"header"`
	Elements []element `json:"elements"`
}

// Render marshals the card into the JSON string expected by Feishu's
// im.v1.messages.create API with msg_type "interactive".
func (c *Card) Render() (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// NewCard creates a Card with the given title and template colour.
// Template values: blue, wathet, turquoise, green, yellow, orange, red,
// carmine, violet, purple, indigo, grey. An empty titleText falls back to
// a neutral "Multica 通知" so the rendered card is never blank.
func NewCard(titleText, template string) *Card {
	if titleText == "" {
		titleText = defaultTitle
	}
	return &Card{
		Header: header{
			Title: title{
				Tag:     "plain_text",
				Content: titleText,
			},
			Template: template,
		},
	}
}

// AddMarkdown appends a markdown element to the card body.
func (c *Card) AddMarkdown(md string) {
	c.Elements = append(c.Elements, element{
		Tag: "div",
		Text: &textNode{
			Tag:     "lark_md",
			Content: md,
		},
	})
}

// AddPlainText appends a plain-text element to the card body.
func (c *Card) AddPlainText(text string) {
	c.Elements = append(c.Elements, element{
		Tag: "div",
		Text: &textNode{
			Tag:     "plain_text",
			Content: text,
		},
	})
}

// AddButton appends an action group with a single URL button.
func (c *Card) AddButton(label, url string) {
	c.Elements = append(c.Elements, element{
		Tag: "action",
		Actions: []action{
			{
				Tag: "button",
				Text: textNode{
					Tag:     "plain_text",
					Content: label,
				},
				Type: "primary",
				URL:  url,
			},
		},
	})
}

// AddHR appends a horizontal rule divider.
func (c *Card) AddHR() {
	c.Elements = append(c.Elements, element{Tag: "hr"})
}

// TruncateRunes returns s clipped to at most n runes (NOT bytes), so a
// Chinese / emoji string that decodes to fewer than n runes survives
// unchanged while a 5KB ASCII blob is shortened to a preview-sized chunk.
//
// Edge cases:
//   - n <= 0 returns "".
//   - len(s) in runes <= n returns s as-is (no allocation).
//   - Otherwise the first n runes of s, joined back into a string.
//
// This helper is exported because some callers (e.g. the outbound
// aggregator T14) need to size content before deciding whether to
// emit a card or a follow-up text reply.
func TruncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n])
}
