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
// mangle them, then restored last in reverse order so nested placeholders
// resolve.
var (
	reFenced      = regexp.MustCompile("(?s)(```(?:[^\\n]*\\n)?.*?```)")
	reInlineCode  = regexp.MustCompile("(`[^`]+`)")
	reMdLink      = regexp.MustCompile(`(!?)\[([^\]]+)\]\(([^()]*(?:\([^()]*\)[^()]*)*)\)`)
	reSlackEntity = regexp.MustCompile(`(<(?:[@#!]|(?:https?|mailto|tel):)[^>\n]+>)`)
	reBlockquote  = regexp.MustCompile(`(?m)^(>+\s)`)
	reHeader      = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
	reInnerBold   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBoldItalic  = regexp.MustCompile(`\*\*\*(.+?)\*\*\*`)
	reBold        = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reItalic      = regexp.MustCompile(`\*(\S(?:[^*\n]*?\S)?)\*`)
	reStrike      = regexp.MustCompile(`~~(.+?)~~`)
)

// formatMrkdwn converts standard Markdown to Slack mrkdwn.
func formatMrkdwn(content string) string {
	if content == "" {
		return content
	}
	p := &mrkdwnPlaceholders{values: map[string]string{}}
	text := content

	// 1) Protect fenced code blocks, then 2) inline code.
	text = reFenced.ReplaceAllStringFunc(text, p.stash)
	text = reInlineCode.ReplaceAllStringFunc(text, p.stash)

	// 3) Markdown links [text](url) -> <url|text>; image links (![..]) are left
	//    untouched (Slack does not render inline images from markdown).
	text = reMdLink.ReplaceAllStringFunc(text, func(m string) string {
		sub := reMdLink.FindStringSubmatch(m)
		if sub[1] == "!" {
			return m
		}
		url := strings.TrimSpace(sub[3])
		if strings.HasPrefix(url, "<") && strings.HasSuffix(url, ">") {
			url = strings.TrimSpace(url[1 : len(url)-1])
		}
		return p.stash("<" + url + "|" + sub[2] + ">")
	})

	// 4) Protect existing Slack entities / manual links, 5) blockquote markers.
	text = reSlackEntity.ReplaceAllStringFunc(text, p.stash)
	text = reBlockquote.ReplaceAllStringFunc(text, p.stash)

	// 6) Escape Slack control chars (unescape first so input isn't double-escaped).
	text = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">").Replace(text)
	text = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)

	// 7) Headers (## Title) -> *Title* (strip redundant bold inside).
	text = reHeader.ReplaceAllStringFunc(text, func(m string) string {
		inner := strings.TrimSpace(reHeader.FindStringSubmatch(m)[1])
		inner = reInnerBold.ReplaceAllString(inner, "$1")
		return p.stash("*" + inner + "*")
	})

	// 8) ***bold italic*** -> *_text_*, 9) **bold** -> *bold*,
	// 10) *italic* -> _italic_, 11) ~~strike~~ -> ~strike~.
	text = reBoldItalic.ReplaceAllStringFunc(text, func(m string) string {
		return p.stash("*_" + reBoldItalic.FindStringSubmatch(m)[1] + "_*")
	})
	text = reBold.ReplaceAllStringFunc(text, func(m string) string {
		return p.stash("*" + reBold.FindStringSubmatch(m)[1] + "*")
	})
	text = reItalic.ReplaceAllStringFunc(text, func(m string) string {
		return p.stash("_" + reItalic.FindStringSubmatch(m)[1] + "_")
	})
	text = reStrike.ReplaceAllStringFunc(text, func(m string) string {
		return p.stash("~" + reStrike.FindStringSubmatch(m)[1] + "~")
	})

	// 13) Restore placeholders in reverse insertion order (nested ones resolve).
	for i := len(p.order) - 1; i >= 0; i-- {
		k := p.order[i]
		text = strings.ReplaceAll(text, k, p.values[k])
	}
	return text
}

type mrkdwnPlaceholders struct {
	values map[string]string
	order  []string
	n      int
}

func (p *mrkdwnPlaceholders) stash(v string) string {
	key := "\x00SL" + strconv.Itoa(p.n) + "\x00"
	p.n++
	p.values[key] = v
	p.order = append(p.order, key)
	return key
}
