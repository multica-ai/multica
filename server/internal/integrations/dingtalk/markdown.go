package dingtalk

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// DingTalk documents a 15000-byte msgParam limit for groupMessages.send:
// https://open.dingtalk.com/document/orgapp/the-robot-sends-a-group-message
// The send path measures serialized title + text, including JSON escapes.
// Use the same conservative budget for private messages.

const (
	// markdownByteBudget bounds one chunk's body in UTF-8 bytes.
	markdownByteBudget        = 14000
	markdownPayloadByteBudget = 15000
	// Only the source-user preview displayed above a Bot reply is truncated.
	// This is a presentation policy, not a provider title limit.
	quotePreviewByteBudget = 256
	// A split inside a code block appends "\n```" to make the emitted chunk
	// self-contained. Keep this space out of the content budget so the final
	// wire body, not just the pre-rendered slice, stays under the hard limit.
	markdownSyntheticFenceCloseBytes = len("\n```")
	markdownContentByteBudget        = markdownByteBudget - markdownSyntheticFenceCloseBytes
	// A continuation repeats the opening fence line. Bound that synthetic
	// prefix so an adversarially long info string cannot consume the entire
	// piece budget (or make it negative) when the next code line is split.
	maxMarkdownFenceInfoBytes = 256
	// defaultMarkdownTitle is the preview for answers without a leading heading.
	defaultMarkdownTitle = "Multica has replied."
)

// markdownTitle derives the sampleMarkdown title (the notification preview) from
// the body's first ATX heading, falling back to a default. The heading is left
// in the body; only its leading hashes are stripped for the preview.
func markdownTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if heading != "" {
				return heading
			}
		}
		if trimmed != "" {
			break
		}
	}
	return defaultMarkdownTitle
}

// quotePreview is only the source-user excerpt displayed in an outbound reply.
// It never bounds the answer title or the inbound selected context.
func quotePreview(body string) string {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if body == "" {
		return ""
	}
	return boundedPreview(body)
}

func boundedPreview(body string) string {
	const suffix = "\n..."
	if len(body) <= quotePreviewByteBudget {
		return body
	}
	limit := quotePreviewByteBudget - len(suffix)
	for !utf8.RuneStart(body[limit]) {
		limit--
	}
	// A partial web address can link to a different resource. Omit a URL that
	// straddles the preview boundary rather than publishing a truncated target.
	for _, span := range webURLSpans(body) {
		if span[0] < limit && span[1] > limit {
			limit = span[0]
			break
		}
	}
	prefix := strings.TrimRight(body[:limit], " \t\r\n")
	if prefix == "" {
		return "..."
	}
	return prefix + suffix
}

// These are source spans, not parsed/re-serialized URLs. Preserve the original
// spelling, encoding, query and fragment when DingTalk auto-links quote text.
var webURLStart = regexp.MustCompile(`(?i)(?:https?://|www\.)`)

func webURLSpans(body string) [][]int {
	var spans [][]int
	for start := 0; start < len(body); {
		span := webURLStart.FindStringIndex(body[start:])
		if span == nil {
			break
		}
		span[0] += start
		contentStart := start + span[1]
		span[1] = len(body)
		var closing []rune
	urlToken:
		for offset, r := range body[contentStart:] {
			if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("<>\"\\`", r) {
				span[1] = contentStart + offset
				break
			}
			switch r {
			case '(':
				closing = append(closing, ')')
			case '[':
				closing = append(closing, ']')
			case ')', ']':
				if len(closing) == 0 || closing[len(closing)-1] != r {
					span[1] = contentStart + offset
					break urlToken
				}
				closing = closing[:len(closing)-1]
			}
		}
		spans = append(spans, span)
		start = span[1]
	}
	return spans
}

// chunkMarkdown splits body into pieces each at most markdownByteBudget bytes,
// preferring line boundaries. A code fence (```) left open at a boundary is
// closed at the end of the chunk and reopened at the start of the next so each
// chunk is self-contained markdown. A single line longer than the budget is
// hard-split on a byte boundary that respects UTF-8 rune edges.
func chunkMarkdown(body string) []string {
	return chunkMarkdownWithBudget(body, markdownByteBudget)
}

func chunkMarkdownWithBudget(body string, byteBudget int) []string {
	contentByteBudget := byteBudget - markdownSyntheticFenceCloseBytes
	if len(body) <= byteBudget {
		return []string{body}
	}

	var (
		chunks    []string
		cur       strings.Builder
		fenceOpen bool
		// fenceInfo is the opening fence line (e.g. "```go") of the currently open
		// block, so a continuation chunk can reopen with the SAME info string.
		// DingTalk highlights by that language tag; a bare "```" reopen renders the
		// continuation unhighlighted.
		fenceInfo string
	)
	flush := func(reopen bool) {
		if cur.Len() == 0 {
			return
		}
		text := cur.String()
		if fenceOpen {
			text += "\n```"
		}
		// Drop a chunk that carries only fence/blank lines — e.g. an opening fence
		// stranded right before an oversized line, or a reopened fence with nothing
		// after it — so it never renders as an empty code block.
		if !isBlankChunk(text) {
			chunks = append(chunks, text)
		}
		cur.Reset()
		if reopen && fenceOpen {
			cur.WriteString(fenceInfo + "\n")
		}
	}

	for _, line := range splitKeepNewline(body) {
		// A single oversized line cannot fit a chunk; hard-split it.
		if len(line) > contentByteBudget {
			flush(true)
			pieceBudget := contentByteBudget
			quotePrefix := ""
			if fenceOpen {
				pieceBudget = byteBudget - len(fenceInfo) - len("\n") - len("\n```")
			} else if strings.HasPrefix(line, "> ") {
				// Each wire message must retain the source attribution of a long
				// quoted line. A prefix on only the first piece makes later pieces
				// appear to be the robot's own answer.
				quotePrefix = "> "
				line = strings.TrimPrefix(line, quotePrefix)
				pieceBudget -= len(quotePrefix)
			}
			for _, piece := range hardSplit(line, pieceBudget) {
				// A piece split out of an oversized line inside a code block must
				// carry its own fences, or it would render as plain text.
				if fenceOpen {
					piece = fenceInfo + "\n" + piece + "\n```"
				}
				piece = quotePrefix + piece
				chunks = append(chunks, piece)
			}
			continue
		}
		if cur.Len()+len(line) > contentByteBudget {
			flush(true)
		}
		if isFenceLine(line) {
			if fenceOpen {
				fenceOpen = false
				fenceInfo = ""
			} else {
				fenceOpen = true
				fenceInfo = continuationFence(line)
			}
		}
		cur.WriteString(line)
	}
	flush(false)
	return chunks
}

func continuationFence(line string) string {
	fence := strings.TrimRight(line, "\r\n")
	if len(fence) > maxMarkdownFenceInfoBytes {
		return "```"
	}
	return fence
}

// splitKeepNewline splits s into lines, keeping the trailing "\n" on each line
// so reassembly (strings.Join) is exact. SplitAfter yields a trailing "" when s
// ends in "\n"; drop it so an exact-newline body does not gain a blank line.
func splitKeepNewline(s string) []string {
	lines := strings.SplitAfter(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// isFenceLine reports whether a line opens or closes a fenced code block (its
// first non-space content is ```).
func isFenceLine(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), "```")
}

// isBlankChunk reports whether text carries no renderable content — every line
// is blank or a fence marker. Such a chunk would render as an empty code block,
// so the chunker drops it instead of sending it.
func isBlankChunk(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t != "" && !strings.HasPrefix(t, "```") {
			return false
		}
	}
	return true
}

// hardSplit breaks s into byte-budget pieces without cutting a UTF-8 rune.
func hardSplit(s string, budget int) []string {
	// All production callers provide a much larger budget. Keep this helper
	// total anyway: a future synthetic prefix change must not turn a malformed
	// budget into an infinite loop, negative slice, or split UTF-8 rune.
	if budget < utf8.UTFMax {
		budget = utf8.UTFMax
	}
	var pieces []string
	for len(s) > budget {
		cut := budget
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		if cut == 0 {
			cut = budget
		}
		pieces = append(pieces, s[:cut])
		s = s[cut:]
	}
	if s != "" {
		pieces = append(pieces, s)
	}
	return pieces
}
