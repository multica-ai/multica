package telegram

import (
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// This file converts the agent's standard Markdown to Telegram HTML parse
// mode. HTML is chosen over MarkdownV2 deliberately: MarkdownV2 requires
// escaping 18 punctuation characters in ALL text (one missed escape fails the
// whole sendMessage), while HTML only needs &, <, > escaped and unknown tags
// are the only failure mode. The conversion is line-oriented and intentionally
// conservative — unrecognized markdown passes through as escaped plain text
// rather than risking a rejected message.

var (
	reHeading = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	reBullet  = regexp.MustCompile(`^(\s*)[-*]\s+`)
)

// formatHTML renders markdown text as Telegram HTML. Code blocks are handled
// first (their content must stay verbatim, only entity-escaped); the rest is
// converted line by line.
func formatHTML(md string) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	var out strings.Builder
	lines := strings.Split(md, "\n")
	inCode := false
	var fenceChar byte
	var fenceLen int
	var codeLang string
	var codeBuf []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inCode {
			if isFenceClose(trimmed, fenceChar, fenceLen) {
				out.WriteString(renderCodeBlock(codeLang, codeBuf))
				out.WriteString("\n")
				inCode, fenceChar, fenceLen, codeLang, codeBuf = false, 0, 0, "", nil
				continue
			}
			codeBuf = append(codeBuf, line)
			continue
		}
		if ch, n, lang, ok := parseFenceOpen(trimmed); ok {
			inCode = true
			fenceChar = ch
			fenceLen = n
			codeLang = lang
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

func parseFenceOpen(trimmed string) (byte, int, string, bool) {
	if len(trimmed) < 3 {
		return 0, 0, "", false
	}
	ch := trimmed[0]
	if ch != '`' && ch != '~' {
		return 0, 0, "", false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, "", false
	}
	info := strings.TrimSpace(trimmed[n:])
	if ch == '`' && strings.ContainsRune(info, '`') {
		return 0, 0, "", false
	}
	lang := ""
	if fields := strings.Fields(info); len(fields) > 0 {
		lang = fields[0]
	}
	return ch, n, lang, true
}

func isFenceClose(trimmed string, fenceChar byte, fenceLen int) bool {
	if len(trimmed) < fenceLen {
		return false
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != fenceChar {
			return false
		}
	}
	return true
}

func renderCodeBlock(lang string, lines []string) string {
	body := html.EscapeString(strings.Join(lines, "\n"))
	if lang != "" {
		return `<pre><code class="language-` + html.EscapeString(lang) + `">` + body + `</code></pre>`
	}
	return "<pre>" + body + "</pre>"
}

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
	return renderInline(s, true)
}

func renderInline(s string, allowLink bool) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		switch s[i] {
		case '`':
			content, next, ok := parseCodeSpan(s, i)
			if ok {
				out.WriteString("<code>")
				out.WriteString(html.EscapeString(content))
				out.WriteString("</code>")
			} else {
				out.WriteString(s[i:next])
			}
			i = next
			continue
		case '[':
			if allowLink {
				if label, dest, next, ok := parseLink(s, i); ok {
					out.WriteString(`<a href="`)
					out.WriteString(html.EscapeString(dest))
					out.WriteString(`">`)
					out.WriteString(renderInline(label, false))
					out.WriteString("</a>")
					i = next
					continue
				}
			}
		case '*':
			runEnd := i
			for runEnd < len(s) && s[runEnd] == '*' {
				runEnd++
			}
			runLen := runEnd - i
			if runLen <= 3 && !startsWithSpace(s[runEnd:]) {
				switch runLen {
				case 3:
					if innerEnd, next, ok := findStarClose(s, runEnd, 3, allowLink); ok {
						out.WriteString("<b><i>")
						out.WriteString(renderInline(s[runEnd:innerEnd], allowLink))
						out.WriteString("</i></b>")
						i = next
						continue
					}
					if innerEnd, next, ok := findStarClose(s, i+1, 1, allowLink); ok {
						out.WriteString("<i>")
						out.WriteString(renderInline(s[i+1:innerEnd], allowLink))
						out.WriteString("</i>")
						i = next
						continue
					}
					if innerEnd, next, ok := findStarClose(s, i+2, 2, allowLink); ok {
						out.WriteString("<b>")
						out.WriteString(renderInline(s[i+2:innerEnd], allowLink))
						out.WriteString("</b>")
						i = next
						continue
					}
				case 2:
					if innerEnd, next, ok := findStarClose(s, runEnd, 2, allowLink); ok {
						out.WriteString("<b>")
						out.WriteString(renderInline(s[runEnd:innerEnd], allowLink))
						out.WriteString("</b>")
						i = next
						continue
					}
				case 1:
					if innerEnd, next, ok := findStarClose(s, runEnd, 1, allowLink); ok {
						out.WriteString("<i>")
						out.WriteString(renderInline(s[runEnd:innerEnd], allowLink))
						out.WriteString("</i>")
						i = next
						continue
					}
				}
			}
			out.WriteString(s[i:runEnd])
			i = runEnd
			continue
		case '~':
			runEnd := i
			for runEnd < len(s) && s[runEnd] == '~' {
				runEnd++
			}
			if runEnd-i == 2 && !startsWithSpace(s[runEnd:]) {
				if closeIdx, ok := findStrikeClose(s, runEnd, allowLink); ok {
					out.WriteString("<s>")
					out.WriteString(renderInline(s[runEnd:closeIdx], allowLink))
					out.WriteString("</s>")
					i = closeIdx + 2
					continue
				}
			}
			out.WriteString(s[i:runEnd])
			i = runEnd
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		out.WriteString(html.EscapeString(s[i : i+size]))
		i += size
	}
	return out.String()
}

func parseCodeSpan(s string, start int) (string, int, bool) {
	n := 0
	for start+n < len(s) && s[start+n] == '`' {
		n++
	}
	for i := start + n; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] == '`' {
			j++
		}
		if j-i == n {
			raw := s[start+n : i]
			if len(raw) >= 2 && raw[0] == ' ' && raw[len(raw)-1] == ' ' && strings.Trim(raw, " ") != "" {
				raw = raw[1 : len(raw)-1]
			}
			return raw, j, true
		}
		i = j
	}
	return "", start + n, false
}

func parseLink(s string, start int) (string, string, int, bool) {
	if start >= len(s) || s[start] != '[' {
		return "", "", 0, false
	}
	depth := 1
	i := start + 1
	label := ""
	for i < len(s) && depth > 0 {
		switch s[i] {
		case '\\':
			if i+1 < len(s) {
				i += 2
				continue
			}
		case '`':
			_, end, _ := parseCodeSpan(s, i)
			i = end
			continue
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				label = s[start+1 : i]
				i++
				goto parseDest
			}
		}
		i++
	}
	return "", "", 0, false

parseDest:
	if label == "" || i >= len(s) || s[i] != '(' {
		return "", "", 0, false
	}
	i++
	if i >= len(s) {
		return "", "", 0, false
	}
	if s[i] == '<' {
		closeAngle := strings.IndexByte(s[i+1:], '>')
		if closeAngle < 0 {
			return "", "", 0, false
		}
		dest := s[i+1 : i+1+closeAngle]
		end := i + 1 + closeAngle + 1
		if dest == "" || end >= len(s) || s[end] != ')' {
			return "", "", 0, false
		}
		return label, dest, end + 1, true
	}
	destStart := i
	parens := 0
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' {
			return "", "", 0, false
		}
		if c == '(' {
			parens++
		} else if c == ')' {
			if parens == 0 {
				dest := s[destStart:i]
				if dest == "" {
					return "", "", 0, false
				}
				return label, dest, i + 1, true
			}
			parens--
		}
		i++
	}
	return "", "", 0, false
}

func findStarClose(s string, from int, want int, allowLink bool) (int, int, bool) {
	inBold := false
	inItalic := false
	fallback := -1
	for j := from; j < len(s); {
		switch s[j] {
		case '`':
			_, end, _ := parseCodeSpan(s, j)
			j = end
			continue
		case '[':
			if allowLink {
				if _, _, end, ok := parseLink(s, j); ok {
					j = end
					continue
				}
			}
		case '*':
			runEnd := j
			for runEnd < len(s) && s[runEnd] == '*' {
				runEnd++
			}
			k := runEnd - j
			canOpen := !startsWithSpace(s[runEnd:])
			canClose := j > from && !endsWithSpace(s[from:j])
			switch want {
			case 3:
				if k == 3 && canClose {
					return j, runEnd, true
				}
			case 2:
				if k == 1 {
					if inItalic && canClose {
						inItalic = false
						fallback = -1
					} else if !inItalic && canOpen {
						inItalic = true
					}
				} else if k == 2 && canClose {
					if !inItalic {
						return j, runEnd, true
					}
					if fallback < 0 {
						fallback = j
					}
				} else if k == 3 && canClose {
					if inItalic {
						return j + 1, runEnd, true
					}
					return j, j + 2, true
				}
			case 1:
				if k == 2 {
					if inBold && canClose {
						inBold = false
						fallback = -1
					} else if !inBold && canOpen {
						inBold = true
					}
				} else if k == 1 && canClose {
					if !inBold {
						return j, runEnd, true
					}
					if fallback < 0 {
						fallback = j
					}
				} else if k == 3 && canClose {
					if inBold {
						return j + 2, runEnd, true
					}
					return j, j + 1, true
				}
			}
			j = runEnd
			continue
		}
		j++
	}
	if fallback >= 0 {
		return fallback, fallback + want, true
	}
	return 0, 0, false
}

func findStrikeClose(s string, from int, allowLink bool) (int, bool) {
	for j := from; j < len(s); {
		switch s[j] {
		case '`':
			_, end, _ := parseCodeSpan(s, j)
			j = end
			continue
		case '[':
			if allowLink {
				if _, _, end, ok := parseLink(s, j); ok {
					j = end
					continue
				}
			}
		case '~':
			runEnd := j
			for runEnd < len(s) && s[runEnd] == '~' {
				runEnd++
			}
			if runEnd-j == 2 && j > from && !endsWithSpace(s[from:j]) {
				return j, true
			}
			j = runEnd
			continue
		}
		j++
	}
	return 0, false
}

func startsWithSpace(s string) bool {
	if s == "" {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsSpace(r)
}

func endsWithSpace(s string) bool {
	if s == "" {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s)
	return unicode.IsSpace(r)
}
