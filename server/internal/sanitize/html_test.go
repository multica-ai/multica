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
