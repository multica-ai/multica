package wecom

// markdown.go — keeping member-authored text from becoming markdown the bot
// appears to have written. Shared by the inbox card (inbox_message.go) and the
// /issue confirmation (replier.go), both of which splice someone else's words
// into a message that goes out under the bot's name.

import (
	"strings"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// breakMemberLinks is what every caller splicing member-authored text into a
// bot-signed message runs it through. Both breaks below are needed and they
// close different constructs, so they live behind one entry point: a call site
// cannot take one and forget the other.
//
// Both are pure insertions of a single space, so the two orders agree and the
// pair is idempotent: neither can produce the pattern the other looks for.
func breakMemberLinks(s string) string {
	return breakLinkReferenceDefinitions(breakLinkAdjacency(s))
}

// breakLinkAdjacency stops member-authored text from forming a markdown link
// in a message the bot signs. An issue titled
// "[click here](http://evil.example)" otherwise arrives as a working link
// inside a card the recipient has every reason to trust: it is delivered by
// the bot, and nothing in it marks which parts are quoted from a user.
//
// It separates rather than escapes. A link is only formed when "]" and "(" are
// adjacent — CommonMark requires the link text to be followed *immediately* by
// "(", and the naive `\[([^\]]+)\]\(([^)]+)\)` rewriters that stand in for a
// real parser require it too. Image syntax "![x](u)" needs the same adjacency,
// so it is covered by the same rule. One plain space between them is enough,
// and it is the only edit made: text that does not contain "](" comes back
// byte-identical, so the common "[Bug] 登录失败" title is untouched.
//
// The space is confirmed on the real renderer, not inferred: sent on its own,
// "[重置密码] (https://evil.example)" came back as plain black text with both
// brackets intact and the parenthesised URL not consumed as a destination.
//
// Backslash escaping was tried first and is unusable here. On a live tenant
// "\[Bug\]" came back as an italic serif "Bug" with the brackets gone, which
// is how a display-math block renders — the mechanism is inferred from the
// rendering, no WeCom doc describes it — and the conversation-list preview,
// which renders no markdown at all, showed the backslashes raw. So this
// function must never emit a backslash: it would either be visible or pull
// member text into a math block, and an unpaired "[" in a title would open one
// that never closes.
//
// Two properties the callers rely on:
//
//   - The inserted space can never begin a line. A "]" always precedes it on
//     the same line, so it cannot open an indented code block or turn into a
//     trailing hard-break.
//   - It adds one rune per occurrence, so callers that budget a length cap
//     must call it before measuring, not after.
//
// What it does not cover. A bare URL is still auto-linkified by the client —
// observed, and out of scope: a bare URL displays its own destination, and the
// attack being closed is a label claiming to go somewhere it does not. Inline
// HTML is inert here, also observed: `<a href="https://evil.example">重置密码</a>`
// arrived as plain black text. The one construct that does reach a working
// link without "](" is a CommonMark link reference definition, and this
// renderer resolves those — see breakLinkReferenceDefinitions.
func breakLinkAdjacency(s string) string {
	return channel.BreakMarkdownLinkAdjacency(s)
}

// breakLinkReferenceDefinitions inserts a space between ] and : to stop
// member-authored reference definitions from creating links under the bot's name.
// Breaking inside the label would not work because label matching normalizes space.
//
// Only block scaffolding may precede a valid label, and the destination must be
// capable of leaving the client's host. Relative destinations remain unchanged
// to preserve forms such as "[Bug]: 登录失败". Labels may span lines; the same
// container prefix must be used when locating a next-line destination.
//
// Container detection intentionally over-approximates CommonMark: an extra space
// is preferable to leaving an external reference active. See markdown_test.go for
// inline, collapsed, shortcut, and nested-container cases. Apply before measuring
// message length; the transformation adds a rune per match and is idempotent.
func breakLinkReferenceDefinitions(s string) string {
	if !strings.Contains(s, "]:") {
		return s
	}
	var out strings.Builder
	prev := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '[' {
			continue
		}
		container, ok := containerPrefixBefore(s, i)
		if !ok {
			continue
		}
		end, ok := linkLabelEnd(s, i)
		if !ok {
			continue
		}
		// A label holds no unescaped "[", so nothing inside it can open
		// another one — resume past it either way.
		i = end
		if end+1 >= len(s) || s[end+1] != ':' {
			continue
		}
		if !looksLikeLinkDestination(s[end+2:], container) {
			continue
		}
		out.WriteString(s[prev : end+1])
		out.WriteByte(' ')
		prev = end + 1
	}
	if prev == 0 {
		return s
	}
	out.WriteString(s[prev:])
	return out.String()
}

// containerPrefix is what a line's block scaffolding opens: the blockquote
// nesting the line sits inside. It is the one notion of a container the rule
// has, and both halves of the rule work from it — the half that decides what
// may precede the label produces one, the half that scans past the colon
// replays it. Neither can be taught about a container the other has not,
// which is the whole reason it exists as a value rather than as a bool.
//
// Only ">" markers are counted. Indentation and list bullets are scaffolding
// too, and they need no replay: CommonMark continues a list item with plain
// indentation, which the destination scan already steps over as whitespace.
// A blockquote is different — it repeats its marker on every line it holds,
// so a destination on the next line arrives as "> https://…", and a scan that
// does not expect the marker reads it as the destination.
type containerPrefix struct {
	quotes int
}

// containerPrefixBefore reports the containers holding the "[" at s[i], and
// whether everything between the start of its line and i is block scaffolding
// at all: indentation, ">" markers and list bullets. Anything else — a word, a
// "**" — means the "[" is inside a paragraph, where no definition can begin.
//
// It walks back only as far as the first byte that cannot be scaffolding, so a
// "[" in the middle of prose is settled by reading one byte rather than the
// whole line, and a body full of them stays linear.
func containerPrefixBefore(s string, i int) (containerPrefix, bool) {
	start := i
	for start > 0 && isBlockScaffoldByte(s[start-1]) {
		start--
	}
	if start > 0 && s[start-1] != '\n' {
		return containerPrefix{}, false
	}
	return parseContainerPrefix(s[start:i])
}

// parseContainerPrefix reads p as a whole run of block scaffolding and reports
// the containers it opens. ok is false when p holds anything that is not
// scaffolding.
func parseContainerPrefix(p string) (prefix containerPrefix, ok bool) {
	for len(p) > 0 {
		switch {
		case p[0] == ' ' || p[0] == '\t':
			p = p[1:]
		case p[0] == '>':
			prefix.quotes++
			p = p[1:]
		case (p[0] == '-' || p[0] == '+' || p[0] == '*') && len(p) > 1 && (p[1] == ' ' || p[1] == '\t'):
			p = p[2:]
		default:
			// An ordered list marker: up to 9 digits, then "." or ")",
			// then a space.
			n := 0
			for n < len(p) && n < 9 && p[n] >= '0' && p[n] <= '9' {
				n++
			}
			if n == 0 || n+1 >= len(p) || (p[n] != '.' && p[n] != ')') || (p[n+1] != ' ' && p[n+1] != '\t') {
				return containerPrefix{}, false
			}
			p = p[n+2:]
		}
	}
	return prefix, true
}

// skipContinuationPrefix steps over the markers of the containers in prefix at
// the start of a continuation line, and returns where the line's content
// begins. It is the replay half of containerPrefixBefore.
//
// It consumes at most prefix.quotes markers and never more. Fewer is allowed
// because a blockquote takes lazy continuation lines: "> [x]:" followed by a
// bare "https://…" with no marker still defines the reference. More is not a
// continuation of this definition at all — a line deeper than the block it
// continues opens a new one — so the scan stops and lets the leftover ">"
// speak for itself, which is to say it is not a destination.
func skipContinuationPrefix(s string, i int, prefix containerPrefix) int {
	i = skipSpacesTabs(s, i)
	for n := 0; n < prefix.quotes; n++ {
		if i >= len(s) || s[i] != '>' {
			return i
		}
		i = skipSpacesTabs(s, i+1)
	}
	return i
}

// isBlockScaffoldByte is the character set a scaffolding prefix can draw on —
// a superset of the markers parseContainerPrefix then parses exactly. It only
// decides how far back to walk; whether the run really is scaffolding is
// parseContainerPrefix's answer.
func isBlockScaffoldByte(c byte) bool {
	switch {
	case c == ' ', c == '\t', c == '>':
		return true
	case c == '-', c == '+', c == '*', c == '.', c == ')':
		return true
	case c >= '0' && c <= '9':
		return true
	}
	return false
}

// linkLabelEnd returns the byte offset of the "]" closing the link label that
// opens at s[open], and whether what it encloses is a label at all. Follows
// CommonMark: the first unescaped "]" closes it, an unescaped "[" disqualifies
// it, it must hold at least one non-whitespace character and at most 999, and
// it may run across line endings.
func linkLabelEnd(s string, open int) (int, bool) {
	const maxLabelRunes = 999
	runes, content := 0, false
	for i := open + 1; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == ']' {
			return i, content
		}
		if r == '[' {
			return 0, false
		}
		if runes++; runes > maxLabelRunes {
			return 0, false
		}
		if r == '\\' && i+size < len(s) {
			// The backslash escapes what follows, so "\]" does not close
			// the label and "\[" does not disqualify it.
			_, n := utf8.DecodeRuneInString(s[i+size:])
			i += size + n
			runes++
			content = true
			continue
		}
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			content = true
		}
		i += size
	}
	return 0, false
}

// looksLikeLinkDestination reports whether rest — everything after a
// "[label]:" — opens a link destination that could send the reader to another
// host. This is the test that keeps "[Bug]: 登录失败" whole.
//
// A destination qualifies when it carries a scheme ("https:", and equally
// "javascript:" or "data:"), or is scheme-relative ("//host/path"), or uses
// the escape machinery that spells either of those after decoding.
//
// prefix is the containers the definition's own line sits inside, and it is a
// parameter rather than something rediscovered here for the reason given on
// containerPrefix: when the destination is on the next line it arrives behind
// the same markers, and a scan that walks past the colon without them reads
// the marker as the destination and clears a definition that resolves.
func looksLikeLinkDestination(rest string, prefix containerPrefix) bool {
	i := skipSpacesTabs(rest, 0)
	// CommonMark allows up to one line ending between the colon and the
	// destination, so the definition can span two lines. A second line
	// ending ends the block and there is no definition — falling through
	// with the newline still in place takes care of that, since a line
	// ending never begins a destination.
	if i < len(rest) && rest[i] == '\r' {
		i++
	}
	if i < len(rest) && rest[i] == '\n' {
		i = skipContinuationPrefix(rest, i+1, prefix)
	}
	if i < len(rest) && (rest[i] == '<' || rest[i] == '(') {
		// "<…>" is the spec's bracketed destination form, and it may hold
		// leading spaces that a client strips back off the URL.
		i = skipSpacesTabs(rest, i+1)
	}
	dest := rest[i:]
	if j := strings.IndexAny(dest, " \t\r\n"); j >= 0 {
		dest = dest[:j]
	}
	if dest == "" {
		return false
	}
	if strings.HasPrefix(dest, "//") || hasURIScheme(dest) {
		return true
	}
	// Backslash escapes and character references are recognised inside a
	// destination, and all of "https\://evil.example", "\/\/evil.example"
	// and "&#x68;ttps://evil.example" resolve to a working off-host URL —
	// checked against a CommonMark parser, all three. Rather than decode
	// them, take any use of that machinery as a destination.
	//
	// The machinery, not the characters. A lone "\" or "&" spells nothing:
	// "R&D", "\d+", "docs\setup" and "foo&bar" are all relative destinations,
	// which this function promises to leave whole, and they carry no escape a
	// parser would act on.
	return hasBackslashEscape(dest) || hasCharacterReference(dest)
}

// hasBackslashEscape reports whether s uses a CommonMark backslash escape — a
// "\" before ASCII punctuation, which is the only place the backslash means
// anything. In "\d+" and "docs\setup" it stays literal text.
func hasBackslashEscape(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '\\' && isASCIIPunct(s[i+1]) {
			return true
		}
	}
	return false
}

// isASCIIPunct is CommonMark's ASCII punctuation set — every ASCII character
// that is neither a letter, a digit, nor a space.
func isASCIIPunct(c byte) bool {
	switch {
	case c >= '!' && c <= '/', c >= ':' && c <= '@', c >= '[' && c <= '`', c >= '{' && c <= '~':
		return true
	}
	return false
}

// hasCharacterReference reports whether s holds a character reference — "&",
// a name or a "#"-led numeric body, then ";". "R&D" and "foo&bar" have no ";"
// to close one, so nothing in them decodes.
//
// It does not check the name against HTML5's table: an unknown name decodes to
// nothing and breaking it costs one space, which is the side of this call the
// rest of the file already runs on.
func hasCharacterReference(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '&' {
			continue
		}
		for j := i + 1; j < len(s); j++ {
			c := s[j]
			if c == ';' {
				return j > i+1
			}
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '#') {
				break
			}
		}
	}
	return false
}

// hasURIScheme reports whether s begins with a URI scheme followed by ":" —
// a letter, then letters, digits, "+", "-" or "." (RFC 3986).
func hasURIScheme(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		case c == ':':
			return i > 0
		default:
			return false
		}
	}
	return false
}

func skipSpacesTabs(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}
