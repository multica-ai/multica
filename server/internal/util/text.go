package util

import (
	"strings"
	"unicode/utf8"
)

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

// sanitizeJSONMaxDepth bounds SanitizeJSONForPostgres's recursion. Task tool
// input is a handful of levels deep in practice; anything past this is either
// pathological or hostile, and is dropped rather than walked so a deeply
// nested payload cannot turn a persistence guard into a stack overflow.
const sanitizeJSONMaxDepth = 32

// SanitizeTextForPostgres makes an arbitrary externally-produced string safe to
// persist in a PostgreSQL TEXT or JSONB column.
//
// Two distinct failure modes, either of which aborts the entire INSERT and
// rolls the surrounding transaction back:
//
//   - Embedded NUL (0x00). PostgreSQL rejects it in TEXT with SQLSTATE 22021
//     ("invalid byte sequence for encoding UTF8"), and in JSONB with 22P05
//     ("unsupported Unicode escape sequence") once encoding/json has escaped it.
//     Removed outright: it has no useful rendering, and the caller wants the
//     surrounding diagnostic far more than it wants an invisible byte.
//   - Any other invalid UTF-8 byte sequence — a byte-bounded tail that cut a
//     multi-byte rune in half, a Windows-1252 smart quote, raw binary from a
//     child process that crashed. Replaced with U+FFFD so the text keeps its
//     shape and a reader can see that something was there.
//
// IMPORTANT: strings.ToValidUTF8 alone does NOT cover the NUL case — 0x00 is a
// perfectly valid UTF-8 encoding of U+0000, so it passes through untouched.
// The explicit ReplaceAll is load-bearing. Do not "simplify" it away; that is
// exactly the gap that let GH #7098 wedge tasks in 'running'.
func SanitizeTextForPostgres(s string) string {
	// Overwhelmingly the common case: leave ordinary text completely alone.
	if !strings.ContainsRune(s, 0) && utf8.ValidString(s) {
		return s
	}
	return strings.ReplaceAll(strings.ToValidUTF8(s, "\uFFFD"), "\x00", "")
}

// SanitizeJSONForPostgres walks a decoded JSON value — the `any` tree
// encoding/json produces — and applies SanitizeTextForPostgres to every string
// inside it, object keys included: a poisoned key breaks the INSERT exactly
// like a poisoned value does.
//
// This exists because sanitizing only the top-level string fields of a request
// is not enough when one of those fields is itself a JSONB blob (agent tool
// input, for instance), where the offending byte can sit at any depth.
//
// Subtrees deeper than sanitizeJSONMaxDepth are replaced with nil rather than
// walked. That is a deliberate, bounded loss: the alternative is letting an
// adversarial payload dictate our recursion depth.
func SanitizeJSONForPostgres(v any) any {
	return sanitizeJSONValue(v, 0)
}

func sanitizeJSONValue(v any, depth int) any {
	if depth > sanitizeJSONMaxDepth {
		return nil
	}
	switch t := v.(type) {
	case string:
		return SanitizeTextForPostgres(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[SanitizeTextForPostgres(k)] = sanitizeJSONValue(val, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = sanitizeJSONValue(val, depth+1)
		}
		return out
	default:
		// Numbers, bools, nil — nothing a TEXT/JSONB column can choke on.
		return v
	}
}
