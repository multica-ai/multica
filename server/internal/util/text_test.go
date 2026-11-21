package util

import "testing"

func TestUnescapeBackslashEscapes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no escapes", "hello world", "hello world"},
		{"single newline", `line1\nline2`, "line1\nline2"},
		{"double newline becomes paragraph", `para1\n\npara2`, "para1\n\npara2"},
		{"tab and carriage return", `a\tb\rc`, "a\tb\rc"},
		{"escaped backslash preserved as literal", `keep\\nliteral`, `keep\nliteral`},
		{"trailing lone backslash kept verbatim", `tail\`, `tail\`},
		{"unknown escape kept verbatim", `\x not touched`, `\x not touched`},
		{"mixed real and escaped newlines", "real\n" + `and\nescaped`, "real\nand\nescaped"},
		{"unicode untouched", `中文段落\n下一段`, "中文段落\n下一段"},
		// Contract boundary: only \n \r \t \\ are decoded. Common regex /
		// path / formatter escape sequences such as \d, \w, \s, \u, \0 must
		// pass through verbatim — this lets users paste regex snippets or
		// printf-style format strings into --content without surprise
		// mutation. Anyone who genuinely wants the literal characters \\n
		// can either double the backslash or pipe the body via stdin.
		{"regex digit class untouched", `\d+\s*\w+`, `\d+\s*\w+`},
		{"unicode escape untouched", `café`, `café`},
		{"null escape untouched", `\0 sentinel`, `\0 sentinel`},
		{"windows path no special chars", `C:\Users\bob`, `C:\Users\bob`},
		{"backslash-quote pair untouched", `quote\"inside`, `quote\"inside`},
		// Documented sharp edge of the contract: a path or string that
		// embeds a literal backslash-n IS rewritten because the helper
		// cannot distinguish "model emitted \n thinking it would become a
		// newline" from "user pasted a path that happens to start with
		// \new". Callers who need the literal sequence must double the
		// backslash (`\\new`) or pipe the body via --content-stdin /
		// --description-stdin. This test pins that intentional behavior.
		{"path starting with backslash-n is mutated", `C:\new\folder`, "C:\new\\folder"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnescapeBackslashEscapes(tt.in)
			if got != tt.want {
				t.Errorf("UnescapeBackslashEscapes(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDecodeEscapedAgentOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain single line", "done", "done"},
		{"escaped blob is decoded", `para1\n\npara2\n- item`, "para1\n\npara2\n- item"},
		{"escaped blob keeps tab and carriage-return commands", `$a \times b$, $\rho$\n\nnext`, "$a \\times b$, $\\rho$\n\nnext"},
		{"escaped blob keeps escaped backslash pairs", `a \\neq b\n\nnext`, "a \\\\neq b\n\nnext"},
		{"escaped backslash does not start a paragraph break", `C:\\n\n`, `C:\\n\n`},
		{"single escaped newline is not a blob", `line one\nline two`, `line one\nline two`},
		// GH #9014: single-line LaTeX and paths that begin with `\n`.
		{"single line neq", `$n \neq -1$`, `$n \neq -1$`},
		{"single line times and neq", `$a \times b \neq 0$`, `$a \times b \neq 0$`},
		{"single line node_modules path", `Error: cannot read C:\tools\node_modules\repo`, `Error: cannot read C:\tools\node_modules\repo`},
		// GH #8845 / #9014: formatted output carries content backslashes.
		{"latex in formatted reply", "Result:\n$a \\times b$, \\theta, \\text{x}, \\top, \\neq, \\rho", "Result:\n$a \\times b$, \\theta, \\text{x}, \\top, \\neq, \\rho"},
		{"windows path in formatted reply", "Path:\n`D:\\Data\\secrets\\read-link.env` and `C:\\tools\\node_modules`", "Path:\n`D:\\Data\\secrets\\read-link.env` and `C:\\tools\\node_modules`"},
		{"quoted escape in formatted reply", "Use `\\n` for a newline\nand `\\t` for a tab", "Use `\\n` for a newline\nand `\\t` for a tab"},
		{"crlf output untouched", "line1\r\n\\temp", "line1\r\n\\temp"},
		{"single line latex without escaped newline", `$a \times b$ and \theta`, `$a \times b$ and \theta`},
		{"single line windows path without escaped newline", `C:\tools\repo`, `C:\tools\repo`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecodeEscapedAgentOutput(tt.in)
			if got != tt.want {
				t.Errorf("DecodeEscapedAgentOutput(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
