package sanitize

import (
	"testing"
)

func TestHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "safe markdown",
			input: "**bold** and *italic*",
			want:  "**bold** and *italic*",
		},
		{
			name:  "script tag stripped",
			input: `<script>alert(1)</script>`,
			want:  "",
		},
		{
			name:  "iframe stripped",
			input: `<iframe srcdoc="<script>parent.__xss=1</script>"></iframe>`,
			want:  "",
		},
		{
			name:  "img with onerror stripped",
			input: `<img src=x onerror="alert(1)">`,
			want:  `<img src="x">`,
		},
		{
			name:  "safe link preserved",
			input: `<a href="https://example.com">link</a>`,
			want:  `<a href="https://example.com" rel="nofollow">link</a>`,
		},
		{
			name:  "file card div preserved",
			input: `<div data-type="fileCard" data-href="https://cdn.example.com/file.pdf" data-filename="report.pdf"></div>`,
			want:  `<div data-type="fileCard" data-href="https://cdn.example.com/file.pdf" data-filename="report.pdf"></div>`,
		},
		{
			name:  "object tag stripped",
			input: `<object data="evil.swf"></object>`,
			want:  "",
		},
		{
			name:  "embed tag stripped",
			input: `<embed src="evil.swf">`,
			want:  "",
		},
		{
			name:  "style tag stripped",
			input: `<style>body{display:none}</style>`,
			want:  "",
		},
		{
			name:  "mention link preserved",
			input: `[@User](mention://member/abc-123)`,
			want:  `[@User](mention://member/abc-123)`,
		},
		{
			name:  "file card with javascript href stripped",
			input: `<div data-type="fileCard" data-href="javascript:alert(1)" data-filename="evil.pdf"></div>`,
			want:  `<div data-type="fileCard" data-filename="evil.pdf"></div>`,
		},
		{
			name:  "file card with data URI stripped",
			input: `<div data-type="fileCard" data-href="data:text/html,<script>alert(1)</script>" data-filename="x.html"></div>`,
			want:  `<div data-type="fileCard" data-filename="x.html"></div>`,
		},
		{
			name:  "file card with http href preserved",
			input: `<div data-type="fileCard" data-href="http://example.com/file.pdf" data-filename="file.pdf"></div>`,
			want:  `<div data-type="fileCard" data-href="http://example.com/file.pdf" data-filename="file.pdf"></div>`,
		},
		// Code block preservation — entities must NOT be escaped inside code.
		{
			name:  "fenced code block preserves ampersands",
			input: "```\na && b\n```",
			want:  "```\na && b\n```",
		},
		{
			name:  "fenced code block preserves angle brackets",
			input: "```html\n<div class=\"x\">hello</div>\n```",
			want:  "```html\n<div class=\"x\">hello</div>\n```",
		},
		{
			name:  "inline code preserves ampersands",
			input: "run `a && b` in shell",
			want:  "run `a && b` in shell",
		},
		{
			name:  "inline code preserves angle brackets",
			input: "use `x < y && y > z`",
			want:  "use `x < y && y > z`",
		},
		{
			name:  "double backtick inline code preserved",
			input: "use ``a && b`` here",
			want:  "use ``a && b`` here",
		},
		{
			name:  "script in fenced code block preserved",
			input: "```\n<script>alert(1)</script>\n```",
			want:  "```\n<script>alert(1)</script>\n```",
		},
		{
			name:  "script outside code block still stripped",
			input: "hello <script>alert(1)</script> world",
			want:  "hello  world",
		},
		{
			name:  "mixed code and non-code",
			input: "text `a && b` more <script>x</script> end",
			want:  "text `a && b` more  end",
		},
		{
			name:  "tilde fenced code block preserves content",
			input: "~~~\na && b\n~~~",
			want:  "~~~\na && b\n~~~",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HTML(tt.input)
			if got != tt.want {
				t.Errorf("HTML(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
