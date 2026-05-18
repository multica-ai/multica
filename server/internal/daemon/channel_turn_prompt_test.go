package daemon

import "testing"

func TestBuildPrompt_ChannelTurnUsesAgentPrompt(t *testing.T) {
	t.Parallel()

	const prompt = `Use multica issue list --output json, then answer naturally.`
	got := BuildPrompt(Task{
		ChannelTurnPrompt:  prompt,
		ChannelTurnMessage: "各项目进展怎么样？",
	}, "codex")
	if got != prompt {
		t.Fatalf("prompt = %q, want channel turn prompt", got)
	}
}
