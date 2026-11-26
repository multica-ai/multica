// The formatMrkdwn function below is a Go port of the Markdown-to-mrkdwn
// converter (format_message) from Nous Research's Hermes Agent, used under the
// MIT License. Source:
// https://github.com/NousResearch/hermes-agent/blob/main/plugins/platforms/slack/adapter.py
//
// Copyright (c) 2025 Nous Research
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package slack

import (
	"regexp"
	"strconv"
	"strings"
)

// Slack renders its own "mrkdwn" dialect, not standard Markdown: bold is *one*
// star (not two), italic is _underscore_, links are <url|label>, headers and
// ~~strike~~ are not supported. The agent emits standard Markdown, so an
// unconverted reply shows literal `**`, `##`, and `[text](url)` in Slack. This
// converter is a faithful Go port of Hermes Agent's slack `format_message`
// (MIT; see the license notice at the top of this file): protected regions
// (code, converted links, existing Slack
// entities) are stashed behind NUL-delimited placeholders so later passes never
// mangle them, then expanded in a single pass so nested placeholders resolve.
var (
	reMdLink      = regexp.MustCompile(`(!?)\[([^\]]+)\]\(([^()]*(?:\([^()]*\)[^()]*)*)\)`)
	reSlackEntity = regexp.MustCompile(`(<(?:[@#!]|(?:https?|mailto|tel):)[^>\n]+>)`)
	reBlockquote  = regexp.MustCompile(`(?m)^(>+\s)`)
	reHeader      = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	reInnerBold   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBoldItalic  = regexp.MustCompile(`\*\*\*(.+?)\*\*\*`)
	reBold        = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reItalic      = regexp.MustCompile(`\*(\S(?:[^*\n]*?\S)?)\*`)
	reStrike      = regexp.MustCompile(`~~(.+?)~~`)

	unescapeSlackReplacer = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">")
	escapeSlackReplacer   = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
)

func escapeSlackText(s string) string {
	return escapeSlackReplacer.Replace(unescapeSlackReplacer.Replace(s))
}

// formatMrkdwn converts standard Markdown to Slack mrkdwn.
func formatMrkdwn(content string) string {
	if content == "" {
		return content
	}
	p := newMrkdwnPlaceholders(content)
	text := content

	// 1) Protect fenced code blocks, then 2) inline code.
	text = stashFencedCode(text, p)
	text = stashInlineCode(text, p)

	// 3) Markdown links [text](url) -> <url|text>; image links (![..]) are left
	//    untouched (Slack does not render inline images from markdown).
	if strings.Contains(text, "](") {
		text = reMdLink.ReplaceAllStringFunc(text, func(m string) string {
			sub := reMdLink.FindStringSubmatch(m)
			if sub[1] == "!" {
				return m
			}
			url := strings.TrimSpace(sub[3])
			if strings.HasPrefix(url, "<") && strings.HasSuffix(url, ">") {
				url = strings.TrimSpace(url[1 : len(url)-1])
			}
			url = strings.ReplaceAll(escapeSlackText(p.restore(url)), "|", "%7C")
			label := escapeSlackText(p.restore(sub[2]))
			return p.stash("<" + url + "|" + label + ">")
		})
	}

	// 4) Protect existing Slack entities / manual links, 5) blockquote markers.
	if strings.IndexByte(text, '<') >= 0 {
		text = reSlackEntity.ReplaceAllStringFunc(text, p.stash)
	}
	if strings.IndexByte(text, '>') >= 0 {
		text = reBlockquote.ReplaceAllStringFunc(text, p.stash)
	}

	// 6) Escape Slack control chars (unescape first so input isn't double-escaped).
	text = escapeSlackText(text)

	// 7) Headers (## Title) -> *Title* (strip redundant bold inside).
	if strings.IndexByte(text, '#') >= 0 {
		text = reHeader.ReplaceAllStringFunc(text, func(m string) string {
			inner := strings.TrimSpace(reHeader.FindStringSubmatch(m)[1])
			inner = reInnerBold.ReplaceAllString(inner, "$1")
			return p.stash("*" + inner + "*")
		})
	}

	// 8) ***bold italic*** -> *_text_*, 9) **bold** -> *bold*,
	// 10) *italic* -> _italic_, 11) ~~strike~~ -> ~strike~.
	if strings.IndexByte(text, '*') >= 0 {
		if strings.Contains(text, "***") {
			text = reBoldItalic.ReplaceAllStringFunc(text, func(m string) string {
				return p.stash("*_" + reBoldItalic.FindStringSubmatch(m)[1] + "_*")
			})
		}
		if strings.Contains(text, "**") {
			text = reBold.ReplaceAllStringFunc(text, func(m string) string {
				return p.stash("*" + reBold.FindStringSubmatch(m)[1] + "*")
			})
		}
		if strings.IndexByte(text, '*') >= 0 {
			text = reItalic.ReplaceAllStringFunc(text, func(m string) string {
				return p.stash("_" + reItalic.FindStringSubmatch(m)[1] + "_")
			})
		}
	}
	if strings.Contains(text, "~~") {
		text = reStrike.ReplaceAllStringFunc(text, func(m string) string {
			return p.stash("~" + reStrike.FindStringSubmatch(m)[1] + "~")
		})
	}

	return p.restore(text)
}

func stashFencedCode(text string, p *mrkdwnPlaceholders) string {
	if !strings.Contains(text, "```") && !strings.Contains(text, "~~~") {
		return text
	}
	var (
		b          strings.Builder
		flushed    int
		fenceChar  byte
		fenceCount int
		blockStart int
	)
	for i := 0; i < len(text); {
		nl := strings.IndexByte(text[i:], '\n')
		var contentEnd, next int
		if nl < 0 {
			contentEnd = len(text)
			next = len(text)
		} else {
			contentEnd = i + nl
			next = contentEnd + 1
		}
		if contentEnd > i && text[contentEnd-1] == '\r' {
			contentEnd--
		}
		line := text[i:contentEnd]
		if fenceChar == 0 {
			if ch, count, ok := parseMrkdwnFenceOpen(line); ok {
				b.WriteString(text[flushed:i])
				fenceChar, fenceCount, blockStart = ch, count, i
			}
		} else if isMrkdwnFenceClose(line, fenceChar, fenceCount) {
			b.WriteString(p.stash(text[blockStart:contentEnd]))
			flushed = contentEnd
			fenceChar, fenceCount = 0, 0
		}
		i = next
	}
	if fenceChar != 0 {
		b.WriteString(p.stash(text[blockStart:]))
		flushed = len(text)
	}
	if flushed == 0 {
		return text
	}
	b.WriteString(text[flushed:])
	return b.String()
}

func parseMrkdwnFenceOpen(line string) (byte, int, bool) {
	spaces := 0
	for spaces < len(line) && spaces < 3 && line[spaces] == ' ' {
		spaces++
	}
	rest := line[spaces:]
	if len(rest) < 3 || (rest[0] != '`' && rest[0] != '~') {
		return 0, 0, false
	}
	ch := rest[0]
	count := 0
	for count < len(rest) && rest[count] == ch {
		count++
	}
	if count < 3 {
		return 0, 0, false
	}
	if ch == '`' && strings.IndexByte(rest[count:], '`') >= 0 {
		return 0, 0, false
	}
	return ch, count, true
}

func isMrkdwnFenceClose(line string, ch byte, minCount int) bool {
	spaces := 0
	for spaces < len(line) && spaces < 3 && line[spaces] == ' ' {
		spaces++
	}
	rest := strings.TrimRight(line[spaces:], " \t")
	if len(rest) < minCount {
		return false
	}
	for i := 0; i < len(rest); i++ {
		if rest[i] != ch {
			return false
		}
	}
	return true
}

func stashInlineCode(text string, p *mrkdwnPlaceholders) string {
	if strings.IndexByte(text, '`') < 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	paraStart := 0
	for i := 0; i < len(text); {
		nl := strings.IndexByte(text[i:], '\n')
		var contentEnd, next int
		if nl < 0 {
			contentEnd = len(text)
			next = len(text)
		} else {
			contentEnd = i + nl
			next = contentEnd + 1
		}
		if contentEnd > i && text[contentEnd-1] == '\r' {
			contentEnd--
		}
		if isBlankLine(text[i:contentEnd]) {
			if i > paraStart {
				stashInlineCodeSegment(text[paraStart:i], p, &b)
			}
			b.WriteString(text[i:next])
			paraStart = next
		}
		i = next
	}
	if paraStart < len(text) {
		stashInlineCodeSegment(text[paraStart:], p, &b)
	}
	return b.String()
}

func isBlankLine(line string) bool {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return false
		}
	}
	return true
}

func stashInlineCodeSegment(seg string, p *mrkdwnPlaceholders, b *strings.Builder) {
	if strings.IndexByte(seg, '`') < 0 {
		b.WriteString(seg)
		return
	}
	type backtickRun struct{ start, end int }
	var runs []backtickRun
	byLen := make(map[int][]int)
	for i := 0; i < len(seg); {
		if seg[i] != '`' {
			i++
			continue
		}
		j := i + 1
		for j < len(seg) && seg[j] == '`' {
			j++
		}
		idx := len(runs)
		runs = append(runs, backtickRun{start: i, end: j})
		byLen[j-i] = append(byLen[j-i], idx)
		i = j
	}
	cursor := make(map[int]int, len(byLen))
	pos := 0
	for i := 0; i < len(runs); i++ {
		r := runs[i]
		n := r.end - r.start
		list := byLen[n]
		c := cursor[n]
		for c < len(list) && list[c] <= i {
			c++
		}
		cursor[n] = c
		if c < len(list) {
			closeIdx := list[c]
			cursor[n] = c + 1
			b.WriteString(seg[pos:r.start])
			b.WriteString(p.stash(seg[r.start:runs[closeIdx].end]))
			pos = runs[closeIdx].end
			i = closeIdx
		}
	}
	b.WriteString(seg[pos:])
}

type mrkdwnPlaceholders struct {
	open   string
	close  string
	values []string
}

func newMrkdwnPlaceholders(content string) *mrkdwnPlaceholders {
	const base = "\x00SL"
	open := base
	if strings.Contains(content, base) {
		maxRun := 0
		for off := 0; ; {
			idx := strings.Index(content[off:], base)
			if idx < 0 {
				break
			}
			pos := off + idx + len(base)
			run := 0
			for pos+run < len(content) && content[pos+run] == '_' {
				run++
			}
			if run > maxRun {
				maxRun = run
			}
			off = pos
		}
		open = base + strings.Repeat("_", maxRun+1) + ":"
	}
	return &mrkdwnPlaceholders{open: open, close: "\x00"}
}

func (p *mrkdwnPlaceholders) stash(v string) string {
	idx := len(p.values)
	p.values = append(p.values, v)
	return p.open + strconv.Itoa(idx) + p.close
}

func (p *mrkdwnPlaceholders) restore(text string) string {
	if len(p.values) == 0 || !strings.Contains(text, p.open) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	stack := []string{text}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for cur != "" {
			start := strings.Index(cur, p.open)
			if start < 0 {
				b.WriteString(cur)
				break
			}
			b.WriteString(cur[:start])
			rest := cur[start+len(p.open):]
			end := strings.Index(rest, p.close)
			if end < 0 {
				b.WriteString(cur[start:])
				break
			}
			idx, err := strconv.Atoi(rest[:end])
			if err != nil || idx < 0 || idx >= len(p.values) {
				b.WriteString(cur[start : start+len(p.open)+end+len(p.close)])
				cur = rest[end+len(p.close):]
				continue
			}
			if after := rest[end+len(p.close):]; after != "" {
				stack = append(stack, after)
			}
			cur = p.values[idx]
		}
	}
	return b.String()
}
