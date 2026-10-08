package telegram

import (
	"html"
	"regexp"
	"strings"
)

// This file converts the agent's standard Markdown to Telegram HTML parse
// mode. HTML is chosen over MarkdownV2 deliberately: MarkdownV2 requires
// escaping 18 punctuation characters in ALL text (one missed escape fails the
// whole sendMessage), while HTML only needs &, <, > escaped and unknown tags
// are the only failure mode. The conversion is line-oriented and intentionally
// conservative — unrecognized markdown passes through as escaped plain text
// rather than risking a rejected message.

var (
	reBold       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reItalic     = regexp.MustCompile(`(^|[^*])\*([^*]+?)\*`)
	reStrike     = regexp.MustCompile(`~~(.+?)~~`)
	reInlineCode = regexp.MustCompile("`([^`]+)`")
	reLink       = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	reHeading    = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	reBullet     = regexp.MustCompile(`^(\s*)[-*]\s+`)
)

// formatHTML renders markdown text as Telegram HTML. Code blocks are handled
// first (their content must stay verbatim, only entity-escaped); the rest is
// converted line by line.
func formatHTML(md string) string {
	var out strings.Builder
	lines := strings.Split(md, "\n")
	inCode := false
	var codeLang string
	var codeBuf []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				out.WriteString(renderCodeBlock(codeLang, codeBuf))
				out.WriteString("\n")
				inCode, codeBuf, codeLang = false, nil, ""
			} else {
				inCode = true
				codeLang = strings.TrimPrefix(trimmed, "```")
			}
			continue
		}
		if inCode {
			codeBuf = append(codeBuf, line)
			continue
		}
		out.WriteString(formatLine(line))
		out.WriteString("\n")
	}
	if inCode {
		// Unterminated fence (mid-stream snapshot): render what we have so the
		// throttled edit still shows the partial block.
		out.WriteString(renderCodeBlock(codeLang, codeBuf))
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

func renderCodeBlock(lang string, lines []string) string {
	body := html.EscapeString(strings.Join(lines, "\n"))
	if lang != "" {
		return `<pre><code class="language-` + html.EscapeString(lang) + `">` + body + `</code></pre>`
	}
	return "<pre>" + body + "</pre>"
}

// formatLine converts one non-code line: escape first, then re-introduce the
// allowed tags by pattern. Placeholders guard inline code spans from the
// styling passes.
func formatLine(line string) string {
	if m := reHeading.FindStringSubmatch(line); m != nil {
		return "<b>" + formatInline(m[1]) + "</b>"
	}
	if m := reBullet.FindStringSubmatchIndex(line); m != nil {
		return line[m[2]:m[3]] + "• " + formatInline(line[m[1]:])
	}
	return formatInline(line)
}

func formatInline(s string) string {
	// Extract inline code spans before escaping/styling so their content is
	// untouched by the emphasis passes.
	type span struct{ content string }
	var spans []span
	s = reInlineCode.ReplaceAllStringFunc(s, func(m string) string {
		content := strings.Trim(m, "`")
		spans = append(spans, span{content})
		return "\x00CODE\x00"
	})

	// Links: capture before escaping so the URL survives untouched.
	type link struct{ label, url string }
	var links []link
	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		g := reLink.FindStringSubmatch(m)
		links = append(links, link{g[1], g[2]})
		return "\x00LINK\x00"
	})

	s = html.EscapeString(s)
	s = reBold.ReplaceAllString(s, "<b>$1</b>")
	s = reItalic.ReplaceAllString(s, "$1<i>$2</i>")
	s = reStrike.ReplaceAllString(s, "<s>$1</s>")

	for _, l := range links {
		tag := `<a href="` + html.EscapeString(l.url) + `">` + html.EscapeString(l.label) + `</a>`
		s = strings.Replace(s, "\x00LINK\x00", tag, 1)
	}
	for _, c := range spans {
		s = strings.Replace(s, "\x00CODE\x00", "<code>"+html.EscapeString(c.content)+"</code>", 1)
	}
	return s
}
