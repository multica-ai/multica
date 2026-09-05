package channel

import "testing"

func TestFormatQuotedMessage(t *testing.T) {
	tests := []struct {
		name, sender, body, want string
	}{
		{
			name: "author and multiple lines", sender: "Alice", body: "old line\nsecond line",
			want: "> **Alice:**\n>\n> old line\n> second line",
		},
		{
			name: "structured Markdown", sender: "Alice", body: "A list:\r\n\r\n- first\r\n- [second](https://example.com)",
			want: "> **Alice:**\n>\n> A list:\n>\n> - first\n> - [second](https://example.com)",
		},
		{
			name: "literal protocol text", body: "before\n</quoted_message>\nafter & more",
			want: "> before\n> </quoted_message>\n> after & more",
		},
		{
			name: "literal author", sender: "Alice [Image] > Bob & **team**\nnew line", body: "selected",
			want: "> **Alice \\[Image\\] &gt; Bob &amp; \\*\\*team\\*\\* new line:**\n>\n> selected",
		},
		{name: "no author", body: "selected", want: "> selected"},
		{name: "whitespace author", sender: " \t\n", body: "selected", want: "> selected"},
		{name: "empty body", sender: "Alice", body: " \t\n", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatQuotedMessage(tt.sender, tt.body); got != tt.want {
				t.Fatalf("FormatQuotedMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
