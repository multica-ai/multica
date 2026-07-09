package util

import "strings"

// UnescapeBackslashEscapes decodes the common backslash escape sequences
// (\n, \r, \t, \\) that LLM agents routinely emit as 4-character literals
// because Python/JSON-style string conventions are their default. The same
// helper is used by the CLI to fix bash-double-quote bodies (where the shell
// doesn't expand \n) and by the daemon-task completion path to fix raw agent
// stdout that arrives with literal `\n\n` between paragraphs.
//
// Only \n / \r / \t / \\ are decoded. Other escape sequences (\d, \w, \s,
// \u, \0, \", etc.) pass through verbatim so regex literals and printf
// format strings survive without surprise mutation. Callers that need the
// literal 4-char sequence intact should bypass this helper entirely (the CLI
// exposes --content-stdin / --description-stdin for that case).
func UnescapeBackslashEscapes(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case 'r':
				b.WriteByte('\r')
				i++
				continue
			case 't':
				b.WriteByte('\t')
				i++
				continue
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}
