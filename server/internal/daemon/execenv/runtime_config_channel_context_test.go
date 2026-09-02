package execenv

import (
	"strings"
	"testing"
)

// Room audience belongs to the per-turn message, not the cached brief. The
// original implementation added a long `## Conversation Channel` section and
// pinned its full prose, including pointers to conditionally rendered
// `## Task Initiator` / `## Requesting User` sections. Those pins were retired
// because they duplicated platform context and made dangling pointers pass.
// The semantic audience boundaries are pinned in daemon/prompt_test.go instead.
func TestBriefChatWorkflowDoesNotAssertAudience(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		ChatSessionID:   "8d154f4b-8a7d-4f10-99ef-c1a16e2e7868",
		ChatChannelType: ChannelTypeSlack,
		AgentID:         "9df55bf2-67de-4ae6-803f-4de2fbd45df1",
		AgentName:       "Eve",
	})
	if !strings.Contains(out, "**You are in chat mode.**") {
		t.Fatal("chat brief must still identify chat mode")
	}
	for _, retired := range []string{
		"## Conversation Channel",
		"Audience:",
		"messaging you directly",
		"group room",
		"direct room",
	} {
		if strings.Contains(out, retired) {
			t.Errorf("cached brief must not contain per-session audience text %q", retired)
		}
	}
}

func TestAudienceOf(t *testing.T) {
	t.Parallel()

	if got := ChatAudience(0); got != ChatAudienceUnknown {
		t.Fatalf("zero-value audience = %v, want unknown", got)
	}

	cases := []struct {
		name        string
		channelType string
		chatType    string
		want        ChatAudience
	}{
		{name: "group binding", channelType: ChannelTypeSlack, chatType: ChatTypeGroup, want: ChatAudienceGroup},
		{name: "direct binding", channelType: ChannelTypeFeishu, chatType: ChatTypeP2P, want: ChatAudienceDirect},
		{name: "web chat", want: ChatAudienceDirect},
		{name: "old server", channelType: ChannelTypeSlack, want: ChatAudienceUnknown},
		{name: "future discriminator", channelType: ChannelTypeSlack, chatType: "broadcast", want: ChatAudienceUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AudienceOf(tc.channelType, tc.chatType); got != tc.want {
				t.Errorf("AudienceOf(%q, %q) = %v, want %v", tc.channelType, tc.chatType, got, tc.want)
			}
		})
	}
}
