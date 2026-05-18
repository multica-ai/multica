package intent_test

import (
	"strings"
	"testing"

	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
	in "github.com/multica-ai/multica/server/internal/channel/intent"
)

func TestBuildChannelAgentTurnPrompt_IncludesContextEntities(t *testing.T) {
	t.Parallel()

	req := in.IntentRequest{
		WorkspaceID: "ws-1",
		Text:        "把它关掉",
		ContextEntities: []channelconversation.EntityRef{
			{EntityKey: "STA-68", EntityType: channelconversation.EntityTypeIssue},
		},
	}

	prompt := in.BuildChannelAgentTurnPrompt(req)
	if !strings.Contains(prompt, "Conversation context:") {
		t.Fatal("prompt missing 'Conversation context:' section")
	}
	if !strings.Contains(prompt, "STA-68") {
		t.Fatal("prompt missing context entity STA-68")
	}
}

func TestBuildChannelAgentTurnPrompt_IncludesExplicitEntities(t *testing.T) {
	t.Parallel()

	req := in.IntentRequest{
		WorkspaceID: "ws-1",
		Text:        "看看这个",
		ExplicitEntities: []channelconversation.EntityRef{
			{EntityKey: "STA-99", EntityType: channelconversation.EntityTypeIssue},
		},
	}

	prompt := in.BuildChannelAgentTurnPrompt(req)
	if !strings.Contains(prompt, "Explicit context:") {
		t.Fatal("prompt missing explicit context section")
	}
	if !strings.Contains(prompt, "STA-99") {
		t.Fatal("prompt missing explicit entity STA-99")
	}
}

func TestBuildChannelAgentTurnPrompt_IncludesQuotedText(t *testing.T) {
	t.Parallel()

	req := in.IntentRequest{
		WorkspaceID:     "ws-1",
		Text:            "详情看看",
		QuotedText:      "STA-68 的状态是 open",
		QuotedMessageID: "om_quoted_001",
	}

	prompt := in.BuildChannelAgentTurnPrompt(req)
	if !strings.Contains(prompt, "The user quoted this message:") {
		t.Fatal("prompt missing quoted text section")
	}
}

func TestBuildChannelAgentTurnPrompt_ContextResolutionRules(t *testing.T) {
	t.Parallel()

	req := in.IntentRequest{WorkspaceID: "ws-1", Text: "test"}
	prompt := in.BuildChannelAgentTurnPrompt(req)
	if strings.Contains(prompt, "require an explicit issue key in the message") {
		t.Fatal("prompt still contains old strict rule")
	}
	if !strings.Contains(prompt, "resolvable from ExplicitEntities/ContextEntities") {
		t.Fatal("prompt missing new context-resolution rule")
	}
}

func TestBuildChannelAgentTurnPrompt_CloseIssueRuleIsLanguageNeutral(t *testing.T) {
	t.Parallel()

	req := in.IntentRequest{WorkspaceID: "ws-1", Text: "close it"}
	prompt := in.BuildChannelAgentTurnPrompt(req)
	if !strings.Contains(prompt, "in any language") {
		t.Fatal("prompt should describe close/cancel semantics as language-neutral")
	}
	if !strings.Contains(prompt, "status update to `cancelled`") {
		t.Fatal("prompt missing issue cancelled status rule")
	}
	if !strings.Contains(prompt, "confirmation code") {
		t.Fatal("prompt should distinguish issue cancellation from action-code cancellation")
	}
}
