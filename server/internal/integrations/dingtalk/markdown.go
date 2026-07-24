package dingtalk

import (
	"strings"
	"unicode/utf8"
)

// DingTalk's robot send APIs (oToMessages.batchSend / groupMessages.send) hard-
// reject a sampleMarkdown body over ~20000 bytes and drop the whole message, so
// we chunk well under that. The budget is measured in UTF-8 BYTES, not runes,
// because the limit is on the encoded payload. A code fence open at a chunk
// boundary is closed at the end of the chunk and reopened at the start of the
// next, so neither half renders as broken markdown.

const (
	// markdownByteBudget bounds one chunk's body in UTF-8 bytes.
	markdownByteBudget = 16000
	// defaultMarkdownTitle is the chat-list notification preview used when the
	// body carries no leading heading. DingTalk shows the title only in the push
	// preview, not in the message body.
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

// chunkMarkdown splits body into pieces each at most markdownByteBudget bytes,
// preferring line boundaries. A code fence (```) left open at a boundary is
// closed at the end of the chunk and reopened at the start of the next so each
// chunk is self-contained markdown. A single line longer than the budget is
// hard-split on a byte boundary that respects UTF-8 rune edges.
func chunkMarkdown(body string) []string {
	if len(body) <= markdownByteBudget {
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
		if len(line) > markdownByteBudget {
			flush(true)
			for _, piece := range hardSplit(line, markdownByteBudget) {
				// A piece split out of an oversized line inside a code block must
				// carry its own fences, or it would render as plain text.
				if fenceOpen {
					piece = fenceInfo + "\n" + piece + "\n```"
				}
				chunks = append(chunks, piece)
			}
			continue
		}
		if cur.Len()+len(line) > markdownByteBudget {
			flush(true)
		}
		if isFenceLine(line) {
			if fenceOpen {
				fenceOpen = false
				fenceInfo = ""
			} else {
				fenceOpen = true
				fenceInfo = strings.TrimRight(line, "\r\n")
			}
		}
		cur.WriteString(line)
	}
	flush(false)
	return chunks
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
