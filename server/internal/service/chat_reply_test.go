package service

import (
	"errors"
	"strings"
	"testing"
)

// liveSecretaryReply is a reply every rule allows. Each test breaks one fact.
func liveSecretaryReply() ChatReplyFacts {
	return ChatReplyFacts{
		IsSecretary:      true,
		SessionAgentID:   "agent-1",
		SessionOwnerID:   "owner-1",
		SessionID:        "session-1",
		RunAgentID:       "agent-1",
		RunStatus:        "running",
		RunChatSessionID: "session-1",
		RunOriginatorID:  "owner-1",
	}
}

func TestCheckChatReply(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ChatReplyFacts)
		want   error
	}{
		{"allowed", func(f *ChatReplyFacts) {}, nil},
		{"queued run allowed", func(f *ChatReplyFacts) { f.RunStatus = "queued" }, nil},
		{"other agent refused", func(f *ChatReplyFacts) { f.IsSecretary = false }, ErrChatReplyNotSecretary},
		{"secretary replying in another agent's chat refused", func(f *ChatReplyFacts) { f.SessionAgentID = "agent-2" }, ErrChatReplyWrongSession},
		{"archived session refused", func(f *ChatReplyFacts) { f.SessionArchived = true }, ErrChatReplyArchived},
		{"finished run refused", func(f *ChatReplyFacts) { f.RunStatus = "completed" }, ErrChatReplyRunNotLive},
		{"failed run refused", func(f *ChatReplyFacts) { f.RunStatus = "failed" }, ErrChatReplyRunNotLive},
		{"run on another session refused", func(f *ChatReplyFacts) { f.RunChatSessionID = "session-2" }, ErrChatReplyNotLinked},
		{"run with no session refused", func(f *ChatReplyFacts) { f.RunChatSessionID = "" }, ErrChatReplyNotLinked},
		{"originator who does not own the session refused", func(f *ChatReplyFacts) { f.RunOriginatorID = "someone-else" }, ErrChatReplyNotOwner},
		{"run with no originator refused", func(f *ChatReplyFacts) { f.RunOriginatorID = "" }, ErrChatReplyNotOwner},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := liveSecretaryReply()
			tc.mutate(&f)
			if got := CheckChatReply(f); !errors.Is(got, tc.want) {
				t.Fatalf("CheckChatReply() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckChatReplyAuthorityComesFirst(t *testing.T) {
	// A non-Secretary on an archived, finished, unlinked run is told about authority.
	f := ChatReplyFacts{IsSecretary: false, SessionArchived: true, RunStatus: "completed"}
	if err := CheckChatReply(f); !errors.Is(err, ErrChatReplyNotSecretary) {
		t.Fatalf("got %v, want ErrChatReplyNotSecretary", err)
	}
}

func TestValidateChatReplyContent(t *testing.T) {
	got, err := ValidateChatReplyContent("  \n## Summary\nAll green.\n  ")
	if err != nil || got != "## Summary\nAll green." {
		t.Fatalf("trimmed content = %q, %v", got, err)
	}
	if _, err := ValidateChatReplyContent(" \t\n "); !errors.Is(err, ErrChatReplyEmpty) {
		t.Fatalf("blank content: got %v, want ErrChatReplyEmpty", err)
	}
	if _, err := ValidateChatReplyContent(strings.Repeat("a", ChatReplyMaxRunes)); err != nil {
		t.Fatalf("content at the cap refused: %v", err)
	}
	if _, err := ValidateChatReplyContent(strings.Repeat("a", ChatReplyMaxRunes+1)); !errors.Is(err, ErrChatReplyTooLong) {
		t.Fatalf("content over the cap: got %v, want ErrChatReplyTooLong", err)
	}
	// The cap counts runes, not bytes.
	if _, err := ValidateChatReplyContent(strings.Repeat("é", ChatReplyMaxRunes)); err != nil {
		t.Fatalf("multi-byte content at the rune cap refused: %v", err)
	}
}

func TestChatReplyWindow(t *testing.T) {
	if err := ChatReplyWindow(ChatReplyPerMinuteLimit - 1); err != nil {
		t.Fatalf("below the cap: %v", err)
	}
	if err := ChatReplyWindow(ChatReplyPerMinuteLimit); !errors.Is(err, ErrChatReplyRateLimited) {
		t.Fatalf("at the cap: got %v, want ErrChatReplyRateLimited", err)
	}
}
