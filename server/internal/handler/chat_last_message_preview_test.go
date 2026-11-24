package handler

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestChatLastMessagePreview(t *testing.T) {
	long := strings.Repeat("回复", chatLastMessagePreviewMaxRunes)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"short message unchanged", "Done. See the PR.", "Done. See the PR."},
		{"whitespace collapsed", "line one\n\n  line two\t", "line one line two"},
		{"leading code fence dropped before truncating",
			"```go\n" + strings.Repeat("x := 1\n", 100) + "```\nThe fix is above.",
			"The fix is above."},
		{"unclosed fence keeps its text", "```go\nx := 1", "go x := 1"},
		{"leading Markdown markers do not use the preview budget",
			strings.Repeat("*", chatLastMessagePreviewMaxRunes) + "\n部署完成", "部署完成"},
		{"Markdown markers removed before collapsing whitespace", "# **Done** \n> See `logs` ~soon~.", "Done See logs soon."},
		{"matches client whitespace", "\ufeffDone\u00a0\t today\u0085.", "Done today\u0085."},
		{"does not end with a truncated separator",
			strings.Repeat("a", chatLastMessagePreviewMaxRunes-1) + " b",
			strings.Repeat("a", chatLastMessagePreviewMaxRunes-1)},
		{"capped by runes, not bytes", long, string([]rune(long)[:chatLastMessagePreviewMaxRunes])},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := chatLastMessagePreview(tc.in)
			if got != tc.want {
				t.Fatalf("chatLastMessagePreview() = %q, want %q", got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("preview is not valid UTF-8: %q", got)
			}
		})
	}
}

func TestBuildChatLastMessageCapsContent(t *testing.T) {
	at := pgtype.Timestamptz{Valid: true}
	msg := buildChatLastMessage(at, strings.Repeat("a", 20_000), "assistant", pgtype.Text{}, protocol.ChatMessageKindMessage)
	if msg == nil {
		t.Fatal("buildChatLastMessage returned nil")
	}
	if n := utf8.RuneCountInString(msg.Content); n != chatLastMessagePreviewMaxRunes {
		t.Fatalf("content length = %d runes, want %d", n, chatLastMessagePreviewMaxRunes)
	}
}
