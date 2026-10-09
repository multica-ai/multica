package dingtalk

import (
	"encoding/json"
	"strings"
	"unicode"
)

// Project the observed rendered-card snapshot, not a template's cardParamMap.
// UNKNOWN children are excluded from selected answer context: the captured
// group snapshots put source attribution and layout there. We deliberately do
// not decode their serialized values. Unsupported non-UNKNOWN nodes still mark
// missing content. TEXT nodes are inline runs, not necessarily paragraphs.
// The public robot message-type list is not a card-node schema. This projection
// covers observed RICHTEXT children: TEXT prose, LINK destination strings, IMAGE
// placeholders, and UNKNOWN layout/source data. Do not infer other node shapes.
func renderDingTalkQuotedCard(data json.RawMessage) string {
	const unavailable = "[quoted content unavailable]"
	var blocks []json.RawMessage
	if json.Unmarshal(data, &blocks) != nil || len(blocks) == 0 {
		return unavailable
	}
	var body strings.Builder
	appendText := func(s string) { body.WriteString(s) }
	missing := func() {
		if !strings.HasSuffix(body.String(), unavailable+"\n") {
			if body.Len() > 0 && !strings.HasSuffix(body.String(), "\n") {
				appendText("\n")
			}
			appendText(unavailable + "\n")
		}
	}
	for _, raw := range blocks {
		var block struct {
			ElementType string            `json:"elementType"`
			Children    []json.RawMessage `json:"children"`
		}
		if json.Unmarshal(raw, &block) != nil || block.ElementType != "RICHTEXT" || len(block.Children) == 0 {
			missing()
			continue
		}
		blockStarted := false
		for _, rawChild := range block.Children {
			var node struct {
				ElementType string          `json:"elementType"`
				Value       json.RawMessage `json:"value"`
			}
			if json.Unmarshal(rawChild, &node) != nil {
				missing()
				continue
			}
			if node.ElementType == "UNKNOWN" {
				continue
			}
			if !blockStarted && body.Len() > 0 {
				appendText("\n\n")
			}
			blockStarted = true
			switch node.ElementType {
			case "TEXT", "LINK":
				var value string
				if len(node.Value) == 0 || string(node.Value) == "null" || json.Unmarshal(node.Value, &value) != nil {
					missing()
					continue
				}
				// Apply the existing conservative policy to raw provider values,
				// preserving neighboring nodes and generated image placeholders.
				if dingTalkReadableQuotedText(value) != value {
					missing()
					continue
				}
				if node.ElementType == "LINK" {
					if strings.TrimSpace(value) == "" {
						missing()
						continue
					}
					// Keep the observed destination verbatim, separate from an
					// adjacent run. A link label/URL object is not a known shape.
					if body.Len() > 0 && len(strings.TrimRightFunc(body.String(), unicode.IsSpace)) == body.Len() {
						appendText("\n")
					}
				}
				// A URL at a run boundary must not absorb the following run into its
				// destination. Other runs concatenate, preserving inline emphasis splits.
				spans := webURLSpans(body.String())
				if value != "" && len(spans) > 0 && spans[len(spans)-1][1] == body.Len() {
					appendText("\n")
				}
				appendText(value)
			case "IMAGE":
				// Rendered card snapshots expose an opaque code, not the original
				// Markdown URL. Observed codes fail the robot file-download API;
				// retain position without treating them as uploaded-message media.
				if body.Len() > 0 && !strings.HasSuffix(body.String(), "\n") {
					appendText("\n")
				}
				appendText(dingtalkImagePlaceholder + "\n")
			default:
				missing()
			}
		}
	}
	result := strings.TrimSpace(body.String())
	if result == "" {
		result = unavailable
	}
	return result
}
