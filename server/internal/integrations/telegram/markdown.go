package telegram

import (
	"html"
	"regexp"
	"sort"
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
	if !strings.ContainsAny(s, "`[*~") {
		return html.EscapeString(s)
	}
	return renderInline(s, true)
}

func renderInline(s string, allowLink bool) string {
	if !strings.ContainsAny(s, "`[*~") {
		return html.EscapeString(s)
	}
	idx := newInlineIndex(s, allowLink)
	var out strings.Builder
	out.Grow(len(s))
	idx.render(&out, 0, len(s), 0, len(idx.stars))
	return out.String()
}

type starRun struct {
	pos      int32
	k        uint8
	canOpen  bool
	canClose bool
}

const noStarMatch int32 = -1

// starLookup answers findStarClose queries for want in {1, 2} in O(1) time
// after a linear precomputation over the line's star runs.
// direct entries encode: >= 0 for a state-0 match at index v, <= -2 for a
// state-1 match at index -v-2, and -1 for no direct match.
type starLookup struct {
	built        bool
	want         int
	other        int
	direct0      []int32
	direct1      []int32
	nextFallback []int32
	nextConst1   []int32
	flipPrefix   []int32
	lastReset    []int32
	lastExit     []int32
}

func buildStarLookup(stars []starRun, want int) starLookup {
	m := len(stars)
	other := 3 - want
	n := m + 1
	buf := make([]int32, 7*n)
	sl := starLookup{
		built:        true,
		want:         want,
		other:        other,
		direct0:      buf[0*n : 1*n],
		direct1:      buf[1*n : 2*n],
		nextFallback: buf[2*n : 3*n],
		nextConst1:   buf[3*n : 4*n],
		flipPrefix:   buf[4*n : 5*n],
		lastReset:    buf[5*n : 6*n],
		lastExit:     buf[6*n : 7*n],
	}

	wantU := uint8(want)
	otherU := uint8(other)
	sl.direct0[m] = noStarMatch
	sl.direct1[m] = noStarMatch
	sl.nextFallback[m] = -1
	sl.nextConst1[m] = -1
	for i := m - 1; i >= 0; i-- {
		r := stars[i]
		if (r.k == wantU || r.k == 3) && r.canClose {
			sl.direct0[i] = int32(i)
		} else if r.k == otherU && r.canOpen {
			sl.direct0[i] = sl.direct1[i+1]
		} else {
			sl.direct0[i] = sl.direct0[i+1]
		}

		if r.k == 3 && r.canClose {
			sl.direct1[i] = int32(-i - 2)
		} else if r.k == otherU && r.canClose {
			sl.direct1[i] = sl.direct0[i+1]
		} else {
			sl.direct1[i] = sl.direct1[i+1]
		}

		if r.k == wantU && r.canClose {
			sl.nextFallback[i] = int32(i)
		} else {
			sl.nextFallback[i] = sl.nextFallback[i+1]
		}

		if r.k == otherU && r.canOpen && !r.canClose {
			sl.nextConst1[i] = int32(i)
		} else {
			sl.nextConst1[i] = sl.nextConst1[i+1]
		}
	}

	sl.lastReset[0] = -1
	sl.lastExit[0] = -1
	for i := 0; i < m; i++ {
		r := stars[i]
		sl.flipPrefix[i+1] = sl.flipPrefix[i]
		sl.lastReset[i+1] = sl.lastReset[i]
		sl.lastExit[i+1] = sl.lastExit[i]
		if r.k == otherU {
			if r.canOpen && r.canClose {
				sl.flipPrefix[i+1]++
			}
			if r.canOpen != r.canClose {
				sl.lastReset[i+1] = int32(i)
			}
			if r.canClose {
				sl.lastExit[i+1] = int32(i)
			}
		}
	}
	return sl
}

func (sl *starLookup) query(stars []starRun, startIdx, endIdx, from, end, initState int) (int, int, int, bool) {
	if startIdx > endIdx {
		return 0, 0, 0, false
	}
	if startIdx < endIdx {
		var dm int32
		if initState == 0 {
			dm = sl.direct0[startIdx]
		} else {
			dm = sl.direct1[startIdx]
		}
		if dm >= 0 {
			idx := int(dm)
			if idx < endIdx {
				pos := int(stars[idx].pos)
				return pos, pos + sl.want, idx, true
			}
		} else if dm <= -2 {
			idx := int(-dm - 2)
			if idx < endIdx {
				pos := int(stars[idx].pos)
				return pos + sl.other, pos + 3, idx, true
			}
		}
	}

	// No direct match in unclipped stars[startIdx:endIdx]; compute end state
	// and earliest surviving fallback for the last state-1 interval reaching endIdx.
	stateAtEnd := 0
	fallbackIdx := -1
	if startIdx == endIdx {
		stateAtEnd = initState
	} else {
		intervalStart := -1
		le := int(sl.lastExit[endIdx])
		if le < startIdx {
			if initState == 1 {
				intervalStart = startIdx
			} else if c1 := int(sl.nextConst1[startIdx]); c1 >= 0 && c1 < endIdx {
				intervalStart = c1 + 1
			}
		} else {
			lr := int(sl.lastReset[le+1])
			var sAfter int
			if lr >= startIdx {
				base := 0
				if stars[lr].canOpen {
					base = 1
				}
				flips := int(sl.flipPrefix[le+1] - sl.flipPrefix[lr+1])
				sAfter = base ^ (flips & 1)
			} else {
				flips := int(sl.flipPrefix[le+1] - sl.flipPrefix[startIdx])
				sAfter = initState ^ (flips & 1)
			}
			if sAfter == 1 {
				intervalStart = le + 1
			} else if c1 := int(sl.nextConst1[le+1]); c1 >= 0 && c1 < endIdx {
				intervalStart = c1 + 1
			}
		}
		if intervalStart >= 0 {
			stateAtEnd = 1
			if fb := int(sl.nextFallback[intervalStart]); fb >= 0 && fb < endIdx {
				fallbackIdx = fb
			}
		}
	}

	// A trailing "***" run sliced by end appears as "*" or "**" inside [from, end).
	if endIdx < len(stars) {
		tailPos := int(stars[endIdx].pos)
		if tailPos > from && tailPos < end {
			tailK := end - tailPos
			tailCanClose := stars[endIdx].canClose
			if tailK == sl.other && tailCanClose && stateAtEnd == 1 {
				stateAtEnd = 0
				fallbackIdx = -1
			} else if tailK == sl.want && tailCanClose {
				if stateAtEnd == 0 {
					return tailPos, tailPos + sl.want, endIdx, true
				}
				if fallbackIdx < 0 {
					fallbackIdx = endIdx
				}
			}
		}
	}

	if fallbackIdx >= 0 {
		pos := int(stars[fallbackIdx].pos)
		return pos, pos + sl.want, fallbackIdx, true
	}
	return 0, 0, 0, false
}

// inlineIndex precomputes delimiter matches for a single line in O(N) time so
// unclosed markers never trigger repeated scans across the rest of the line.
type inlineIndex struct {
	s                string
	allowLink        bool
	codeEnd          []int
	nextMark         []int
	nextRightBracket []int
	nextGT           []int
	parenClose       map[int]int
	strikeCloses     []int
	stars            []starRun
	hasClose1        bool
	hasClose2        bool
	hasClose3        bool
	nextClose3       []int32
	star1            starLookup
	star2            starLookup
}

func newInlineIndex(s string, allowLink bool) *inlineIndex {
	idx := &inlineIndex{s: s, allowLink: allowLink}

	// 1. Precompute the end of the code span starting at every backtick byte.
	// Starting inside a longer run is possible when a scan skips an escaped
	// byte, so the index covers every byte, not just maximal run starts.
	if strings.IndexByte(s, '`') >= 0 {
		type tickRun struct{ start, end int }
		var runs []tickRun
		for i := 0; i < len(s); {
			if s[i] != '`' {
				i++
				continue
			}
			j := i + 1
			for j < len(s) && s[j] == '`' {
				j++
			}
			runs = append(runs, tickRun{i, j})
			i = j
		}
		idx.codeEnd = make([]int, len(s))
		nearestByLen := make(map[int]int)
		for r := len(runs) - 1; r >= 0; r-- {
			run := runs[r]
			for p := run.start; p < run.end; p++ {
				k := run.end - p
				if closeStart, ok := nearestByLen[k]; ok {
					idx.codeEnd[p] = closeStart + k
				}
			}
			nearestByLen[run.end-run.start] = run.start
		}
	}

	// 2. Precompute jumps to the bytes that can affect link parsing. Ordinary
	// text is skipped in one step so an unclosed label cannot rescan the line.
	if allowLink && strings.IndexByte(s, '[') >= 0 && strings.IndexByte(s, ']') >= 0 {
		n := len(s)
		idx.nextMark = make([]int, n+1)
		idx.nextRightBracket = make([]int, n+1)
		idx.nextGT = make([]int, n+1)
		idx.nextMark[n], idx.nextRightBracket[n], idx.nextGT[n] = -1, -1, -1
		for i := n - 1; i >= 0; i-- {
			idx.nextMark[i] = idx.nextMark[i+1]
			idx.nextRightBracket[i] = idx.nextRightBracket[i+1]
			idx.nextGT[i] = idx.nextGT[i+1]
			switch s[i] {
			case '`', '[', ']', '\\':
				idx.nextMark[i] = i
			}
			if s[i] == ']' {
				idx.nextRightBracket[i] = i
			}
			if s[i] == '>' {
				idx.nextGT[i] = i
			}
		}
		var parens []int
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case ' ', '\t':
				parens = parens[:0]
			case '(':
				parens = append(parens, i)
			case ')':
				if len(parens) > 0 {
					open := parens[len(parens)-1]
					parens = parens[:len(parens)-1]
					if idx.parenClose == nil {
						idx.parenClose = make(map[int]int)
					}
					idx.parenClose[open] = i
				}
			}
		}
	}

	// 3. Collect emphasis star runs and closing strikethrough runs outside code
	// spans (and outside valid links when allowLink is true).
	if strings.IndexByte(s, '*') >= 0 || strings.IndexByte(s, '~') >= 0 {
		for i := 0; i < len(s); {
			switch s[i] {
			case '`':
				_, next, _ := idx.parseCodeSpan(i, len(s))
				i = next
				continue
			case '[':
				if allowLink {
					if _, _, next, ok := idx.parseLink(i, len(s)); ok {
						i = next
						continue
					}
				}
			case '*':
				runEnd := i + 1
				for runEnd < len(s) && s[runEnd] == '*' {
					runEnd++
				}
				k := runEnd - i
				if k <= 3 {
					canClose := i > 0 && !endsWithSpace(s[:i])
					idx.stars = append(idx.stars, starRun{
						pos:      int32(i),
						k:        uint8(k),
						canOpen:  !startsWithSpace(s[runEnd:]),
						canClose: canClose,
					})
					if canClose {
						switch k {
						case 1:
							idx.hasClose1 = true
						case 2:
							idx.hasClose2 = true
						case 3:
							idx.hasClose3 = true
						}
					}
				}
				i = runEnd
				continue
			case '~':
				runEnd := i + 1
				for runEnd < len(s) && s[runEnd] == '~' {
					runEnd++
				}
				if runEnd-i == 2 && i > 0 && !endsWithSpace(s[:i]) {
					idx.strikeCloses = append(idx.strikeCloses, i)
				}
				i = runEnd
				continue
			}
			i++
		}
	}

	return idx
}

func (idx *inlineIndex) render(out *strings.Builder, start, end, startStarIdx, endStarIdx int) {
	s := idx.s
	starCur := startStarIdx
	for i := start; i < end; {
		switch s[i] {
		case '`':
			content, next, ok := idx.parseCodeSpan(i, end)
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
			if idx.allowLink {
				if label, dest, next, ok := idx.parseLink(i, end); ok {
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
			runEnd := i + 1
			for runEnd < end && s[runEnd] == '*' {
				runEnd++
			}
			runLen := runEnd - i
			if runLen <= 3 && !startsWithSpace(s[runEnd:end]) {
				for starCur < endStarIdx && int(idx.stars[starCur].pos) < runEnd {
					starCur++
				}
				switch runLen {
				case 3:
					if innerEnd, next, matchIdx, ok := idx.findStarClose(starCur, endStarIdx, runEnd, end, 3, 0); ok {
						out.WriteString("<b><i>")
						idx.render(out, runEnd, innerEnd, starCur, matchIdx)
						out.WriteString("</i></b>")
						i = next
						starCur = matchIdx
						continue
					}
					if innerEnd, next, matchIdx, ok := idx.findStarClose(starCur, endStarIdx, runEnd, end, 1, 1); ok {
						out.WriteString("<i>")
						idx.render(out, i+1, innerEnd, starCur, matchIdx)
						out.WriteString("</i>")
						i = next
						starCur = matchIdx
						continue
					}
					if innerEnd, next, matchIdx, ok := idx.findStarClose(starCur, endStarIdx, runEnd, end, 2, 1); ok {
						out.WriteString("<b>")
						idx.render(out, i+2, innerEnd, starCur, matchIdx)
						out.WriteString("</b>")
						i = next
						starCur = matchIdx
						continue
					}
				case 2:
					if innerEnd, next, matchIdx, ok := idx.findStarClose(starCur, endStarIdx, runEnd, end, 2, 0); ok {
						out.WriteString("<b>")
						idx.render(out, runEnd, innerEnd, starCur, matchIdx)
						out.WriteString("</b>")
						i = next
						starCur = matchIdx
						continue
					}
				case 1:
					if innerEnd, next, matchIdx, ok := idx.findStarClose(starCur, endStarIdx, runEnd, end, 1, 0); ok {
						out.WriteString("<i>")
						idx.render(out, runEnd, innerEnd, starCur, matchIdx)
						out.WriteString("</i>")
						i = next
						starCur = matchIdx
						continue
					}
				}
			}
			out.WriteString(s[i:runEnd])
			i = runEnd
			continue
		case '~':
			runEnd := i + 1
			for runEnd < end && s[runEnd] == '~' {
				runEnd++
			}
			if runEnd-i == 2 && !startsWithSpace(s[runEnd:end]) {
				if closeIdx, ok := idx.findStrikeClose(runEnd, end); ok {
					for starCur < endStarIdx && int(idx.stars[starCur].pos) < runEnd {
						starCur++
					}
					subEndStarIdx := starCur
					if starCur < endStarIdx {
						subEndStarIdx = starCur + sort.Search(endStarIdx-starCur, func(k int) bool {
							r := idx.stars[starCur+k]
							return int(r.pos)+int(r.k) > closeIdx
						})
					}
					out.WriteString("<s>")
					idx.render(out, runEnd, closeIdx, starCur, subEndStarIdx)
					out.WriteString("</s>")
					i = closeIdx + 2
					starCur = subEndStarIdx
					continue
				}
			}
			out.WriteString(s[i:runEnd])
			i = runEnd
			continue
		}
		plainStart := i
		needsEscape := false
		for i < end {
			c := s[i]
			if i > plainStart && (c == '`' || c == '[' || c == '*' || c == '~') {
				break
			}
			if c == '&' || c == '<' || c == '>' || c == '"' || c == '\'' {
				needsEscape = true
			}
			i++
		}
		if needsEscape {
			out.WriteString(html.EscapeString(s[plainStart:i]))
		} else {
			out.WriteString(s[plainStart:i])
		}
	}
}

func (idx *inlineIndex) parseCodeSpan(start, end int) (string, int, bool) {
	s := idx.s
	n := 0
	for start+n < end && s[start+n] == '`' {
		n++
	}
	if closeEnd := idx.codeEnd[start]; closeEnd > 0 && closeEnd <= end {
		raw := s[start+n : closeEnd-n]
		if len(raw) >= 2 && raw[0] == ' ' && raw[len(raw)-1] == ' ' && strings.Trim(raw, " ") != "" {
			raw = raw[1 : len(raw)-1]
		}
		return raw, closeEnd, true
	}
	return "", start + n, false
}

func (idx *inlineIndex) parseLink(start, end int) (string, string, int, bool) {
	s := idx.s
	if !idx.allowLink || start >= end || s[start] != '[' || idx.nextRightBracket == nil {
		return "", "", 0, false
	}
	depth := 1
	var label string
	i := start + 1
	for i < end {
		j := idx.nextMark[i]
		if j < 0 || j >= end {
			return "", "", 0, false
		}
		i = j
		switch s[i] {
		case '\\':
			if i+1 < end {
				i += 2
			} else {
				i++
			}
		case '`':
			_, next, _ := idx.parseCodeSpan(i, end)
			i = next
		case '[':
			depth++
			i++
		case ']':
			depth--
			if depth == 0 {
				label = s[start+1 : i]
				i++
				goto parseDest
			}
			i++
		}
	}
	return "", "", 0, false

parseDest:
	if label == "" || i >= end || s[i] != '(' {
		return "", "", 0, false
	}
	i++
	if i >= end {
		return "", "", 0, false
	}
	if s[i] == '<' {
		closeAngle := idx.nextGT[i+1]
		if closeAngle < 0 || closeAngle >= end {
			return "", "", 0, false
		}
		closeParen := closeAngle + 1
		if closeParen >= end || s[closeParen] != ')' {
			return "", "", 0, false
		}
		dest := s[i+1 : closeAngle]
		if dest == "" {
			return "", "", 0, false
		}
		return label, dest, closeParen + 1, true
	}
	openParen := i - 1
	closeParen, ok := idx.parenClose[openParen]
	if !ok || closeParen >= end {
		return "", "", 0, false
	}
	dest := s[i:closeParen]
	if dest == "" {
		return "", "", 0, false
	}
	return label, dest, closeParen + 1, true
}

func (idx *inlineIndex) findStarClose(startIdx, endStarIdx, from, end, want, initState int) (int, int, int, bool) {
	switch want {
	case 3:
		if !idx.hasClose3 || startIdx >= endStarIdx {
			return 0, 0, 0, false
		}
		if idx.nextClose3 == nil {
			m := len(idx.stars)
			idx.nextClose3 = make([]int32, m+1)
			idx.nextClose3[m] = -1
			for i := m - 1; i >= 0; i-- {
				if idx.stars[i].k == 3 && idx.stars[i].canClose {
					idx.nextClose3[i] = int32(i)
				} else {
					idx.nextClose3[i] = idx.nextClose3[i+1]
				}
			}
		}
		if m := int(idx.nextClose3[startIdx]); m >= 0 && m < endStarIdx {
			pos := int(idx.stars[m].pos)
			return pos, pos + 3, m, true
		}
		return 0, 0, 0, false
	case 2:
		if !idx.hasClose2 && !idx.hasClose3 {
			return 0, 0, 0, false
		}
		if !idx.star2.built {
			idx.star2 = buildStarLookup(idx.stars, 2)
		}
		return idx.star2.query(idx.stars, startIdx, endStarIdx, from, end, initState)
	case 1:
		if !idx.hasClose1 && !idx.hasClose3 {
			return 0, 0, 0, false
		}
		if !idx.star1.built {
			idx.star1 = buildStarLookup(idx.stars, 1)
		}
		return idx.star1.query(idx.stars, startIdx, endStarIdx, from, end, initState)
	default:
		return 0, 0, 0, false
	}
}

func (idx *inlineIndex) findStrikeClose(from, end int) (int, bool) {
	if len(idx.strikeCloses) == 0 {
		return 0, false
	}
	pos := sort.SearchInts(idx.strikeCloses, from+1)
	if pos < len(idx.strikeCloses) && idx.strikeCloses[pos]+2 <= end {
		return idx.strikeCloses[pos], true
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
